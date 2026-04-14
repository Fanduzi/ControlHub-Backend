// Package model provides domain types for the resource topology read model.
// input: none
// output: TopologyResponse, TopologyNode, TopologyEdge, TopologyGroup, TopologyQuery
// pos: Response and query types for GET /resources/{id}/topology
// note: if this file changes, update header and README.md
package model

type TopologyDirection string

const (
	TopologyDirectionBoth      TopologyDirection = "both"
	TopologyDirectionUpstream  TopologyDirection = "upstream"
	TopologyDirectionDownstream TopologyDirection = "downstream"
)

type TopologyQuery struct {
	RootID       string
	Depth        int
	Direction    TopologyDirection
	RelationType RelationType
}

type TopologyNode struct {
	ID              string       `json:"id"`
	ResourceType    ResourceType `json:"resourceType"`
	ResourceSubtype string       `json:"resourceSubtype"`
	Name            string       `json:"name"`
	DisplayName     string       `json:"displayName"`
	EnvironmentID   string       `json:"environmentId"`
	OwnerID         string       `json:"ownerId"`
	LifecycleStatus string       `json:"lifecycleStatus"`
	HealthStatus    string       `json:"healthStatus"`
	IsRoot          bool         `json:"isRoot"`
	Distance        int          `json:"distance"`
}

type TopologyEdge struct {
	ID             string       `json:"id"`
	FromResourceID string       `json:"fromResourceId"`
	ToResourceID   string       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
}

type TopologyGroup struct {
	ID           string       `json:"id"`
	Label        string       `json:"label"`
	ResourceType ResourceType `json:"resourceType"`
	NodeIDs      []string     `json:"nodeIds"`
}

type TopologyResponse struct {
	RootResourceID string          `json:"rootResourceId"`
	Depth          int             `json:"depth"`
	Direction      TopologyDirection `json:"direction"`
	Nodes          []TopologyNode  `json:"nodes"`
	Edges          []TopologyEdge  `json:"edges"`
	Groups         []TopologyGroup `json:"groups"`
}
