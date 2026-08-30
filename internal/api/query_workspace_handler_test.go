// Package api provides tests for query-workspace HTTP handlers.
// input: context, encoding/json, net/http, net/http/httptest, strings, testing, internal/model, internal/service
// output: authenticated owner GET/PUT, strict JSON, size bound, validation, and OCC error-mapping tests
// pos: HTTP contract regression coverage for the singular query-workspace aggregate route
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type stubQueryWorkspaceService struct {
	workspace model.QueryWorkspace
	err       error
	ownerID   uint64
	put       model.QueryWorkspacePutRequest
}

func (s *stubQueryWorkspaceService) Get(_ context.Context, ownerUserID uint64) (model.QueryWorkspace, error) {
	s.ownerID = ownerUserID
	return s.workspace, s.err
}

func (s *stubQueryWorkspaceService) Put(_ context.Context, ownerUserID uint64, req model.QueryWorkspacePutRequest) (model.QueryWorkspace, error) {
	s.ownerID = ownerUserID
	s.put = req
	return s.workspace, s.err
}

func newQueryWorkspaceRouter(stub queryWorkspaceAPI) http.Handler {
	return NewRouter(Dependencies{
		AuthService:           service.NewAuthService(testAuthUsers, "qe-test-secret"),
		QueryWorkspaceService: stub,
		QueryExecutionAuth:    QueryExecutionAuthConfig{Clock: fixedClock(qeTestNow)},
	})
}

func TestQueryWorkspaceGetReturnsAuthenticatedOwnerAggregate(t *testing.T) {
	stub := &stubQueryWorkspaceService{workspace: model.QueryWorkspace{OwnerUserID: 42, Worksheets: []model.QueryWorkspaceWorksheet{}}}
	recorder := httptest.NewRecorder()
	token := mintToken(t, "qe-test-secret", 42, "admin", qeTestNow)

	newQueryWorkspaceRouter(stub).ServeHTTP(recorder, qeRequest(http.MethodGet, "/query-workspace", "", token))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var response model.QueryWorkspace
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if stub.ownerID != 42 || response.Version != 0 || response.Worksheets == nil || strings.Contains(recorder.Body.String(), "ownerUserId") {
		t.Fatalf("owner/response = %d/%+v; body=%s", stub.ownerID, response, recorder.Body.String())
	}
}

func TestQueryWorkspacePutReplacesAuthenticatedOwnerAggregate(t *testing.T) {
	database := "orders"
	wantWorksheet := model.QueryWorkspaceWorksheet{ID: "ws-1", Name: "Orders", TargetResourceID: 9, Statement: "not sql", ActiveDatabase: &database}
	stub := &stubQueryWorkspaceService{workspace: model.QueryWorkspace{OwnerUserID: 42, Version: 4, Worksheets: []model.QueryWorkspaceWorksheet{wantWorksheet}}}
	recorder := httptest.NewRecorder()
	token := mintToken(t, "qe-test-secret", 42, "viewer", qeTestNow)
	body := `{"expectedVersion":3,"worksheets":[{"id":"ws-1","name":"Orders","targetResourceId":9,"statement":"not sql","activeDatabase":"orders"}]}`

	newQueryWorkspaceRouter(stub).ServeHTTP(recorder, qeRequest(http.MethodPut, "/query-workspace", body, token))

	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	if stub.ownerID != 42 || stub.put.ExpectedVersion != 3 || len(stub.put.Worksheets) != 1 {
		t.Fatalf("owner/request = %d/%+v", stub.ownerID, stub.put)
	}
	got := stub.put.Worksheets[0]
	if got.ID != wantWorksheet.ID || got.Name != wantWorksheet.Name || got.TargetResourceID != wantWorksheet.TargetResourceID || got.Statement != wantWorksheet.Statement || got.ActiveDatabase == nil || *got.ActiveDatabase != database {
		t.Fatalf("worksheet = %+v, want %+v", got, wantWorksheet)
	}
}

func TestQueryWorkspacePutRejectsNonContractJSONBeforeService(t *testing.T) {
	token := mintToken(t, "qe-test-secret", 42, "viewer", qeTestNow)
	tests := []struct {
		name string
		body string
	}{
		{"missing expectedVersion", `{"worksheets":[]}`},
		{"missing worksheets", `{"expectedVersion":0}`},
		{"null worksheets", `{"expectedVersion":0,"worksheets":null}`},
		{"missing activeDatabase", `{"expectedVersion":0,"worksheets":[{"id":"ws-1","name":"One","targetResourceId":9,"statement":"select 1"}]}`},
		{"unknown top-level field", `{"expectedVersion":0,"worksheets":[],"ownerUserId":99}`},
		{"unknown worksheet field", `{"expectedVersion":0,"worksheets":[{"id":"ws-1","name":"One","targetResourceId":9,"statement":"select 1","activeDatabase":null,"resultRows":[]}]}`},
		{"duplicate nested field", `{"expectedVersion":0,"worksheets":[{"id":"ws-1","id":"ws-2","name":"One","targetResourceId":9,"statement":"select 1","activeDatabase":null}]}`},
		{"multiple values", `{"expectedVersion":0,"worksheets":[]} {}`},
		{"oversized body", `{"expectedVersion":0,"worksheets":[{"id":"ws-1","name":"One","targetResourceId":9,"statement":"` + strings.Repeat("private", model.MaxQueryWorkspaceJSONSize) + `","activeDatabase":null}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQueryWorkspaceService{}
			recorder := httptest.NewRecorder()
			newQueryWorkspaceRouter(stub).ServeHTTP(recorder, qeRequest(http.MethodPut, "/query-workspace", tc.body, token))
			if recorder.Code != http.StatusBadRequest || stub.ownerID != 0 {
				t.Fatalf("status/service = %d/%d, want 400/no call; body=%s", recorder.Code, stub.ownerID, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "privateprivate") {
				t.Fatalf("response leaked request content: %s", recorder.Body.String())
			}
		})
	}
}

func TestQueryWorkspacePutMapsOCCConflict(t *testing.T) {
	stub := &stubQueryWorkspaceService{err: service.ErrQueryWorkspaceConflict}
	recorder := httptest.NewRecorder()
	token := mintToken(t, "qe-test-secret", 42, "viewer", qeTestNow)

	newQueryWorkspaceRouter(stub).ServeHTTP(recorder, qeRequest(http.MethodPut, "/query-workspace", `{"expectedVersion":0,"worksheets":[]}`, token))

	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), `"error":"query_workspace_conflict"`) {
		t.Fatalf("conflict status/body = %d/%s", recorder.Code, recorder.Body.String())
	}
}
