// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestListEnvironments/Owners/Roles/ResourceTypes/RelationTypes/LifecycleStatuses/HealthStatuses/ResourceSubtypes including service worker
// pos: Validates all dictionary endpoints return correct items, including Domain Name dns, Virtual IP floating, and service worker
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestListEnvironments(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/environments", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.Environment `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 2 {
		t.Fatalf("expected 2 environments, got %d", len(body.Items))
	}

	env := body.Items[0]
	if env.Name != "Production" {
		t.Fatalf("expected first environment name 'Production', got %s", env.Name)
	}
	if env.Slug != "prod" {
		t.Fatalf("expected slug 'prod', got %s", env.Slug)
	}
	if env.Description != "Production environment" {
		t.Fatalf("expected description 'Production environment', got %s", env.Description)
	}
	if env.CreatedAt.IsZero() {
		t.Fatal("expected createdAt to be set")
	}
}

func TestListOwners(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/owners", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.Owner `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 2 {
		t.Fatalf("expected 2 owners, got %d", len(body.Items))
	}

	owner := body.Items[0]
	if owner.Name != "Platform Team" {
		t.Fatalf("expected first owner name 'Platform Team', got %s", owner.Name)
	}
	if owner.Email != "platform@example.com" {
		t.Fatalf("expected email 'platform@example.com', got %s", owner.Email)
	}
	if owner.CreatedAt.IsZero() {
		t.Fatal("expected createdAt to be set")
	}
}

func TestListRoles(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/roles", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.Role `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 2 {
		t.Fatalf("expected 2 roles, got %d", len(body.Items))
	}

	role := body.Items[0]
	if role.Name != "admin" {
		t.Fatalf("expected first role name 'admin', got %s", role.Name)
	}
	if role.Description != "Full platform access" {
		t.Fatalf("expected description 'Full platform access', got %s", role.Description)
	}
	if role.CreatedAt.IsZero() {
		t.Fatal("expected createdAt to be set")
	}
}

func TestListEnvironmentsResponseShape(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/environments", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Fatalf("expected content-type application/json, got %s", contentType)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode as envelope: %v", err)
	}

	if _, ok := raw["items"]; !ok {
		t.Fatal("expected 'items' key in response")
	}
}

func TestListResourceTypes(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resource-types", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.DictionaryItem `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 8 {
		t.Fatalf("expected 8 resource types, got %d", len(body.Items))
	}

	item := body.Items[0]
	if item.Key != string(model.ResourceTypeHost) {
		t.Fatalf("expected first resource type key host, got %s", item.Key)
	}
	if item.Label == "" {
		t.Fatal("expected label to be set")
	}
	if item.Description == "" {
		t.Fatal("expected description to be set")
	}
}

func TestListRelationTypes(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/relation-types", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.DictionaryItem `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 7 {
		t.Fatalf("expected 7 relation types, got %d", len(body.Items))
	}

	item := body.Items[0]
	if item.Key != string(model.RelationTypeDependsOn) {
		t.Fatalf("expected first relation type key depends_on, got %s", item.Key)
	}
	if item.Label == "" {
		t.Fatal("expected label to be set")
	}
	if item.Description == "" {
		t.Fatal("expected description to be set")
	}
}

func TestListLifecycleStatuses(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/lifecycle-statuses", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.DictionaryItem `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 5 {
		t.Fatalf("expected 5 lifecycle statuses, got %d", len(body.Items))
	}

	item := body.Items[0]
	if item.Key != string(model.LifecycleStatusProvisioning) {
		t.Fatalf("expected first lifecycle status key provisioning, got %s", item.Key)
	}
	if item.Label == "" {
		t.Fatal("expected label to be set")
	}
	if item.Description == "" {
		t.Fatal("expected description to be set")
	}
}

func TestListHealthStatuses(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/health-statuses", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Items []model.DictionaryItem `json:"items"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(body.Items) != 4 {
		t.Fatalf("expected 4 health statuses, got %d", len(body.Items))
	}

	item := body.Items[0]
	if item.Key != string(model.HealthStatusHealthy) {
		t.Fatalf("expected first health status key healthy, got %s", item.Key)
	}
	if item.Label == "" {
		t.Fatal("expected label to be set")
	}
	if item.Description == "" {
		t.Fatal("expected description to be set")
	}
}

func TestListResourceSubtypes(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resource-subtypes?resourceType=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		ResourceType string                 `json:"resourceType"`
		Subtypes     []model.DictionaryItem `json:"subtypes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.ResourceType != "database_instance" {
		t.Fatalf("expected resourceType database_instance, got %s", body.ResourceType)
	}

	if len(body.Subtypes) != 6 {
		t.Fatalf("expected 6 subtypes for database_instance, got %d", len(body.Subtypes))
	}

	if body.Subtypes[0].Key != "mysql" {
		t.Fatalf("expected first subtype key mysql, got %s", body.Subtypes[0].Key)
	}
	if body.Subtypes[0].Label != "MySQL" {
		t.Fatalf("expected first subtype label MySQL, got %s", body.Subtypes[0].Label)
	}
}

func TestListResourceSubtypes_ServiceIncludesWorker(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resource-subtypes?resourceType=service", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		ResourceType string                 `json:"resourceType"`
		Subtypes     []model.DictionaryItem `json:"subtypes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.ResourceType != "service" {
		t.Fatalf("resourceType = %q, want service", body.ResourceType)
	}
	found := false
	for _, item := range body.Subtypes {
		if item.Key == "worker" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("service subtypes missing worker, got %#v", body.Subtypes)
	}
}

func TestListResourceSubtypes_MissingResourceType(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resource-subtypes", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListResourceSubtypes_UnknownType(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resource-subtypes?resourceType=not_a_type", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		ResourceType string                 `json:"resourceType"`
		Subtypes     []model.DictionaryItem `json:"subtypes"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body.ResourceType != "not_a_type" {
		t.Fatalf("expected resourceType not_a_type, got %s", body.ResourceType)
	}

	if len(body.Subtypes) != 0 {
		t.Fatalf("expected 0 subtypes for unknown type, got %d", len(body.Subtypes))
	}
}

func TestListResourceSubtypes_DomainNameAndVirtualIP(t *testing.T) {
	server := NewTestServer()
	cases := []struct {
		resourceType string
		wantKey      string
	}{
		{"domain_name", "dns"},
		{"virtual_ip", "floating"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/resource-subtypes?resourceType="+tc.resourceType, nil)
		rec := httptest.NewRecorder()
		server.Router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tc.resourceType, rec.Code)
		}
		var body struct {
			ResourceType string                 `json:"resourceType"`
			Subtypes     []model.DictionaryItem `json:"subtypes"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("%s: decode: %v", tc.resourceType, err)
		}
		if len(body.Subtypes) != 1 || body.Subtypes[0].Key != tc.wantKey {
			t.Fatalf("%s: subtypes = %#v, want [%s]", tc.resourceType, body.Subtypes, tc.wantKey)
		}
	}
}
