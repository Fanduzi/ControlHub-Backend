// Package api provides tests for the topology HTTP handler.
// input: net/http/httptest, internal/api
// output: topology handler test suite
// pos: Handler tests for GET /resources/{id}/topology
// note: if this file changes, update this header and module README.md.
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestGetTopology_DefaultParams(t *testing.T) {
	server := NewTestServer()

	// res-db-cluster has relations from res-db-instance (member_of)
	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RootResourceID != 4 {
		t.Errorf("root = %d, want 4", resp.RootResourceID)
	}
	if resp.Depth != 2 {
		t.Errorf("depth = %d, want 2", resp.Depth)
	}
	if resp.Direction != model.TopologyDirectionBoth {
		t.Errorf("direction = %q, want both", resp.Direction)
	}
	if len(resp.Nodes) < 1 {
		t.Errorf("nodes = %d, want >= 1", len(resp.Nodes))
	}
	if !resp.Nodes[0].IsRoot {
		t.Error("first node should be root")
	}
}

func TestGetTopology_Depth2(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?depth=2", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Depth != 2 {
		t.Errorf("depth = %d, want 2", resp.Depth)
	}
}

func TestGetTopology_DirectionUpstream(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?direction=upstream", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Direction != model.TopologyDirectionUpstream {
		t.Errorf("direction = %q, want upstream", resp.Direction)
	}
}

func TestGetTopology_DirectionDownstream(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/3/topology?direction=downstream", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Direction != model.TopologyDirectionDownstream {
		t.Errorf("direction = %q, want downstream", resp.Direction)
	}
}

func TestGetTopology_RelationTypeFilter(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?relationType=member_of", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range resp.Edges {
		if e.RelationType != model.RelationTypeMemberOf {
			t.Errorf("edge relation type = %q, want member_of", e.RelationType)
		}
	}
}

func TestGetTopology_MissingRoot(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/999/topology", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

func TestGetTopology_Depth5(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?depth=5", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Depth != 5 {
		t.Errorf("depth = %d, want 5", resp.Depth)
	}
}

func TestGetTopology_InvalidDepth(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?depth=0", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestGetTopology_InvalidDirection(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?direction=sideways", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}
}

func TestGetTopology_RootWithNoRelations(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/7/topology", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Errorf("nodes = %d, want 1", len(resp.Nodes))
	}
	if len(resp.Edges) != 0 {
		t.Errorf("edges = %d, want 0", len(resp.Edges))
	}
	if resp.Nodes[0].ID != 7 {
		t.Errorf("node id = %d, want 7", resp.Nodes[0].ID)
	}
}

func TestGetTopology_GroupsPresent(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Groups) == 0 {
		t.Error("expected groups in response")
	}
}

func TestGetTopology_InvalidRelationType(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?relationType=invalid_type", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", w.Code, w.Body.String())
	}

	var errResp errorResponse
	if err := json.NewDecoder(w.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if errResp.Error != "validation_failed" {
		t.Errorf("error code = %q, want validation_failed", errResp.Error)
	}
}

func TestGetTopology_ValidRelationTypeStillWorks(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/resources/4/topology?relationType=member_of", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, e := range resp.Edges {
		if e.RelationType != model.RelationTypeMemberOf {
			t.Errorf("edge relation type = %q, want member_of", e.RelationType)
		}
	}
}

func TestGetEnvironmentTopology_NoRootReturnsEnvironmentCandidates(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/environments/1/topology", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RootResourceID != 0 {
		t.Fatalf("rootResourceId = %d, want 0 when no root is selected", resp.RootResourceID)
	}
	if resp.Depth != 2 {
		t.Fatalf("depth = %d, want default 2", resp.Depth)
	}
	got := map[uint64]bool{}
	for _, node := range resp.Nodes {
		got[node.ID] = true
		if node.EnvironmentID != 1 {
			t.Fatalf("node %d environmentId = %d, want 1", node.ID, node.EnvironmentID)
		}
	}
	for _, id := range []uint64{4, 5, 10} {
		if !got[id] {
			t.Fatalf("missing candidate node %d in %v", id, got)
		}
	}
	for _, id := range []uint64{6, 7} {
		if got[id] {
			t.Fatalf("non-candidate node %d included in %v", id, got)
		}
	}
	if len(resp.Edges) != 0 {
		t.Fatalf("edges = %+v, want none for no-root workspace", resp.Edges)
	}
}

func TestGetEnvironmentTopology_WithRootUsesEnvironmentScope(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/environments/1/topology?rootResourceId=4", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var resp model.TopologyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RootResourceID != 4 {
		t.Fatalf("rootResourceId = %d, want 4", resp.RootResourceID)
	}
	for _, node := range resp.Nodes {
		if node.EnvironmentID != 1 {
			t.Fatalf("node %d environmentId = %d, want 1", node.ID, node.EnvironmentID)
		}
	}
}

func TestGetEnvironmentTopology_WrongEnvironmentRootIsNotFound(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/environments/2/topology?rootResourceId=4", nil)
	w := httptest.NewRecorder()
	server.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}
