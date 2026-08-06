// Package api provides tests for the template-execution handler.
// input: encoding/json, errors, net/http, net/http/httptest, strings, testing, time, chi, internal/model, internal/service
// output: TestTemplateExecute_* (strict request decoding, controlled field errors, error mapping, actor from token)
// pos: Handler + auth coverage for POST /query-targets/{id}/saved-statements/{statementId}/execute (Phase 38W)
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func newTemplateExecRouter(stub queryExecutionAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:                service.NewAuthService(nil, "qe-test-secret"),
		QueryExecutionService:      stub,
		QuerySavedStatementService: &fakeSavedStatementService{},
		QueryExecutionAuth: QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       fixedClock(qeTestNow),
		},
	}
	return NewRouter(deps)
}

func templateExecToken(t *testing.T) string {
	t.Helper()
	return mintToken(t, "qe-test-secret", 42, "viewer", qeTestNow)
}

func TestTemplateExecute_RequiresBearer(t *testing.T) {
	router := newTemplateExecRouter(&stubQueryExec{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute",
		`{"values":{"status":"paid"}}`, ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestTemplateExecute_Success(t *testing.T) {
	stub := &stubQueryExec{templateResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess, RowCount: 1}}
	router := newTemplateExecRouter(stub)
	body := `{"values":{"status":"paid","minimum_total":"100.50"},"maxRows":100,"pagination":{"page":1,"pageSize":10}}`

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute", body, templateExecToken(t)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.templateCalled {
		t.Fatal("ExecuteSavedStatement was not called")
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor = %d, want 42 (from token, not body)", stub.gotActor)
	}
	if stub.gotTargetID != 22 || stub.templateStmt != 7 {
		t.Fatalf("target=%d stmt=%d, want 22/7 from path", stub.gotTargetID, stub.templateStmt)
	}
	if stub.templateReq.MaxRows != 100 {
		t.Fatalf("maxRows = %d, want 100", stub.templateReq.MaxRows)
	}
	if stub.templateReq.Pagination == nil || stub.templateReq.Pagination.Page != 1 {
		t.Fatalf("pagination = %+v, want page 1", stub.templateReq.Pagination)
	}
	if got := string(stub.templateReq.Values["status"]); got != `"paid"` {
		t.Fatalf("status value = %s, want raw JSON string", got)
	}
}

func TestTemplateExecute_RejectsUnknownAndForbiddenFields(t *testing.T) {
	forbidden := map[string]string{
		"SQL text":            `{"values":{"status":"paid"},"statement":"SELECT 1"}`,
		"parameter defs":      `{"values":{"status":"paid"},"parameters":[{"name":"x","type":"string"}]}`,
		"actor identity":      `{"values":{"status":"paid"},"actorUserId":42}`,
		"role":                `{"values":{"status":"paid"},"role":"admin"}`,
		"credential":          `{"values":{"status":"paid"},"credentialRef":"PROD"}`,
		"DSN":                 `{"values":{"status":"paid"},"dsn":"user:pass@tcp(x:3306)/y"}`,
		"disclosure policy":   `{"values":{"status":"paid"},"disclosurePolicy":"redact"}`,
		"audit payload":       `{"values":{"status":"paid"},"audit":{"actor":"x"}}`,
		"result fields":       `{"values":{"status":"paid"},"result":{"rows":[]}}`,
		"owner":               `{"values":{"status":"paid"},"ownerUserId":1}`,
		"target identity":     `{"values":{"status":"paid"},"targetResourceId":1}`,
	}
	for name, body := range forbidden {
		t.Run(name, func(t *testing.T) {
			router := newTemplateExecRouter(&stubQueryExec{})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute", body, templateExecToken(t)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "validation_failed") {
				t.Fatalf("body = %s, want validation_failed", rec.Body.String())
			}
		})
	}
}

func TestTemplateExecute_RejectsDuplicateKeysAndMalformedJSON(t *testing.T) {
	router := newTemplateExecRouter(&stubQueryExec{})
	token := templateExecToken(t)

	cases := []string{
		`{"values":{"status":"paid","status":"paid"}}`,
		`{"values":{"status":"paid"},"maxRows":1,"maxRows":2}`,
		`{"values":`,
		`{"values":{"status":"paid"}} trailing`,
	}
	for _, body := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute", body, token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, rec.Code)
		}
	}
}

func TestTemplateExecute_RejectsOversizedValuesObject(t *testing.T) {
	router := newTemplateExecRouter(&stubQueryExec{})
	big := `"` + strings.Repeat("x", 16*1024) + `"`
	body := `{"values":{"a":` + big + `}}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute", body, templateExecToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

func TestTemplateExecute_RejectsInvalidPaginationAndMaxRows(t *testing.T) {
	router := newTemplateExecRouter(&stubQueryExec{})
	token := templateExecToken(t)

	cases := []string{
		`{"values":{"status":"paid"},"pagination":{"page":0,"pageSize":10}}`,
		`{"values":{"status":"paid"},"pagination":{"page":1,"pageSize":7}}`,
		`{"values":{"status":"paid"},"maxRows":-1}`,
	}
	for _, body := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute", body, token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q status = %d, want 400", body, rec.Code)
		}
	}
}

func TestTemplateExecute_ControlledFieldErrorsNeverEchoValues(t *testing.T) {
	stub := &stubQueryExec{templateErr: &service.TemplateValueValidationError{
		Fields: map[string]string{"status": "missing", "bogus": "unknown"},
	}}
	router := newTemplateExecRouter(stub)
	body := `{"values":{"status":"paid","bogus":"secret-input"}}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute", body, templateExecToken(t)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Error   string            `json:"error"`
		Message string            `json:"message"`
		Details map[string]string `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if envelope.Error != "validation_failed" {
		t.Fatalf("error code = %q, want validation_failed", envelope.Error)
	}
	if envelope.Details["status"] != "missing" || envelope.Details["bogus"] != "unknown" {
		t.Fatalf("details = %v, want per-parameter field codes", envelope.Details)
	}
	// Parameter names are declared identifiers; supplied values are not echoed.
	if strings.Contains(rec.Body.String(), "paid") || strings.Contains(rec.Body.String(), "secret-input") {
		t.Fatalf("response echoes a supplied value: %s", rec.Body.String())
	}
}

func TestTemplateExecute_ErrorMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"statement not found", service.ErrQuerySavedStatementNotFound, http.StatusNotFound},
		{"target not found", service.ErrQueryTargetNotFound, http.StatusNotFound},
		{"validation", service.ErrQueryValidationFailed, http.StatusBadRequest},
		{"not allowed", service.ErrQueryNotAllowed, http.StatusForbidden},
		{"timeout", service.ErrQueryTimeout, http.StatusRequestTimeout},
		{"backend failure", service.ErrQueryBackendFailure, http.StatusBadGateway},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{templateErr: tc.err}
			router := newTemplateExecRouter(stub)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/saved-statements/7/execute",
				`{"values":{"status":"paid"}}`, templateExecToken(t)))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}
