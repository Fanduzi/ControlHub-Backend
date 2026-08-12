// Package api provides tests for the governed Explain handler.
// input: bytes, context, errors, net/http, net/http/httptest, strings, testing, internal/model, internal/service
// output: TestQueryExplain_* covering error mapping, fixed messages, no-leak
// pos: Phase 38N — prove the handler uses fixed messages (never err.Error()) and rejects typed EXPLAIN/DML/DDL
// note: mirrors the execute handler test pattern; the stub implements queryExplainAPI
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// stubExplainAPI implements queryExplainAPI for handler tests.
type stubExplainAPI struct {
	gotActor  uint64
	gotTarget uint64
	gotReq    model.ExplainRequest
	resp      model.ExplainResponse
	err       error
	called    bool
}

func (s *stubExplainAPI) Explain(_ context.Context, actor uint64, target uint64, req model.ExplainRequest) (model.ExplainResponse, error) {
	s.called = true
	s.gotActor = actor
	s.gotTarget = target
	s.gotReq = req
	return s.resp, s.err
}

func newExplainRouter(stub queryExplainAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:         service.NewAuthService(testAuthUsers, "qe-test-secret"),
		QueryExplainService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			Clock: fixedClock(qeTestNow),
		},
	}
	return NewRouter(deps)
}

func TestQueryExplain_Success(t *testing.T) {
	stub := &stubExplainAPI{resp: model.ExplainResponse{
		TargetResourceID: 616,
		Engine:           model.ExplainEngineMySQL,
		FormatVersion:    model.ExplainFormatVersion,
		Nodes:            []model.ExplainNode{{ID: "0", Operation: model.ExplainOpTableAccess, Access: model.ExplainAccessFullScan}},
		Risks:            []model.ExplainRisk{{Code: model.ExplainRiskFullTableScan, Severity: model.ExplainSeverityWarning}},
		Truncated:        false,
	}}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select * from big"}`, token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.called {
		t.Fatal("stub.Explain was not called")
	}
	if stub.gotActor != 42 {
		t.Errorf("actor = %d, want 42 (from token, not body)", stub.gotActor)
	}
	if stub.gotTarget != 616 {
		t.Errorf("target = %d, want 616", stub.gotTarget)
	}
	if stub.gotReq.Statement != "select * from big" {
		t.Errorf("statement = %q, want 'select * from big'", stub.gotReq.Statement)
	}
	body := rec.Body.String()
	for _, want := range []string{`"targetResourceId":616`, `"engine":"mysql"`, `"formatVersion":1`, `"full_table_scan"`, `"warning"`} {
		if !strings.Contains(body, want) {
			t.Errorf("response body must contain %q, got: %s", want, body)
		}
	}
	for _, banned := range []string{"table_name", "possible_keys", "dsn", "password", "actorUserId", "credential"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(banned)) {
			t.Errorf("response body must not contain %q, got: %s", banned, body)
		}
	}
}

func TestQueryExplain_MissingStatement(t *testing.T) {
	stub := &stubExplainAPI{}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"   "}`, token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "statement is required") {
		t.Errorf("body must contain 'statement is required', got: %s", rec.Body.String())
	}
	if stub.called {
		t.Error("stub must NOT be called for missing statement")
	}
}

func TestQueryExplain_MalformedJSON(t *testing.T) {
	stub := &stubExplainAPI{}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{not-json`, token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid request payload") {
		t.Errorf("body must contain 'invalid request payload', got: %s", rec.Body.String())
	}
}

func TestQueryExplain_UnknownFields(t *testing.T) {
	stub := &stubExplainAPI{}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1","actorUserId":999}`, token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (unknown field rejected); body=%s", rec.Code, rec.Body.String())
	}
	if stub.called {
		t.Error("stub must NOT be called when unknown fields are present")
	}
}

func TestQueryExplain_InvalidTargetID(t *testing.T) {
	stub := &stubExplainAPI{}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/abc/explain", `{"statement":"select 1"}`, token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestQueryExplain_AuthMissing(t *testing.T) {
	stub := &stubExplainAPI{}
	router := newExplainRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (fresh-actor middleware)", rec.Code)
	}
	if stub.called {
		t.Error("stub must NOT be called without auth")
	}
}

func TestQueryExplain_StaleToken(t *testing.T) {
	stub := &stubExplainAPI{}
	router := newExplainRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, "not-a-valid-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestQueryExplain_ValidationFailed(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryValidationFailed}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"explain select 1"}`, token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"validation_failed"`) {
		t.Errorf("body must contain validation_failed, got: %s", body)
	}
	if !strings.Contains(body, "statement is not a permitted read-only SELECT") {
		t.Errorf("body must contain the fixed message, got: %s", body)
	}
}

func TestQueryExplain_TypedExplainRejected(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryValidationFailed}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	for _, stmt := range []string{"explain select 1", "EXPLAIN SELECT * FROM t", "explain format=json select 1"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"`+stmt+`"}`, token))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("stmt %q: status = %d, want 400", stmt, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, "statement is not a permitted read-only SELECT") {
			t.Errorf("stmt %q: body must contain fixed message, got: %s", stmt, body)
		}
		if strings.Contains(strings.ToLower(body), strings.ToLower(stmt)) {
			t.Errorf("stmt %q: body must NOT echo the user statement, got: %s", stmt, body)
		}
	}
}

func TestQueryExplain_DMLRejected(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryValidationFailed}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	for _, stmt := range []string{"insert into t values (1)", "update t set a=1", "delete from t", "drop table t"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"`+stmt+`"}`, token))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("stmt %q: status = %d, want 400", stmt, rec.Code)
		}
	}
}

func TestQueryExplain_LiteralBearingRejectedSQLNoLeak(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryValidationFailed}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select * from t where ssn='123-45-6789'"}`, token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "123-45-6789") {
		t.Errorf("body must NOT contain the literal value, got: %s", body)
	}
	if strings.Contains(body, "ssn") {
		t.Errorf("body must NOT contain the column name, got: %s", body)
	}
}

func TestQueryExplain_NotAllowed(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryNotAllowed}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, token))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "target is not enabled for execution") {
		t.Errorf("body must contain fixed message, got: %s", rec.Body.String())
	}
}

func TestQueryExplain_TargetNotFound(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryTargetNotFound}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/999/explain", `{"statement":"select 1"}`, token))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "query target not found") {
		t.Errorf("body must contain fixed message, got: %s", rec.Body.String())
	}
}

func TestQueryExplain_NotSupported(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryExplainNotSupported}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/617/explain", `{"statement":"select 1"}`, token))
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "query_explain_not_supported") {
		t.Errorf("body must contain query_explain_not_supported, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "explain is not supported for this target engine") {
		t.Errorf("body must contain fixed message, got: %s", rec.Body.String())
	}
}

func TestQueryExplain_Timeout(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryTimeout}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, token))
	if rec.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want 408", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "explain exceeded the timeout") {
		t.Errorf("body must contain fixed message, got: %s", rec.Body.String())
	}
}

func TestQueryExplain_BackendFailure(t *testing.T) {
	stub := &stubExplainAPI{err: service.ErrQueryBackendFailure}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, token))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "target database rejected the explain request") {
		t.Errorf("body must contain fixed message, got: %s", rec.Body.String())
	}
}

func TestQueryExplain_NoRawErrorLeak(t *testing.T) {
	leaky := errors.New("connection refused: rouser:secret-dsn-do-not-leak@tcp(db.internal:3306)/sandbox")
	stub := &stubExplainAPI{err: fmt.Errorf("%w: %v", service.ErrQueryBackendFailure, leaky)}
	router := newExplainRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, token))
	body := rec.Body.String()
	for _, banned := range []string{"secret-dsn-do-not-leak", "rouser", "db.internal", "3306", "connection refused"} {
		if strings.Contains(body, banned) {
			t.Errorf("body must NOT contain %q (raw error leak), got: %s", banned, body)
		}
	}
}

func TestQueryExplain_FixedMessageRegression(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		msg    string
	}{
		{"validation", service.ErrQueryValidationFailed, 400, "validation_failed", "statement is not a permitted read-only SELECT"},
		{"not_allowed", service.ErrQueryNotAllowed, 403, "query_not_allowed", "target is not enabled for execution"},
		{"not_found", service.ErrQueryTargetNotFound, 404, "query_target_not_found", "query target not found"},
		{"not_supported", service.ErrQueryExplainNotSupported, 409, "query_explain_not_supported", "explain is not supported for this target engine"},
		{"timeout", service.ErrQueryTimeout, 408, "query_timeout", "explain exceeded the timeout"},
		{"backend", service.ErrQueryBackendFailure, 502, "query_backend_error", "target database rejected the explain request"},
	}
	for _, tc := range cases {
		stub := &stubExplainAPI{err: tc.err}
		router := newExplainRouter(stub)
		token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/616/explain", `{"statement":"select 1"}`, token))
		if rec.Code != tc.status {
			t.Errorf("case %s: status = %d, want %d", tc.name, rec.Code, tc.status)
		}
		body := rec.Body.String()
		if !strings.Contains(body, tc.code) {
			t.Errorf("case %s: body must contain code %q, got: %s", tc.name, tc.code, body)
		}
		if !strings.Contains(body, tc.msg) {
			t.Errorf("case %s: body must contain fixed message %q, got: %s", tc.name, tc.msg, body)
		}
	}
}
