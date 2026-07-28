package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
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

	body, _ := json.Marshal(map[string]string{
		"name":      "Test",
		"statement": "SELECT 1",
		"scope":     "personal",
	})
	rec := httptest.NewRecorder()
	srv.Router.ServeHTTP(rec, ssRequest(http.MethodPost, "/query-targets/22/saved-statements",
		string(body), ssAdminToken(t)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
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
