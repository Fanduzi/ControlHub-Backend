// Package model provides domain types for the resource topology read model.
// input: none
// output: TopologyResponse, TopologyNode, TopologyEdge, TopologyGroup, TopologyQuery
// pos: Response and query types for GET /resources/{id}/topology
// note: if this file changes, update header and README.md
package model

type TopologyDirection string

const (
	TopologyDirectionBoth       TopologyDirection = "both"
	TopologyDirectionUpstream   TopologyDirection = "upstream"
	TopologyDirectionDownstream TopologyDirection = "downstream"
)

// TopologyRole describes the semantic role a node plays in the topology graph.
// Used by frontend to determine rendering style, icon, and layer placement.
type TopologyRole string

const (
	TopologyRoleApplication         TopologyRole = "application"
	TopologyRoleEntry               TopologyRole = "entry"
	TopologyRoleProxyActive         TopologyRole = "proxy_active"
	TopologyRoleProxyStandby        TopologyRole = "proxy_standby"
	TopologyRoleCluster             TopologyRole = "cluster"
	TopologyRolePrimary             TopologyRole = "primary"
	TopologyRoleReplica             TopologyRole = "replica"
	TopologyRoleReplicaIntermediate TopologyRole = "replica_intermediate"
	TopologyRoleHost                TopologyRole = "host"
	TopologyRoleControlPlane        TopologyRole = "control_plane"
	TopologyRoleService             TopologyRole = "service"
	TopologyRoleGeneric             TopologyRole = "generic"
)

// TopologyLayer describes the semantic band/layer a node belongs to.
// Used by frontend for layered graph layout (top-to-bottom rendering).
type TopologyLayer string

const (
	TopologyLayerApplication  TopologyLayer = "application"
	TopologyLayerEntry        TopologyLayer = "entry"
	TopologyLayerCluster      TopologyLayer = "cluster"
	TopologyLayerReplication  TopologyLayer = "replication"
	TopologyLayerControlPlane TopologyLayer = "control_plane"
	TopologyLayerHost         TopologyLayer = "host"
	TopologyLayerGeneric      TopologyLayer = "generic"
)

// EdgeSemanticType describes the semantic meaning of a topology edge.
// Used by frontend to choose line style, color, and arrow rendering.
type EdgeSemanticType string

const (
	EdgeSemanticTraffic     EdgeSemanticType = "traffic"
	EdgeSemanticFailover    EdgeSemanticType = "failover"
	EdgeSemanticReplication EdgeSemanticType = "replication"
	EdgeSemanticMembership  EdgeSemanticType = "membership"
	EdgeSemanticPlacement   EdgeSemanticType = "placement"
	EdgeSemanticManagement  EdgeSemanticType = "management"
	EdgeSemanticDependency  EdgeSemanticType = "dependency"
	EdgeSemanticMonitoring  EdgeSemanticType = "monitoring"
)

type TopologyQuery struct {
	RootID       string
	Depth        int
	Direction    TopologyDirection
	RelationType RelationType
}

type TopologyNode struct {
	ID                  string        `json:"id"`
	ResourceType        ResourceType  `json:"resourceType"`
	ResourceSubtype     string        `json:"resourceSubtype"`
	Name                string        `json:"name"`
	DisplayName         string        `json:"displayName"`
	EnvironmentID       string        `json:"environmentId"`
	OwnerID             string        `json:"ownerId"`
	LifecycleStatus     string        `json:"lifecycleStatus"`
	HealthStatus        string        `json:"healthStatus"`
	IsRoot              bool          `json:"isRoot"`
	Distance            int           `json:"distance"`
	TopologyRole        TopologyRole  `json:"topologyRole"`
	TopologyLayer       TopologyLayer `json:"topologyLayer"`
	GroupKey            string        `json:"groupKey,omitempty"`
	VisualImportance    int           `json:"visualImportance"`
	IsDatabaseTopology  bool          `json:"isDatabaseTopology"`
	ReplicationDepth    int              `json:"replicationDepth,omitempty"`
	ReplicationParentID string           `json:"replicationParentId,omitempty"`
	Hostname            string           `json:"hostname,omitempty"`
	IP                  string           `json:"ip,omitempty"`
	Port                int              `json:"port,omitempty"`
	Problems            []TopologyProblem `json:"problems,omitempty"`
	Labels              map[string]string `json:"labels,omitempty"`
}

type TopologyEdge struct {
	ID             string           `json:"id"`
	FromResourceID string           `json:"fromResourceId"`
	ToResourceID   string           `json:"toResourceId"`
	RelationType   RelationType     `json:"relationType"`
	SemanticType   EdgeSemanticType `json:"semanticType"`
}

type TopologyGroup struct {
	ID           string       `json:"id"`
	Label        string       `json:"label"`
	ResourceType ResourceType `json:"resourceType"`
	NodeIDs      []string     `json:"nodeIds"`
}

type TopologyResponse struct {
	RootResourceID     string            `json:"rootResourceId"`
	Depth              int               `json:"depth"`
	Direction          TopologyDirection `json:"direction"`
	Nodes              []TopologyNode    `json:"nodes"`
	Edges              []TopologyEdge    `json:"edges"`
	Groups             []TopologyGroup   `json:"groups"`
	IsDatabaseTopology bool              `json:"isDatabaseTopology"`
	Problems           []TopologyProblemSummary `json:"problems,omitempty"`
}

type TopologyProblem struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Code     string `json:"code"`
}

type TopologyProblemSummary struct {
	ResourceID   string            `json:"resourceId"`
	ResourceName string            `json:"resourceName"`
	ResourceType string            `json:"resourceType"`
	Severity     string            `json:"severity"`
	Problems     []TopologyProblem `json:"problems"`
}
