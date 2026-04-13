// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestListAuditEvents*, TestListResourceAuditEvents_Unchanged
// pos: Validates audit event listing with pagination and filtering
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type paginatedAuditResponse struct {
	Items    []model.AuditEvent `json:"items"`
	PageInfo *model.PageInfo    `json:"pageInfo"`
}

func TestListAuditEvents_DefaultPagination(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.PageInfo == nil {
		t.Fatal("expected pageInfo in response")
	}
	if resp.PageInfo.Page != 1 {
		t.Fatalf("expected page 1, got %d", resp.PageInfo.Page)
	}
	if resp.PageInfo.PageSize != 20 {
		t.Fatalf("expected pageSize 20, got %d", resp.PageInfo.PageSize)
	}
	if resp.PageInfo.TotalItems != 2 {
		t.Fatalf("expected totalItems 2, got %d", resp.PageInfo.TotalItems)
	}
	if resp.PageInfo.TotalPages != 1 {
		t.Fatalf("expected totalPages 1, got %d", resp.PageInfo.TotalPages)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
}

func TestListAuditEvents_CustomPagination(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?page=1&pageSize=1", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item on first page, got %d", len(resp.Items))
	}
	if resp.PageInfo.TotalPages != 2 {
		t.Fatalf("expected totalPages 2, got %d", resp.PageInfo.TotalPages)
	}
}

func TestListAuditEvents_FilterByEventType(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?eventType=resource.updated", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 resource.updated event, got %d", len(resp.Items))
	}
	if resp.Items[0].EventType != "resource.updated" {
		t.Fatalf("expected eventType resource.updated, got %s", resp.Items[0].EventType)
	}
}

func TestListAuditEvents_FilterByResult(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?result=success", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 success event, got %d", len(resp.Items))
	}
	if resp.Items[0].Result != "success" {
		t.Fatalf("expected result success, got %s", resp.Items[0].Result)
	}
}

func TestListAuditEvents_FilterByTargetResourceId(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?targetResourceId=res-1", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 event for res-1, got %d", len(resp.Items))
	}
	if resp.Items[0].TargetResourceID != "res-1" {
		t.Fatalf("expected targetResourceId res-1, got %s", resp.Items[0].TargetResourceID)
	}
}

func TestListAuditEvents_NoMatch(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?result=unknown", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 events, got %d", len(resp.Items))
	}
	if resp.PageInfo.TotalItems != 0 {
		t.Fatalf("expected totalItems 0, got %d", resp.PageInfo.TotalItems)
	}
}

func TestListResourceAuditEvents_Unchanged(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-1/audit-events", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Nested route returns { "items": [...] } without pageInfo
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}
	if _, ok := raw["items"]; !ok {
		t.Fatal("expected 'items' key in nested audit response")
	}
	if _, ok := raw["pageInfo"]; ok {
		t.Fatal("nested audit route should not have pageInfo")
	}
}
