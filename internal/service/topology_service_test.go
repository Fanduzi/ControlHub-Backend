// Package service provides tests for the resource topology projection.
// input: internal/model, internal/service
// output: topology service test suite, including bounded relation reads and sentinel truncation
// pos: TDD tests for TopologyService.BuildTopology response semantics and repository budgets
// note: if this file changes, update this header and module README.md.
package service

import (
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

const (
	topoClusterID  uint64 = 1001
	topoInstance1  uint64 = 1002
	topoInstance2  uint64 = 1003
	topoHost1      uint64 = 1004
	topoService1   uint64 = 1005
	topoProxy1     uint64 = 1006
	topoIsolatedID uint64 = 1007
)

// fakeTopologyRepo implements TopologyRepository for testing.
type fakeTopologyRepo struct {
	resources          map[uint64]model.Resource
	hidden             map[uint64]bool
	relations          []model.ResourceRelation
	candidateIDs       []uint64
	topologyReadLimits []int
}

func (f *fakeTopologyRepo) GetResource(id uint64) (*model.Resource, error) {
	if f.hidden[id] {
		return nil, ErrResourceNotFound
	}
	r, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	copied := r
	return &copied, nil
}

func (f *fakeTopologyRepo) ListRelationsByResourceIDs(ids []uint64) ([]model.ResourceRelation, error) {
	idSet := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var result []model.ResourceRelation
	for _, rel := range f.relations {
		if idSet[rel.FromResourceID] || idSet[rel.ToResourceID] {
			result = append(result, rel)
		}
	}
	return result, nil
}

func (f *fakeTopologyRepo) ListTopologyRelationsByResourceIDs(ids []uint64, direction model.TopologyDirection, relationType model.RelationType, limit int) ([]model.ResourceRelation, error) {
	f.topologyReadLimits = append(f.topologyReadLimits, limit)
	items, err := f.ListRelationsByResourceIDs(ids)
	if err != nil {
		return nil, err
	}
	idSet := make(map[uint64]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	filtered := items[:0]
	for _, item := range items {
		if relationType != "" && item.RelationType != relationType {
			continue
		}
		if direction == model.TopologyDirectionUpstream && !idSet[item.ToResourceID] {
			continue
		}
		if direction == model.TopologyDirectionDownstream && !idSet[item.FromResourceID] {
			continue
		}
		filtered = append(filtered, item)
	}
	items = filtered
	sortTopologyRelations(items)
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (f *fakeTopologyRepo) ListTopologyCandidates(environmentID uint64) ([]model.Resource, error) {
	var result []model.Resource
	for _, id := range f.candidateIDs {
		r, ok := f.resources[id]
		if !ok || f.hidden[id] || r.EnvironmentID != environmentID {
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

func buildTestRepo() *fakeTopologyRepo {
	return &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			topoClusterID: {
				ID: topoClusterID, ResourceType: model.ResourceTypeDatabaseCluster,
				ResourceSubtype: "mysql", Name: "order-cluster", DisplayName: "Order Cluster",
				EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			topoInstance1: {
				ID: topoInstance1, ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "order-mysql-1", DisplayName: "Order MySQL 1",
				EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			topoInstance2: {
				ID: topoInstance2, ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "order-mysql-2", DisplayName: "Order MySQL 2",
				EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			topoHost1: {
				ID: topoHost1, ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "prod-host-1", DisplayName: "Prod Host 1",
				EnvironmentID: testEnvID, OwnerID: 3,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			topoService1: {
				ID: topoService1, ResourceType: model.ResourceTypeService,
				ResourceSubtype: "api", Name: "order-api", DisplayName: "Order API",
				EnvironmentID: testEnvID, OwnerID: 3,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			topoProxy1: {
				ID: topoProxy1, ResourceType: model.ResourceTypeDatabaseProxy,
				ResourceSubtype: "proxysql", Name: "order-proxy", DisplayName: "Order Proxy",
				EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			topoIsolatedID: {
				ID: topoIsolatedID, ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "isolated-host", DisplayName: "Isolated Host",
				EnvironmentID: 2, OwnerID: 3,
				LifecycleStatus: "stopped", HealthStatus: "unknown",
			},
		},
		relations: []model.ResourceRelation{
			{ID: 1, FromResourceID: topoInstance1, ToResourceID: topoClusterID, RelationType: model.RelationTypeMemberOf},
			{ID: 2, FromResourceID: topoInstance2, ToResourceID: topoClusterID, RelationType: model.RelationTypeMemberOf},
			{ID: 3, FromResourceID: topoInstance1, ToResourceID: topoHost1, RelationType: model.RelationTypeRunsOn},
			{ID: 4, FromResourceID: topoService1, ToResourceID: topoClusterID, RelationType: model.RelationTypeDependsOn},
			{ID: 5, FromResourceID: topoProxy1, ToResourceID: topoClusterID, RelationType: model.RelationTypeFronts},
			{ID: 6, FromResourceID: topoInstance2, ToResourceID: topoHost1, RelationType: model.RelationTypeRunsOn},
		},
	}
}

func TestBuildTopology_RootWithNoRelations(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoIsolatedID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RootResourceID != topoIsolatedID {
		t.Errorf("root = %d, want %d", resp.RootResourceID, topoIsolatedID)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	if !resp.Nodes[0].IsRoot {
		t.Error("node isRoot = false, want true")
	}
	if resp.Nodes[0].Distance != 0 {
		t.Errorf("distance = %d, want 0", resp.Nodes[0].Distance)
	}
	if len(resp.Edges) != 0 {
		t.Errorf("edges = %d, want 0", len(resp.Edges))
	}
	if len(resp.Groups) != 1 {
		t.Errorf("groups = %d, want 1", len(resp.Groups))
	}
}

func TestBuildTopology_Depth1(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeIDs := nodeIDs(resp)
	if len(nodeIDs) != 5 {
		t.Errorf("got %d nodes, want 5; nodes: %v", len(nodeIDs), nodeIDs)
	}
	if len(resp.Edges) != 4 {
		t.Errorf("got %d edges, want 4", len(resp.Edges))
	}
	if resp.Nodes[0].ID != topoClusterID {
		t.Errorf("first node = %d, want %d", resp.Nodes[0].ID, topoClusterID)
	}
	for _, n := range resp.Nodes[1:] {
		if n.Distance != 1 {
			t.Errorf("node %d distance = %d, want 1", n.ID, n.Distance)
		}
	}
}

func TestBuildTopology_Depth2(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeIDs := nodeIDs(resp)
	if _, ok := nodeIDs[topoHost1]; !ok {
		t.Errorf("missing host-1 in nodes: %v", nodeIDs)
	}
	for _, n := range resp.Nodes {
		if n.ID == topoHost1 && n.Distance != 2 {
			t.Errorf("host-1 distance = %d, want 2", n.Distance)
		}
	}
}

func TestBuildTopology_DefaultDepthIs2(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Depth != 2 {
		t.Fatalf("depth = %d, want 2", resp.Depth)
	}
	nodeIDs := nodeIDs(resp)
	if _, ok := nodeIDs[topoHost1]; !ok {
		t.Fatalf("missing depth-2 host in nodes: %v", nodeIDs)
	}
}

func TestBuildTopology_Depth5(t *testing.T) {
	repo := buildChainRepo(6)
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: 1, Depth: 5, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Nodes) != 6 {
		t.Fatalf("nodes = %d, want 6", len(resp.Nodes))
	}
	if resp.Nodes[5].ID != 6 || resp.Nodes[5].Distance != 5 {
		t.Fatalf("last node = %+v, want id 6 distance 5", resp.Nodes[5])
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestBuildTopology_DirectionUpstream(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionUpstream})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeIDs(resp)) != 5 {
		t.Errorf("got %d nodes, want 5: %v", len(nodeIDs(resp)), nodeIDs(resp))
	}
}

func TestBuildTopology_DirectionDownstream(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoInstance1, Depth: 1, Direction: model.TopologyDirectionDownstream})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeIDs(resp)) != 3 {
		t.Errorf("got %d nodes, want 3: %v", len(nodeIDs(resp)), nodeIDs(resp))
	}
}

func TestBuildTopology_DirectionBoth(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoInstance1, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeIDs(resp)) != 3 {
		t.Errorf("got %d nodes, want 3: %v", len(nodeIDs(resp)), nodeIDs(resp))
	}
}

func TestBuildTopology_RelationTypeFilter(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth, RelationType: model.RelationTypeMemberOf})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeIDs(resp)) != 3 {
		t.Errorf("got %d nodes, want 3: %v", len(nodeIDs(resp)), nodeIDs(resp))
	}
	for _, e := range resp.Edges {
		if e.RelationType != model.RelationTypeMemberOf {
			t.Errorf("edge relation type = %q, want member_of", e.RelationType)
		}
	}
}

func TestBuildTopology_CyclicGraphNoLoop(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			1: {ID: 1, ResourceType: model.ResourceTypeHost, Name: "a", DisplayName: "A"},
			2: {ID: 2, ResourceType: model.ResourceTypeHost, Name: "b", DisplayName: "B"},
			3: {ID: 3, ResourceType: model.ResourceTypeHost, Name: "c", DisplayName: "C"},
		},
		relations: []model.ResourceRelation{
			{ID: 1, FromResourceID: 1, ToResourceID: 2, RelationType: model.RelationTypeDependsOn},
			{ID: 2, FromResourceID: 2, ToResourceID: 3, RelationType: model.RelationTypeDependsOn},
			{ID: 3, FromResourceID: 3, ToResourceID: 1, RelationType: model.RelationTypeDependsOn},
		},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: 1, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(nodeIDs(resp)) != 3 {
		t.Errorf("got %d nodes, want 3: %v", len(nodeIDs(resp)), nodeIDs(resp))
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestBuildTopology_NodeCap(t *testing.T) {
	repo := buildChainRepo(TopologyNodeCap + 1)
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: 1, Depth: TopologyNodeCap, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Nodes) != TopologyNodeCap {
		t.Fatalf("nodes = %d, want cap %d", len(resp.Nodes), TopologyNodeCap)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if ids := nodeIDs(resp); ids[uint64(TopologyNodeCap+1)] {
		t.Fatalf("node beyond cap included: %v", ids)
	}
}

func TestBuildTopology_EdgeCap(t *testing.T) {
	repo := buildParallelEdgeRepo(TopologyEdgeCap + 1)
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: 1, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Edges) != TopologyEdgeCap {
		t.Fatalf("edges = %d, want cap %d", len(resp.Edges), TopologyEdgeCap)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(resp.Nodes))
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if len(repo.topologyReadLimits) != 1 || repo.topologyReadLimits[0] != TopologyEdgeCap+1 {
		t.Fatalf("topology relation limits = %v, want [%d]", repo.topologyReadLimits, TopologyEdgeCap+1)
	}
}

func TestBuildTopology_RelationReadUsesRemainingEdgeBudget(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			1: {ID: 1, ResourceType: model.ResourceTypeHost, Name: "root", DisplayName: "Root"},
			2: {ID: 2, ResourceType: model.ResourceTypeHost, Name: "two", DisplayName: "Two"},
			3: {ID: 3, ResourceType: model.ResourceTypeHost, Name: "three", DisplayName: "Three"},
			4: {ID: 4, ResourceType: model.ResourceTypeHost, Name: "four", DisplayName: "Four"},
		},
		relations: []model.ResourceRelation{
			{ID: 1, FromResourceID: 1, ToResourceID: 2, RelationType: model.RelationTypeDependsOn},
			{ID: 2, FromResourceID: 1, ToResourceID: 3, RelationType: model.RelationTypeDependsOn},
			{ID: 3, FromResourceID: 1, ToResourceID: 4, RelationType: model.RelationTypeDependsOn},
		},
	}

	resp, err := NewTopologyService(repo).BuildTopology(model.TopologyQuery{RootID: 1, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("build topology: %v", err)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
	if len(repo.topologyReadLimits) != 2 || repo.topologyReadLimits[0] != TopologyEdgeCap+1 || repo.topologyReadLimits[1] != TopologyEdgeCap-3+1 {
		t.Fatalf("topology relation limits = %v, want [%d %d]", repo.topologyReadLimits, TopologyEdgeCap+1, TopologyEdgeCap-3+1)
	}
}

func TestBuildTopology_HighFanOutPreservesRelationTypeFilterBeforeSentinel(t *testing.T) {
	repo := buildParallelEdgeRepo(TopologyEdgeCap + 1)
	for i := range repo.relations {
		repo.relations[i].RelationType = model.RelationTypeMemberOf
	}
	repo.relations = append(repo.relations, model.ResourceRelation{
		ID:             TopologyEdgeCap + 2,
		FromResourceID: 1,
		ToResourceID:   2,
		RelationType:   model.RelationTypeRunsOn,
	})

	resp, err := NewTopologyService(repo).BuildTopology(model.TopologyQuery{
		RootID: 1, Depth: 1, Direction: model.TopologyDirectionBoth, RelationType: model.RelationTypeRunsOn,
	})
	if err != nil {
		t.Fatalf("build topology: %v", err)
	}
	if len(resp.Edges) != 1 || resp.Edges[0].ID != TopologyEdgeCap+2 {
		t.Fatalf("edges = %+v, want filtered runs_on edge", resp.Edges)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestBuildTopology_MissingRoot(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{RootID: testMissingID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Errorf("err = %v, want ErrResourceNotFound", err)
	}
}

func TestBuildTopology_ContextMissingOrHiddenRoot(t *testing.T) {
	repo := buildTestRepo()
	repo.hidden = map[uint64]bool{topoClusterID: true}
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{EnvironmentID: testEnvID, RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("err = %v, want ErrResourceNotFound", err)
	}
}

func TestBuildTopology_ContextWrongEnvironmentIsNotFound(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{EnvironmentID: 2, RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("err = %v, want ErrResourceNotFound", err)
	}
}

func TestBuildTopology_ContextSkipsCrossEnvironmentNeighbor(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			1: {ID: 1, ResourceType: model.ResourceTypeService, Name: "api", DisplayName: "API", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			2: {ID: 2, ResourceType: model.ResourceTypeDatabaseCluster, Name: "same-db", DisplayName: "Same DB", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			3: {ID: 3, ResourceType: model.ResourceTypeDatabaseCluster, Name: "other-db", DisplayName: "Other DB", EnvironmentID: 20, HealthStatus: "healthy", LifecycleStatus: "running"},
		},
		relations: []model.ResourceRelation{
			{ID: 1, FromResourceID: 1, ToResourceID: 2, RelationType: model.RelationTypeDependsOn},
			{ID: 2, FromResourceID: 1, ToResourceID: 3, RelationType: model.RelationTypeDependsOn},
		},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{EnvironmentID: 10, RootID: 1, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := nodeIDs(resp)
	if len(ids) != 2 || !ids[1] || !ids[2] || ids[3] {
		t.Fatalf("nodes = %v, want only env 10 nodes 1 and 2", ids)
	}
	if len(resp.Edges) != 1 || resp.Edges[0].ID != 1 {
		t.Fatalf("edges = %+v, want only same-env edge 1", resp.Edges)
	}
}

func TestBuildTopology_InvalidDepth(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: -1, Direction: model.TopologyDirectionBoth})
	if !errors.Is(err, ErrInvalidDepth) {
		t.Errorf("err = %v, want ErrInvalidDepth", err)
	}
}

func TestBuildTopology_InvalidDirection(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: "invalid"})
	if !errors.Is(err, ErrInvalidDirection) {
		t.Errorf("err = %v, want ErrInvalidDirection", err)
	}
}

func TestBuildTopology_NoRootCandidatePredicate(t *testing.T) {
	archivedAt := topoTime()
	repo := &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			1:  {ID: 1, ResourceType: model.ResourceTypeService, Name: "svc", DisplayName: "Service", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			2:  {ID: 2, ResourceType: model.ResourceTypeDatabaseCluster, Name: "cluster", DisplayName: "Cluster", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			3:  {ID: 3, ResourceType: model.ResourceTypeDatabaseProxy, Name: "proxy", DisplayName: "Proxy", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			4:  {ID: 4, ResourceType: model.ResourceTypeHost, Name: "warn-host", DisplayName: "Warn Host", EnvironmentID: 10, HealthStatus: "warning", LifecycleStatus: "running"},
			5:  {ID: 5, ResourceType: model.ResourceTypeDomainName, Name: "crit-domain", DisplayName: "Crit Domain", EnvironmentID: 10, HealthStatus: "critical", LifecycleStatus: "running"},
			6:  {ID: 6, ResourceType: model.ResourceTypeHost, Name: "host", DisplayName: "Host", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			7:  {ID: 7, ResourceType: model.ResourceTypeDomainName, Name: "domain", DisplayName: "Domain", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			8:  {ID: 8, ResourceType: model.ResourceTypeVirtualIP, Name: "vip", DisplayName: "VIP", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			9:  {ID: 9, ResourceType: model.ResourceTypeControlPlaneComponent, Name: "cp", DisplayName: "CP", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"},
			10: {ID: 10, ResourceType: model.ResourceTypeService, Name: "other", DisplayName: "Other", EnvironmentID: 20, HealthStatus: "healthy", LifecycleStatus: "running"},
			11: {ID: 11, ResourceType: model.ResourceTypeService, Name: "archived", DisplayName: "Archived", EnvironmentID: 10, HealthStatus: "critical", LifecycleStatus: "running", ArchivedAt: &archivedAt},
		},
		candidateIDs: []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{EnvironmentID: 10, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := nodeIDs(resp)
	want := map[uint64]bool{1: true, 2: true, 3: true, 4: true, 5: true}
	if len(got) != len(want) {
		t.Fatalf("nodes = %v, want %v", got, want)
	}
	for id := range want {
		if !got[id] {
			t.Fatalf("missing node %d in %v", id, got)
		}
	}
	if len(resp.Edges) != 0 {
		t.Fatalf("edges = %+v, want none", resp.Edges)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestBuildTopology_NoRootDedupesAndCapsCandidates(t *testing.T) {
	resources := make(map[uint64]model.Resource, TopologyNodeCap+2)
	candidateIDs := []uint64{1, 1}
	for i := 1; i <= TopologyNodeCap+1; i++ {
		id := uint64(i)
		resources[id] = model.Resource{ID: id, ResourceType: model.ResourceTypeService, Name: "svc", DisplayName: "Service", EnvironmentID: 10, HealthStatus: "healthy", LifecycleStatus: "running"}
		if i > 1 {
			candidateIDs = append(candidateIDs, id)
		}
	}
	repo := &fakeTopologyRepo{resources: resources, candidateIDs: candidateIDs}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{EnvironmentID: 10, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Nodes) != TopologyNodeCap {
		t.Fatalf("nodes = %d, want cap %d", len(resp.Nodes), TopologyNodeCap)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if ids := nodeIDs(resp); ids[uint64(TopologyNodeCap+1)] {
		t.Fatalf("node beyond cap included: %v", ids)
	}
}

func TestBuildTopology_GroupsByResourceType(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	groupMap := map[model.ResourceType]model.TopologyGroup{}
	for _, g := range resp.Groups {
		groupMap[g.ResourceType] = g
	}

	if g, ok := groupMap[model.ResourceTypeDatabaseCluster]; !ok {
		t.Error("missing database_cluster group")
	} else if len(g.NodeIDs) != 1 || g.NodeIDs[0] != topoClusterID {
		t.Errorf("database_cluster group nodes = %v, want [%d]", g.NodeIDs, topoClusterID)
	}
	if g, ok := groupMap[model.ResourceTypeDatabaseInstance]; !ok {
		t.Error("missing database_instance group")
	} else if len(g.NodeIDs) != 2 {
		t.Errorf("database_instance group count = %d, want 2", len(g.NodeIDs))
	}
}

func TestBuildTopology_DeterministicOrdering(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp1, _ := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth})
	resp2, _ := svc.BuildTopology(model.TopologyQuery{RootID: topoClusterID, Depth: 1, Direction: model.TopologyDirectionBoth})

	for i := range resp1.Nodes {
		if resp1.Nodes[i].ID != resp2.Nodes[i].ID {
			t.Errorf("node ordering differs at index %d: %d vs %d", i, resp1.Nodes[i].ID, resp2.Nodes[i].ID)
		}
	}
	for i := range resp1.Edges {
		if resp1.Edges[i].ID != resp2.Edges[i].ID {
			t.Errorf("edge ordering differs at index %d: %d vs %d", i, resp1.Edges[i].ID, resp2.Edges[i].ID)
		}
	}
}

func nodeIDs(resp *model.TopologyResponse) map[uint64]bool {
	m := make(map[uint64]bool, len(resp.Nodes))
	for _, n := range resp.Nodes {
		m[n.ID] = true
	}
	return m
}

func buildChainRepo(nodes int) *fakeTopologyRepo {
	resources := make(map[uint64]model.Resource, nodes)
	relations := make([]model.ResourceRelation, 0, nodes-1)
	for i := 1; i <= nodes; i++ {
		id := uint64(i)
		resources[id] = model.Resource{ID: id, ResourceType: model.ResourceTypeHost, Name: "node", DisplayName: "Node"}
		if i > 1 {
			relations = append(relations, model.ResourceRelation{
				ID:             uint64(i - 1),
				FromResourceID: uint64(i - 1),
				ToResourceID:   id,
				RelationType:   model.RelationTypeDependsOn,
			})
		}
	}
	return &fakeTopologyRepo{resources: resources, relations: relations}
}

func buildParallelEdgeRepo(edges int) *fakeTopologyRepo {
	resources := make(map[uint64]model.Resource, 2)
	relations := make([]model.ResourceRelation, 0, edges)
	resources[1] = model.Resource{ID: 1, ResourceType: model.ResourceTypeHost, Name: "root", DisplayName: "Root"}
	resources[2] = model.Resource{ID: 2, ResourceType: model.ResourceTypeHost, Name: "leaf", DisplayName: "Leaf"}
	for i := 1; i <= edges; i++ {
		relations = append(relations, model.ResourceRelation{
			ID:             uint64(i),
			FromResourceID: 1,
			ToResourceID:   2,
			RelationType:   model.RelationTypeDependsOn,
		})
	}
	return &fakeTopologyRepo{resources: resources, relations: relations}
}

func topoTime() time.Time {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func TestDetectNodeProblems_HealthyRunning(t *testing.T) {
	res := &model.Resource{HealthStatus: "healthy", LifecycleStatus: "running"}
	problems := detectNodeProblems(res)
	if len(problems) != 0 {
		t.Fatalf("expected 0 problems for healthy/running, got %d", len(problems))
	}
}

func TestDetectNodeProblems_CriticalHealth(t *testing.T) {
	res := &model.Resource{HealthStatus: "critical", LifecycleStatus: "running"}
	problems := detectNodeProblems(res)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(problems))
	}
	if problems[0].Severity != "critical" || problems[0].Code != "health_critical" {
		t.Errorf("problem = %+v, want critical/health_critical", problems[0])
	}
}

func TestDetectNodeProblems_WarningHealth(t *testing.T) {
	res := &model.Resource{HealthStatus: "warning", LifecycleStatus: "running"}
	problems := detectNodeProblems(res)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(problems))
	}
	if problems[0].Severity != "warning" || problems[0].Code != "health_warning" {
		t.Errorf("problem = %+v, want warning/health_warning", problems[0])
	}
}

func TestDetectNodeProblems_StoppedLifecycle(t *testing.T) {
	res := &model.Resource{HealthStatus: "healthy", LifecycleStatus: "stopped"}
	problems := detectNodeProblems(res)
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(problems))
	}
	if problems[0].Severity != "critical" || problems[0].Code != "lifecycle_stopped" {
		t.Errorf("problem = %+v, want critical/lifecycle_stopped", problems[0])
	}
}

func TestDetectNodeProblems_MultipleProblems(t *testing.T) {
	res := &model.Resource{HealthStatus: "critical", LifecycleStatus: "stopped"}
	problems := detectNodeProblems(res)
	if len(problems) != 2 {
		t.Fatalf("expected 2 problems, got %d", len(problems))
	}
	codes := map[string]bool{}
	for _, p := range problems {
		codes[p.Code] = true
	}
	if !codes["health_critical"] || !codes["lifecycle_stopped"] {
		t.Errorf("expected health_critical + lifecycle_stopped, got %+v", problems)
	}
}

func TestBuildProblemSummaries_FilterHealthy(t *testing.T) {
	nodes := []model.TopologyNode{{ID: 1, DisplayName: "A", HealthStatus: "healthy", LifecycleStatus: "running"}}
	summaries := buildProblemSummaries(nodes)
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for healthy node, got %d", len(summaries))
	}
}

func TestBuildProblemSummaries_WorstSeverity(t *testing.T) {
	nodes := []model.TopologyNode{{
		ID: 1, DisplayName: "A", ResourceType: model.ResourceTypeDatabaseInstance,
		HealthStatus: "healthy", LifecycleStatus: "running",
		Problems: []model.TopologyProblem{{Severity: "warning", Code: "health_warning"}, {Severity: "critical", Code: "lifecycle_stopped"}},
	}}
	summaries := buildProblemSummaries(nodes)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Severity != "critical" {
		t.Errorf("severity = %q, want critical", summaries[0].Severity)
	}
}

func TestBuildTopology_ProfileEnrichment(t *testing.T) {
	repo := &fakeTopologyRepo{resources: map[uint64]model.Resource{1: {
		ID: 1, ResourceType: model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql", Name: "mysql-1", DisplayName: "MySQL 1",
		HealthStatus: "healthy", LifecycleStatus: "running",
		ProfileSummary: &model.ProfileSummary{Hostname: "db-host-01.internal", IP: "10.0.10.20", Port: 3306},
	}}, relations: []model.ResourceRelation{}}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: 1, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	n := resp.Nodes[0]
	if n.Hostname != "db-host-01.internal" || n.IP != "10.0.10.20" || n.Port != 3306 {
		t.Fatalf("unexpected profile enrichment: %+v", n)
	}
}

func TestBuildTopology_ProblemSummary(t *testing.T) {
	repo := &fakeTopologyRepo{resources: map[uint64]model.Resource{
		1: {ID: 1, ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql", Name: "ok", DisplayName: "OK Instance", HealthStatus: "healthy", LifecycleStatus: "running"},
		2: {ID: 2, ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql", Name: "bad", DisplayName: "Bad Instance", HealthStatus: "critical", LifecycleStatus: "stopped"},
	}, relations: []model.ResourceRelation{}}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: 2, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Problems) != 1 {
		t.Fatalf("problems = %d, want 1", len(resp.Problems))
	}
	p := resp.Problems[0]
	if p.ResourceID != 2 || p.Severity != "critical" || len(p.Problems) != 2 {
		t.Fatalf("unexpected problem summary: %+v", p)
	}
}
