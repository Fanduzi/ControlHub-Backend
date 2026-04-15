//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"

	_ "github.com/go-sql-driver/mysql"
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

func TestTopology_SeedData_PaymentMySQLProductionChain(t *testing.T) {
	db := setupTestDB(t)
	relRepo := mysql.NewRelationRepository(db)

	// Payment MySQL production resource IDs from seed data.
	const (
		clusterID     = "41000000-0000-0000-0000-000000000010"
		primaryID     = "41000000-0000-0000-0000-000000000022"
		replicaID     = "41000000-0000-0000-0000-000000000023"
		activeProxyID = "41000000-0000-0000-0000-000000000041"
		standbyProxyID = "41000000-0000-0000-0000-000000000044"
		vipID         = "41000000-0000-0000-0000-000000000051"
		domainID      = "41000000-0000-0000-0000-000000000061"
	)

	// Build depth=2 topology from the payment cluster.
	topoSvc := service.NewTopologyService(relRepo)
	resp, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:    clusterID,
		Depth:     2,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("build topology for payment cluster: %v", err)
	}

	// Index edges by relation type for assertion.
	edgesByType := map[string][]model.TopologyEdge{}
	for _, e := range resp.Edges {
		edgesByType[string(e.RelationType)] = append(edgesByType[string(e.RelationType)], e)
	}

	// 1. Primary instance is member_of cluster.
	found := false
	for _, e := range edgesByType["member_of"] {
		if e.FromResourceID == primaryID && e.ToResourceID == clusterID {
			found = true
		}
	}
	if !found {
		t.Error("primary instance should be member_of payment cluster")
	}

	// 2. Replica instance is member_of cluster.
	found = false
	for _, e := range edgesByType["member_of"] {
		if e.FromResourceID == replicaID && e.ToResourceID == clusterID {
			found = true
		}
	}
	if !found {
		t.Error("replica instance should be member_of payment cluster")
	}

	// 3. Primary replicates_to replica (explicit replication topology).
	found = false
	for _, e := range edgesByType["replicates_to"] {
		if e.FromResourceID == primaryID && e.ToResourceID == replicaID {
			found = true
		}
	}
	if !found {
		t.Error("primary should have replicates_to relation to replica")
	}

	// 4. Active proxy fronts the cluster.
	found = false
	for _, e := range edgesByType["fronts"] {
		if e.FromResourceID == activeProxyID && e.ToResourceID == clusterID {
			found = true
		}
	}
	if !found {
		t.Error("active proxy should front payment cluster")
	}

	// 5. Standby proxy also fronts the cluster.
	found = false
	for _, e := range edgesByType["fronts"] {
		if e.FromResourceID == standbyProxyID && e.ToResourceID == clusterID {
			found = true
		}
	}
	if !found {
		t.Error("standby proxy should front payment cluster")
	}

	// 6. Standby proxy appears as a topology node.
	nodeByID := map[string]bool{}
	for _, n := range resp.Nodes {
		nodeByID[n.ID] = true
	}
	if !nodeByID[standbyProxyID] {
		t.Error("standby proxy should appear in topology nodes")
	}

	// 7. VIP fronts the active proxy.
	found = false
	for _, e := range edgesByType["fronts"] {
		if e.FromResourceID == vipID && e.ToResourceID == activeProxyID {
			found = true
		}
	}
	if !found {
		t.Error("VIP should front active proxy")
	}

	// 8. Domain->VIP verified at depth=1 from VIP (domain is 3 hops from cluster).
	resp2, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:    vipID,
		Depth:     1,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("build topology for payment VIP: %v", err)
	}
	found = false
	for _, e := range resp2.Edges {
		if e.FromResourceID == domainID && e.ToResourceID == vipID && string(e.RelationType) == "points_to" {
			found = true
		}
	}
	if !found {
		t.Error("domain should point_to VIP")
	}
}

func TestSeedData_NoMigrationArtifactsInDisplayNames(t *testing.T) {
	db := setupTestDB(t)

	// Verify no display names contain operational status that belongs in
	// health/lifecycle fields rather than the user-facing name.
	rows, err := db.Query(`
		SELECT id, display_name
		FROM resources
		WHERE display_name LIKE '%Currently Disabled%'
		   OR display_name LIKE '%Replication Lag Warning%'
		   OR display_name LIKE '%Critical Disk Pressure%'
		   OR display_name LIKE '%High Storage Density%'
		   OR display_name LIKE '%- Production Primary Endpoint'
	`)
	if err != nil {
		t.Fatalf("query display names: %v", err)
	}
	defer rows.Close()

	var artifacts []string
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			t.Fatalf("scan row: %v", err)
		}
		artifacts = append(artifacts, fmt.Sprintf("%s: %s", id, name))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate rows: %v", err)
	}

	if len(artifacts) > 0 {
		t.Errorf("found operational-status artifacts in display names (use health/lifecycle fields instead):\n  %s",
			strings.Join(artifacts, "\n  "))
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
