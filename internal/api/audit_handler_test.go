// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestParseAuditListQuery*, TestListAuditEvents*, TestListResourceAuditEvents_Unchanged
// pos: Validates audit event listing with pagination, filtering, and search
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestParseAuditListQuery_NormalizesSearchQuery(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/audit-events?q=%20%20Admin%20%20", nil)

	query, err := parseAuditListQuery(req)
	if err != nil {
		t.Fatalf("parse audit query: %v", err)
	}
	if query.Query != "Admin" {
		t.Fatalf("query = %q, want trimmed Admin", query.Query)
	}
}

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
	if resp.PageInfo.HasNextPage {
		t.Fatalf("expected hasNextPage false on single page, got true")
	}
	if resp.PageInfo.HasPreviousPage {
		t.Fatalf("expected hasPreviousPage false on first page, got true")
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
}

func TestListAuditEvents_ReturnsFieldDiffContract(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?targetResourceId=1", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 || len(resp.Items[0].Changes) != 1 {
		t.Fatalf("changes = %#v, want one field diff", resp.Items)
	}
	change := resp.Items[0].Changes[0]
	if change.Field != "ownerId" || change.Operation != model.AuditChangeUpdate || change.Before != float64(1) || change.After != float64(2) {
		t.Fatalf("change = %#v, want ownerId 1 -> 2", change)
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
	if !resp.PageInfo.HasNextPage {
		t.Fatalf("expected hasNextPage true on first of 2 pages, got false")
	}
	if resp.PageInfo.HasPreviousPage {
		t.Fatalf("expected hasPreviousPage false on first page, got true")
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
	req := httptest.NewRequest(http.MethodGet, "/audit-events?targetResourceId=1", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 event for targetResourceId=1, got %d", len(resp.Items))
	}
	if resp.Items[0].TargetResourceID == nil || *resp.Items[0].TargetResourceID != 1 {
		t.Fatalf("expected targetResourceId 1, got %v", resp.Items[0].TargetResourceID)
	}
}

func TestListAuditEvents_RejectsInvalidTargetResourceId(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?targetResourceId=abc", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp apiErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "validation_failed" {
		t.Fatalf("expected validation_failed, got %s", resp.Error)
	}
}

func TestListAuditEvents_RejectsEmptyTargetResourceId(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events?targetResourceId=", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp apiErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "validation_failed" {
		t.Fatalf("expected validation_failed, got %s", resp.Error)
	}
}

func TestListAuditEvents_NullTargetResourceIDRendersAsJSONNull(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/audit-events", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Items []struct {
			ID               uint64  `json:"id"`
			ActorUserID      *uint64 `json:"actorUserId"`
			TargetResourceID *uint64 `json:"targetResourceId"`
		} `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[1].TargetResourceID != nil {
		t.Fatalf("expected second event targetResourceId to be null, got %v", *resp.Items[1].TargetResourceID)
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
	if resp.PageInfo.HasNextPage {
		t.Fatalf("expected hasNextPage false on empty result, got true")
	}
	if resp.PageInfo.HasPreviousPage {
		t.Fatalf("expected hasPreviousPage false on empty result, got true")
	}
}

func TestListResourceAuditEvents_Unchanged(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/1/audit-events", nil)
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

func TestListResourceAuditEvents_RejectsZeroID(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/0/audit-events", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp apiErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Message != "resource id must be a positive integer" {
		t.Fatalf("expected positive integer message, got %q", resp.Message)
	}
}

// --- Phase 12.5: Multi-select audit filter tests ---

func TestListAuditEvents_MultiSelectEventType(t *testing.T) {
	server := NewTestServer()

	// Both audit events: one resource.created, one resource.updated
	req := httptest.NewRequest(http.MethodGet, "/audit-events?eventType=resource.created&eventType=resource.updated", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (both event types), got %d", len(resp.Items))
	}
}

func TestListAuditEvents_MultiSelectResult(t *testing.T) {
	server := NewTestServer()

	// One success, one failure
	req := httptest.NewRequest(http.MethodGet, "/audit-events?result=success&result=failure", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (success + failure), got %d", len(resp.Items))
	}
}

func TestListAuditEvents_MultiSelectANDCombination(t *testing.T) {
	server := NewTestServer()

	// eventType=resource.created AND result=success => only the first audit event
	req := httptest.NewRequest(http.MethodGet, "/audit-events?eventType=resource.created&eventType=resource.updated&result=success", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (resource.updated + success), got %d", len(resp.Items))
	}
	if len(resp.Items) > 0 && resp.Items[0].EventType != "resource.updated" {
		t.Errorf("expected resource.updated, got %s", resp.Items[0].EventType)
	}
}

func TestListAuditEvents_MultiSelectDeduplicates(t *testing.T) {
	server := NewTestServer()

	// Same eventType repeated should not produce duplicates
	req := httptest.NewRequest(http.MethodGet, "/audit-events?eventType=resource.created&eventType=resource.created", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedAuditResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (deduped), got %d", len(resp.Items))
	}
}
