// Package api provides tests for the Phase 38A query credential metadata
// handlers (Task B4): bearer-required access, admin-only writes, strict
// no-secret/no-actor request decoding, validation, and error mapping.
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// stubQueryCredential is a configurable queryCredentialAPI stub for handler
// tests. It records the actor/target/request it received so tests can assert
// the actor comes from the token (never the body) and that non-admin requests
// never reach the service.
type stubQueryCredential struct {
	statusResp   model.QueryCredentialStatusResponse
	statusErr    error
	upsertErr    error
	deleteErr    error
	gotActor     service.AuthenticatedUser
	gotTarget    uint64
	gotReq       model.QueryCredentialUpsertRequest
	upsertCalled bool
	deleteCalled bool
	getCalled    bool
}

func (s *stubQueryCredential) GetStatus(_ context.Context, _ uint64) (model.QueryCredentialStatusResponse, error) {
	s.getCalled = true
	return s.statusResp, s.statusErr
}

func (s *stubQueryCredential) Upsert(_ context.Context, actor service.AuthenticatedUser, targetID uint64, req model.QueryCredentialUpsertRequest) (model.QueryCredentialStatusResponse, error) {
	s.upsertCalled = true
	s.gotActor = actor
	s.gotTarget = targetID
	s.gotReq = req
	if s.upsertErr != nil {
		return model.QueryCredentialStatusResponse{}, s.upsertErr
	}
	return s.statusResp, nil
}

func (s *stubQueryCredential) Delete(_ context.Context, actor service.AuthenticatedUser, targetID uint64) error {
	s.deleteCalled = true
	s.gotActor = actor
	s.gotTarget = targetID
	return s.deleteErr
}

func newCredentialRouter(stub queryCredentialAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:            service.NewAuthService(nil, "qc-test-secret"),
		QueryCredentialService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			TokenMaxAge: 8 * 60 * 60 * 1e9, // 8h, explicit so the helper has no hidden time import
			Clock:       fixedClock(qeTestNow),
		},
	}
	return NewRouter(deps)
}

func configuredStatusResponse() model.QueryCredentialStatusResponse {
	return model.QueryCredentialStatusResponse{
		ResourceID:        22,
		Configured:        true,
		Engine:            "mysql",
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
		RuntimeStatus:     model.QueryCredentialRuntimeSecretResolved,
		ExecutionEligible: true,
		Message:           "Read-only credential is configured and bound to this target.",
	}
}

func adminToken(t *testing.T) string  { return mintToken(t, "qc-test-secret", 42, "admin", qeTestNow) }
func viewerToken(t *testing.T) string { return mintToken(t, "qc-test-secret", 43, "viewer", qeTestNow) }

// TestQueryCredential_GetRequiresBearer proves GET requires a fresh bearer token.
func TestQueryCredential_GetRequiresBearer(t *testing.T) {
	router := newCredentialRouter(&stubQueryCredential{statusResp: configuredStatusResponse()})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/22/credential", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without bearer = %d, want 401", rec.Code)
	}
}

// TestQueryCredential_PutRequiresBearer proves PUT requires a fresh bearer token.
func TestQueryCredential_PutRequiresBearer(t *testing.T) {
	router := newCredentialRouter(&stubQueryCredential{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPut, "/query-targets/22/credential", `{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PUT without bearer = %d, want 401", rec.Code)
	}
}

// TestQueryCredential_DeleteRequiresBearer proves DELETE requires a fresh bearer.
func TestQueryCredential_DeleteRequiresBearer(t *testing.T) {
	router := newCredentialRouter(&stubQueryCredential{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodDelete, "/query-targets/22/credential", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE without bearer = %d, want 401", rec.Code)
	}
}

// TestQueryCredential_PutDeleteRequireAdmin proves PUT/DELETE require the admin
// role; a viewer token gets 403 and the service is never called. WHY: only
// admins may create/update/delete credential metadata.
func TestQueryCredential_PutDeleteRequireAdmin(t *testing.T) {
	t.Run("put viewer forbidden", func(t *testing.T) {
		stub := &stubQueryCredential{}
		router := newCredentialRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPut, "/query-targets/22/credential",
			`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only"}`, viewerToken(t)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer PUT = %d, want 403", rec.Code)
		}
		if stub.upsertCalled {
			t.Fatal("service must not be called for a non-admin PUT")
		}
	})
	t.Run("delete viewer forbidden", func(t *testing.T) {
		stub := &stubQueryCredential{}
		router := newCredentialRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodDelete, "/query-targets/22/credential", "", viewerToken(t)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer DELETE = %d, want 403", rec.Code)
		}
		if stub.deleteCalled {
			t.Fatal("service must not be called for a non-admin DELETE")
		}
	})
}

// TestQueryCredential_PutRejectsSecretAndActorFields proves the strict decoder
// rejects any request body carrying a DSN, password, host, port, or actor field
// with 400 — these must never be accepted. WHY: the request contract is metadata
// only; a secret/actor field would break the no-secret/no-actor invariant.
func TestQueryCredential_PutRejectsSecretAndActorFields(t *testing.T) {
	forbidden := []string{
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only","dsn":"root:pw@tcp(h:3306)/db"}`,
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only","password":"hunter2"}`,
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only","host":"db.internal"}`,
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only","port":3306}`,
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only","actorUserId":42}`,
	}
	for _, body := range forbidden {
		stub := &stubQueryCredential{}
		router := newCredentialRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPut, "/query-targets/22/credential", body, adminToken(t)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("forbidden body must be 400, got %d for %s", rec.Code, body)
		}
		if stub.upsertCalled {
			t.Fatalf("service must not be called for a forbidden body: %s", body)
		}
	}
}

// TestQueryCredential_PutAllEnvironmentsRequiresConfirmation proves the handler
// rejects all_environments without explicit confirmation with 400.
func TestQueryCredential_PutAllEnvironmentsRequiresConfirmation(t *testing.T) {
	stub := &stubQueryCredential{}
	router := newCredentialRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPut, "/query-targets/22/credential",
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"all_environments"}`, adminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("all_environments without confirmation = %d, want 400", rec.Code)
	}
	if stub.upsertCalled {
		t.Fatal("service must not be called when all_environments is unconfirmed")
	}
}

// TestQueryCredential_InvalidTargetID proves a non-numeric target id is 400.
func TestQueryCredential_InvalidTargetID(t *testing.T) {
	router := newCredentialRouter(&stubQueryCredential{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/not-an-id/credential", "", adminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid target id = %d, want 400", rec.Code)
	}
}

// TestQueryCredential_TargetNotFound proves a missing target maps to 404.
func TestQueryCredential_TargetNotFound(t *testing.T) {
	stub := &stubQueryCredential{statusErr: service.ErrQueryTargetNotFound, upsertErr: service.ErrQueryTargetNotFound}
	router := newCredentialRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/777/credential", "", adminToken(t)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing target = %d, want 404", rec.Code)
	}
}

// TestQueryCredential_PutSuccess proves an admin PUT returns the status response
// and passes the token-derived actor (never a body actor) to the service.
func TestQueryCredential_PutSuccess(t *testing.T) {
	stub := &stubQueryCredential{statusResp: configuredStatusResponse()}
	router := newCredentialRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPut, "/query-targets/22/credential",
		`{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only"}`, adminToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("admin PUT = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.upsertCalled {
		t.Fatal("Upsert was not called")
	}
	if stub.gotActor.ID != 42 || stub.gotActor.Role != "admin" {
		t.Fatalf("actor = %+v, want id=42 role=admin from token", stub.gotActor)
	}
	if stub.gotTarget != 22 {
		t.Fatalf("target = %d, want 22", stub.gotTarget)
	}
	if stub.gotReq.CredentialRef != "ORDER_MYSQL_RO" {
		t.Fatalf("req credentialRef = %q", stub.gotReq.CredentialRef)
	}
}

// TestQueryCredential_DeleteSuccess proves an admin DELETE returns 204.
func TestQueryCredential_DeleteSuccess(t *testing.T) {
	stub := &stubQueryCredential{}
	router := newCredentialRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodDelete, "/query-targets/22/credential", "", adminToken(t)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin DELETE = %d, want 204", rec.Code)
	}
	if !stub.deleteCalled {
		t.Fatal("Delete was not called")
	}
}
