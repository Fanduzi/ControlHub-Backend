package service

import (
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// buildDatabaseTestRepo creates a repo with a full Payment MySQL-style topology
// for testing semantic classification.
//
// Topology structure:
//
//	domain → vip → proxy-active → cluster
//	                         proxy-standby ↗
//	service ──────────────→ proxy-active
//	primary-instance → cluster (member_of)
//	replica-instance → cluster (member_of)
//	primary → replica (replicates_to)
//	primary → host (runs_on)
//	replica → host2 (runs_on)
//	orchestrator → cluster (manages)
//	ha-manager → cluster (manages)
func buildDatabaseTestRepo() *fakeTopologyRepo {
	return &fakeTopologyRepo{
		resources: map[string]model.Resource{
			"cluster-1": {
				ID: "cluster-1", ResourceType: model.ResourceTypeDatabaseCluster,
				ResourceSubtype: "mysql", Name: "payment-mysql-cluster-prod", DisplayName: "Payment MySQL Cluster Prod",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"primary-1": {
				ID: "primary-1", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "payment-mysql-primary-prod", DisplayName: "Payment MySQL Primary",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"replica-1": {
				ID: "replica-1", ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "payment-mysql-replica-01-prod", DisplayName: "Payment MySQL Replica 01",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "warning",
			},
			"host-1": {
				ID: "host-1", ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "prod-db-host-02", DisplayName: "Production DB Host 02",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"host-2": {
				ID: "host-2", ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "prod-db-host-03", DisplayName: "Production DB Host 03",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"service-1": {
				ID: "service-1", ResourceType: model.ResourceTypeService,
				ResourceSubtype: "api", Name: "payment-api-prod", DisplayName: "Payment API Prod",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"proxy-active": {
				ID: "proxy-active", ResourceType: model.ResourceTypeDatabaseProxy,
				ResourceSubtype: "proxysql", Name: "payment-proxysql-prod", DisplayName: "Payment ProxySQL Active",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "running", HealthStatus: "healthy",
				Labels: map[string]string{"team": "platform", "tier": "proxy"},
			},
			"proxy-standby": {
				ID: "proxy-standby", ResourceType: model.ResourceTypeDatabaseProxy,
				ResourceSubtype: "proxysql", Name: "payment-proxysql-02-prod", DisplayName: "Payment ProxySQL Standby",
				EnvironmentID: "env-prod", OwnerID: "owner-dba",
				LifecycleStatus: "stopped", HealthStatus: "unknown",
				Labels: map[string]string{"team": "platform", "tier": "proxy", "role": "standby"},
			},
			"vip-1": {
				ID: "vip-1", ResourceType: model.ResourceTypeVirtualIP,
				ResourceSubtype: "floating", Name: "payment-vip-prod", DisplayName: "Payment VIP Prod",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"domain-1": {
				ID: "domain-1", ResourceType: model.ResourceTypeDomainName,
				ResourceSubtype: "dns", Name: "api.payment.internal", DisplayName: "Payment API Endpoint",
				EnvironmentID: "env-prod", OwnerID: "owner-payment",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"orchestrator-1": {
				ID: "orchestrator-1", ResourceType: model.ResourceTypeControlPlaneComponent,
				ResourceSubtype: "orchestrator", Name: "db-orchestrator-prod", DisplayName: "DB Orchestrator",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"ha-manager-1": {
				ID: "ha-manager-1", ResourceType: model.ResourceTypeControlPlaneComponent,
				ResourceSubtype: "ha", Name: "ha-manager-prod", DisplayName: "HA Manager",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
		},
		relations: []model.ResourceRelation{
			{ID: "rel-member-primary", FromResourceID: "primary-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeMemberOf},
			{ID: "rel-member-replica", FromResourceID: "replica-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeMemberOf},
			{ID: "rel-replication", FromResourceID: "primary-1", ToResourceID: "replica-1", RelationType: model.RelationTypeReplicatesTo},
			{ID: "rel-runs-primary", FromResourceID: "primary-1", ToResourceID: "host-1", RelationType: model.RelationTypeRunsOn},
			{ID: "rel-runs-replica", FromResourceID: "replica-1", ToResourceID: "host-2", RelationType: model.RelationTypeRunsOn},
			{ID: "rel-dep-service", FromResourceID: "service-1", ToResourceID: "proxy-active", RelationType: model.RelationTypeDependsOn},
			{ID: "rel-fronts-active", FromResourceID: "proxy-active", ToResourceID: "cluster-1", RelationType: model.RelationTypeFronts},
			{ID: "rel-fronts-standby", FromResourceID: "proxy-standby", ToResourceID: "cluster-1", RelationType: model.RelationTypeFronts},
			{ID: "rel-fronts-vip", FromResourceID: "vip-1", ToResourceID: "proxy-active", RelationType: model.RelationTypeFronts},
			{ID: "rel-points-domain", FromResourceID: "domain-1", ToResourceID: "proxy-active", RelationType: model.RelationTypePointsTo},
			{ID: "rel-manages-orch", FromResourceID: "orchestrator-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeManages},
			{ID: "rel-manages-ha", FromResourceID: "ha-manager-1", ToResourceID: "cluster-1", RelationType: model.RelationTypeManages},
		},
	}
}

func TestSemanticClassification_DatabaseClusterRole(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Cluster node should have role=cluster, layer=cluster
	if n, ok := nodeMap["cluster-1"]; !ok {
		t.Fatal("missing cluster-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleCluster {
			t.Errorf("cluster-1 role = %q, want cluster", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerCluster {
			t.Errorf("cluster-1 layer = %q, want cluster", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_PrimaryReplicaRoles(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Primary instance: role=primary, layer=replication
	if n, ok := nodeMap["primary-1"]; !ok {
		t.Fatal("missing primary-1 node")
	} else {
		if n.TopologyRole != model.TopologyRolePrimary {
			t.Errorf("primary-1 role = %q, want primary", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerReplication {
			t.Errorf("primary-1 layer = %q, want replication", n.TopologyLayer)
		}
	}

	// Replica instance: role=replica, layer=replication
	if n, ok := nodeMap["replica-1"]; !ok {
		t.Fatal("missing replica-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleReplica {
			t.Errorf("replica-1 role = %q, want replica", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerReplication {
			t.Errorf("replica-1 layer = %q, want replication", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_ReplicationDepth(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Primary has replicationDepth=0 (root of replication chain)
	if n, ok := nodeMap["primary-1"]; !ok {
		t.Fatal("missing primary-1 node")
	} else {
		if n.ReplicationDepth != 0 {
			t.Errorf("primary-1 replicationDepth = %d, want 0", n.ReplicationDepth)
		}
	}

	// Replica has replicationDepth=1, parent=primary-1
	if n, ok := nodeMap["replica-1"]; !ok {
		t.Fatal("missing replica-1 node")
	} else {
		if n.ReplicationDepth != 1 {
			t.Errorf("replica-1 replicationDepth = %d, want 1", n.ReplicationDepth)
		}
		if n.ReplicationParentID != "primary-1" {
			t.Errorf("replica-1 replicationParentId = %q, want primary-1", n.ReplicationParentID)
		}
	}
}

func TestSemanticClassification_ProxyRoles(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Active proxy: role=proxy_active, layer=entry
	if n, ok := nodeMap["proxy-active"]; !ok {
		t.Fatal("missing proxy-active node")
	} else {
		if n.TopologyRole != model.TopologyRoleProxyActive {
			t.Errorf("proxy-active role = %q, want proxy_active", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("proxy-active layer = %q, want entry", n.TopologyLayer)
		}
	}

	// Standby proxy: role=proxy_standby, layer=entry
	if n, ok := nodeMap["proxy-standby"]; !ok {
		t.Fatal("missing proxy-standby node")
	} else {
		if n.TopologyRole != model.TopologyRoleProxyStandby {
			t.Errorf("proxy-standby role = %q, want proxy_standby", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("proxy-standby layer = %q, want entry", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_ServiceAndEntryRoles(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Service: role=service, layer=application
	if n, ok := nodeMap["service-1"]; !ok {
		t.Fatal("missing service-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleService {
			t.Errorf("service-1 role = %q, want service", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerApplication {
			t.Errorf("service-1 layer = %q, want application", n.TopologyLayer)
		}
	}

	// VIP: role=entry, layer=entry
	if n, ok := nodeMap["vip-1"]; !ok {
		t.Fatal("missing vip-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleEntry {
			t.Errorf("vip-1 role = %q, want entry", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("vip-1 layer = %q, want entry", n.TopologyLayer)
		}
	}

	// Domain: role=entry, layer=entry
	if n, ok := nodeMap["domain-1"]; !ok {
		t.Fatal("missing domain-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleEntry {
			t.Errorf("domain-1 role = %q, want entry", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("domain-1 layer = %q, want entry", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_HostAndControlPlane(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Host: role=host, layer=host
	if n, ok := nodeMap["host-1"]; !ok {
		t.Fatal("missing host-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleHost {
			t.Errorf("host-1 role = %q, want host", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerHost {
			t.Errorf("host-1 layer = %q, want host", n.TopologyLayer)
		}
	}

	// Orchestrator: role=control_plane, layer=control_plane
	if n, ok := nodeMap["orchestrator-1"]; !ok {
		t.Fatal("missing orchestrator-1 node")
	} else {
		if n.TopologyRole != model.TopologyRoleControlPlane {
			t.Errorf("orchestrator-1 role = %q, want control_plane", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerControlPlane {
			t.Errorf("orchestrator-1 layer = %q, want control_plane", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_EdgeSemanticTypes(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edgeMap := map[string]model.TopologyEdge{}
	for _, e := range resp.Edges {
		edgeMap[e.ID] = e
	}

	// replicates_to → replication
	if e, ok := edgeMap["rel-replication"]; !ok {
		t.Fatal("missing rel-replication edge")
	} else if e.SemanticType != model.EdgeSemanticReplication {
		t.Errorf("replication edge semantic = %q, want replication", e.SemanticType)
	}

	// member_of → membership
	if e, ok := edgeMap["rel-member-primary"]; !ok {
		t.Fatal("missing rel-member-primary edge")
	} else if e.SemanticType != model.EdgeSemanticMembership {
		t.Errorf("member_of edge semantic = %q, want membership", e.SemanticType)
	}

	// runs_on → placement
	if e, ok := edgeMap["rel-runs-primary"]; !ok {
		t.Fatal("missing rel-runs-primary edge")
	} else if e.SemanticType != model.EdgeSemanticPlacement {
		t.Errorf("runs_on edge semantic = %q, want placement", e.SemanticType)
	}

	// fronts from active proxy → traffic
	if e, ok := edgeMap["rel-fronts-active"]; !ok {
		t.Fatal("missing rel-fronts-active edge")
	} else if e.SemanticType != model.EdgeSemanticTraffic {
		t.Errorf("active proxy fronts edge semantic = %q, want traffic", e.SemanticType)
	}

	// fronts from standby proxy → failover
	if e, ok := edgeMap["rel-fronts-standby"]; !ok {
		t.Fatal("missing rel-fronts-standby edge")
	} else if e.SemanticType != model.EdgeSemanticFailover {
		t.Errorf("standby proxy fronts edge semantic = %q, want failover", e.SemanticType)
	}

	// points_to → traffic
	if e, ok := edgeMap["rel-points-domain"]; !ok {
		t.Fatal("missing rel-points-domain edge")
	} else if e.SemanticType != model.EdgeSemanticTraffic {
		t.Errorf("points_to edge semantic = %q, want traffic", e.SemanticType)
	}

	// manages from orchestrator → management
	if e, ok := edgeMap["rel-manages-orch"]; !ok {
		t.Fatal("missing rel-manages-orch edge")
	} else if e.SemanticType != model.EdgeSemanticManagement {
		t.Errorf("orchestrator manages edge semantic = %q, want management", e.SemanticType)
	}

	// depends_on → dependency
	if e, ok := edgeMap["rel-dep-service"]; !ok {
		t.Fatal("missing rel-dep-service edge")
	} else if e.SemanticType != model.EdgeSemanticDependency {
		t.Errorf("depends_on edge semantic = %q, want dependency", e.SemanticType)
	}
}

func TestSemanticClassification_ManagesHAManagerEdge(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edgeMap := map[string]model.TopologyEdge{}
	for _, e := range resp.Edges {
		edgeMap[e.ID] = e
	}

	// manages from HA manager (subtype=ha) → monitoring
	if e, ok := edgeMap["rel-manages-ha"]; !ok {
		t.Fatal("missing rel-manages-ha edge")
	} else if e.SemanticType != model.EdgeSemanticMonitoring {
		t.Errorf("HA manager manages edge semantic = %q, want monitoring", e.SemanticType)
	}
}

func TestSemanticClassification_IsDatabaseTopology(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	// Topology rooted at a database_cluster should be database topology
	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.IsDatabaseTopology {
		t.Error("IsDatabaseTopology = false for database cluster topology, want true")
	}

	// Every node should have IsDatabaseTopology=true
	for _, n := range resp.Nodes {
		if !n.IsDatabaseTopology {
			t.Errorf("node %q IsDatabaseTopology = false, want true", n.ID)
		}
	}
}

func TestSemanticClassification_NonDatabaseTopology(t *testing.T) {
	// Build a non-database topology: just hosts with depends_on
	repo := &fakeTopologyRepo{
		resources: map[string]model.Resource{
			"host-a": {
				ID: "host-a", ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "host-a", DisplayName: "Host A",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			"host-b": {
				ID: "host-b", ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "host-b", DisplayName: "Host B",
				EnvironmentID: "env-prod", OwnerID: "owner-platform",
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
		},
		relations: []model.ResourceRelation{
			{ID: "r1", FromResourceID: "host-a", ToResourceID: "host-b", RelationType: model.RelationTypeDependsOn},
		},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "host-a", Depth: 1, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-database topology should NOT be flagged
	if resp.IsDatabaseTopology {
		t.Error("IsDatabaseTopology = true for host-only topology, want false")
	}

	// Nodes should have generic roles
	for _, n := range resp.Nodes {
		if n.IsDatabaseTopology {
			t.Errorf("node %q IsDatabaseTopology = true in non-database topology, want false", n.ID)
		}
		if n.TopologyRole != model.TopologyRoleHost {
			t.Errorf("node %q role = %q, want host", n.ID, n.TopologyRole)
		}
	}

	// depends_on edge → dependency
	if len(resp.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(resp.Edges))
	}
	if resp.Edges[0].SemanticType != model.EdgeSemanticDependency {
		t.Errorf("depends_on edge semantic = %q, want dependency", resp.Edges[0].SemanticType)
	}
}

func TestSemanticClassification_VisualImportance(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Root cluster should have highest importance
	if n, ok := nodeMap["cluster-1"]; ok {
		if n.VisualImportance < 8 {
			t.Errorf("cluster-1 visualImportance = %d, want >= 8", n.VisualImportance)
		}
	}
	// Primary should have higher importance than replica
	if p, ok := nodeMap["primary-1"]; ok {
		if r, ok2 := nodeMap["replica-1"]; ok2 {
			if p.VisualImportance <= r.VisualImportance {
				t.Errorf("primary importance (%d) should be > replica importance (%d)", p.VisualImportance, r.VisualImportance)
			}
		}
	}
	// Hosts should have lower importance
	if n, ok := nodeMap["host-1"]; ok {
		if n.VisualImportance >= 5 {
			t.Errorf("host-1 visualImportance = %d, want < 5", n.VisualImportance)
		}
	}
}

func TestSemanticClassification_GroupKey(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{
		RootID: "cluster-1", Depth: 2, Direction: model.TopologyDirectionBoth,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := map[string]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}

	// Instances in same cluster should share groupKey
	if p, ok := nodeMap["primary-1"]; ok {
		if p.GroupKey == "" {
			t.Error("primary-1 groupKey is empty, want non-empty cluster group key")
		}
	}
	if r, ok := nodeMap["replica-1"]; ok {
		if r.GroupKey == "" {
			t.Error("replica-1 groupKey is empty, want non-empty cluster group key")
		}
	}
	// Both instances should share the same groupKey (same cluster)
	if p, ok1 := nodeMap["primary-1"]; ok1 {
		if r, ok2 := nodeMap["replica-1"]; ok2 {
			if p.GroupKey != r.GroupKey {
				t.Errorf("primary-1 groupKey (%q) != replica-1 groupKey (%q), want same", p.GroupKey, r.GroupKey)
			}
		}
	}
}
