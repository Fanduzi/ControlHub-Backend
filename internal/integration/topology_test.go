//go:build integration

// Package integration verifies topology behavior against disposable MySQL.
// input: internal/model, internal/repository/mysql, internal/service, Testcontainers-backed MySQL
// output: topology service and bounded repository integration tests, including candidate overflow and effective health
// pos: Proves topology traversal, cap-plus-sentinel candidates, and bounded multi-observer health semantics against real MySQL
// note: if this file changes, update this header and module README.md.

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
func createTopologyFixturesWithName(t *testing.T, resRepo *mysql.ResourceRepository, relRepo *mysql.RelationRepository, ctx context.Context, prefix string) (uint64, uint64, uint64) {
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

func TestRelationRepositoryListTopologyCandidates(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	serviceRes := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-service", model.ResourceTypeService, "api", envProd, model.HealthStatusHealthy)
	cluster := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-cluster", model.ResourceTypeDatabaseCluster, "mysql", envProd, model.HealthStatusHealthy)
	proxy := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-proxy", model.ResourceTypeDatabaseProxy, "proxysql", envProd, model.HealthStatusHealthy)
	warningHost := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-warning-host", model.ResourceTypeHost, "vm", envProd, model.HealthStatusWarning)
	observedWarningHost := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-observed-warning-host", model.ResourceTypeHost, "vm", envProd, model.HealthStatusHealthy)
	if err := resRepo.UpsertHealthObservation(ctx, observedWarningHost.ID, model.HealthObservation{
		Status: model.HealthStatusWarning, ObservedAt: time.Now(), Observer: "topology-test",
	}); err != nil {
		t.Fatalf("observe warning host: %v", err)
	}
	staleWarningHost := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-stale-warning-host", model.ResourceTypeHost, "vm", envProd, model.HealthStatusHealthy)
	if err := resRepo.UpsertHealthObservation(ctx, staleWarningHost.ID, model.HealthObservation{
		Status: model.HealthStatusWarning, ObservedAt: time.Now().Add(-model.DefaultHealthFreshnessThreshold - time.Minute), Observer: "topology-test",
	}); err != nil {
		t.Fatalf("observe stale warning host: %v", err)
	}
	healthyHost := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-healthy-host", model.ResourceTypeHost, "vm", envProd, model.HealthStatusHealthy)
	otherEnvService := createTopologyCandidateResource(t, resRepo, ctx, "topo-candidate-other-env-service", model.ResourceTypeService, "api", 2, model.HealthStatusHealthy)

	items, err := relRepo.ListTopologyCandidates(envProd, service.TopologyNodeCap+1)
	if err != nil {
		t.Fatalf("list topology candidates: %v", err)
	}

	got := map[uint64]bool{}
	for _, item := range items {
		got[item.ID] = true
	}
	for _, id := range []uint64{serviceRes.ID, cluster.ID, proxy.ID, warningHost.ID, observedWarningHost.ID} {
		if !got[id] {
			t.Fatalf("missing candidate %d in %v", id, got)
		}
	}
	for _, id := range []uint64{healthyHost.ID, staleWarningHost.ID, otherEnvService.ID} {
		if got[id] {
			t.Fatalf("unexpected candidate %d in %v", id, got)
		}
	}
}

func TestTopologyCandidatesHighCardinalityUsesSentinel(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	for i := 0; i <= service.TopologyNodeCap; i++ {
		createTopologyCandidateResource(t, resRepo, ctx, fmt.Sprintf("topo-bounded-%03d", i), model.ResourceTypeService, "api", envProd, model.HealthStatusHealthy)
	}

	items, err := relRepo.ListTopologyCandidates(envProd, service.TopologyNodeCap+1)
	if err != nil {
		t.Fatalf("list bounded topology candidates: %v", err)
	}
	if len(items) != service.TopologyNodeCap+1 {
		t.Fatalf("candidates = %d, want sentinel size %d", len(items), service.TopologyNodeCap+1)
	}
	typed := func(item model.Resource) bool {
		return item.ResourceType == model.ResourceTypeService || item.ResourceType == model.ResourceTypeDatabaseCluster || item.ResourceType == model.ResourceTypeDatabaseProxy
	}
	for i := 1; i < len(items); i++ {
		if !typed(items[i-1]) && typed(items[i]) {
			t.Fatalf("typed candidate %q followed abnormal-only candidates", items[i].Name)
		}
		if typed(items[i-1]) == typed(items[i]) && (items[i-1].Name > items[i].Name || items[i-1].Name == items[i].Name && items[i-1].ID > items[i].ID) {
			t.Fatalf("candidate order is not deterministic at %q then %q", items[i-1].Name, items[i].Name)
		}
	}

	resp, err := service.NewTopologyService(relRepo).BuildTopology(model.TopologyQuery{
		EnvironmentID: envProd, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("build environment topology: %v", err)
	}
	if len(resp.Nodes) != service.TopologyNodeCap {
		t.Fatalf("nodes = %d, want cap %d", len(resp.Nodes), service.TopologyNodeCap)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want sentinel overflow to propagate")
	}
}

func TestTopologyCandidateHealthProjection(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	freshWorst := createTopologyCandidateResource(t, resRepo, ctx, "000-topo-health-fresh-worst", model.ResourceTypeService, "api", envProd, model.HealthStatusHealthy)
	criticalAt := now.Add(-2 * time.Hour)
	for _, observation := range []model.HealthObservation{
		{Status: model.HealthStatusHealthy, ObservedAt: now.Add(-time.Hour), Observer: "newer-healthy"},
		{Status: model.HealthStatusCritical, ObservedAt: criticalAt, Observer: "z-critical"},
		{Status: model.HealthStatusCritical, ObservedAt: criticalAt, Observer: "a-critical"},
	} {
		if err := resRepo.UpsertHealthObservation(ctx, freshWorst.ID, observation); err != nil {
			t.Fatalf("observe fresh worst candidate: %v", err)
		}
	}

	staleNewest := createTopologyCandidateResource(t, resRepo, ctx, "000-topo-health-stale-newest", model.ResourceTypeService, "api", envProd, model.HealthStatusHealthy)
	newestStaleAt := now.Add(-model.DefaultHealthFreshnessThreshold - time.Hour)
	for _, observation := range []model.HealthObservation{
		{Status: model.HealthStatusCritical, ObservedAt: newestStaleAt.Add(-time.Hour), Observer: "older-stale"},
		{Status: model.HealthStatusWarning, ObservedAt: newestStaleAt, Observer: "newer-stale"},
	} {
		if err := resRepo.UpsertHealthObservation(ctx, staleNewest.ID, observation); err != nil {
			t.Fatalf("observe stale candidate: %v", err)
		}
	}

	manualOverride := createTopologyCandidateResource(t, resRepo, ctx, "000-topo-health-manual-override", model.ResourceTypeService, "api", envProd, model.HealthStatusCritical)
	if err := resRepo.UpsertHealthObservation(ctx, manualOverride.ID, model.HealthObservation{
		Status: model.HealthStatusWarning, ObservedAt: now, Observer: "fresh-warning",
	}); err != nil {
		t.Fatalf("observe manual override candidate: %v", err)
	}

	items, err := relRepo.ListTopologyCandidates(envProd, service.TopologyNodeCap+1)
	if err != nil {
		t.Fatalf("list topology candidates: %v", err)
	}
	byID := make(map[uint64]model.Resource, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	if item := byID[freshWorst.ID]; item.HealthStatus != string(model.HealthStatusCritical) || item.HealthObserver != "a-critical" || item.HealthObservedAt == nil || !item.HealthObservedAt.Equal(criticalAt) {
		t.Fatalf("fresh worst = (%q, %q, %v), want critical/a-critical/%v", item.HealthStatus, item.HealthObserver, item.HealthObservedAt, criticalAt)
	}
	if item := byID[staleNewest.ID]; item.HealthStatus != string(model.HealthStatusUnknown) || item.HealthFreshness != model.HealthFreshnessStale || item.HealthObserver != "newer-stale" || item.HealthObservedAt == nil || !item.HealthObservedAt.Equal(newestStaleAt) {
		t.Fatalf("stale newest = (%q, %q, %q, %v)", item.HealthStatus, item.HealthFreshness, item.HealthObserver, item.HealthObservedAt)
	}
	if item := byID[manualOverride.ID]; item.HealthStatus != string(model.HealthStatusCritical) || item.HealthObserver != "fresh-warning" {
		t.Fatalf("manual override = (%q, %q), want critical/fresh-warning", item.HealthStatus, item.HealthObserver)
	}
}

func createTopologyCandidateResource(t *testing.T, repo *mysql.ResourceRepository, ctx context.Context, name string, resourceType model.ResourceType, subtype string, environmentID uint64, health model.HealthStatus) *model.Resource {
	t.Helper()
	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    resourceType,
		ResourceSubtype: subtype,
		Name:            name,
		DisplayName:     name,
		EnvironmentID:   environmentID,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    health,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return created
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
	clusterID := lookupResourceIDByName(t, db, "payment-mysql-cluster-prod")
	primaryID := lookupResourceIDByName(t, db, "payment-mysql-primary-prod")
	replicaID := lookupResourceIDByName(t, db, "payment-mysql-replica-01-prod")
	activeProxyID := lookupResourceIDByName(t, db, "payment-proxysql-prod")
	standbyProxyID := lookupResourceIDByName(t, db, "payment-proxysql-02-prod")
	vipID := lookupResourceIDByName(t, db, "payment-vip-prod")
	domainID := lookupResourceIDByName(t, db, "api.payment.internal")

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
	nodeByID := map[uint64]bool{}
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

func TestTopology_SeedData_SemanticMetadata(t *testing.T) {
	db := setupTestDB(t)
	relRepo := mysql.NewRelationRepository(db)

	// Payment MySQL production resource IDs from seed data.
	clusterID := lookupResourceIDByName(t, db, "payment-mysql-cluster-prod")
	primaryID := lookupResourceIDByName(t, db, "payment-mysql-primary-prod")
	replicaID := lookupResourceIDByName(t, db, "payment-mysql-replica-01-prod")
	activeProxyID := lookupResourceIDByName(t, db, "payment-proxysql-prod")
	standbyProxyID := lookupResourceIDByName(t, db, "payment-proxysql-02-prod")

	topoSvc := service.NewTopologyService(relRepo)
	resp, err := topoSvc.BuildTopology(model.TopologyQuery{
		RootID:    clusterID,
		Depth:     2,
		Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("build topology for payment cluster: %v", err)
	}

	// Response must have isDatabaseTopology=true.
	if !resp.IsDatabaseTopology {
		t.Error("expected isDatabaseTopology=true for payment cluster topology")
	}

	nodeMap := map[uint64]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Cluster node: role=cluster, layer=cluster
	if n, ok := nodeMap[clusterID]; !ok {
		t.Fatal("missing cluster node")
	} else {
		if n.TopologyRole != model.TopologyRoleCluster {
			t.Errorf("cluster role = %q, want cluster", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerCluster {
			t.Errorf("cluster layer = %q, want cluster", n.TopologyLayer)
		}
		if n.VisualImportance != 10 {
			t.Errorf("cluster (root) visual importance = %d, want 10", n.VisualImportance)
		}
	}

	// Primary instance: role=primary, layer=replication, groupKey set, replicationDepth=0
	if n, ok := nodeMap[primaryID]; !ok {
		t.Fatal("missing primary instance node")
	} else {
		if n.TopologyRole != model.TopologyRolePrimary {
			t.Errorf("primary role = %q, want primary", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerReplication {
			t.Errorf("primary layer = %q, want replication", n.TopologyLayer)
		}
		if n.GroupKey == "" {
			t.Error("primary should have groupKey via member_of to cluster")
		}
		if n.ReplicationDepth != 0 {
			t.Errorf("primary replicationDepth = %d, want 0", n.ReplicationDepth)
		}
	}

	// Replica instance: role=replica, replicationDepth=1
	if n, ok := nodeMap[replicaID]; !ok {
		t.Fatal("missing replica instance node")
	} else {
		if n.TopologyRole != model.TopologyRoleReplica {
			t.Errorf("replica role = %q, want replica", n.TopologyRole)
		}
		if n.ReplicationDepth != 1 {
			t.Errorf("replica replicationDepth = %d, want 1", n.ReplicationDepth)
		}
		if n.ReplicationParentID == nil || *n.ReplicationParentID != primaryID {
			t.Errorf("replica replicationParentId = %v, want %d", n.ReplicationParentID, primaryID)
		}
	}

	// Active proxy: role=proxy_active, layer=entry
	if n, ok := nodeMap[activeProxyID]; !ok {
		t.Fatal("missing active proxy node")
	} else {
		if n.TopologyRole != model.TopologyRoleProxyActive {
			t.Errorf("active proxy role = %q, want proxy_active", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("active proxy layer = %q, want entry", n.TopologyLayer)
		}
	}

	// Standby proxy: role=proxy_standby, layer=entry
	if n, ok := nodeMap[standbyProxyID]; !ok {
		t.Fatal("missing standby proxy node")
	} else {
		if n.TopologyRole != model.TopologyRoleProxyStandby {
			t.Errorf("standby proxy role = %q, want proxy_standby", n.TopologyRole)
		}
	}

	// Edge semantic types

	// replicates_to edge → semantic=replication
	for _, e := range resp.Edges {
		if e.RelationType == model.RelationTypeReplicatesTo {
			if e.SemanticType != model.EdgeSemanticReplication {
				t.Errorf("replicates_to edge semantic = %q, want replication", e.SemanticType)
			}
		}
	}

	// member_of edge → semantic=membership
	for _, e := range resp.Edges {
		if e.RelationType == model.RelationTypeMemberOf {
			if e.SemanticType != model.EdgeSemanticMembership {
				t.Errorf("member_of edge semantic = %q, want membership", e.SemanticType)
			}
		}
	}

	// Standby proxy fronts edge → semantic=failover
	for _, e := range resp.Edges {
		if e.RelationType == model.RelationTypeFronts && e.FromResourceID == standbyProxyID {
			if e.SemanticType != model.EdgeSemanticFailover {
				t.Errorf("standby proxy fronts edge semantic = %q, want failover", e.SemanticType)
			}
		}
	}

	// Active proxy fronts edge → semantic=traffic
	for _, e := range resp.Edges {
		if e.RelationType == model.RelationTypeFronts && e.FromResourceID == activeProxyID {
			if e.SemanticType != model.EdgeSemanticTraffic {
				t.Errorf("active proxy fronts edge semantic = %q, want traffic", e.SemanticType)
			}
		}
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
	instanceID := lookupResourceIDByName(t, db, "order-mysql-01-prod")

	relations, err := relRepo.ListRelationsByResourceIDs([]uint64{instanceID})
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
