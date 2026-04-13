// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestListResources*, TestGetResourceProfile_*
// pos: Validates resource listing with pagination/filtering and per-type profile responses
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type paginatedResourceResponse struct {
	Items    []model.Resource `json:"items"`
	PageInfo *model.PageInfo  `json:"pageInfo"`
}

func TestListResources(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceType=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" {
		t.Fatal("expected response body")
	}
}

func TestListResources_DefaultPagination(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp paginatedResourceResponse
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

func TestListResources_CustomPage(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?page=1&pageSize=1", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.PageInfo.Page != 1 {
		t.Fatalf("expected page 1, got %d", resp.PageInfo.Page)
	}
	if resp.PageInfo.PageSize != 1 {
		t.Fatalf("expected pageSize 1, got %d", resp.PageInfo.PageSize)
	}
	if resp.PageInfo.TotalItems != 2 {
		t.Fatalf("expected totalItems 2, got %d", resp.PageInfo.TotalItems)
	}
	if resp.PageInfo.TotalPages != 2 {
		t.Fatalf("expected totalPages 2, got %d", resp.PageInfo.TotalPages)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item on first page, got %d", len(resp.Items))
	}
}

func TestListResources_PageBeyondData(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?page=5", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 items on page 5, got %d", len(resp.Items))
	}
	if resp.PageInfo.TotalItems != 2 {
		t.Fatalf("expected totalItems 2, got %d", resp.PageInfo.TotalItems)
	}
}

func TestListResources_PageSizeCap(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?pageSize=500", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.PageInfo.PageSize != 100 {
		t.Fatalf("expected pageSize capped to 100, got %d", resp.PageInfo.PageSize)
	}
}

func TestListResources_FilterByResourceType(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceType=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 database_instance, got %d", len(resp.Items))
	}
	if resp.Items[0].ResourceType != model.ResourceTypeDatabaseInstance {
		t.Fatalf("expected database_instance, got %s", resp.Items[0].ResourceType)
	}
}

func TestListResources_FilterByLifecycleStatus(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?lifecycleStatus=degraded", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 degraded resource, got %d", len(resp.Items))
	}
	if resp.Items[0].LifecycleStatus != "degraded" {
		t.Fatalf("expected lifecycleStatus degraded, got %s", resp.Items[0].LifecycleStatus)
	}
}

func TestListResources_FilterByHealthStatus(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?healthStatus=warning", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 warning resource, got %d", len(resp.Items))
	}
	if resp.Items[0].HealthStatus != "warning" {
		t.Fatalf("expected healthStatus warning, got %s", resp.Items[0].HealthStatus)
	}
}

func TestListResources_SearchQuery(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?q=mysql", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 result for 'mysql', got %d", len(resp.Items))
	}
	if resp.Items[0].Name != "order-mysql-prod" {
		t.Fatalf("expected order-mysql-prod, got %s", resp.Items[0].Name)
	}
}

func TestListResources_SearchQueryNoMatch(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?q=nonexistent", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 results for 'nonexistent', got %d", len(resp.Items))
	}
}

func TestListResources_ResponseBackwardCompat(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode raw: %v", err)
	}

	if _, ok := raw["items"]; !ok {
		t.Fatal("expected 'items' key in response")
	}
	if _, ok := raw["pageInfo"]; !ok {
		t.Fatal("expected 'pageInfo' key in response")
	}
}

func TestGetResourceProfile_DatabaseInstance(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-db-instance/profile", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.ResourceProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ResourceType != model.ResourceTypeDatabaseInstance {
		t.Fatalf("expected resourceType database_instance, got %s", resp.ResourceType)
	}

	checkProfileField(t, resp.Profile, "engine", "mysql")
	checkProfileField(t, resp.Profile, "version", "8.0.36")
	checkProfileField(t, resp.Profile, "host", "prod-db-host-01.internal")
	checkProfileField(t, resp.Profile, "role", "primary")
	if port, ok := resp.Profile["port"]; !ok {
		t.Fatal("missing profile field: port")
	} else if port != float64(3306) {
		t.Fatalf("expected port 3306, got %v", port)
	}
}

func TestGetResourceProfile_DatabaseCluster(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-db-cluster/profile", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.ResourceProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ResourceType != model.ResourceTypeDatabaseCluster {
		t.Fatalf("expected resourceType database_cluster, got %s", resp.ResourceType)
	}

	checkProfileField(t, resp.Profile, "engine", "mysql")
	checkProfileField(t, resp.Profile, "topologyMode", "primary-replica")
	checkProfileField(t, resp.Profile, "primaryEndpoint", "order-mysql-cluster-prod.internal:3306")
}

func TestGetResourceProfile_Service(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-service/profile", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.ResourceProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ResourceType != model.ResourceTypeService {
		t.Fatalf("expected resourceType service, got %s", resp.ResourceType)
	}

	checkProfileField(t, resp.Profile, "systemName", "order-api")
	checkProfileField(t, resp.Profile, "repositoryUrl", "https://example.com/repos/order-api")
	checkProfileField(t, resp.Profile, "runtimeEnv", "kubernetes")
}

func TestGetResourceProfile_Host(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-host/profile", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.ResourceProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.ResourceType != model.ResourceTypeHost {
		t.Fatalf("expected resourceType host, got %s", resp.ResourceType)
	}

	checkProfileField(t, resp.Profile, "hostname", "prod-db-host-01.internal")
	checkProfileField(t, resp.Profile, "ipAddress", "10.0.10.21")
	checkProfileField(t, resp.Profile, "osName", "Ubuntu 24.04")
}

func TestGetResourceProfile_EmptyProfile(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-no-profile/profile", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.ResourceProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Profile) != 0 {
		t.Fatalf("expected empty profile, got %v", resp.Profile)
	}
}

func TestGetResourceProfile_NotFound(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/nonexistent-id/profile", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

func checkProfileField(t *testing.T, profile map[string]any, key, expected string) {
	t.Helper()
	val, ok := profile[key]
	if !ok {
		t.Fatalf("missing profile field: %s", key)
	}
	if val != expected {
		t.Fatalf("profile[%s]: expected %q, got %v", key, expected, val)
	}
}
