// Package service provides tests for the resource topology projection.
// input: internal/model, internal/service
// output: topology service test suite
// pos: TDD tests for TopologyService.BuildTopology
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// fakeTopologyRepo implements TopologyRepository for testing.
type fakeTopologyRepo struct {
	resources map[string]model.Resource
	relations []model.ResourceRelation
}

func (f *fakeTopologyRepo) GetResource(id string) (*model.Resource, error) {
	r, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	copied := r
	return &copied, nil
}

func (f *fakeTopologyRepo) ListRelationsByResourceIDs(ids []string) ([]model.ResourceRelation, error) {
	idSet := make(map[string]bool, len(ids))
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

func buildTestRepo() *fakeTopologyRepo {
	return &fakeTopologyRepo{
		resources: map[string]model.Resource{
			"cluster-1": {
				ID: "cluster-1", ResourceType: model.ResourceTypeDatabaseCluster,
				ResourceSubtype: "mysql", Name: "order-cluster", DisplayName: "Order Cluster",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"instance-1": {
				ID: "instance-1", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "order-mysql-1", DisplayName: "Order MySQL 1",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"instance-2": {
				ID: "instance-2", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "order-mysql-2", DisplayName: "Order MySQL 2",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"host-1": {
				ID: "host-1", ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "prod-host-1", DisplayName: "Prod Host 1",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"service-1": {
				ID: "service-1", ResourceType: model.ResourceTypeService,
				ResourceSubtype: "api", Name: "order-api", DisplayName: "Order API",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"proxy-1": {
				ID: "proxy-1", ResourceType: model.ResourceTypeDatabaseProxy,
				ResourceSubtype: "proxysql", Name: "order-proxy", DisplayName: "Order Proxy",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"isolated": {
				ID: "isolated", ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "isolated-host", DisplayName: "Isolated Host",
				EnvironmentID: "env-staging", OwnerID: "owner-platform",
				LifecycleStatus: "stopped", HealthStatus: "unknown",
			},
		},
		relations: []model.ResourceRelation{
			{ID: "rel-1", FromResourceID: "instance-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeMemberOf},
			{ID: "rel-2", FromResourceID: "instance-2", ToResourceID: "cluster-1", RelationType: model.RelationTypeMemberOf},
			{ID: "rel-3", FromResourceID: "instance-1", ToResourceID: "host-1", RelationType: model.RelationTypeRunsOn},
			{ID: "rel-4", FromResourceID: "service-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeDependsOn},
			{ID: "rel-5", FromResourceID: "proxy-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeFronts},
			{ID: "rel-6", FromResourceID: "instance-2", ToResourceID: "host-1", RelationType: model.RelationTypeRunsOn},
		},
	}
}

func TestBuildTopology_RootWithNoRelations(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "isolated",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.RootResourceID != "isolated" {
		t.Errorf("root = %q, want isolated", resp.RootResourceID)
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

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// cluster-1 has: instance-1 (member_of), instance-2 (member_of),
	// service-1 (depends_on), proxy-1 (fronts) — 4 neighbors at depth 1
	nodeIDs := nodeIDs(resp)
	if len(nodeIDs) != 5 {
		t.Errorf("got %d nodes, want 5; nodes: %v", len(nodeIDs), nodeIDs)
	}
	if len(resp.Edges) != 4 {
		t.Errorf("got %d edges, want 4", len(resp.Edges))
	}
	// Root first
	if resp.Nodes[0].ID != "cluster-1" {
		t.Errorf("first node = %q, want cluster-1", resp.Nodes[0].ID)
	}
	if resp.Nodes[0].Distance != 0 {
		t.Errorf("root distance = %d, want 0", resp.Nodes[0].Distance)
	}
	// All non-root at distance 1
	for _, n := range resp.Nodes[1:] {
		if n.Distance != 1 {
			t.Errorf("node %q distance = %d, want 1", n.ID, n.Distance)
		}
	}
}

func TestBuildTopology_Depth2(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     2,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Depth 1: instance-1, instance-2, service-1, proxy-1
	// Depth 2 from instance-1: host-1 (runs_on)
	// Depth 2 from instance-2: host-1 (runs_on) — already found
	// Depth 2 from service-1: none new
	// Depth 2 from proxy-1: none new
	nodeIDs := nodeIDs(resp)
	if _, ok := nodeIDs["host-1"]; !ok {
		t.Errorf("missing host-1 in nodes: %v", nodeIDs)
	}
	// host-1 should be distance 2
	for _, n := range resp.Nodes {
		if n.ID == "host-1" && n.Distance != 2 {
			t.Errorf("host-1 distance = %d, want 2", n.Distance)
		}
	}
}

func TestBuildTopology_DirectionUpstream(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	// Upstream from cluster-1: resources that point TO cluster-1
	// member_of: instance-1, instance-2
	// depends_on: service-1
	// fronts: proxy-1
	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     1,
		Direction: model.TopologyDirectionUpstream,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeIDs := nodeIDs(resp)
	// Should have cluster-1 + 4 upstream resources
	if len(nodeIDs) != 5 {
		t.Errorf("got %d nodes, want 5: %v", len(nodeIDs), nodeIDs)
	}
}

func TestBuildTopology_DirectionDownstream(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	// Downstream from instance-1: resources instance-1 points TO
	// member_of -> cluster-1, runs_on -> host-1
	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "instance-1",
		Depth:     1,
		Direction: model.TopologyDirectionDownstream,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeIDs := nodeIDs(resp)
	if len(nodeIDs) != 3 {
		t.Errorf("got %d nodes, want 3 (instance-1, cluster-1, host-1): %v", len(nodeIDs), nodeIDs)
	}
}

func TestBuildTopology_DirectionBoth(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "instance-1",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// instance-1: upstream = nothing, downstream = cluster-1, host-1
	nodeIDs := nodeIDs(resp)
	if len(nodeIDs) != 3 {
		t.Errorf("got %d nodes, want 3: %v", len(nodeIDs), nodeIDs)
	}
}

func TestBuildTopology_RelationTypeFilter(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:       "cluster-1",
		Depth:        1,
		Direction:    model.TopologyDirectionBoth,
		RelationType: model.RelationTypeMemberOf,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only member_of relations: instance-1, instance-2
	nodeIDs := nodeIDs(resp)
	if len(nodeIDs) != 3 {
		t.Errorf("got %d nodes, want 3 (cluster-1, instance-1, instance-2): %v", len(nodeIDs), nodeIDs)
	}
	for _, e := range resp.Edges {
		if e.RelationType != model.RelationTypeMemberOf {
			t.Errorf("edge relation type = %q, want member_of", e.RelationType)
		}
	}
}

func TestBuildTopology_CyclicGraphNoLoop(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[string]model.Resource{
			"a": {ID: "a", ResourceType: model.ResourceTypeHost, Name: "a", DisplayName: "A"},
			"b": {ID: "b", ResourceType: model.ResourceTypeHost, Name: "b", DisplayName: "B"},
			"c": {ID: "c", ResourceType: model.ResourceTypeHost, Name: "c", DisplayName: "C"},
		},
		relations: []model.ResourceRelation{
			{ID: "r1", FromResourceID: "a", ToResourceID: "b", RelationType: model.RelationTypeDependsOn},
			{ID: "r2", FromResourceID: "b", ToResourceID: "c", RelationType: model.RelationTypeDependsOn},
			{ID: "r3", FromResourceID: "c", ToResourceID: "a", RelationType: model.RelationTypeDependsOn},
		},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "a",
		Depth:     2,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not loop infinitely and should deduplicate
	nodeIDs := nodeIDs(resp)
	if len(nodeIDs) != 3 {
		t.Errorf("got %d nodes, want 3: %v", len(nodeIDs), nodeIDs)
	}
}

func TestBuildTopology_MissingRoot(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "nonexistent",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Errorf("err = %v, want ErrResourceNotFound", err)
	}
}

func TestBuildTopology_InvalidDepth(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     3,
		Direction: model.TopologyDirectionBoth,
	})
	if !errors.Is(err, ErrInvalidDepth) {
		t.Errorf("err = %v, want ErrInvalidDepth", err)
	}

	_, err = svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     0,
		Direction: model.TopologyDirectionBoth,
	})
	if !errors.Is(err, ErrInvalidDepth) {
		t.Errorf("err = %v, want ErrInvalidDepth", err)
	}
}

func TestBuildTopology_InvalidDirection(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	_, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     1,
		Direction: "invalid",
	})
	if !errors.Is(err, ErrInvalidDirection) {
		t.Errorf("err = %v, want ErrInvalidDirection", err)
	}
}

func TestBuildTopology_GroupsByResourceType(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	groupMap := map[model.ResourceType]model.TopologyGroup{}
	for _, g := range resp.Groups {
		groupMap[g.ResourceType] = g
	}

	if g, ok := groupMap[model.ResourceTypeDatabaseCluster]; !ok {
		t.Error("missing database_cluster group")
	} else {
		if len(g.NodeIDs) != 1 || g.NodeIDs[0] != "cluster-1" {
			t.Errorf("database_cluster group nodes = %v, want [cluster-1]", g.NodeIDs)
		}
	}

	if g, ok := groupMap[model.ResourceTypeDatabaseInstance]; !ok {
		t.Error("missing database_instance group")
	} else {
		if len(g.NodeIDs) != 2 {
			t.Errorf("database_instance group count = %d, want 2", len(g.NodeIDs))
		}
	}
}

func TestBuildTopology_DeterministicOrdering(t *testing.T) {
	repo := buildTestRepo()
	svc := NewTopologyService(repo)

	resp1, _ := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	resp2, _ := svc.BuildTopology(model.TopologyQuery{
		RootID:    "cluster-1",
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})

	for i := range resp1.Nodes {
		if resp1.Nodes[i].ID != resp2.Nodes[i].ID {
			t.Errorf("node ordering differs at index %d: %q vs %q", i, resp1.Nodes[i].ID, resp2.Nodes[i].ID)
		}
	}
	for i := range resp1.Edges {
		if resp1.Edges[i].ID != resp2.Edges[i].ID {
			t.Errorf("edge ordering differs at index %d: %q vs %q", i, resp1.Edges[i].ID, resp2.Edges[i].ID)
		}
	}
}

func nodeIDs(resp *model.TopologyResponse) map[string]bool {
	m := make(map[string]bool, len(resp.Nodes))
	for _, n := range resp.Nodes {
		m[n.ID] = true
	}
	return m
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
	nodes := []model.TopologyNode{
		{ID: "a", DisplayName: "A", HealthStatus: "healthy", LifecycleStatus: "running"},
	}
	summaries := buildProblemSummaries(nodes)
	if len(summaries) != 0 {
		t.Fatalf("expected 0 summaries for healthy node, got %d", len(summaries))
	}
}

func TestBuildProblemSummaries_WorstSeverity(t *testing.T) {
	nodes := []model.TopologyNode{
		{
			ID: "a", DisplayName: "A", ResourceType: model.ResourceTypeDatabaseInstance,
			HealthStatus: "healthy", LifecycleStatus: "running",
			Problems: []model.TopologyProblem{
				{Severity: "warning", Code: "health_warning"},
				{Severity: "critical", Code: "lifecycle_stopped"},
			},
		},
	}
	summaries := buildProblemSummaries(nodes)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Severity != "critical" {
		t.Errorf("severity = %q, want critical", summaries[0].Severity)
	}
}

func TestBuildTopology_ProfileEnrichment(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[string]model.Resource{
			"inst-1": {
				ID: "inst-1", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "mysql-1", DisplayName: "MySQL 1",
				HealthStatus: "healthy", LifecycleStatus: "running",
				ProfileSummary: &model.ProfileSummary{
					Hostname: "db-host-01.internal",
					IP:       "10.0.10.20",
					Port:     3306,
				},
			},
		},
		relations: []model.ResourceRelation{},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "inst-1", Depth: 1, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(resp.Nodes))
	}
	n := resp.Nodes[0]
	if n.Hostname != "db-host-01.internal" {
		t.Errorf("hostname = %q, want db-host-01.internal", n.Hostname)
	}
	if n.IP != "10.0.10.20" {
		t.Errorf("ip = %q, want 10.0.10.20", n.IP)
	}
	if n.Port != 3306 {
		t.Errorf("port = %d, want 3306", n.Port)
	}
}

func TestBuildTopology_ProblemSummary(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[string]model.Resource{
			"inst-ok": {
				ID: "inst-ok", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "ok", DisplayName: "OK Instance",
				HealthStatus: "healthy", LifecycleStatus: "running",
			},
			"inst-bad": {
				ID: "inst-bad", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "bad", DisplayName: "Bad Instance",
				HealthStatus: "critical", LifecycleStatus: "stopped",
			},
		},
		relations: []model.ResourceRelation{},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "inst-bad", Depth: 1, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Problems) != 1 {
		t.Fatalf("problems = %d, want 1", len(resp.Problems))
	}
	p := resp.Problems[0]
	if p.ResourceID != "inst-bad" {
		t.Errorf("resourceId = %q, want inst-bad", p.ResourceID)
	}
	if p.Severity != "critical" {
		t.Errorf("severity = %q, want critical", p.Severity)
	}
	if len(p.Problems) != 2 {
		t.Errorf("problem count = %d, want 2", len(p.Problems))
	}
}
