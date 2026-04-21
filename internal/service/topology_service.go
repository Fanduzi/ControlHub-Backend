// Package service provides business logic for resource topology projection.
// input: internal/model (TopologyQuery, TopologyResponse, TopologyNode, TopologyEdge, TopologyGroup)
// output: NewTopologyService, TopologyService.BuildTopology, TopologyRepository interface
// pos: Business logic for building topology read model from resources and relations
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"fmt"
	"sort"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrInvalidDepth     = errors.New("depth must be 1 or 2")
	ErrInvalidDirection = errors.New("direction must be both, upstream, or downstream")
)

type TopologyRepository interface {
	GetResource(id string) (*model.Resource, error)
	ListRelationsByResourceIDs(ids []string) ([]model.ResourceRelation, error)
}

type TopologyService struct {
	repo TopologyRepository
}

func NewTopologyService(repo TopologyRepository) *TopologyService {
	return &TopologyService{repo: repo}
}

func (s *TopologyService) BuildTopology(query model.TopologyQuery) (*model.TopologyResponse, error) {
	if err := validateTopologyQuery(query); err != nil {
		return nil, err
	}

	root, err := s.repo.GetResource(query.RootID)
	if err != nil {
		return nil, err
	}

	nodeSet := map[string]*model.Resource{root.ID: root}
	edgeSet := map[string]model.ResourceRelation{}
	distance := map[string]int{root.ID: 0}

	frontier := []string{root.ID}

	for hop := 0; hop < query.Depth; hop++ {
		if len(frontier) == 0 {
			break
		}

		relations, err := s.repo.ListRelationsByResourceIDs(frontier)
		if err != nil {
			return nil, err
		}

		var nextFrontier []string
		for _, rel := range relations {
			if query.RelationType != "" && rel.RelationType != query.RelationType {
				continue
			}

			fromInFrontier := contains(frontier, rel.FromResourceID)
			toInFrontier := contains(frontier, rel.ToResourceID)

			var neighborID string
			switch query.Direction {
			case model.TopologyDirectionUpstream:
				if !toInFrontier {
					continue
				}
				neighborID = rel.FromResourceID
			case model.TopologyDirectionDownstream:
				if !fromInFrontier {
					continue
				}
				neighborID = rel.ToResourceID
			default: // both
				if fromInFrontier && toInFrontier {
					// Both ends in frontier — edge between frontier nodes
				} else if fromInFrontier {
					neighborID = rel.ToResourceID
				} else if toInFrontier {
					neighborID = rel.FromResourceID
				} else {
					continue
				}
			}

			if _, seen := edgeSet[rel.ID]; !seen {
				edgeSet[rel.ID] = rel
			}

			if neighborID != "" {
				if _, exists := nodeSet[neighborID]; !exists {
					res, err := s.repo.GetResource(neighborID)
					if err != nil {
						continue
					}
					nodeSet[neighborID] = res
					distance[neighborID] = hop + 1
					nextFrontier = append(nextFrontier, neighborID)
				}
			}
		}

		frontier = nextFrontier
	}

	isDB := detectDatabaseTopology(nodeSet)
	replicationInfo := computeReplicationChain(edgeSet)
	clusterGroupKeys := computeClusterGroupKeys(nodeSet, edgeSet)

	nodes := buildTopologyNodes(nodeSet, distance, edgeSet, root.ID, isDB, replicationInfo, clusterGroupKeys)
	edges := buildTopologyEdges(edgeSet, nodeSet)
	groups := buildTopologyGroups(nodeSet)
	problems := buildProblemSummaries(nodes)

	return &model.TopologyResponse{
		RootResourceID:     root.ID,
		Depth:              query.Depth,
		Direction:          query.Direction,
		Nodes:              nodes,
		Edges:              edges,
		Groups:             groups,
		IsDatabaseTopology: isDB,
		Problems:           problems,
	}, nil
}

func validateTopologyQuery(q model.TopologyQuery) error {
	if q.Depth < 1 || q.Depth > 2 {
		return ErrInvalidDepth
	}
	switch q.Direction {
	case model.TopologyDirectionBoth, model.TopologyDirectionUpstream, model.TopologyDirectionDownstream:
		// ok
	default:
		return ErrInvalidDirection
	}
	return nil
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// detectDatabaseTopology returns true if any node is a database_cluster or database_instance.
func detectDatabaseTopology(nodeSet map[string]*model.Resource) bool {
	for _, res := range nodeSet {
		if res.ResourceType == model.ResourceTypeDatabaseCluster ||
			res.ResourceType == model.ResourceTypeDatabaseInstance {
			return true
		}
	}
	return false
}

// replicationEntry holds computed replication chain info for a database instance.
type replicationEntry struct {
	depth    int
	parentID string
}

// computeReplicationChain traces replicates_to edges and returns depth/parent for each instance.
// Primary (source of replicates_to with no incoming replicates_to) has depth 0.
// Each hop adds 1 to depth.
func computeReplicationChain(edgeSet map[string]model.ResourceRelation) map[string]replicationEntry {
	// Build forward map: source -> targets (replicates_to)
	forward := map[string][]string{}
	// Track who replicates TO each node
	incoming := map[string]bool{}
	for _, rel := range edgeSet {
		if rel.RelationType == model.RelationTypeReplicatesTo {
			forward[rel.FromResourceID] = append(forward[rel.FromResourceID], rel.ToResourceID)
			incoming[rel.ToResourceID] = true
		}
	}

	result := map[string]replicationEntry{}

	// Find roots: nodes with outgoing replicates_to but no incoming
	for source := range forward {
		if !incoming[source] {
			// BFS from this root
			type queueItem struct {
				id     string
				depth  int
				parent string
			}
			queue := []queueItem{{id: source, depth: 0, parent: ""}}
			for len(queue) > 0 {
				item := queue[0]
				queue = queue[1:]
				if _, exists := result[item.id]; exists {
					continue
				}
				result[item.id] = replicationEntry{depth: item.depth, parentID: item.parent}
				for _, target := range forward[item.id] {
					queue = append(queue, queueItem{id: target, depth: item.depth + 1, parent: item.id})
				}
			}
		}
	}

	return result
}

// computeClusterGroupKeys assigns a groupKey to each database instance based on which
// cluster it is a member_of. Returns map[instanceID] -> groupKey.
func computeClusterGroupKeys(nodeSet map[string]*model.Resource, edgeSet map[string]model.ResourceRelation) map[string]string {
	clusterKeys := map[string]string{}
	instanceGroups := map[string]string{}

	// Assign stable keys to clusters
	for id, res := range nodeSet {
		if res.ResourceType == model.ResourceTypeDatabaseCluster {
			clusterKeys[id] = fmt.Sprintf("cluster:%s", id)
		}
	}

	// Map instances to their cluster group key via member_of edges
	for _, rel := range edgeSet {
		if rel.RelationType == model.RelationTypeMemberOf {
			if clusterKey, ok := clusterKeys[rel.ToResourceID]; ok {
				instanceGroups[rel.FromResourceID] = clusterKey
			}
		}
	}

	return instanceGroups
}

// classifyNodeRole determines the semantic role of a node based on its type, labels, and edges.
func classifyNodeRole(res *model.Resource, edgeSet map[string]model.ResourceRelation) model.TopologyRole {
	switch res.ResourceType {
	case model.ResourceTypeService:
		return model.TopologyRoleService
	case model.ResourceTypeDomainName, model.ResourceTypeVirtualIP:
		return model.TopologyRoleEntry
	case model.ResourceTypeDatabaseProxy:
		if res.Labels != nil && res.Labels["role"] == "standby" {
			return model.TopologyRoleProxyStandby
		}
		return model.TopologyRoleProxyActive
	case model.ResourceTypeDatabaseCluster:
		return model.TopologyRoleCluster
	case model.ResourceTypeDatabaseInstance:
		// Check replication chain: if this instance has outgoing replicates_to, it's a primary
		// If it only receives replicates_to, it's a replica (or intermediate)
		hasOutgoing := false
		hasIncoming := false
		for _, rel := range edgeSet {
			if rel.RelationType == model.RelationTypeReplicatesTo {
				if rel.FromResourceID == res.ID {
					hasOutgoing = true
				}
				if rel.ToResourceID == res.ID {
					hasIncoming = true
				}
			}
		}
		if hasOutgoing && !hasIncoming {
			return model.TopologyRolePrimary
		}
		if hasOutgoing && hasIncoming {
			return model.TopologyRoleReplicaIntermediate
		}
		return model.TopologyRoleReplica
	case model.ResourceTypeHost:
		return model.TopologyRoleHost
	case model.ResourceTypeControlPlaneComponent:
		return model.TopologyRoleControlPlane
	default:
		return model.TopologyRoleGeneric
	}
}

// classifyTopologyLayer determines the semantic layer for a node.
func classifyTopologyLayer(role model.TopologyRole) model.TopologyLayer {
	switch role {
	case model.TopologyRoleService:
		return model.TopologyLayerApplication
	case model.TopologyRoleEntry, model.TopologyRoleProxyActive, model.TopologyRoleProxyStandby:
		return model.TopologyLayerEntry
	case model.TopologyRoleCluster:
		return model.TopologyLayerCluster
	case model.TopologyRolePrimary, model.TopologyRoleReplica, model.TopologyRoleReplicaIntermediate:
		return model.TopologyLayerReplication
	case model.TopologyRoleControlPlane:
		return model.TopologyLayerControlPlane
	case model.TopologyRoleHost:
		return model.TopologyLayerHost
	default:
		return model.TopologyLayerGeneric
	}
}

// classifyVisualImportance assigns a relative importance score (1-10).
// Higher values mean the node should be visually more prominent.
func classifyVisualImportance(res *model.Resource, role model.TopologyRole, isRoot bool) int {
	if isRoot {
		return 10
	}
	switch role {
	case model.TopologyRolePrimary:
		return 9
	case model.TopologyRoleCluster:
		return 8
	case model.TopologyRoleProxyActive:
		return 7
	case model.TopologyRoleEntry:
		return 6
	case model.TopologyRoleService:
		return 6
	case model.TopologyRoleReplicaIntermediate:
		return 5
	case model.TopologyRoleReplica:
		return 4
	case model.TopologyRoleProxyStandby:
		return 4
	case model.TopologyRoleControlPlane:
		return 3
	case model.TopologyRoleHost:
		return 2
	default:
		return 1
	}
}

// classifyEdgeSemanticType determines the semantic meaning of an edge.
func classifyEdgeSemanticType(rel model.ResourceRelation, nodeSet map[string]*model.Resource) model.EdgeSemanticType {
	switch rel.RelationType {
	case model.RelationTypeReplicatesTo:
		return model.EdgeSemanticReplication
	case model.RelationTypeMemberOf:
		return model.EdgeSemanticMembership
	case model.RelationTypeRunsOn:
		return model.EdgeSemanticPlacement
	case model.RelationTypePointsTo:
		return model.EdgeSemanticTraffic
	case model.RelationTypeDependsOn:
		return model.EdgeSemanticDependency
	case model.RelationTypeFronts:
		// Distinguish active proxy (traffic) from standby proxy (failover)
		if from, ok := nodeSet[rel.FromResourceID]; ok {
			if from.ResourceType == model.ResourceTypeDatabaseProxy &&
				from.Labels != nil && from.Labels["role"] == "standby" {
				return model.EdgeSemanticFailover
			}
		}
		return model.EdgeSemanticTraffic
	case model.RelationTypeManages:
		// HA manager (subtype=ha) → monitoring; orchestrator → management
		if from, ok := nodeSet[rel.FromResourceID]; ok {
			if from.ResourceSubtype == "ha" {
				return model.EdgeSemanticMonitoring
			}
		}
		return model.EdgeSemanticManagement
	default:
		return model.EdgeSemanticDependency
	}
}

func buildTopologyNodes(
	nodeSet map[string]*model.Resource,
	distance map[string]int,
	edgeSet map[string]model.ResourceRelation,
	rootID string,
	isDatabaseTopology bool,
	replicationInfo map[string]replicationEntry,
	clusterGroupKeys map[string]string,
) []model.TopologyNode {
	nodes := make([]model.TopologyNode, 0, len(nodeSet))
	for id, res := range nodeSet {
		role := classifyNodeRole(res, edgeSet)
		layer := classifyTopologyLayer(role)
		importance := classifyVisualImportance(res, role, id == rootID)

		node := model.TopologyNode{
			ID:                 id,
			ResourceType:       res.ResourceType,
			ResourceSubtype:    res.ResourceSubtype,
			Name:               res.Name,
			DisplayName:        res.DisplayName,
			EnvironmentID:      res.EnvironmentID,
			OwnerID:            res.OwnerID,
			LifecycleStatus:    res.LifecycleStatus,
			HealthStatus:       res.HealthStatus,
			IsRoot:             id == rootID,
			Distance:           distance[id],
			TopologyRole:       role,
			TopologyLayer:      layer,
			VisualImportance:   importance,
			IsDatabaseTopology: isDatabaseTopology,
		}

			if res.ProfileSummary != nil {
				node.Hostname = res.ProfileSummary.Hostname
				node.IP = res.ProfileSummary.IP
				node.Port = res.ProfileSummary.Port
			}

			node.Problems = detectNodeProblems(res)
			node.Labels = res.Labels

		// Attach group key for database instances
		if gk, ok := clusterGroupKeys[id]; ok {
			node.GroupKey = gk
		}

		// Attach replication metadata for database instances
		if ri, ok := replicationInfo[id]; ok {
			node.ReplicationDepth = ri.depth
			node.ReplicationParentID = ri.parentID
		}

		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Distance != nodes[j].Distance {
			return nodes[i].Distance < nodes[j].Distance
		}
		if nodes[i].ResourceType != nodes[j].ResourceType {
			return nodes[i].ResourceType < nodes[j].ResourceType
		}
		if nodes[i].Name != nodes[j].Name {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

func buildTopologyEdges(edgeSet map[string]model.ResourceRelation, nodeSet map[string]*model.Resource) []model.TopologyEdge {
	edges := make([]model.TopologyEdge, 0, len(edgeSet))
	for _, rel := range edgeSet {
		edges = append(edges, model.TopologyEdge{
			ID:             rel.ID,
			FromResourceID: rel.FromResourceID,
			ToResourceID:   rel.ToResourceID,
			RelationType:   rel.RelationType,
			SemanticType:   classifyEdgeSemanticType(rel, nodeSet),
		})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].RelationType != edges[j].RelationType {
			return edges[i].RelationType < edges[j].RelationType
		}
		if edges[i].FromResourceID != edges[j].FromResourceID {
			return edges[i].FromResourceID < edges[j].FromResourceID
		}
		if edges[i].ToResourceID != edges[j].ToResourceID {
			return edges[i].ToResourceID < edges[j].ToResourceID
		}
		return edges[i].ID < edges[j].ID
	})
	return edges
}

func buildTopologyGroups(nodeSet map[string]*model.Resource) []model.TopologyGroup {
	typeMap := map[model.ResourceType][]string{}
	for id, res := range nodeSet {
		typeMap[res.ResourceType] = append(typeMap[res.ResourceType], id)
	}

	type groupEntry struct {
		resourceType model.ResourceType
		label        string
		nodeIDs      []string
	}
	entries := make([]groupEntry, 0, len(typeMap))
	for rt, ids := range typeMap {
		label := resourceTypeLabel(rt)
		sort.Strings(ids)
		entries = append(entries, groupEntry{resourceType: rt, label: label, nodeIDs: ids})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].resourceType < entries[j].resourceType
	})

	groups := make([]model.TopologyGroup, 0, len(entries))
	for _, e := range entries {
		groups = append(groups, model.TopologyGroup{
			ID:           fmt.Sprintf("group-%s", e.resourceType),
			Label:        e.label,
			ResourceType: e.resourceType,
			NodeIDs:      e.nodeIDs,
		})
	}
	return groups
}

func resourceTypeLabel(rt model.ResourceType) string {
	for _, item := range model.ResourceTypeDictionary() {
		if item.Key == string(rt) {
			return item.Label
		}
	}
	return string(rt)
}

func detectNodeProblems(res *model.Resource) []model.TopologyProblem {
	var problems []model.TopologyProblem

	switch model.HealthStatus(res.HealthStatus) {
	case model.HealthStatusCritical:
		problems = append(problems, model.TopologyProblem{
			Severity: "critical", Code: "health_critical",
			Message: "Resource health is critical",
		})
	case model.HealthStatusWarning:
		problems = append(problems, model.TopologyProblem{
			Severity: "warning", Code: "health_warning",
			Message: "Resource health is degraded",
		})
	}

	switch model.LifecycleStatus(res.LifecycleStatus) {
	case model.LifecycleStatusStopped:
		problems = append(problems, model.TopologyProblem{
			Severity: "critical", Code: "lifecycle_stopped",
			Message: "Resource is stopped",
		})
	case model.LifecycleStatusProvisioning:
		problems = append(problems, model.TopologyProblem{
			Severity: "warning", Code: "lifecycle_provisioning",
			Message: "Resource is provisioning",
		})
	case model.LifecycleStatusDecommissioning:
		problems = append(problems, model.TopologyProblem{
			Severity: "warning", Code: "lifecycle_decommissioning",
			Message: "Resource is being decommissioned",
		})
	}

	return problems
}

func buildProblemSummaries(nodes []model.TopologyNode) []model.TopologyProblemSummary {
	var summaries []model.TopologyProblemSummary
	for _, n := range nodes {
		if len(n.Problems) == 0 {
			continue
		}
		worstSeverity := "warning"
		for _, p := range n.Problems {
			if p.Severity == "critical" {
				worstSeverity = "critical"
				break
			}
		}
		summaries = append(summaries, model.TopologyProblemSummary{
			ResourceID:   n.ID,
			ResourceName: n.DisplayName,
			ResourceType: string(n.ResourceType),
			Severity:     worstSeverity,
			Problems:     n.Problems,
		})
	}
	return summaries
}
