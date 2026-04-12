package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestListResources(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?type=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" {
		t.Fatal("expected response body")
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
