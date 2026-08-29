// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: Relation list/create/delete tests and the server-owned rule-discovery contract
// pos: Validates relation listing, maintenance, rejection, and source-specific rule discovery
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestCreateResourceRelation(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2,"relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.ResourceRelation
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.FromResourceID != 1 {
		t.Fatalf("expected fromResourceId 1, got %d", resp.FromResourceID)
	}
	if resp.ToResourceID != 2 {
		t.Fatalf("expected toResourceId 2, got %d", resp.ToResourceID)
	}
	if resp.RelationType != model.RelationTypeDependsOn {
		t.Fatalf("expected relationType depends_on, got %s", resp.RelationType)
	}
}

func TestGetResourceRelationRules(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/1/relation-rules", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		SourceResourceID    uint64 `json:"sourceResourceId"`
		SourceEnvironmentID uint64 `json:"sourceEnvironmentId"`
		Rules               []struct {
			RelationType        model.RelationType   `json:"relationType"`
			TargetResourceTypes []model.ResourceType `json:"targetResourceTypes"`
			SameEnvironment     bool                 `json:"sameEnvironment"`
		} `json:"rules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.SourceResourceID != 1 {
		t.Fatalf("sourceResourceId = %d, want 1", response.SourceResourceID)
	}
	if response.SourceEnvironmentID != 1 {
		t.Fatalf("sourceEnvironmentId = %d, want 1", response.SourceEnvironmentID)
	}
	want := map[model.RelationType]struct {
		targets         []model.ResourceType
		sameEnvironment bool
	}{
		model.RelationTypeMemberOf: {
			targets:         []model.ResourceType{model.ResourceTypeDatabaseCluster},
			sameEnvironment: true,
		},
		model.RelationTypeRunsOn: {
			targets:         []model.ResourceType{model.ResourceTypeHost},
			sameEnvironment: true,
		},
		model.RelationTypeReplicatesTo: {
			targets:         []model.ResourceType{model.ResourceTypeDatabaseInstance},
			sameEnvironment: true,
		},
		model.RelationTypeDependsOn: {targets: []model.ResourceType{
			model.ResourceTypeHost,
			model.ResourceTypeDatabaseInstance,
			model.ResourceTypeDatabaseCluster,
			model.ResourceTypeService,
			model.ResourceTypeDomainName,
			model.ResourceTypeVirtualIP,
			model.ResourceTypeDatabaseProxy,
			model.ResourceTypeControlPlaneComponent,
		}},
	}
	if len(response.Rules) != len(want) {
		t.Fatalf("rules = %#v, want %d source rules", response.Rules, len(want))
	}
	for _, rule := range response.Rules {
		expected, ok := want[rule.RelationType]
		if !ok {
			t.Fatalf("unexpected relation type %q", rule.RelationType)
		}
		if rule.SameEnvironment != expected.sameEnvironment || strings.Join(resourceTypeStrings(rule.TargetResourceTypes), ",") != strings.Join(resourceTypeStrings(expected.targets), ",") {
			t.Fatalf("rule %q = %#v, want targets %#v sameEnvironment %t", rule.RelationType, rule, expected.targets, expected.sameEnvironment)
		}
	}
}

func resourceTypeStrings(items []model.ResourceType) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = string(item)
	}
	return result
}

func TestCreateResourceRelationRejectsMissingTarget(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":999,"relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "resource_not_found")
}

func TestCreateResourceRelationRejectsUnsupportedRelationType(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2,"relationType":"unsupported"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRelationRejectsDisallowedMatrixPair(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2,"relationType":"member_of"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRelationRejectsDuplicate(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2,"relationType":"depends_on"}`

	firstReq := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	firstRec := httptest.NewRecorder()
	server.Router.ServeHTTP(firstRec, firstReq)
	if firstRec.Code != http.StatusCreated {
		t.Fatalf("expected first create 201, got %d; body: %s", firstRec.Code, firstRec.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	secondRec := httptest.NewRecorder()
	server.Router.ServeHTTP(secondRec, secondReq)

	assertAPIError(t, secondRec, http.StatusConflict, "relation_conflict")
}

func TestCreateResourceRelationRejectsSelfRelation(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":1,"relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRelationRejectsMalformedJSON(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(`{"toResourceId":`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "malformed_json")
}

func TestCreateResourceRelationRejectsEmptyToResourceId(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":0,"relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRelationRejectsMissingToResourceId(t *testing.T) {
	server := NewTestServer()
	body := `{"relationType":"depends_on"}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestCreateResourceRelationRejectsMissingRelationType(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2}`
	req := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestDeleteResourceRelation(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2,"relationType":"depends_on"}`
	createReq := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	createRec := httptest.NewRecorder()
	server.Router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}

	var created model.ResourceRelation
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created relation: %v", err)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/resource-relations/"+strconv.FormatUint(created.ID, 10), nil)
	deleteRec := httptest.NewRecorder()
	server.Router.ServeHTTP(deleteRec, deleteReq)

	if deleteRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", deleteRec.Code, deleteRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/resources/1/relations", nil)
	listRec := httptest.NewRecorder()
	server.Router.ServeHTTP(listRec, listReq)

	var listed struct {
		Items []model.ResourceRelation `json:"items"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	for _, item := range listed.Items {
		if item.ID == created.ID {
			t.Fatalf("expected relation %d to be deleted", created.ID)
		}
	}
}

func TestDeleteResourceRelationRejectsMissingRelation(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodDelete, "/resource-relations/999", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusNotFound, "relation_not_found")
}

func TestListResourceRelationsReflectsCreate(t *testing.T) {
	server := NewTestServer()
	body := `{"toResourceId":2,"relationType":"depends_on"}`
	createReq := httptest.NewRequest(http.MethodPost, "/resources/1/relations", strings.NewReader(body))
	createRec := httptest.NewRecorder()
	server.Router.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/resources/1/relations", nil)
	listRec := httptest.NewRecorder()
	server.Router.ServeHTTP(listRec, listReq)

	var listed struct {
		Items []model.ResourceRelation `json:"items"`
	}
	if err := json.NewDecoder(listRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Items) == 0 {
		t.Fatal("expected created relation in list")
	}
}

func TestListResourceRelations(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/1/relations", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" {
		t.Fatal("expected response body")
	}
}
