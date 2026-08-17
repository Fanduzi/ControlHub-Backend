// Package api provides tests for the query execution handlers.
// input: bytes, context, fmt, net/http, net/http/httptest, strings, testing, time, chi, internal/model, internal/service
// output: TestQueryExecution_* (execute success/errors, auth, history; actor taken from token not body)
// pos: Handler + auth middleware + error-mapping coverage for the Phase 37 query sandbox endpoints, Phase 38S governed result paging
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
	executeResp    model.QueryExecuteResponse
	executeErr     error
	listItems      []model.QueryExecutionRecord
	listErr        error
	navResp        model.RelatedRecordNavigationResponse
	navErr         error
	gotActor       uint64
	gotRole        string
	gotTargetID    uint64
	gotMaxRows     int
	gotPagination  *model.QueryExecutePaginationRequest
	gotNavRequest  model.RelatedRecordNavigationRequest
	gotQuery       model.QueryExecutionListQuery
	executeCalled  bool
	listCalled     bool
	navCalled      bool
	templateResp   model.QueryExecuteResponse
	templateErr    error
	templateReq    model.QuerySavedStatementExecuteRequest
	templateStmt   uint64
	templateCalled bool
}

func (s *stubQueryExec) Execute(_ context.Context, actorUserID uint64, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	s.executeCalled = true
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.gotMaxRows = req.MaxRows
	s.gotPagination = req.Pagination
	resp := s.executeResp
	if req.Pagination != nil {
		resp.Pagination = &model.QueryExecutePaginationResponse{
			Page:            req.Pagination.Page,
			PageSize:        req.Pagination.PageSize,
			HasPreviousPage: req.Pagination.Page > 1,
			HasNextPage:     false,
		}
	}
	return resp, s.executeErr
}

func (s *stubQueryExec) ExecuteSavedStatement(_ context.Context, actorUserID, targetID, statementID uint64, req model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error) {
	s.templateCalled = true
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.templateStmt = statementID
	s.templateReq = req
	return s.templateResp, s.templateErr
}

func (s *stubQueryExec) ListHistory(_ context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error) {
	s.listCalled = true
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.gotRole = actorRole
	s.gotQuery = q
	if s.listErr != nil {
		return nil, s.listErr
	}
	page := &model.QueryExecutionCursorPage{Items: s.listItems}
	if q.Page > 0 {
		p, ps := model.NormalizePagination(q.Page, q.PageSize)
		info := model.NewPageInfo(p, ps, len(s.listItems))
		page.PageInfo = &info
	}
	return page, nil
}

// QueryEvidencePersistenceFailures returns the stub counter (0); the live
// counter path is covered by repository/integration tests.
func (s *stubQueryExec) QueryEvidencePersistenceFailures() int64 { return 0 }

func (s *stubQueryExec) NavigateRelatedRecords(_ context.Context, actorUserID uint64, targetID uint64, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error) {
	s.navCalled = true
	s.gotActor = actorUserID
	s.gotTargetID = targetID
	s.gotNavRequest = req
	return s.navResp, s.navErr
}

func newQueryExecRouter(stub queryExecutionAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:           service.NewAuthService(testAuthUsers, "qe-test-secret"),
		QueryExecutionService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			Clock: fixedClock(qeTestNow),
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
	stub := &stubQueryExec{listItems: items}
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
	// WHY: explicit ?page= triggers offset mode → response carries pageInfo and nextCursor:null.
	if !strings.Contains(body, `"items"`) || !strings.Contains(body, `"pageInfo"`) {
		t.Fatalf("response missing items/pageInfo: %s", body)
	}
	if !strings.Contains(body, `"nextCursor":null`) {
		t.Fatalf("offset mode response must include nextCursor:null: %s", body)
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
	stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
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

// --- cursor-based pagination filter tests ---

func TestQueryExecution_ListHistory_ValidFiltersParsed(t *testing.T) {
	stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?status=success&from=2025-01-01T00:00:00Z&to=2025-06-01T00:00:00Z", "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if stub.gotQuery.Status == nil || *stub.gotQuery.Status != model.QueryExecutionSuccess {
		t.Fatalf("status filter not passed: got %v, want success", stub.gotQuery.Status)
	}
	if stub.gotQuery.From == nil {
		t.Fatal("from filter not passed")
	}
	if stub.gotQuery.To == nil {
		t.Fatal("to filter not passed")
	}
}

func TestQueryExecution_ListHistory_InvalidStatusReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?status=INVALID", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_InvalidTimestampReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?from=not-a-date", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_InvertedTimeWindowReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	// WHY: from must be before to; an inverted window (from > to) is rejected
	// to prevent confusing empty result sets that look like bugs.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?from=2025-06-01T00:00:00Z&to=2025-01-01T00:00:00Z", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_PageAndCursorReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	// WHY: page (offset) and cursor (keyset) are mutually exclusive pagination
	// modes. Accepting both would produce ambiguous query semantics.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?page=1&cursor=abc123", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_CursorPassedToService(t *testing.T) {
	cursorVal := "test-cursor-value"
	stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?cursor="+cursorVal, "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if stub.gotQuery.Cursor == nil || *stub.gotQuery.Cursor != cursorVal {
		t.Fatalf("cursor not passed: got %v, want %q", stub.gotQuery.Cursor, cursorVal)
	}
}

func TestQueryExecution_ListHistory_DefaultPaginationUsed(t *testing.T) {
	stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions", "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// WHY: no ?page= param → cursor-initial mode (Page=0, PageSize=20).
	if stub.gotQuery.Page != 0 || stub.gotQuery.PageSize != 20 {
		t.Fatalf("default pagination = page:%d pageSize:%d, want 0/20", stub.gotQuery.Page, stub.gotQuery.PageSize)
	}
	body := rec.Body.String()
	// WHY: no ?page= param → cursor mode, so pageInfo must be absent.
	if strings.Contains(body, `"pageInfo"`) {
		t.Fatalf("cursor-initial response must not include pageInfo: %s", body)
	}
}

// --- P2: reject explicitly supplied empty filters ---
// WHY: `status=`, `from=`, `to=`, and `cursor=` with empty values are
// ambiguous requests, not absent filters. Treating them as absent would
// silently change the query semantics. They must produce a controlled 400.

func TestQueryExecution_ListHistory_EmptyStatusReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?status=", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_EmptyFromReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?from=", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_EmptyToReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?to=", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_EmptyCursorReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?cursor=", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQueryExecution_ListHistory_EqualTimeWindowReturns400(t *testing.T) {
	router := newQueryExecRouter(&stubQueryExec{})
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	// WHY: from == to is a zero-width window and must be rejected — it
	// would produce an empty result that looks like a bug rather than a
	// validation error.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?from=2025-06-01T00:00:00Z&to=2025-06-01T00:00:00Z", "", token))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

// --- P2: envelope consistency ---

// TestQueryExecution_ListHistory_CursorEnvelopeShape proves the response
// envelope is internally consistent: cursor mode always includes nextCursor
// (null when no more pages) and never includes pageInfo.
func TestQueryExecution_ListHistory_CursorEnvelopeShape(t *testing.T) {
	items := []model.QueryExecutionRecord{
		{ID: 1, TargetResourceID: 22, ActorUserID: 42, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Engine: "mysql", StatementDigest: "select 1", StatementPreview: "select 1", Status: model.QueryExecutionSuccess, CreatedAt: qeTestNow},
	}
	stub := &stubQueryExec{listItems: items}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions", "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// WHY: nextCursor must always be present in cursor mode, even when null.
	if !strings.Contains(body, `"nextCursor"`) {
		t.Fatalf("cursor mode response must include nextCursor: %s", body)
	}
	// WHY: pageInfo must be absent in cursor mode.
	if strings.Contains(body, `"pageInfo"`) {
		t.Fatalf("cursor mode response must not include pageInfo: %s", body)
	}
}

// TestQueryExecution_ListHistory_OffsetEnvelopeShape proves the response
// envelope is internally consistent: offset mode always includes nextCursor
// (null) and pageInfo.
func TestQueryExecution_ListHistory_OffsetEnvelopeShape(t *testing.T) {
	items := []model.QueryExecutionRecord{
		{ID: 1, TargetResourceID: 22, ActorUserID: 42, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Engine: "mysql", StatementDigest: "select 1", StatementPreview: "select 1", Status: model.QueryExecutionSuccess, CreatedAt: qeTestNow},
	}
	stub := &stubQueryExec{listItems: items}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodGet,
		"/query-targets/22/executions?page=1", "", token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// WHY: nextCursor must always be present, even in offset mode (null).
	if !strings.Contains(body, `"nextCursor":null`) {
		t.Fatalf("offset mode response must include nextCursor:null: %s", body)
	}
	// WHY: pageInfo must be present in offset mode.
	if !strings.Contains(body, `"pageInfo"`) {
		t.Fatalf("offset mode response must include pageInfo: %s", body)
	}
}

// --- P1: explicit invalid page/pageSize must return 400 and never call the service ---
//
// WHY: the prior parser used r.URL.Query().Get(), which collapses "?page=" (present
// but empty) and "?page=0"/"?page=-1"/"?page=abc" (present but invalid) into the
// "absent" branch, silently selecting cursor-initial mode and dropping pageInfo
// from the response. The public baseline for explicit ?page= was offset pagination
// with pageInfo, so silently switching to cursor mode is a response-shape
// regression. Explicit invalid values must be rejected with a controlled 400 and
// must not reach ListHistory.

func TestQueryExecution_ListHistory_InvalidPageReturns400(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", "/query-targets/22/executions?page="},
		{"zero", "/query-targets/22/executions?page=0"},
		{"negative", "/query-targets/22/executions?page=-1"},
		{"non-numeric", "/query-targets/22/executions?page=abc"},
		{"non-integer", "/query-targets/22/executions?page=1.5"},
		{"repeated", "/query-targets/22/executions?page=1&page=2"},
		{"repeated-with-empty", "/query-targets/22/executions?page=1&page="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodGet, tc.url, "", token))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "validation_failed") {
				t.Fatalf("body = %s, want validation_failed", rec.Body.String())
			}
			// WHY: an invalid explicit page must not invoke the service. If it did,
			// the silent cursor-initial fallback would re-emerge and pageInfo would
			// disappear from a request that explicitly asked for offset pagination.
			if stub.listCalled {
				t.Fatalf("ListHistory must not be called for invalid page; gotQuery=%#v", stub.gotQuery)
			}
		})
	}
}

func TestQueryExecution_ListHistory_InvalidPageSizeReturns400(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"empty", "/query-targets/22/executions?pageSize="},
		{"zero", "/query-targets/22/executions?pageSize=0"},
		{"negative", "/query-targets/22/executions?pageSize=-1"},
		{"non-numeric", "/query-targets/22/executions?pageSize=abc"},
		{"over-max", "/query-targets/22/executions?pageSize=501"},
		{"non-integer", "/query-targets/22/executions?pageSize=1.5"},
		{"repeated", "/query-targets/22/executions?pageSize=20&pageSize=501"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodGet, tc.url, "", token))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "validation_failed") {
				t.Fatalf("body = %s, want validation_failed", rec.Body.String())
			}
			// WHY: pageSize validation must apply in both cursor and offset modes.
			// The service must not see an out-of-range value that NormalizePagination
			// would silently clamp, hiding the client error.
			if stub.listCalled {
				t.Fatalf("ListHistory must not be called for invalid pageSize; gotQuery=%#v", stub.gotQuery)
			}
		})
	}
}

// TestQueryExecution_ListHistory_PageAndCursorConflictPrecedence proves the
// page+cursor conflict message wins over page-value validation: the conflict
// is a more specific diagnosis and keeps the existing 400 contract stable for
// clients that already special-case the "cannot use both" message.
func TestQueryExecution_ListHistory_PageAndCursorConflictPrecedence(t *testing.T) {
	cases := []struct {
		name        string
		url         string
		wantMessage string
	}{
		{"valid-page-plus-cursor", "/query-targets/22/executions?page=1&cursor=abc123", "cannot use both page and cursor parameters"},
		{"invalid-page-plus-cursor", "/query-targets/22/executions?page=abc&cursor=xyz", "cannot use both page and cursor parameters"},
		{"empty-page-plus-cursor", "/query-targets/22/executions?page=&cursor=xyz", "cannot use both page and cursor parameters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodGet, tc.url, "", token))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, "validation_failed") {
				t.Fatalf("body = %s, want validation_failed", body)
			}
			if !strings.Contains(body, tc.wantMessage) {
				t.Fatalf("body = %s, want message %q (conflict must take precedence over page-value validation)", body, tc.wantMessage)
			}
			if stub.listCalled {
				t.Fatalf("ListHistory must not be called when page+cursor conflict; gotQuery=%#v", stub.gotQuery)
			}
		})
	}
}

// TestQueryExecution_ListHistory_ValidPageStaysOffset proves that a valid
// explicit ?page=N keeps the legacy offset contract (pageInfo present,
// nextCursor null) — the regression being fixed is that invalid pages were
// silently dropping this shape, but valid pages must continue to work.
func TestQueryExecution_ListHistory_ValidPageStaysOffset(t *testing.T) {
	cases := []struct {
		name string
		url  string
		page int
	}{
		{"page-1", "/query-targets/22/executions?page=1", 1},
		{"page-2", "/query-targets/22/executions?page=2", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items := []model.QueryExecutionRecord{
				{ID: 1, TargetResourceID: 22, ActorUserID: 42, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Status: model.QueryExecutionSuccess, CreatedAt: qeTestNow},
			}
			stub := &stubQueryExec{listItems: items}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodGet, tc.url, "", token))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if !stub.listCalled {
				t.Fatal("ListHistory must be called for valid page")
			}
			if stub.gotQuery.Page != tc.page {
				t.Fatalf("service received Page = %d, want %d", stub.gotQuery.Page, tc.page)
			}
			body := rec.Body.String()
			if !strings.Contains(body, `"pageInfo"`) {
				t.Fatalf("offset mode response must include pageInfo: %s", body)
			}
			if !strings.Contains(body, `"nextCursor":null`) {
				t.Fatalf("offset mode response must include nextCursor:null: %s", body)
			}
		})
	}
}

// TestQueryExecution_ListHistory_ValidPageSizeAccepted proves that valid
// pageSize values in [1, MaxPageSize] are passed through verbatim to the
// service, in both cursor and offset modes.
func TestQueryExecution_ListHistory_ValidPageSizeAccepted(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantSize int
	}{
		{"cursor-pageSize-1", "/query-targets/22/executions?pageSize=1", 1},
		{"cursor-pageSize-500", "/query-targets/22/executions?pageSize=500", 500},
		{"offset-pageSize-50", "/query-targets/22/executions?page=1&pageSize=50", 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{listItems: []model.QueryExecutionRecord{}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodGet, tc.url, "", token))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if !stub.listCalled {
				t.Fatal("ListHistory must be called for valid pageSize")
			}
			if stub.gotQuery.PageSize != tc.wantSize {
				t.Fatalf("service received PageSize = %d, want %d", stub.gotQuery.PageSize, tc.wantSize)
			}
		})
	}
}

// --- Phase 38S: governed query-result paging contract (RED tests) ---

func TestQueryExecution_Execute_StrictPaginationDecoding_AcceptsPageAndPageSize(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute",
		`{"statement":"select 1","pagination":{"page":1,"pageSize":10}}`, token))

	if rec.Code != http.StatusOK {
		t.Errorf("handler should accept pagination fields, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.executeCalled {
		t.Error("Execute must be called for valid pagination")
	}
	if stub.gotPagination == nil {
		t.Fatal("pagination must be passed to service")
	}
	if stub.gotPagination.Page != 1 || stub.gotPagination.PageSize != 10 {
		t.Errorf("pagination = page:%d pageSize:%d, want 1/10", stub.gotPagination.Page, stub.gotPagination.PageSize)
	}
}

func TestQueryExecution_Execute_StrictPaginationDecoding_ValidPageSizes(t *testing.T) {
	cases := []struct {
		name     string
		pageSize int
	}{
		{"size_10", 10},
		{"size_25", 25},
		{"size_50", 50},
		{"size_100", 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			body := fmt.Sprintf(`{"statement":"select 1","pagination":{"page":1,"pageSize":%d}}`, tc.pageSize)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", body, token))

			if rec.Code != http.StatusOK {
				t.Errorf("pageSize=%d should be accepted, got %d; body=%s", tc.pageSize, rec.Code, rec.Body.String())
			}
			if stub.gotPagination == nil || stub.gotPagination.PageSize != tc.pageSize {
				t.Errorf("service did not receive pageSize=%d", tc.pageSize)
			}
		})
	}
}

func TestQueryExecution_Execute_StrictPaginationDecoding_RejectsInvalidPage(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"zero_page", `{"statement":"select 1","pagination":{"page":0,"pageSize":10}}`},
		{"negative_page", `{"statement":"select 1","pagination":{"page":-1,"pageSize":10}}`},
		{"fractional_page", `{"statement":"select 1","pagination":{"page":1.5,"pageSize":10}}`},
		{"string_page", `{"statement":"select 1","pagination":{"page":"abc","pageSize":10}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", tc.body, token))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s should return 400, got %d; body=%s", tc.name, rec.Code, rec.Body.String())
			}
			if stub.executeCalled {
				t.Errorf("%s must not call Execute", tc.name)
			}
		})
	}
}

func TestQueryExecution_Execute_StrictPaginationDecoding_RejectsInvalidPageSize(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"zero_page_size", `{"statement":"select 1","pagination":{"page":1,"pageSize":0}}`},
		{"negative_page_size", `{"statement":"select 1","pagination":{"page":1,"pageSize":-1}}`},
		{"fractional_page_size", `{"statement":"select 1","pagination":{"page":1,"pageSize":1.5}}`},
		{"overflow_page_size", `{"statement":"select 1","pagination":{"page":1,"pageSize":999999999999}}`},
		{"string_page_size", `{"statement":"select 1","pagination":{"page":1,"pageSize":"abc"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", tc.body, token))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s should return 400, got %d; body=%s", tc.name, rec.Code, rec.Body.String())
			}
			if stub.executeCalled {
				t.Errorf("%s must not call Execute", tc.name)
			}
		})
	}
}

func TestQueryExecution_Execute_StrictPaginationDecoding_RejectsUnknownFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"unknown_top_level", `{"statement":"select 1","pagination":{"page":1,"pageSize":10},"bogus":true}`},
		{"unknown_in_pagination", `{"statement":"select 1","pagination":{"page":1,"pageSize":10,"bogus":true}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
			router := newQueryExecRouter(stub)
			token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute", tc.body, token))

			if rec.Code != http.StatusBadRequest {
				t.Errorf("unknown field must be rejected, got %d; body=%s", rec.Code, rec.Body.String())
			}
			if stub.executeCalled {
				t.Error("Execute must not be called when unknown fields are present")
			}
		})
	}
}

func TestQueryExecution_Execute_StrictPaginationDecoding_RejectsMissingPageWithPageSize(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute",
		`{"statement":"select 1","pagination":{"pageSize":10}}`, token))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("pageSize without page must return 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if stub.executeCalled {
		t.Error("Execute must not be called when page is missing from pagination")
	}
}

func TestQueryExecution_Execute_StrictPaginationDecoding_RejectsOverflowedOffset(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute",
		`{"statement":"select 1","pagination":{"page":9223372036854775807,"pageSize":2}}`, token))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("overflowed offset must return 400, got %d; body=%s", rec.Code, rec.Body.String())
	}
	if stub.executeCalled {
		t.Error("Execute must not be called when offset overflows")
	}
}

func TestQueryExecution_Execute_ResponseIncludesPaginationEnvelope(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{
		Status:   model.QueryExecutionSuccess,
		RowCount: 5,
	}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute",
		`{"statement":"select 1","pagination":{"page":1,"pageSize":10}}`, token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, field := range []string{`"page"`, `"pageSize"`, `"hasPreviousPage"`, `"hasNextPage"`} {
		if !strings.Contains(body, field) {
			t.Errorf("response JSON must include %s for pagination; body=%s", field, body)
		}
	}
}

func TestQueryExecution_Execute_PaginationNotIncludedWhenAbsent(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{
		Status:   model.QueryExecutionSuccess,
		RowCount: 1,
	}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute",
		`{"statement":"select 1"}`, token))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"pagination"`) {
		t.Errorf("response must not include pagination when request omits it; body=%s", body)
	}
}

func TestQueryExecution_Execute_MetadataStatementWithPaginationAccepted(t *testing.T) {
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{
		Status:   model.QueryExecutionSuccess,
		RowCount: 3,
	}}
	router := newQueryExecRouter(stub)
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, qeRequest(http.MethodPost, "/query-targets/22/execute",
		`{"statement":"show tables","pagination":{"page":1,"pageSize":10}}`, token))

	if rec.Code != http.StatusOK {
		t.Errorf("metadata statement with pagination should be accepted, got %d; body=%s", rec.Code, rec.Body.String())
	}
}
