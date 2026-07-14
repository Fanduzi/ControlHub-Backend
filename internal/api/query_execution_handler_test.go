// Package api provides tests for the query execution handlers.
// input: bytes, context, fmt, net/http, net/http/httptest, strings, testing, time, chi, internal/model, internal/service
// output: TestQueryExecution_* (execute success/errors, auth, history; actor taken from token not body)
// pos: Handler + auth middleware + error-mapping coverage for the Phase 37 query sandbox endpoints
// note: if this file changes, update header and README.md
package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

var qeTestNow = time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

// stubQueryExec is a configurable queryExecutionAPI stub for handler tests.
type stubQueryExec struct {
	executeResp   model.QueryExecuteResponse
	executeErr    error
	listItems     []model.QueryExecutionRecord
	listPageInfo  *model.PageInfo
	listErr       error
	navResp       model.RelatedRecordNavigationResponse
	navErr        error
	gotActor      uint64
	gotRole       string
	gotTargetID   uint64
	gotMaxRows    int
	gotNavRequest model.RelatedRecordNavigationRequest
	executeCalled bool
	navCalled     bool
}

func (s *stubQueryExec) Execute(_ context.Context, actorUserID uint64, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	s.executeCalled = true
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.gotMaxRows = req.MaxRows
	return s.executeResp, s.executeErr
}

func (s *stubQueryExec) ListHistory(_ context.Context, actorUserID uint64, actorRole string, targetID uint64, _ model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, *model.PageInfo, error) {
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.gotRole = actorRole
	return s.listItems, s.listPageInfo, s.listErr
}

func (s *stubQueryExec) NavigateRelatedRecords(_ context.Context, actorUserID uint64, targetID uint64, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error) {
	s.navCalled = true
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.gotNavRequest = req
	return s.navResp, s.navErr
}

func newQueryExecRouter(stub queryExecutionAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:           service.NewAuthService(nil, "qe-test-secret"),
		QueryExecutionService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       fixedClock(qeTestNow),
		},
	}
	return NewRouter(deps)
}

func qeRequest(method, path, body, bearer string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestQueryExecution_Execute_Success(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{
		Status:   model.QueryExecutionSuccess,
		RowCount: 1,
		Columns:  []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:     [][]any{{int64(1)}},
	}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	// WHY: the actor must come from the verified token, never from the request
	// body. The body carries only the statement and maxRows.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", `{"statement":"select 1","maxRows":50}`, token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.executeCalled {
		t.Fatal("Execute was not called")
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor passed to service = %d, want 42 (from token, not body)", stub.gotActor)
	}
	if stub.gotTargetID != 22 {
		t.Fatalf("target id = %d, want 22", stub.gotTargetID)
	}
	if stub.gotMaxRows != 50 {
		t.Fatalf("maxRows = %d, want 50", stub.gotMaxRows)
	}
	if !strings.Contains(rec.Body.String(), `"status":"success"`) {
		t.Fatalf("response missing success status: %s", rec.Body.String())
	}
}

func TestQueryExecution_Execute_InvalidID(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/abc/execute", `{"statement":"select 1"}`, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_Execute_UnsafeStatement(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{
		executeErr: fmt.Errorf("%w: only a single SELECT statement is allowed", service.ErrQueryValidationFailed),
	})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", `{"statement":"delete from t"}`, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_Execute_DisabledTarget(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{executeErr: service.ErrQueryNotAllowed})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", `{"statement":"select 1"}`, token))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "query_not_allowed") {
		t.Fatalf("body = %s, want query_not_allowed", rec.Body.String())
	}
}

func TestQueryExecution_Execute_RejectsActorIDInBody(t *testing.T) {
	stub := &stubQueryExec{}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	// WHY: the handler must never accept an actor id from the request body.
	// Strict decoding rejects the unknown actorUserId field outright.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", `{"statement":"select 1","actorUserId":999}`, token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (actor id must not be accepted in body)", rec.Code)
	}
	if stub.executeCalled {
		t.Fatal("Execute must not run when the body carries an actor id")
	}
}

func TestQueryExecution_Execute_MissingBearer(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", `{"statement":"select 1"}`, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestQueryExecution_Execute_InvalidBearer(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", `{"statement":"select 1"}`, "not-a-valid-token"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestQueryExecution_ListHistory(t *testing.T) {
	items := []model.QueryExecutionRecord{
		{
			ID: 1, TargetResourceID: 22, ActorUserID: 42,
			Actor:  model.QueryExecutionActor{DisplayName: "ControlHub Admin"},
			Status: model.QueryExecutionSuccess, RowCount: 1, CreatedAt: qeTestNow,
		},
		{
			ID: 2, TargetResourceID: 22, ActorUserID: 42,
			Actor:  model.QueryExecutionActor{DisplayName: "ControlHub Admin"},
			Status: model.QueryExecutionRejected, CreatedAt: qeTestNow,
		},
	}
	pageInfo := &model.PageInfo{Page: 1, PageSize: 20, TotalItems: 2, TotalPages: 1}
	stub := &stubQueryExec{listItems: items, listPageInfo: pageInfo}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/22/executions?page=1&pageSize=20", "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor passed to ListHistory = %d, want 42 from token", stub.gotActor)
	}
	if stub.gotRole != "admin" {
		t.Fatalf("role passed to ListHistory = %q, want admin", stub.gotRole)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"items"`) || !strings.Contains(body, `"pageInfo"`) {
		t.Fatalf("response missing items/pageInfo: %s", body)
	}
	// WHY: history must carry metadata only, never full result rows.
	if strings.Contains(body, `"rows"`) {
		t.Fatalf("history response must not include result rows: %s", body)
	}
	// WHY: public history uses nested actor.displayName, never actorUserId.
	if strings.Contains(body, `"actorUserId"`) {
		t.Fatalf("history must not expose actorUserId: %s", body)
	}
	if !strings.Contains(body, `"displayName":"ControlHub Admin"`) {
		t.Fatalf("history missing actor.displayName: %s", body)
	}
	// WHY: hasNextPage/hasPreviousPage must be present in the serialized response.
	if !strings.Contains(body, `"hasNextPage"`) {
		t.Fatalf("response missing hasNextPage: %s", body)
	}
	if !strings.Contains(body, `"hasPreviousPage"`) {
		t.Fatalf("response missing hasPreviousPage: %s", body)
	}
}

func TestQueryExecution_ListHistory_UnknownTarget(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{listErr: service.ErrQueryTargetNotFound})
	token := mintToken(t, "qe-test-secret", 42, "editor", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/999/executions", "", token))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "query_target_not_found") {
		t.Fatalf("body = %s, want query_target_not_found", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_PassesNonAdminRole(t *testing.T) {
	stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}, listPageInfo: &model.PageInfo{Page: 1, PageSize: 20}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 7, "editor", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/22/executions", "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if stub.gotActor != 7 || stub.gotRole != "editor" {
		t.Fatalf("got actor=%d role=%q, want 7/editor", stub.gotActor, stub.gotRole)
	}
}

func TestQueryExecution_ListHistory_MissingBearer(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet, "/query-targets/22/executions", "", ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
