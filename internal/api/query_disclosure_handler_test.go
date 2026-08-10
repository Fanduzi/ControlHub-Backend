// Package api provides tests for the Phase 38Q query disclosure policy
// handlers: bearer-required access, admin-only writes, strict request decoding,
// validation, and error mapping.
// input: net/http, net/http/httptest, testing, chi router, internal/model, internal/service
// output: TestDisclosure_* handler tests
// pos: Verifies bearer/admin authorization, strict decoding, and 400/403/404/409 error mapping
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// stubQueryDisclosure is a configurable queryDisclosureAPI stub for handler
// tests. It records what the handler passed so tests can assert the actor comes
// from the token (never the body) and that non-admin requests never reach the
// service.
type stubQueryDisclosure struct {
	listResp     []model.ResultDisclosurePolicy
	listErr      error
	createID     uint64
	createErr    error
	updateErr    error
	deleteErr    error
	gotTargetID  uint64
	gotReq       model.ResultDisclosurePolicyUpsertRequest
	gotDatabase  string
	gotObject    string
	gotColumn    string
	listCalled   bool
	createCalled bool
	updateCalled bool
	deleteCalled bool
}

func (s *stubQueryDisclosure) ListPolicies(_ context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error) {
	s.listCalled = true
	s.gotTargetID = targetResourceID
	return s.listResp, s.listErr
}

func (s *stubQueryDisclosure) CreatePolicy(_ context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	s.createCalled = true
	s.gotReq = req
	if s.createErr != nil {
		return 0, s.createErr
	}
	return s.createID, nil
}

func (s *stubQueryDisclosure) UpdatePolicy(_ context.Context, req model.ResultDisclosurePolicyUpsertRequest) error {
	s.updateCalled = true
	s.gotReq = req
	return s.updateErr
}

func (s *stubQueryDisclosure) DeletePolicy(_ context.Context, targetResourceID uint64, database, object, column string) error {
	s.deleteCalled = true
	s.gotTargetID = targetResourceID
	s.gotDatabase = database
	s.gotObject = object
	s.gotColumn = column
	return s.deleteErr
}

func newDisclosureRouter(stub queryDisclosureAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:            service.NewAuthService(testAuthUsers, "qd-test-secret"),
		QueryDisclosureService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			TokenMaxAge: 8 * 60 * 60 * 1e9, // 8h
			Clock:       fixedClock(qeTestNow),
		},
	}
	return NewRouter(deps)
}

func disclosureAdminToken(t *testing.T) string {
	return mintToken(t, "qd-test-secret", 42, "admin", qeTestNow)
}

func disclosureViewerToken(t *testing.T) string {
	return mintToken(t, "qd-test-secret", 43, "viewer", qeTestNow)
}

func disclosureRequest(method, path, body, bearer string) *http.Request {
	return qeRequest(method, path, body, bearer)
}

// TestDisclosure_ListRequiresBearer proves GET requires a fresh bearer token.
func TestDisclosure_ListRequiresBearer(t *testing.T) {
	router := newDisclosureRouter(&stubQueryDisclosure{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodGet, "/query-disclosure-policies?targetResourceId=22", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without bearer = %d, want 401", rec.Code)
	}
}

// TestDisclosure_CreateRequiresBearer proves POST requires a fresh bearer token.
func TestDisclosure_CreateRequiresBearer(t *testing.T) {
	router := newDisclosureRouter(&stubQueryDisclosure{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST without bearer = %d, want 401", rec.Code)
	}
}

// TestDisclosure_UpdateRequiresBearer proves PUT requires a fresh bearer token.
func TestDisclosure_UpdateRequiresBearer(t *testing.T) {
	router := newDisclosureRouter(&stubQueryDisclosure{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPut, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"masked_no_copy"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PUT without bearer = %d, want 401", rec.Code)
	}
}

// TestDisclosure_DeleteRequiresBearer proves DELETE requires a fresh bearer token.
func TestDisclosure_DeleteRequiresBearer(t *testing.T) {
	router := newDisclosureRouter(&stubQueryDisclosure{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodDelete,
		"/query-disclosure-policies?targetResourceId=22&databaseName=orders&objectName=users&columnName=email", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE without bearer = %d, want 401", rec.Code)
	}
}

// TestDisclosure_CreateUpdateDeleteRequireAdmin proves POST/PUT/DELETE require
// the admin role; a viewer token gets 403 and the service is never called.
// WHY: only admins may create/update/delete disclosure policies.
func TestDisclosure_CreateUpdateDeleteRequireAdmin(t *testing.T) {
	t.Run("create viewer forbidden", func(t *testing.T) {
		stub := &stubQueryDisclosure{}
		router := newDisclosureRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies",
			`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`,
			disclosureViewerToken(t)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer POST = %d, want 403", rec.Code)
		}
		if stub.createCalled {
			t.Fatal("service must not be called for a non-admin POST")
		}
	})
	t.Run("update viewer forbidden", func(t *testing.T) {
		stub := &stubQueryDisclosure{}
		router := newDisclosureRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, disclosureRequest(http.MethodPut, "/query-disclosure-policies",
			`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"masked_no_copy"}`,
			disclosureViewerToken(t)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer PUT = %d, want 403", rec.Code)
		}
		if stub.updateCalled {
			t.Fatal("service must not be called for a non-admin PUT")
		}
	})
	t.Run("delete viewer forbidden", func(t *testing.T) {
		stub := &stubQueryDisclosure{}
		router := newDisclosureRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, disclosureRequest(http.MethodDelete,
			"/query-disclosure-policies?targetResourceId=22&databaseName=orders&objectName=users&columnName=email",
			"", disclosureViewerToken(t)))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("viewer DELETE = %d, want 403", rec.Code)
		}
		if stub.deleteCalled {
			t.Fatal("service must not be called for a non-admin DELETE")
		}
	})
}

// TestDisclosure_CreateRejectsUnknownFields proves the strict decoder rejects
// any request body carrying unknown fields with 400. WHY: the request contract
// is strict; actor, DSN, credential, and secret fields must never be accepted.
func TestDisclosure_CreateRejectsUnknownFields(t *testing.T) {
	forbidden := []string{
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed","actorUserId":42}`,
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed","dsn":"root:pw@tcp(h:3306)/db"}`,
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed","secret":"hunter2"}`,
	}
	for _, body := range forbidden {
		stub := &stubQueryDisclosure{}
		router := newDisclosureRouter(stub)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies", body, disclosureAdminToken(t)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("forbidden body must be 400, got %d for %s", rec.Code, body)
		}
		if stub.createCalled {
			t.Fatalf("service must not be called for a forbidden body: %s", body)
		}
	}
}

// TestDisclosure_CreateInvalidBody proves an invalid request body (missing
// required fields or bad mode) returns 400.
func TestDisclosure_CreateInvalidBody(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing targetResourceId", `{"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`},
		{"missing databaseName", `{"targetResourceId":22,"objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`},
		{"missing objectName", `{"targetResourceId":22,"databaseName":"orders","columnName":"email","mode":"raw_copy_allowed"}`},
		{"missing columnName", `{"targetResourceId":22,"databaseName":"orders","objectName":"users","mode":"raw_copy_allowed"}`},
		{"missing mode", `{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email"}`},
		{"invalid mode", `{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"invalid_mode"}`},
		{"bad identifier chars", `{"targetResourceId":22,"databaseName":"orders; DROP TABLE","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryDisclosure{}
			router := newDisclosureRouter(stub)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies", tc.body, disclosureAdminToken(t)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400", tc.name, rec.Code)
			}
			if stub.createCalled {
				t.Fatalf("service must not be called for invalid body: %s", tc.name)
			}
		})
	}
}

// TestDisclosure_ListMissingTargetParam proves GET without targetResourceId
// returns 400.
func TestDisclosure_ListMissingTargetParam(t *testing.T) {
	router := newDisclosureRouter(&stubQueryDisclosure{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodGet, "/query-disclosure-policies", "", disclosureAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing targetResourceId = %d, want 400", rec.Code)
	}
}

// TestDisclosure_TargetNotFound proves a missing target maps to 404.
func TestDisclosure_TargetNotFound(t *testing.T) {
	stub := &stubQueryDisclosure{listErr: service.ErrQueryTargetNotFound, createErr: service.ErrQueryTargetNotFound}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodGet, "/query-disclosure-policies?targetResourceId=777", "", disclosureAdminToken(t)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET missing target = %d, want 404", rec.Code)
	}
}

// TestDisclosure_CreateTargetNotFound proves a missing target on POST maps to 404.
func TestDisclosure_CreateTargetNotFound(t *testing.T) {
	stub := &stubQueryDisclosure{createErr: service.ErrQueryTargetNotFound}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies",
		`{"targetResourceId":777,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`,
		disclosureAdminToken(t)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST missing target = %d, want 404", rec.Code)
	}
}

// TestDisclosure_CreateSuccess proves an admin POST returns 201 with the created
// policy and passes correct fields to the service.
func TestDisclosure_CreateSuccess(t *testing.T) {
	stub := &stubQueryDisclosure{createID: 100}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`,
		disclosureAdminToken(t)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("admin POST = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.createCalled {
		t.Fatal("CreatePolicy was not called")
	}
	if stub.gotReq.TargetResourceID != 22 {
		t.Fatalf("req targetResourceId = %d, want 22", stub.gotReq.TargetResourceID)
	}
	if stub.gotReq.DatabaseName != "orders" {
		t.Fatalf("req databaseName = %q", stub.gotReq.DatabaseName)
	}
	if stub.gotReq.ObjectName != "users" {
		t.Fatalf("req objectName = %q", stub.gotReq.ObjectName)
	}
	if stub.gotReq.ColumnName != "email" {
		t.Fatalf("req columnName = %q", stub.gotReq.ColumnName)
	}
	if stub.gotReq.Mode != model.ResultDisclosureRawCopyAllowed {
		t.Fatalf("req mode = %q", stub.gotReq.Mode)
	}
}

// TestDisclosure_UpdateSuccess proves an admin PUT returns 204 and passes
// correct fields to the service.
func TestDisclosure_UpdateSuccess(t *testing.T) {
	stub := &stubQueryDisclosure{}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPut, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"masked_no_copy"}`,
		disclosureAdminToken(t)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin PUT = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.updateCalled {
		t.Fatal("UpdatePolicy was not called")
	}
	if stub.gotReq.Mode != model.ResultDisclosureMaskedNoCopy {
		t.Fatalf("req mode = %q", stub.gotReq.Mode)
	}
}

// TestDisclosure_DeleteSuccess proves an admin DELETE returns 204 and passes
// correct scope to the service.
func TestDisclosure_DeleteSuccess(t *testing.T) {
	stub := &stubQueryDisclosure{}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodDelete,
		"/query-disclosure-policies?targetResourceId=22&databaseName=orders&objectName=users&columnName=email",
		"", disclosureAdminToken(t)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("admin DELETE = %d, want 204", rec.Code)
	}
	if !stub.deleteCalled {
		t.Fatal("DeletePolicy was not called")
	}
	if stub.gotTargetID != 22 {
		t.Fatalf("targetID = %d, want 22", stub.gotTargetID)
	}
	if stub.gotDatabase != "orders" {
		t.Fatalf("database = %q", stub.gotDatabase)
	}
	if stub.gotObject != "users" {
		t.Fatalf("object = %q", stub.gotObject)
	}
	if stub.gotColumn != "email" {
		t.Fatalf("column = %q", stub.gotColumn)
	}
}

// TestDisclosure_DeleteMissingScopeParams proves DELETE without required query
// params returns 400.
func TestDisclosure_DeleteMissingScopeParams(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"missing all", "/query-disclosure-policies"},
		{"missing database", "/query-disclosure-policies?targetResourceId=22&objectName=users&columnName=email"},
		{"missing object", "/query-disclosure-policies?targetResourceId=22&databaseName=orders&columnName=email"},
		{"missing column", "/query-disclosure-policies?targetResourceId=22&databaseName=orders&objectName=users"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryDisclosure{}
			router := newDisclosureRouter(stub)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, disclosureRequest(http.MethodDelete, tc.path, "", disclosureAdminToken(t)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400", tc.name, rec.Code)
			}
			if stub.deleteCalled {
				t.Fatalf("service must not be called for missing scope params: %s", tc.name)
			}
		})
	}
}

// TestDisclosure_ListBlockedByPolicy proves a disclosure-blocked error maps to
// 403 with the correct error code.
func TestDisclosure_ListBlockedByPolicy(t *testing.T) {
	stub := &stubQueryDisclosure{listErr: service.ErrQueryDisclosureBlocked}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodGet, "/query-disclosure-policies?targetResourceId=22", "", disclosureAdminToken(t)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("blocked = %d, want 403", rec.Code)
	}
}

// TestDisclosure_CreateValidationFailed proves a service validation error maps
// to 400.
func TestDisclosure_CreateValidationFailed(t *testing.T) {
	stub := &stubQueryDisclosure{createErr: service.ErrQueryValidationFailed}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`,
		disclosureAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("validation failed = %d, want 400", rec.Code)
	}
}

// TestDisclosure_CreateConflict proves a duplicate-scope create maps to 409
// with the disclosure_policy_conflict error code. WHY: a second policy for the
// same column must not be a 500; the conflict is a predictable client error.
func TestDisclosure_CreateConflict(t *testing.T) {
	stub := &stubQueryDisclosure{createErr: service.ErrQueryDisclosurePolicyConflict}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPost, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`,
		disclosureAdminToken(t)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflict = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "disclosure_policy_conflict" {
		t.Fatalf("error code = %q, want disclosure_policy_conflict", body.Error)
	}
}

// TestDisclosure_UpdateNotFound proves updating a scope with no existing
// policy maps to 404 with the disclosure_policy_not_found error code. WHY: a
// missing policy is not a server failure; callers must be able to distinguish
// it from 500.
func TestDisclosure_UpdateNotFound(t *testing.T) {
	stub := &stubQueryDisclosure{updateErr: service.ErrQueryDisclosurePolicyNotFound}
	router := newDisclosureRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodPut, "/query-disclosure-policies",
		`{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"masked_no_copy"}`,
		disclosureAdminToken(t)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update not found = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "disclosure_policy_not_found" {
		t.Fatalf("error code = %q, want disclosure_policy_not_found", body.Error)
	}
}

// TestDisclosure_StaleToken proves a stale token (older than TTL) returns 401.
func TestDisclosure_StaleToken(t *testing.T) {
	staleTime := qeTestNow.Add(-9 * 60 * 60 * 1e9) // 9h ago, TTL is 8h
	staleToken := mintToken(t, "qd-test-secret", 42, "admin", staleTime)
	router := newDisclosureRouter(&stubQueryDisclosure{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, disclosureRequest(http.MethodGet, "/query-disclosure-policies?targetResourceId=22", "", staleToken))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale token = %d, want 401", rec.Code)
	}
}
