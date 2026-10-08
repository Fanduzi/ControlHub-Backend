// Package api provides tests for the T8-B result-contract upgrade refusal HTTP mapping.
// input: net/http, net/http/httptest, strings, testing, internal/service
// output: TestResultContractUpgradeRequired_* cases (execute, saved-execute, related-records)
// pos: The post-finalize capability refusal maps to HTTP 400 result_contract_upgrade_required on all three governed result endpoints with no rows in the envelope
// note: if this file changes, update header and README.md
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/service"
)

func assertUpgradeRequiredResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, body)
	}
	if !strings.Contains(body, `"error":"result_contract_upgrade_required"`) {
		t.Fatalf("error code missing result_contract_upgrade_required: %s", body)
	}
	if strings.Contains(body, `"rows"`) || strings.Contains(body, `"cellTruncated"`) {
		t.Fatalf("refusal envelope must carry no result payload: %s", body)
	}
}

func TestResultContractUpgradeRequired_ExecuteMapsTo400(t *testing.T) {
	stub := &stubQueryExec{executeErr: service.ErrResultContractUpgradeRequired}
	recorder := httptest.NewRecorder()
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	newQueryExecRouter(stub).ServeHTTP(recorder,
		qeRequest(http.MethodPost, "/query-targets/9001/execute",
			`{"statement":"select payload from t where id = 1"}`, token))

	assertUpgradeRequiredResponse(t, recorder)
	if !stub.executeCalled {
		t.Fatal("execute handler must reach the service — refusal is produced post-finalize, not by request validation")
	}
}

func TestResultContractUpgradeRequired_SavedStatementMapsTo400(t *testing.T) {
	stub := &stubQueryExec{templateErr: service.ErrResultContractUpgradeRequired}
	recorder := httptest.NewRecorder()

	newTemplateExecRouter(stub).ServeHTTP(recorder,
		qeRequest(http.MethodPost, "/query-targets/9001/saved-statements/7/execute",
			`{"values":{}}`, templateExecToken(t)))

	assertUpgradeRequiredResponse(t, recorder)
	if !stub.templateCalled {
		t.Fatal("saved-execute handler must reach the service — refusal is produced post-finalize")
	}
}

func TestResultContractUpgradeRequired_NavigationMapsTo400(t *testing.T) {
	stub := &stubQueryExec{navErr: service.ErrResultContractUpgradeRequired}
	recorder := httptest.NewRecorder()

	newNavRouter(stub).ServeHTTP(recorder,
		qeRequest(http.MethodPost, "/query-targets/9001/related-records",
			`{"source":{"database":"db","object":"tbl","kind":"table","foreignKey":"fk"},"localValues":["1"]}`, navBearer(t)))

	assertUpgradeRequiredResponse(t, recorder)
	if !stub.navCalled {
		t.Fatal("related-records handler must reach the service — refusal is produced post-finalize")
	}
}

// The refusal is a delivery decision, not request validation: unknown
// capability tokens reach the service unchanged and never satisfy the gate.
func TestResultContractUpgradeRequired_UnknownCapabilityPassesThrough(t *testing.T) {
	stub := &stubQueryExec{}
	recorder := httptest.NewRecorder()
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	newQueryExecRouter(stub).ServeHTTP(recorder,
		qeRequest(http.MethodPost, "/query-targets/9001/execute",
			`{"statement":"select 1","capabilities":["celltruncated","bogus"]}`, token))

	if !stub.executeCalled {
		t.Fatal("handler must not reject unknown capability tokens at the door")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for stub success; body=%s", recorder.Code, recorder.Body.String())
	}
}
