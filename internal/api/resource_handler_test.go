// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestListResources*, TestGetResourceProfile_*
// pos: Validates resource listing with pagination/filtering and per-type profile responses
// note: if this file changes, update header and README.md
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type paginatedResourceResponse struct {
	Items    []model.Resource `json:"items"`
	PageInfo *model.PageInfo  `json:"pageInfo"`
}

type apiErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func TestCreateResource(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"resourceSubtype":"mysql",
		"name":"order-mysql-02-prod",
		"displayName":"Order MySQL 02 Prod",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"externalId":"order-mysql-02-prod",
		"labels":{"team":"order","tier":"data"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.Resource
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("expected generated resource id")
	}
	if resp.Name != "order-mysql-02-prod" {
		t.Fatalf("expected created resource name, got %s", resp.Name)
	}
	if resp.Labels["team"] != "order" {
		t.Fatalf("expected team label order, got %q", resp.Labels["team"])
	}
}

func TestCreateResourceRejectsUnsupportedResourceType(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"unsupported",
		"name":"bad-resource",
		"displayName":"Bad Resource",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRejectsUnsupportedLifecycleStatus(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"bad-lifecycle",
		"displayName":"Bad Lifecycle",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"paused",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRejectsUnsupportedHealthStatus(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"bad-health",
		"displayName":"Bad Health",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"offline",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRejectsMissingEnvironment(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"resourceSubtype":"mysql",
		"name":"missing-env",
		"displayName":"Missing Env",
		"environmentId":"missing-env",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "environment_not_found")
}

func TestCreateResourceRejectsMissingOwner(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"resourceSubtype":"mysql",
		"name":"missing-owner",
		"displayName":"Missing Owner",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"missing-owner",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "owner_not_found")
}

func TestCreateResourceRejectsDuplicateNameWithinEnvironment(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"resourceSubtype":"mysql",
		"name":"order-mysql-prod",
		"displayName":"Order MySQL Duplicate",
		"environmentId":"env-prod",
		"ownerId":"owner-dba",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusConflict, "resource_conflict")
}

func TestCreateResourceRejectsScalarLabels(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"bad-labels",
		"displayName":"Bad Labels",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":"team=order"
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "malformed_json")
}

func TestCreateResourceRejectsMalformedJSON(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(`{"resourceType":`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "malformed_json")
}

func TestCreateResourceRejectsEmptyName(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"",
		"displayName":"Empty Name",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRejectsEmptyDisplayName(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"valid-name",
		"displayName":"",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRejectsInvalidSource(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"valid-name",
		"displayName":"Valid Name",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"auto",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRejectsInvalidName(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"name":"INVALID",
		"displayName":"Invalid Name",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestPatchResource(t *testing.T) {
	server := NewTestServer()
	body := `{
		"displayName":"Order MySQL Primary Prod",
		"healthStatus":"warning",
		"labels":{"team":"order","tier":"data","pci":"false"}
	}`
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.Resource
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.DisplayName != "Order MySQL Primary Prod" {
		t.Fatalf("expected updated displayName, got %s", resp.DisplayName)
	}
	if resp.HealthStatus != "warning" {
		t.Fatalf("expected warning healthStatus, got %s", resp.HealthStatus)
	}
	if resp.Labels["pci"] != "false" {
		t.Fatalf("expected pci label false, got %q", resp.Labels["pci"])
	}
}

func TestPatchResourceRejectsImmutableFields(t *testing.T) {
	server := NewTestServer()
	body := `{"resourceType":"host"}`
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestPatchResourceRejectsEmptyDisplayName(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"displayName":""}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestPatchResourceRejectsInvalidSource(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"source":"auto"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestPatchResourceRejectsNoMutableFields(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestPatchResourceRejectsMalformedJSON(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"displayName":`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "malformed_json")
}

func TestPatchResourceRejectsMissingEnvironment(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"environmentId":"missing-env"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "environment_not_found")
}

func TestPatchResourceRejectsMissingOwner(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"ownerId":"missing-owner"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "owner_not_found")
}

func TestPatchResourceRejectsUnsupportedHealthStatus(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"healthStatus":"offline"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestPatchResourceRejectsNotFound(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/missing-resource", strings.NewReader(`{"displayName":"Missing"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "resource_not_found")
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

func TestListResources_SearchQueryMatchesLabels(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?q=platform", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(resp.Items) == 0 {
		t.Fatal("expected at least 1 result matching label value 'platform'")
	}
	for _, item := range resp.Items {
		if item.Labels["team"] != "platform" && !strings.Contains(item.Name, "platform") && !strings.Contains(item.DisplayName, "platform") && !strings.Contains(item.ExternalID, "platform") {
			t.Fatalf("result %s does not match 'platform' in name, display_name, external_id, or labels", item.Name)
		}
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

func assertAPIError(t *testing.T, rec *httptest.ResponseRecorder, expectedStatus int, expectedError string) {
	t.Helper()
	if rec.Code != expectedStatus {
		t.Fatalf("expected status %d, got %d; body: %s", expectedStatus, rec.Code, rec.Body.String())
	}

	var resp apiErrorResponse
	if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Error != expectedError {
		t.Fatalf("expected error %q, got %q", expectedError, resp.Error)
	}
	if resp.Message == "" {
		t.Fatal("expected non-empty error message")
	}
}

// --- Archive handler tests ---

func TestArchiveResource_ValidResource(t *testing.T) {
	server := NewTestServer()
	body := `{"reason":"e2e cleanup"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.Resource
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ArchivedAt == nil {
		t.Fatal("expected archivedAt to be set")
	}
	if resp.ArchiveReason == nil || *resp.ArchiveReason != "e2e cleanup" {
		t.Fatalf("expected archiveReason 'e2e cleanup', got %v", resp.ArchiveReason)
	}
}

func TestArchiveResource_NotFound(t *testing.T) {
	server := NewTestServer()
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/resources/nonexistent/archive", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestArchiveResource_Idempotent(t *testing.T) {
	server := NewTestServer()
	body := `{"reason":"retired"}`

	// First archive
	req1 := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(body))
	rec1 := httptest.NewRecorder()
	server.Router.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first archive: expected 200, got %d", rec1.Code)
	}

	var first model.Resource
	json.NewDecoder(rec1.Body).Decode(&first)

	// Second archive — should be idempotent
	req2 := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	server.Router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second archive: expected 200, got %d", rec2.Code)
	}

	var second model.Resource
	json.NewDecoder(rec2.Body).Decode(&second)
	if !first.ArchivedAt.Equal(*second.ArchivedAt) {
		t.Fatal("expected idempotent archive to return original archivedAt")
	}
}

func TestArchiveResource_BlankReason(t *testing.T) {
	server := NewTestServer()
	body := `{"reason":"   "}`
	req := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp apiErrorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "validation_failed" {
		t.Fatalf("expected validation_failed, got %s", resp.Error)
	}
}

func TestArchiveResource_NoReason(t *testing.T) {
	server := NewTestServer()
	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
}

// --- Archive + list behavior ---

func TestListResources_ExcludesArchived(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, item := range resp.Items {
		if item.ArchivedAt != nil {
			t.Fatalf("default list should not include archived resources, got %s", item.ID)
		}
	}
}

func TestListResources_IncludeArchived(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?includeArchived=true", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	hasArchived := false
	for _, item := range resp.Items {
		if item.ID == "res-archived" {
			hasArchived = true
			if item.ArchivedAt == nil {
				t.Fatal("expected archivedAt on res-archived")
			}
		}
	}
	if !hasArchived {
		t.Fatal("expected res-archived in results when includeArchived=true")
	}
}

func TestListResources_IncludeArchivedPagination(t *testing.T) {
	server := NewTestServer()

	// Without includeArchived — total 2
	req1 := httptest.NewRequest(http.MethodGet, "/resources", nil)
	rec1 := httptest.NewRecorder()
	server.Router.ServeHTTP(rec1, req1)
	var resp1 paginatedResourceResponse
	json.NewDecoder(rec1.Body).Decode(&resp1)

	// With includeArchived — total 3
	req2 := httptest.NewRequest(http.MethodGet, "/resources?includeArchived=true", nil)
	rec2 := httptest.NewRecorder()
	server.Router.ServeHTTP(rec2, req2)
	var resp2 paginatedResourceResponse
	json.NewDecoder(rec2.Body).Decode(&resp2)

	if resp1.PageInfo.TotalItems >= resp2.PageInfo.TotalItems {
		t.Fatalf("includeArchived should have more items: %d vs %d", resp1.PageInfo.TotalItems, resp2.PageInfo.TotalItems)
	}
}

// --- Archive + detail ---

func TestGetResource_ReturnsArchivedResource(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-archived", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp model.Resource
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ID != "res-archived" {
		t.Fatalf("expected res-archived, got %s", resp.ID)
	}
	if resp.ArchivedAt == nil {
		t.Fatal("expected archivedAt to be set")
	}
}

// --- Archive + mutation rejection ---

func TestPatchResource_RejectsArchived(t *testing.T) {
	server := NewTestServer()
	body := `{"displayName":"New Name"}`
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-archived", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp apiErrorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "resource_archived" {
		t.Fatalf("expected resource_archived, got %s", resp.Error)
	}
}

func TestCreateRelation_RejectsArchivedSource(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":"res-2","relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/res-archived/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp apiErrorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "resource_archived" {
		t.Fatalf("expected resource_archived, got %s", resp.Error)
	}
}

func TestCreateRelation_RejectsArchivedTarget(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":"res-archived","relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/res-1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp apiErrorResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Error != "resource_archived" {
		t.Fatalf("expected resource_archived, got %s", resp.Error)
	}
}

// --- Archive + reads remain available ---

func TestListRelations_ArchivedResourceReadable(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-archived/relations", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestTopology_ArchivedResourceReadable(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-archived/topology", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// --- Phase 12.2: archivedOnly filter ---

func TestListResources_ArchivedOnly(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?archivedOnly=true", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 archived item, got %d", len(resp.Items))
	}
	if resp.Items[0].ID != "res-archived" {
		t.Fatalf("expected res-archived, got %s", resp.Items[0].ID)
	}
	if resp.Items[0].ArchivedAt == nil {
		t.Fatal("expected archivedAt to be set")
	}
	if resp.PageInfo.TotalItems != 1 {
		t.Fatalf("expected totalItems 1, got %d", resp.PageInfo.TotalItems)
	}
}

func TestListResources_ArchivedOnlyPagination(t *testing.T) {
	server := NewTestServer()

	// archivedOnly=true should have total 1
	req := httptest.NewRequest(http.MethodGet, "/resources?archivedOnly=true", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.PageInfo.TotalItems != 1 {
		t.Fatalf("archivedOnly: expected totalItems 1, got %d", resp.PageInfo.TotalItems)
	}
	if resp.PageInfo.TotalPages != 1 {
		t.Fatalf("archivedOnly: expected totalPages 1, got %d", resp.PageInfo.TotalPages)
	}
}

func TestListResources_ArchivedOnlyTakesPrecedenceOverIncludeArchived(t *testing.T) {
	server := NewTestServer()

	// archivedOnly=true + includeArchived=true should still return only archived
	req := httptest.NewRequest(http.MethodGet, "/resources?archivedOnly=true&includeArchived=true", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Items) != 1 {
		t.Fatalf("archivedOnly should take precedence: expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].ID != "res-archived" {
		t.Fatalf("expected res-archived, got %s", resp.Items[0].ID)
	}
}

func TestListResources_ArchivedOnlyWithOtherFilters(t *testing.T) {
	server := NewTestServer()

	// archivedOnly + resourceType filter
	req := httptest.NewRequest(http.MethodGet, "/resources?archivedOnly=true&resourceType=host", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	var resp paginatedResourceResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 archived host, got %d", len(resp.Items))
	}
}

// --- Phase 12.2: Unarchive endpoint ---

func TestUnarchiveResource_ArchivedResource(t *testing.T) {
	server := NewTestServer()

	// First archive res-1
	archiveBody := `{"reason":"temp"}`
	archiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(archiveBody))
	archiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(archiveRec, archiveReq)
	if archiveRec.Code != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d", archiveRec.Code)
	}

	// Now unarchive it
	unarchiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-1/unarchive", nil)
	unarchiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(unarchiveRec, unarchiveReq)

	if unarchiveRec.Code != http.StatusOK {
		t.Fatalf("unarchive: expected 200, got %d; body: %s", unarchiveRec.Code, unarchiveRec.Body.String())
	}

	var resp model.Resource
	if err := json.NewDecoder(unarchiveRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ArchivedAt != nil {
		t.Fatal("expected archivedAt to be nil after unarchive")
	}
	if resp.ArchiveReason != nil {
		t.Fatal("expected archiveReason to be nil after unarchive")
	}
	if resp.ArchivedBy != nil {
		t.Fatal("expected archivedBy to be nil after unarchive")
	}
}

func TestUnarchiveResource_NotFound(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPost, "/resources/nonexistent/unarchive", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "resource_not_found")
}

func TestUnarchiveResource_IdempotentForActive(t *testing.T) {
	server := NewTestServer()

	// res-1 is active — unarchive should be idempotent
	req := httptest.NewRequest(http.MethodPost, "/resources/res-1/unarchive", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.Resource
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ArchivedAt != nil {
		t.Fatal("expected archivedAt to remain nil for active resource")
	}
}

func TestUnarchivedResource_ReappearsInDefaultList(t *testing.T) {
	server := NewTestServer()

	// Archive res-1
	archiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(`{}`))
	archiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(archiveRec, archiveReq)

	// Confirm hidden from default list
	listRec1 := httptest.NewRecorder()
	server.Router.ServeHTTP(listRec1, httptest.NewRequest(http.MethodGet, "/resources", nil))
	var list1 paginatedResourceResponse
	json.NewDecoder(listRec1.Body).Decode(&list1)
	if list1.PageInfo.TotalItems != 1 {
		t.Fatalf("after archive: expected 1 item, got %d", list1.PageInfo.TotalItems)
	}

	// Unarchive
	unarchiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-1/unarchive", nil)
	unarchiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(unarchiveRec, unarchiveReq)
	if unarchiveRec.Code != http.StatusOK {
		t.Fatalf("unarchive: expected 200, got %d", unarchiveRec.Code)
	}

	// Confirm back in default list
	listReq2 := httptest.NewRequest(http.MethodGet, "/resources", nil)
	listRec2 := httptest.NewRecorder()
	server.Router.ServeHTTP(listRec2, listReq2)
	var list2 paginatedResourceResponse
	json.NewDecoder(listRec2.Body).Decode(&list2)
	if list2.PageInfo.TotalItems != 2 {
		t.Fatalf("after unarchive: expected 2 items, got %d", list2.PageInfo.TotalItems)
	}
}

func TestPatchOnArchived_ThenSucceedsAfterUnarchive(t *testing.T) {
	server := NewTestServer()

	// Archive res-1
	archiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-1/archive", strings.NewReader(`{}`))
	archiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(archiveRec, archiveReq)

	// Patch should fail
	patchBody := `{"displayName":"Should Fail"}`
	patchReq1 := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(patchBody))
	patchRec1 := httptest.NewRecorder()
	server.Router.ServeHTTP(patchRec1, patchReq1)
	assertAPIError(t, patchRec1, http.StatusConflict, "resource_archived")

	// Unarchive
	unarchiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-1/unarchive", nil)
	unarchiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(unarchiveRec, unarchiveReq)

	// Patch should now succeed
	patchReq2 := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(patchBody))
	patchRec2 := httptest.NewRecorder()
	server.Router.ServeHTTP(patchRec2, patchReq2)
	if patchRec2.Code != http.StatusOK {
		t.Fatalf("expected patch to succeed after unarchive, got %d; body: %s", patchRec2.Code, patchRec2.Body.String())
	}
}

func TestRelationCreateOnArchived_ThenSucceedsAfterUnarchive(t *testing.T) {
	server := NewTestServer()

	// Archive res-2
	archiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-2/archive", strings.NewReader(`{}`))
	archiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(archiveRec, archiveReq)

	// Relation creation should fail
	relBody := `{"toResourceId":"res-1","relationType":"depends_on"}`
	relReq1 := httptest.NewRequest(http.MethodPost, "/resources/res-2/relations", strings.NewReader(relBody))
	relRec1 := httptest.NewRecorder()
	server.Router.ServeHTTP(relRec1, relReq1)
	assertAPIError(t, relRec1, http.StatusConflict, "resource_archived")

	// Unarchive
	unarchiveReq := httptest.NewRequest(http.MethodPost, "/resources/res-2/unarchive", nil)
	unarchiveRec := httptest.NewRecorder()
	server.Router.ServeHTTP(unarchiveRec, unarchiveReq)

	// Relation creation should now succeed
	relReq2 := httptest.NewRequest(http.MethodPost, "/resources/res-2/relations", strings.NewReader(relBody))
	relRec2 := httptest.NewRecorder()
	server.Router.ServeHTTP(relRec2, relReq2)
	if relRec2.Code != http.StatusCreated {
		t.Fatalf("expected relation create to succeed after unarchive, got %d; body: %s", relRec2.Code, relRec2.Body.String())
	}
}

// --- Phase 12.5: Multi-select filter tests ---

func TestListResources_MultiSelectResourceType(t *testing.T) {
	server := NewTestServer()

	// res-1 = database_instance, res-2 = host
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceType=database_instance&resourceType=host", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (both types), got %d", len(resp.Items))
	}
}

func TestListResources_MultiSelectEnvironmentID(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources?environmentId=env-prod&environmentId=env-staging", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (both envs), got %d", len(resp.Items))
	}
}

func TestListResources_MultiSelectLifecycleStatus(t *testing.T) {
	server := NewTestServer()

	// res-1 = running, res-2 = degraded
	req := httptest.NewRequest(http.MethodGet, "/resources?lifecycleStatus=running&lifecycleStatus=degraded", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (running + degraded), got %d", len(resp.Items))
	}
}

func TestListResources_MultiSelectHealthStatus(t *testing.T) {
	server := NewTestServer()

	// res-1 = healthy, res-2 = warning
	req := httptest.NewRequest(http.MethodGet, "/resources?healthStatus=healthy&healthStatus=warning", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (healthy + warning), got %d", len(resp.Items))
	}
}

func TestListResources_MultiSelectANDCombination(t *testing.T) {
	server := NewTestServer()

	// type=database_instance AND env=env-prod => only res-1
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceType=database_instance&resourceType=host&environmentId=env-prod", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (database_instance in env-prod), got %d", len(resp.Items))
	}
	if len(resp.Items) > 0 && resp.Items[0].ResourceType != "database_instance" {
		t.Errorf("expected database_instance, got %s", resp.Items[0].ResourceType)
	}
}

func TestListResources_MultiSelectWithSearch(t *testing.T) {
	server := NewTestServer()

	// search + type filter combined
	req := httptest.NewRequest(http.MethodGet, "/resources?q=mysql&resourceType=database_instance&resourceType=host", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Only res-1 (mysql-primary-01) matches 'mysql' and is database_instance
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (mysql + database_instance), got %d", len(resp.Items))
	}
}

func TestListResources_MultiSelectDeduplicates(t *testing.T) {
	server := NewTestServer()

	// Same type repeated — should still return only matching resources without duplicates
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceType=database_instance&resourceType=database_instance&resourceType=database_instance", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (deduped database_instance), got %d", len(resp.Items))
	}
}

// --- Phase 12.7: resourceSubtype filter tests ---

func TestListResources_FilterByResourceSubtype(t *testing.T) {
	server := NewTestServer()

	// res-1 = mysql, res-2 = vm
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceSubtype=mysql", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 mysql resource, got %d", len(resp.Items))
	}
	if resp.Items[0].ResourceSubtype != "mysql" {
		t.Fatalf("expected resourceSubtype mysql, got %s", resp.Items[0].ResourceSubtype)
	}
}

func TestListResources_MultiSelectResourceSubtype(t *testing.T) {
	server := NewTestServer()

	// res-1 = mysql, res-2 = vm — OR combination
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceSubtype=mysql&resourceSubtype=vm", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Errorf("expected 2 items (mysql + vm), got %d", len(resp.Items))
	}
}

func TestListResources_ResourceSubtypeDeduplicates(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources?resourceSubtype=mysql&resourceSubtype=mysql&resourceSubtype=mysql", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (deduped mysql), got %d", len(resp.Items))
	}
}

func TestListResources_ResourceSubtypeWithEnvironmentID(t *testing.T) {
	server := NewTestServer()

	// mysql + env-prod — AND combination
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceSubtype=mysql&environmentId=env-prod", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (mysql in env-prod), got %d", len(resp.Items))
	}
	if len(resp.Items) > 0 {
		if resp.Items[0].ResourceSubtype != "mysql" {
			t.Errorf("expected mysql, got %s", resp.Items[0].ResourceSubtype)
		}
		if resp.Items[0].EnvironmentID != "env-prod" {
			t.Errorf("expected env-prod, got %s", resp.Items[0].EnvironmentID)
		}
	}
}

func TestListResources_ResourceSubtypeWithResourceType(t *testing.T) {
	server := NewTestServer()

	// subtype=mysql AND type=database_instance
	req := httptest.NewRequest(http.MethodGet, "/resources?resourceSubtype=mysql&resourceType=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (mysql database_instance), got %d", len(resp.Items))
	}
}

func TestListResources_ResourceSubtypeWithSearch(t *testing.T) {
	server := NewTestServer()

	// q=mysql AND subtype=mysql
	req := httptest.NewRequest(http.MethodGet, "/resources?q=mysql&resourceSubtype=mysql", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Errorf("expected 1 item (name matches mysql AND subtype=mysql), got %d", len(resp.Items))
	}
}

func TestListResources_ResourceSubtypeNoMatch(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources?resourceSubtype=postgres", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp paginatedResourceResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items for non-existent subtype, got %d", len(resp.Items))
	}
}
