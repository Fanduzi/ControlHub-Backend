// Package api provides tests for the related-record navigation handler.
// input: bytes, context, net/http, net/http/httptest, strings, testing, time, chi, internal/model, internal/service
// output: TestNavigateRelatedRecords_* (handler auth, body validation, error mapping, response shape)
// pos: Handler + auth middleware coverage for POST /query-targets/{id}/related-records (Phase 38J)
// note: if this file changes, update header and README.md
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

var navTestNow = time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)

func newNavRouter(stub queryExecutionAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:           service.NewAuthService(nil, "nav-test-secret"),
		QueryExecutionService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       fixedClock(navTestNow),
		},
	}
	return NewRouter(deps)
}

func navBearer(t *testing.T) string {
	t.Helper()
	return mintToken(t, "nav-test-secret", 42, "admin", navTestNow)
}

func TestNavigateRelatedRecords_Success(t *testing.T) {
	stub := &stubQueryExec{
		navResp: model.RelatedRecordNavigationResponse{
			Status:             model.QueryExecutionSuccess,
			ExecutionID:        1,
			TargetResourceID:   9001,
			Engine:             "mysql",
			Columns:            []model.QueryResultColumn{{Name: "id", DatabaseType: "BIGINT"}},
			Rows:               [][]any{{int64(100)}},
			RowCount:           1,
			Truncated:          false,
			DurationMs:         5,
			LimitApplied:       100,
			SourceDatabase:     "orders_db",
			SourceObject:       "order_items",
			ForeignKey:         "fk_order_items_order",
			ReferencedDatabase: "orders",
			ReferencedObject:   "orders",
			ReferencedColumns:  []string{"id"},
		},
	}
	router := newNavRouter(stub)
	token := navBearer(t)

	body := `{"source":{"database":"orders_db","object":"order_items","kind":"table","foreignKey":"fk_order_items_order"},"localValues":["42"],"maxRows":100}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.navCalled {
		t.Fatal("NavigateRelatedRecords was not called")
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor = %d, want 42 (from token)", stub.gotActor)
	}
	if stub.gotTargetID != 9001 {
		t.Fatalf("targetID = %d, want 9001", stub.gotTargetID)
	}
	if stub.gotNavRequest.Source.Database != "orders_db" {
		t.Fatalf("source.database = %q, want %q", stub.gotNavRequest.Source.Database, "orders_db")
	}
	if stub.gotNavRequest.Source.ForeignKey != "fk_order_items_order" {
		t.Fatalf("source.foreignKey = %q, want %q", stub.gotNavRequest.Source.ForeignKey, "fk_order_items_order")
	}
	bodyStr := rec.Body.String()
	if !strings.Contains(bodyStr, `"referencedDatabase":"orders"`) {
		t.Fatalf("response missing referencedDatabase: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, `"foreignKey":"fk_order_items_order"`) {
		t.Fatalf("response missing foreignKey: %s", bodyStr)
	}
}

func TestNavigateRelatedRecords_MissingBearer(t *testing.T) {
	router := newNavRouter(&stubQueryExec{})

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestNavigateRelatedRecords_InvalidID(t *testing.T) {
	router := newNavRouter(&stubQueryExec{})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/abc/related-records", body, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestNavigateRelatedRecords_EmptyBody(t *testing.T) {
	router := newNavRouter(&stubQueryExec{})
	token := navBearer(t)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", `{}`, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestNavigateRelatedRecords_NonTableKind(t *testing.T) {
	router := newNavRouter(&stubQueryExec{})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"view","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "source kind must be") {
		t.Fatalf("body = %s, want kind validation error", rec.Body.String())
	}
}

func TestNavigateRelatedRecords_EmptyLocalValues(t *testing.T) {
	router := newNavRouter(&stubQueryExec{})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":[]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestNavigateRelatedRecords_TargetNotFound(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrQueryTargetNotFound})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "query_target_not_found") {
		t.Fatalf("body = %s, want query_target_not_found", rec.Body.String())
	}
}

func TestNavigateRelatedRecords_NotAllowed(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrQueryNotAllowed})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "query_not_allowed") {
		t.Fatalf("body = %s, want query_not_allowed", rec.Body.String())
	}
}

func TestNavigateRelatedRecords_ValidationFailed(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrQueryValidationFailed})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestNavigateRelatedRecords_FKNotFound(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrNavigationSourceNotFound})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk_nonexistent"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestNavigateRelatedRecords_ValueMismatch(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrNavigationValueMismatch})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1","2"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestNavigateRelatedRecords_Timeout(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrQueryTimeout})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408", rec.Code)
	}
}

func TestNavigateRelatedRecords_BackendError(t *testing.T) {
	router := newNavRouter(&stubQueryExec{navErr: service.ErrQueryBackendFailure})
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// Actor must come from token, never from request body.
func TestNavigateRelatedRecords_ActorFromToken(t *testing.T) {
	stub := &stubQueryExec{
		navResp: model.RelatedRecordNavigationResponse{
			Status: model.QueryExecutionSuccess,
		},
	}
	router := newNavRouter(stub)
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor = %d, want 42 (from token, not body)", stub.gotActor)
	}
}

// Response must not contain SQL, DSN, or credentials.
func TestNavigateRelatedRecords_ResponseNoSQLNoDSN(t *testing.T) {
	stub := &stubQueryExec{
		navResp: model.RelatedRecordNavigationResponse{
			Status:           model.QueryExecutionSuccess,
			ExecutionID:      1,
			TargetResourceID: 9001,
			Engine:           "mysql",
			Columns:          []model.QueryResultColumn{{Name: "id", DatabaseType: "BIGINT"}},
			Rows:             [][]any{{int64(1)}},
			RowCount:         1,
		},
	}
	router := newNavRouter(stub)
	token := navBearer(t)

	body := `{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/9001/related-records", body, token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	bodyStr := rec.Body.String()
	// Must not contain SQL keywords or DSN patterns.
	if strings.Contains(bodyStr, "SELECT") || strings.Contains(bodyStr, "tcp(") {
		t.Fatalf("response must not contain SQL or DSN: %s", bodyStr)
	}
}
