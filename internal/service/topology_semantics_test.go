package service

import (
	"testing"

	"github.com/fan/controlhub/internal/model"
)

const (
	semanticClusterID      uint64 = 2001
	semanticPrimaryID      uint64 = 2002
	semanticReplicaID      uint64 = 2003
	semanticHost1ID        uint64 = 2004
	semanticHost2ID        uint64 = 2005
	semanticServiceID      uint64 = 2006
	semanticProxyActiveID  uint64 = 2007
	semanticProxyStandbyID uint64 = 2008
	semanticVIPID          uint64 = 2009
	semanticDomainID       uint64 = 2010
	semanticOrchestratorID uint64 = 2011
	semanticHAManagerID    uint64 = 2012

	semanticEnvProd       uint64 = 1
	semanticOwnerDBA      uint64 = 2
	semanticOwnerPlatform uint64 = 3
	semanticOwnerPayment  uint64 = 4

	semanticRelMemberPrimary uint64 = 3001
	semanticRelMemberReplica uint64 = 3002
	semanticRelReplication   uint64 = 3003
	semanticRelRunsPrimary   uint64 = 3004
	semanticRelRunsReplica   uint64 = 3005
	semanticRelDepService    uint64 = 3006
	semanticRelFrontsActive  uint64 = 3007
	semanticRelFrontsStandby uint64 = 3008
	semanticRelFrontsVIP     uint64 = 3009
	semanticRelPointsDomain  uint64 = 3010
	semanticRelManagesOrch   uint64 = 3011
	semanticRelManagesHA     uint64 = 3012
	semanticHostAID          uint64 = 4001
	semanticHostBID          uint64 = 4002
	semanticHostRelID        uint64 = 4003
)

func buildDatabaseTestRepo() *fakeTopologyRepo {
	return &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			semanticClusterID: {
				ID: semanticClusterID, ResourceType: model.ResourceTypeDatabaseCluster,
				ResourceSubtype: "mysql", Name: "payment-mysql-cluster-prod", DisplayName: "Payment MySQL Cluster Prod",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerDBA,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticPrimaryID: {
				ID: semanticPrimaryID, ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "payment-mysql-primary-prod", DisplayName: "Payment MySQL Primary",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerDBA,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticReplicaID: {
				ID: semanticReplicaID, ResourceType: model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql", Name: "payment-mysql-replica-01-prod", DisplayName: "Payment MySQL Replica 01",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerDBA,
				LifecycleStatus: "running", HealthStatus: "warning",
			},
			semanticHost1ID: {
				ID: semanticHost1ID, ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "prod-db-host-02", DisplayName: "Production DB Host 02",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticHost2ID: {
				ID: semanticHost2ID, ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "prod-db-host-03", DisplayName: "Production DB Host 03",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticServiceID: {
				ID: semanticServiceID, ResourceType: model.ResourceTypeService,
				ResourceSubtype: "api", Name: "payment-api-prod", DisplayName: "Payment API Prod",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticProxyActiveID: {
				ID: semanticProxyActiveID, ResourceType: model.ResourceTypeDatabaseProxy,
				ResourceSubtype: "proxysql", Name: "payment-proxysql-prod", DisplayName: "Payment ProxySQL Active",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerDBA,
				LifecycleStatus: "running", HealthStatus: "healthy",
				Labels: map[string]string{"team": "platform", "tier": "proxy"},
			},
			semanticProxyStandbyID: {
				ID: semanticProxyStandbyID, ResourceType: model.ResourceTypeDatabaseProxy,
				ResourceSubtype: "proxysql", Name: "payment-proxysql-02-prod", DisplayName: "Payment ProxySQL Standby",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerDBA,
				LifecycleStatus: "stopped", HealthStatus: "unknown",
				Labels: map[string]string{"team": "platform", "tier": "proxy", "role": "standby"},
			},
			semanticVIPID: {
				ID: semanticVIPID, ResourceType: model.ResourceTypeVirtualIP,
				ResourceSubtype: "floating", Name: "payment-vip-prod", DisplayName: "Payment VIP Prod",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticDomainID: {
				ID: semanticDomainID, ResourceType: model.ResourceTypeDomainName,
				ResourceSubtype: "dns", Name: "api.payment.internal", DisplayName: "Payment API Endpoint",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPayment,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticOrchestratorID: {
				ID: semanticOrchestratorID, ResourceType: model.ResourceTypeControlPlaneComponent,
				ResourceSubtype: "orchestrator", Name: "db-orchestrator-prod", DisplayName: "DB Orchestrator",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticHAManagerID: {
				ID: semanticHAManagerID, ResourceType: model.ResourceTypeControlPlaneComponent,
				ResourceSubtype: "ha_monitor", Name: "ha-manager-prod", DisplayName: "HA Manager",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
		},
		relations: []model.ResourceRelation{
			{ID: semanticRelMemberPrimary, FromResourceID: semanticPrimaryID, ToResourceID: semanticClusterID, RelationType: model.RelationTypeMemberOf},
			{ID: semanticRelMemberReplica, FromResourceID: semanticReplicaID, ToResourceID: semanticClusterID, RelationType: model.RelationTypeMemberOf},
			{ID: semanticRelReplication, FromResourceID: semanticPrimaryID, ToResourceID: semanticReplicaID, RelationType: model.RelationTypeReplicatesTo},
			{ID: semanticRelRunsPrimary, FromResourceID: semanticPrimaryID, ToResourceID: semanticHost1ID, RelationType: model.RelationTypeRunsOn},
			{ID: semanticRelRunsReplica, FromResourceID: semanticReplicaID, ToResourceID: semanticHost2ID, RelationType: model.RelationTypeRunsOn},
			{ID: semanticRelDepService, FromResourceID: semanticServiceID, ToResourceID: semanticProxyActiveID, RelationType: model.RelationTypeDependsOn},
			{ID: semanticRelFrontsActive, FromResourceID: semanticProxyActiveID, ToResourceID: semanticClusterID, RelationType: model.RelationTypeFronts},
			{ID: semanticRelFrontsStandby, FromResourceID: semanticProxyStandbyID, ToResourceID: semanticClusterID, RelationType: model.RelationTypeFronts},
			{ID: semanticRelFrontsVIP, FromResourceID: semanticVIPID, ToResourceID: semanticProxyActiveID, RelationType: model.RelationTypeFronts},
			{ID: semanticRelPointsDomain, FromResourceID: semanticDomainID, ToResourceID: semanticProxyActiveID, RelationType: model.RelationTypePointsTo},
			{ID: semanticRelManagesOrch, FromResourceID: semanticOrchestratorID, ToResourceID: semanticClusterID, RelationType: model.RelationTypeManages},
			{ID: semanticRelManagesHA, FromResourceID: semanticHAManagerID, ToResourceID: semanticClusterID, RelationType: model.RelationTypeManages},
		},
	}
}

func buildSemanticNodeMap(resp *model.TopologyResponse) map[uint64]model.TopologyNode {
	nodeMap := map[uint64]model.TopologyNode{}
	for _, n := range resp.Nodes {
		nodeMap[n.ID] = n
	}
	return nodeMap
}

func buildSemanticEdgeMap(resp *model.TopologyResponse) map[uint64]model.TopologyEdge {
	edgeMap := map[uint64]model.TopologyEdge{}
	for _, e := range resp.Edges {
		edgeMap[e.ID] = e
	}
	return edgeMap
}

func TestSemanticClassification_DatabaseClusterRole(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticClusterID]; !ok {
		t.Fatal("missing cluster node")
	} else {
		if n.TopologyRole != model.TopologyRoleCluster {
			t.Errorf("cluster role = %q, want cluster", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerCluster {
			t.Errorf("cluster layer = %q, want cluster", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_PrimaryReplicaRoles(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticPrimaryID]; !ok {
		t.Fatal("missing primary node")
	} else {
		if n.TopologyRole != model.TopologyRolePrimary {
			t.Errorf("primary role = %q, want primary", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerReplication {
			t.Errorf("primary layer = %q, want replication", n.TopologyLayer)
		}
	}
	if n, ok := nodeMap[semanticReplicaID]; !ok {
		t.Fatal("missing replica node")
	} else {
		if n.TopologyRole != model.TopologyRoleReplica {
			t.Errorf("replica role = %q, want replica", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerReplication {
			t.Errorf("replica layer = %q, want replication", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_ReplicationDepth(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticPrimaryID]; !ok {
		t.Fatal("missing primary node")
	} else if n.ReplicationDepth != 0 {
		t.Errorf("primary replicationDepth = %d, want 0", n.ReplicationDepth)
	}
	if n, ok := nodeMap[semanticReplicaID]; !ok {
		t.Fatal("missing replica node")
	} else {
		if n.ReplicationDepth != 1 {
			t.Errorf("replica replicationDepth = %d, want 1", n.ReplicationDepth)
		}
		if n.ReplicationParentID == nil || *n.ReplicationParentID != semanticPrimaryID {
			t.Errorf("replica replicationParentId = %v, want %d", n.ReplicationParentID, semanticPrimaryID)
		}
	}
}

func TestSemanticClassification_ProxyRoles(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticProxyActiveID]; !ok {
		t.Fatal("missing active proxy")
	} else {
		if n.TopologyRole != model.TopologyRoleProxyActive {
			t.Errorf("active proxy role = %q, want proxy_active", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("active proxy layer = %q, want entry", n.TopologyLayer)
		}
	}
	if n, ok := nodeMap[semanticProxyStandbyID]; !ok {
		t.Fatal("missing standby proxy")
	} else {
		if n.TopologyRole != model.TopologyRoleProxyStandby {
			t.Errorf("standby proxy role = %q, want proxy_standby", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("standby proxy layer = %q, want entry", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_ServiceAndEntryRoles(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticServiceID]; !ok {
		t.Fatal("missing service")
	} else {
		if n.TopologyRole != model.TopologyRoleService {
			t.Errorf("service role = %q, want service", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerApplication {
			t.Errorf("service layer = %q, want application", n.TopologyLayer)
		}
	}
	if n, ok := nodeMap[semanticVIPID]; !ok {
		t.Fatal("missing vip")
	} else {
		if n.TopologyRole != model.TopologyRoleEntry {
			t.Errorf("vip role = %q, want entry", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("vip layer = %q, want entry", n.TopologyLayer)
		}
	}
	if n, ok := nodeMap[semanticDomainID]; !ok {
		t.Fatal("missing domain")
	} else {
		if n.TopologyRole != model.TopologyRoleEntry {
			t.Errorf("domain role = %q, want entry", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerEntry {
			t.Errorf("domain layer = %q, want entry", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_HostAndControlPlane(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticHost1ID]; !ok {
		t.Fatal("missing host")
	} else {
		if n.TopologyRole != model.TopologyRoleHost {
			t.Errorf("host role = %q, want host", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerHost {
			t.Errorf("host layer = %q, want host", n.TopologyLayer)
		}
	}
	if n, ok := nodeMap[semanticOrchestratorID]; !ok {
		t.Fatal("missing orchestrator")
	} else {
		if n.TopologyRole != model.TopologyRoleControlPlane {
			t.Errorf("orchestrator role = %q, want control_plane", n.TopologyRole)
		}
		if n.TopologyLayer != model.TopologyLayerControlPlane {
			t.Errorf("orchestrator layer = %q, want control_plane", n.TopologyLayer)
		}
	}
}

func TestSemanticClassification_EdgeSemanticTypes(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edgeMap := buildSemanticEdgeMap(resp)
	if e, ok := edgeMap[semanticRelReplication]; !ok {
		t.Fatal("missing replication edge")
	} else if e.SemanticType != model.EdgeSemanticReplication {
		t.Errorf("replication semantic = %q, want replication", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelMemberPrimary]; !ok {
		t.Fatal("missing membership edge")
	} else if e.SemanticType != model.EdgeSemanticMembership {
		t.Errorf("membership semantic = %q, want membership", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelRunsPrimary]; !ok {
		t.Fatal("missing placement edge")
	} else if e.SemanticType != model.EdgeSemanticPlacement {
		t.Errorf("placement semantic = %q, want placement", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelFrontsActive]; !ok {
		t.Fatal("missing active fronts edge")
	} else if e.SemanticType != model.EdgeSemanticTraffic {
		t.Errorf("active fronts semantic = %q, want traffic", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelFrontsStandby]; !ok {
		t.Fatal("missing standby fronts edge")
	} else if e.SemanticType != model.EdgeSemanticFailover {
		t.Errorf("standby fronts semantic = %q, want failover", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelPointsDomain]; !ok {
		t.Fatal("missing points_to edge")
	} else if e.SemanticType != model.EdgeSemanticTraffic {
		t.Errorf("points_to semantic = %q, want traffic", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelManagesOrch]; !ok {
		t.Fatal("missing manages edge")
	} else if e.SemanticType != model.EdgeSemanticManagement {
		t.Errorf("manages semantic = %q, want management", e.SemanticType)
	}
	if e, ok := edgeMap[semanticRelDepService]; !ok {
		t.Fatal("missing depends_on edge")
	} else if e.SemanticType != model.EdgeSemanticDependency {
		t.Errorf("depends_on semantic = %q, want dependency", e.SemanticType)
	}
}

func TestSemanticClassification_ManagesHAManagerEdge(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	edgeMap := buildSemanticEdgeMap(resp)
	if e, ok := edgeMap[semanticRelManagesHA]; !ok {
		t.Fatal("missing ha manages edge")
	} else if e.SemanticType != model.EdgeSemanticMonitoring {
		t.Errorf("ha manages semantic = %q, want monitoring", e.SemanticType)
	}
}

func TestSemanticClassification_IsDatabaseTopology(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsDatabaseTopology {
		t.Error("IsDatabaseTopology = false for database cluster topology, want true")
	}
	for _, n := range resp.Nodes {
		if !n.IsDatabaseTopology {
			t.Errorf("node %d IsDatabaseTopology = false, want true", n.ID)
		}
	}
}

func TestSemanticClassification_NonDatabaseTopology(t *testing.T) {
	repo := &fakeTopologyRepo{
		resources: map[uint64]model.Resource{
			semanticHostAID: {
				ID: semanticHostAID, ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "host-a", DisplayName: "Host A",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
			semanticHostBID: {
				ID: semanticHostBID, ResourceType: model.ResourceTypeHost,
				ResourceSubtype: "vm", Name: "host-b", DisplayName: "Host B",
				EnvironmentID: semanticEnvProd, OwnerID: semanticOwnerPlatform,
				LifecycleStatus: "running", HealthStatus: "healthy",
			},
		},
		relations: []model.ResourceRelation{{ID: semanticHostRelID, FromResourceID: semanticHostAID, ToResourceID: semanticHostBID, RelationType: model.RelationTypeDependsOn}},
	}
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticHostAID, Depth: 1, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsDatabaseTopology {
		t.Error("IsDatabaseTopology = true for host-only topology, want false")
	}
	for _, n := range resp.Nodes {
		if n.IsDatabaseTopology {
			t.Errorf("node %d IsDatabaseTopology = true in non-database topology, want false", n.ID)
		}
		if n.TopologyRole != model.TopologyRoleHost {
			t.Errorf("node %d role = %q, want host", n.ID, n.TopologyRole)
		}
	}
	if len(resp.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(resp.Edges))
	}
	if resp.Edges[0].SemanticType != model.EdgeSemanticDependency {
		t.Errorf("depends_on semantic = %q, want dependency", resp.Edges[0].SemanticType)
	}
}

func TestSemanticClassification_VisualImportance(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if n, ok := nodeMap[semanticClusterID]; ok && n.VisualImportance < 8 {
		t.Errorf("cluster visualImportance = %d, want >= 8", n.VisualImportance)
	}
	if p, ok := nodeMap[semanticPrimaryID]; ok {
		if r, ok2 := nodeMap[semanticReplicaID]; ok2 && p.VisualImportance <= r.VisualImportance {
			t.Errorf("primary importance (%d) should be > replica importance (%d)", p.VisualImportance, r.VisualImportance)
		}
	}
	if n, ok := nodeMap[semanticHost1ID]; ok && n.VisualImportance >= 5 {
		t.Errorf("host visualImportance = %d, want < 5", n.VisualImportance)
	}
}

func TestSemanticClassification_GroupKey(t *testing.T) {
	repo := buildDatabaseTestRepo()
	svc := NewTopologyService(repo)

	resp, err := svc.BuildTopology(model.TopologyQuery{RootID: semanticClusterID, Depth: 2, Direction: model.TopologyDirectionBoth})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nodeMap := buildSemanticNodeMap(resp)
	if p, ok := nodeMap[semanticPrimaryID]; ok && p.GroupKey == "" {
		t.Error("primary groupKey is empty, want non-empty cluster group key")
	}
	if r, ok := nodeMap[semanticReplicaID]; ok && r.GroupKey == "" {
		t.Error("replica groupKey is empty, want non-empty cluster group key")
	}
	if p, ok1 := nodeMap[semanticPrimaryID]; ok1 {
		if r, ok2 := nodeMap[semanticReplicaID]; ok2 && p.GroupKey != r.GroupKey {
			t.Errorf("primary groupKey (%q) != replica groupKey (%q), want same", p.GroupKey, r.GroupKey)
		}
	}
}
