// Package api provides tests for the saved-statement handlers.
// input: context, encoding/json, net/http, net/http/httptest, strings, testing, time, internal/model, internal/service
// output: TestSavedStatement* — strict decoding, context field round trip, controlled error mapping
// pos: HTTP decode/error-mapping regression coverage for CRUD /query-targets/{id}/saved-statements incl. the (database, schema) request fields (T11)
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func ssAdminToken(t *testing.T) string {
	t.Helper()
	return mintToken(t, "test-secret", 42, "admin", time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC))
}

func ssViewerToken(t *testing.T) string {
	t.Helper()
	return mintToken(t, "test-secret", 43, "viewer", time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC))
}

func ssRequest(method, path, body, bearer string) *http.Request {
	return qeRequest(method, path, body, bearer)
}

func TestSavedStatement_ListRequiresBearer(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodGet, "/query-targets/22/saved-statements", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET without bearer = %d, want 401", rec.Code)
	}
}

func TestSavedStatement_CreateRequiresBearer(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		`{"name":"Test","statement":"SELECT 1","scope":"personal"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST without bearer = %d, want 401", rec.Code)
	}
}

func TestSavedStatement_UpdateRequiresBearer(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPut, "/query-targets/22/saved-statements/1",
		`{"name":"Updated","statement":"SELECT 2"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("PUT without bearer = %d, want 401", rec.Code)
	}
}

func TestSavedStatement_DeleteRequiresBearer(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodDelete, "/query-targets/22/saved-statements/1", "", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE without bearer = %d, want 401", rec.Code)
	}
}

func TestSavedStatement_ListSuccess(t *testing.T) {
	srv := NewTestServer()
	srv.deps.QuerySavedStatementService = &fakeSavedStatementService{
		listResp: model.QuerySavedStatementListResponse{
			Items: []model.QuerySavedStatement{
				{ID: 1, Name: "Test", Statement: "SELECT 1", Scope: model.QuerySavedStatementPersonal},
			},
			CanManageSharedTemplates: false,
		},
	}
	srv.Router = NewRouter(srv.deps)

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodGet, "/query-targets/22/saved-statements", "", ssAdminToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.QuerySavedStatementListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].Name != "Test" {
		t.Fatalf("expected name 'Test', got %q", resp.Items[0].Name)
	}
}

func TestSavedStatement_CreateSuccess(t *testing.T) {
	srv := NewTestServer()
	srv.deps.QuerySavedStatementService = &fakeSavedStatementService{
		createResp: model.QuerySavedStatement{ID: 1, Name: "Test"},
	}
	srv.Router = NewRouter(srv.deps)

	body, _ := json.Marshal(map[string]any{
		"name":       "Test",
		"statement":  "SELECT id FROM orders WHERE status = :status",
		"scope":      "personal",
		"parameters": []map[string]string{{"name": "status", "type": "string"}},
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_CreateRoundTripsDatabaseSchemaContext(t *testing.T) {
	srv := NewTestServer()
	srv.deps.QuerySavedStatementService = &fakeSavedStatementService{
		createResp: model.QuerySavedStatement{ID: 1, Name: "Ctx", DatabaseName: "db_a", SchemaName: "app"},
	}
	srv.Router = NewRouter(srv.deps)

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		`{"name":"Ctx","statement":"SELECT id FROM app.orders WHERE status = :status","scope":"personal","database":"db_a","schema":"app","parameters":[{"name":"status","type":"string"}]}`, ssAdminToken(t)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	fake := srv.deps.QuerySavedStatementService.(*fakeSavedStatementService)
	if fake.createReq.Database != "db_a" || fake.createReq.Schema != "app" {
		t.Fatalf("decoded context = %q/%q, want db_a/app", fake.createReq.Database, fake.createReq.Schema)
	}
	var resp model.QuerySavedStatement
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.DatabaseName != "db_a" || resp.SchemaName != "app" {
		t.Fatalf("response context = %q/%q, want db_a/app", resp.DatabaseName, resp.SchemaName)
	}
}

func TestSavedStatement_UpdateRoundTripsDatabaseSchemaContext(t *testing.T) {
	srv := NewTestServer()
	fake := &fakeSavedStatementService{}
	srv.deps.QuerySavedStatementService = fake
	srv.Router = NewRouter(srv.deps)

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPut, "/query-targets/22/saved-statements/1",
		`{"name":"Ctx","statement":"SELECT 1","database":"db_b","schema":"analytics"}`, ssAdminToken(t)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.updateReq.Database != "db_b" || fake.updateReq.Schema != "analytics" {
		t.Fatalf("decoded context = %q/%q, want db_b/analytics", fake.updateReq.Database, fake.updateReq.Schema)
	}
}

// Real-service stubs: wire a genuine QuerySavedStatementService into the
// router so a PG guard/pagination rejection travels the full handler path.
type ssStubTargets struct{ targets []model.QueryTarget }

func (s ssStubTargets) ListQueryTargets(context.Context, model.QueryTargetListQuery) ([]model.QueryTarget, int, error) {
	return s.targets, len(s.targets), nil
}

type ssStubCreds struct{}

func (ssStubCreds) GetCredential(context.Context, uint64, string) (model.QueryCredentialMetadata, error) {
	return model.QueryCredentialMetadata{ResourceID: 22, DatabaseName: "db_a", Engine: "postgresql", Enabled: true}, nil
}

type ssStubReader struct{ stmt model.QuerySavedStatement }

func (ssStubReader) ListVisible(context.Context, model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error) {
	return model.QuerySavedStatementListResponse{}, nil
}

func (s ssStubReader) GetByID(context.Context, uint64, uint64) (model.QuerySavedStatement, error) {
	return s.stmt, nil
}

type ssStubWriter struct {
	createCalled bool
	updateCalled bool
}

func (w *ssStubWriter) CreateWithAudit(context.Context, uint64, uint64, model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	w.createCalled = true
	return model.QuerySavedStatement{ID: 1}, nil
}

func (w *ssStubWriter) UpdateWithAudit(context.Context, uint64, uint64, uint64, model.QuerySavedStatementUpdateRequest, bool) error {
	w.updateCalled = true
	return nil
}

func (w *ssStubWriter) DeleteWithAudit(context.Context, uint64, uint64, uint64, bool) error {
	return nil
}

type ssStubGuard struct{}

func (ssStubGuard) GuardSavedStatement(statement string) (string, error) { return statement, nil }

// G8 contract: a template that compiles but fails the real pagination gate
// must publish its own controlled code at HTTP, not collapse into
// validation_failed — and nothing may persist.
func TestSavedStatement_LimitPlaceholderRejectSurfacesControlledCode(t *testing.T) {
	writer := &ssStubWriter{}
	realSvc := service.NewQuerySavedStatementService(
		ssStubReader{stmt: model.QuerySavedStatement{ID: 9, OwnerUserID: 42, Scope: model.QuerySavedStatementPersonal, DatabaseName: "db_a", SchemaName: "app"}},
		writer,
		ssStubTargets{targets: []model.QueryTarget{{
			ResourceID:        22,
			ConnectionContext: model.QueryTargetConnectionContext{Engine: "postgresql"},
		}}},
		ssStubGuard{},
	).WithPGContext(ssStubCreds{}, nil)
	srv := NewTestServer()
	srv.deps.QuerySavedStatementService = realSvc
	srv.Router = NewRouter(srv.deps)

	createBody := `{"name":"paged","statement":"SELECT id FROM app.orders LIMIT :n","scope":"personal","database":"db_a","schema":"app","parameters":[{"name":"n","type":"integer"}]}`
	updateBody := `{"name":"paged","statement":"SELECT id FROM app.orders LIMIT :n","database":"db_a","schema":"app","parameters":[{"name":"n","type":"integer"}]}`

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements", createBody, ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var createErr struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createErr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if createErr.Error != "unsupported_limit_offset_form" {
		t.Fatalf("create error code = %q, want unsupported_limit_offset_form (body %s)", createErr.Error, rec.Body.String())
	}
	if writer.createCalled {
		t.Fatal("rejected create must never reach the repository")
	}

	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPut, "/query-targets/22/saved-statements/9", updateBody, ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	var updateErr struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updateErr); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updateErr.Error != "unsupported_limit_offset_form" {
		t.Fatalf("update error code = %q, want unsupported_limit_offset_form (body %s)", updateErr.Error, rec.Body.String())
	}
	if writer.updateCalled {
		t.Fatal("rejected update must never reach the repository")
	}
}

func TestSavedStatement_ListPassesDatabaseScope(t *testing.T) {
	srv := NewTestServer()
	fake := &fakeSavedStatementService{}
	srv.deps.QuerySavedStatementService = fake
	srv.Router = NewRouter(srv.deps)

	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodGet, "/query-targets/22/saved-statements?database=db_a", "", ssAdminToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if fake.listDB != "db_a" {
		t.Fatalf("service saw database = %q, want db_a", fake.listDB)
	}

	rec = httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodGet, "/query-targets/22/saved-statements?database="+strings.Repeat("x", 129), "", ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized database param = %d, want 400", rec.Code)
	}
}

func TestSavedStatement_CreateRejectsDuplicateJSONFields(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		`{"name":"Test","name":"Other","statement":"SELECT 1","scope":"personal"}`, ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate field, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_CreateRejectsParameterValues(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		`{"name":"Test","statement":"SELECT 1","scope":"personal","parameters":[{"name":"status","type":"string","value":"paid"}]}`, ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for parameter value, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_CreateRejectsUnknownFields(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]any{
		"name":        "Test",
		"statement":   "SELECT 1",
		"scope":       "personal",
		"ownerUserId": 999,
		"actorUserId": 888,
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_CreateRejectsInvalidScope(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]string{
		"name":      "Test",
		"statement": "SELECT 1",
		"scope":     "public",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_UpdateSuccess(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]string{
		"name":      "Updated",
		"statement": "SELECT 2",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPut, "/query-targets/22/saved-statements/1",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_DeleteSuccess(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodDelete, "/query-targets/22/saved-statements/1",
		"", ssAdminToken(t)))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_InvalidTargetID(t *testing.T) {
	srv := NewTestServer()
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodGet, "/query-targets/abc/saved-statements",
		"", ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", rec.Code)
	}
}

func TestSavedStatement_InvalidStatementID(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]string{
		"name":      "Updated",
		"statement": "SELECT 2",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPut, "/query-targets/22/saved-statements/abc",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid statementId, got %d", rec.Code)
	}
}

func TestSavedStatement_CreateMissingName(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]string{
		"statement": "SELECT 1",
		"scope":     "personal",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_CreateMissingStatement(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]string{
		"name":  "Test",
		"scope": "personal",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing statement, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSavedStatement_CreateMissingScope(t *testing.T) {
	srv := NewTestServer()
	body, _ := json.Marshal(map[string]string{
		"name":      "Test",
		"statement": "SELECT 1",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing scope, got %d: %s", rec.Code, rec.Body.String())
	}
}
