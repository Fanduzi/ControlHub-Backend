//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// createTopologyFixtures creates a 3-node chain with unique name prefix:
//
//	host-A --runs_on--> db-instance-B --member_of--> db-cluster-C
//
// Returns (hostA_ID, instanceB_ID, clusterC_ID).
func createTopologyFixturesWithName(t *testing.T, resRepo *mysql.ResourceRepository, relRepo *mysql.RelationRepository, ctx context.Context, prefix string) (string, string, string) {
	t.Helper()

	hostA, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            prefix + "-host-a",
		DisplayName:     prefix + " Host A",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create host A: %v", err)
	}

	instanceB, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            prefix + "-instance-b",
		DisplayName:     prefix + " Instance B",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create instance B: %v", err)
	}

	clusterC, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseCluster,
		ResourceSubtype: "mysql",
		Name:            prefix + "-cluster-c",
		DisplayName:     prefix + " Cluster C",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create cluster C: %v", err)
	}

	// host-A --runs_on--> instance-B
	_, err = relRepo.CreateRelation(ctx, model.RelationCreateInput{
		FromResourceID: hostA.ID,
		ToResourceID:   instanceB.ID,
		RelationType:   model.RelationTypeRunsOn,
	})
	if err != nil {
		t.Fatalf("create relation A->B: %v", err)
	}

	// instance-B --member_of--> cluster-C
	_, err = relRepo.CreateRelation(ctx, model.RelationCreateInput{
		FromResourceID: instanceB.ID,
		ToResourceID:   clusterC.ID,
		RelationType:   model.RelationTypeMemberOf,
	})
	if err != nil {
		t.Fatalf("create relation B->C: %v", err)
	}

	return hostA.ID, instanceB.ID, clusterC.ID
}

func TestTopology_Depth1_ReturnsDirectNeighbors(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	idA, _, _ := createTopologyFixturesWithName(t, resRepo, relRepo, ctx, "topo-d1")

	topoSvc := service.NewTopologyService(relRepo)

	resp, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:    idA,
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("build topology: %v", err)
	}

	// Should have root (A) + 1 direct neighbor (B) = 2 nodes.
	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes count = %d, want 2", len(resp.Nodes))
	}

	// Should have 1 edge (A -> B).
	if len(resp.Edges) != 1 {
		t.Fatalf("edges count = %d, want 1", len(resp.Edges))
	}

	// Root node check.
	rootFound := false
	for _, n := range resp.Nodes {
		if n.ID == idA && n.IsRoot {
			rootFound = true
		}
	}
	if !rootFound {
		t.Error("root node not found or not marked as root")
	}
}

func TestTopology_Depth2_ReturnsSecondHop(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	idA, _, idC := createTopologyFixturesWithName(t, resRepo, relRepo, ctx, "topo-d2")

	topoSvc := service.NewTopologyService(relRepo)

	resp, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:    idA,
		Depth:     2,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("build topology: %v", err)
	}

	// Should have A + B + C = 3 nodes.
	if len(resp.Nodes) != 3 {
		t.Fatalf("nodes count = %d, want 3", len(resp.Nodes))
	}

	// Should have 2 edges.
	if len(resp.Edges) != 2 {
		t.Fatalf("edges count = %d, want 2", len(resp.Edges))
	}

	// Cluster C should be at distance 2.
	for _, n := range resp.Nodes {
		if n.ID == idC && n.Distance != 2 {
			t.Errorf("cluster C distance = %d, want 2", n.Distance)
		}
	}
}

func TestTopology_DirectionDownstream(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	idA, _, _ := createTopologyFixturesWithName(t, resRepo, relRepo, ctx, "topo-ds")

	topoSvc := service.NewTopologyService(relRepo)

	resp, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:    idA,
		Depth:     1,
		Direction: model.TopologyDirectionDownstream,
	})
	if err != nil {
		t.Fatalf("build topology: %v", err)
	}

	// Downstream from A means A is from_resource, so it looks for edges where A is fromResourceId.
	// The edge is host-A --runs_on--> instance-B, so A is the from side, B is downstream.
	if len(resp.Edges) != 1 {
		t.Fatalf("downstream edges = %d, want 1", len(resp.Edges))
	}
}

func TestTopology_RelationTypeFilter(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	_, idB, _ := createTopologyFixturesWithName(t, resRepo, relRepo, ctx, "topo-rt")

	topoSvc := service.NewTopologyService(relRepo)

	// Filter to only member_of relations. B has both runs_on (incoming) and member_of (outgoing).
	resp, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:       idB,
		Depth:        1,
		Direction:    model.TopologyDirectionBoth,
		RelationType: model.RelationTypeMemberOf,
	})
	if err != nil {
		t.Fatalf("build topology: %v", err)
	}

	// Only member_of edge should appear.
	for _, e := range resp.Edges {
		if e.RelationType != model.RelationTypeMemberOf {
			t.Errorf("unexpected edge type %q, want only member_of", e.RelationType)
		}
	}

	// Should have at least 1 member_of edge.
	if len(resp.Edges) < 1 {
		t.Fatal("expected at least 1 member_of edge")
	}
}

func TestTopology_SeedData_NeighborhoodQuery(t *testing.T) {
	db := setupTestDB(t)
	relRepo := mysql.NewRelationRepository(db)

	// Use seed data: resource 40000000-0000-0000-0000-000000000002 (order-mysql-01-prod)
	// has relations:
	//   - 50000000-0000-0000-0000-000000000001: order-api -> instance (depends_on) [instance is to_resource]
	//   - 50000000-0000-0000-0000-000000000002: instance -> cluster (member_of) [instance is from_resource]
	//   - 50000000-0000-0000-0000-000000000003: instance -> host (runs_on) [instance is from_resource]
	instanceID := "40000000-0000-0000-0000-000000000002"

	relations, err := relRepo.ListRelationsByResourceIDs([]string{instanceID})
	if err != nil {
		t.Fatalf("list relations for seed instance: %v", err)
	}

	if len(relations) < 3 {
		t.Fatalf("expected at least 3 relations for seed instance, got %d", len(relations))
	}

	// Verify both incoming and outgoing edges are present.
	relationTypes := map[string]bool{}
	for _, r := range relations {
		relationTypes[string(r.RelationType)] = true
	}
	for _, expected := range []string{"depends_on", "member_of", "runs_on"} {
		if !relationTypes[expected] {
			t.Errorf("missing relation type %q in results", expected)
		}
	}
}
