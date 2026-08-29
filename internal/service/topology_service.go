// Package service provides business logic for resource topology projection.
// input: internal/model (TopologyQuery, TopologyResponse, TopologyNode, TopologyEdge, TopologyGroup)
// output: NewTopologyService, TopologyService.BuildTopology, TopologyRepository interface
// pos: Business logic for building topology read model from resources and relations
// note: if this file changes, update this header and module README.md.
package service

import (
	"errors"
	"fmt"
	"sort"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrInvalidDepth     = errors.New("depth must be non-negative")
	ErrInvalidDirection = errors.New("direction must be both, upstream, or downstream")
)

const (
	TopologyNodeCap = 200
	TopologyEdgeCap = 400
)

type TopologyRepository interface {
	GetResource(id uint64) (*model.Resource, error)
	ListRelationsByResourceIDs(ids []uint64) ([]model.ResourceRelation, error)
	ListTopologyCandidates(environmentID uint64) ([]model.Resource, error)
}

type TopologyService struct {
	repo TopologyRepository
}

func NewTopologyService(repo TopologyRepository) *TopologyService {
	return &TopologyService{repo: repo}
}

func (s *TopologyService) BuildTopology(query model.TopologyQuery) (*model.TopologyResponse, error) {
	if query.Depth == 0 {
		query.Depth = 2
	}
	if query.Direction == "" {
		query.Direction = model.TopologyDirectionBoth
	}
	if err := validateTopologyQuery(query); err != nil {
		return nil, err
	}
	if query.RootID == 0 {
		return s.buildTopologyCandidates(query)
	}

	root, err := s.repo.GetResource(query.RootID)
	if err != nil {
		return nil, err
	}
	if query.EnvironmentID != 0 && root.EnvironmentID != query.EnvironmentID {
		return nil, ErrResourceNotFound
	}

	nodeSet := map[uint64]*model.Resource{root.ID: root}
	edgeSet := map[uint64]model.ResourceRelation{}
	distance := map[uint64]int{root.ID: 0}
	truncated := false

	frontier := []uint64{root.ID}

	for hop := 0; hop < query.Depth; hop++ {
		if len(frontier) == 0 {
			break
		}

		relations, err := s.repo.ListRelationsByResourceIDs(frontier)
		if err != nil {
			return nil, err
		}
		sortTopologyRelations(relations)

		var nextFrontier []uint64
		for _, rel := range relations {
			if query.RelationType != "" && rel.RelationType != query.RelationType {
				continue
			}

			fromInFrontier := contains(frontier, rel.FromResourceID)
			toInFrontier := contains(frontier, rel.ToResourceID)

			var neighborID uint64
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
			default:
				if fromInFrontier && toInFrontier {
				} else if fromInFrontier {
					neighborID = rel.ToResourceID
				} else if toInFrontier {
					neighborID = rel.FromResourceID
				} else {
					continue
				}
			}

			_, edgeSeen := edgeSet[rel.ID]
			_, nodeSeen := nodeSet[neighborID]
			if !edgeSeen && len(edgeSet) >= TopologyEdgeCap {
				truncated = true
				continue
			}
			if neighborID != 0 && !nodeSeen && len(nodeSet) >= TopologyNodeCap {
				truncated = true
				continue
			}
			if neighborID != 0 {
				if !nodeSeen {
					res, err := s.repo.GetResource(neighborID)
					if err != nil {
						continue
					}
					if query.EnvironmentID != 0 && res.EnvironmentID != query.EnvironmentID {
						continue
					}
					nodeSet[neighborID] = res
					distance[neighborID] = hop + 1
					nextFrontier = append(nextFrontier, neighborID)
				}
			}
			if !edgeSeen {
				edgeSet[rel.ID] = rel
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
		Truncated:          truncated,
		Problems:           problems,
	}, nil
}

func (s *TopologyService) buildTopologyCandidates(query model.TopologyQuery) (*model.TopologyResponse, error) {
	candidates, err := s.repo.ListTopologyCandidates(query.EnvironmentID)
	if err != nil {
		return nil, err
	}

	nodeSet := map[uint64]*model.Resource{}
	distance := map[uint64]int{}
	truncated := false
	for i := range candidates {
		res := candidates[i]
		if res.IsArchived() || !isTopologyCandidate(&res) {
			continue
		}
		if _, ok := nodeSet[res.ID]; ok {
			continue
		}
		if len(nodeSet) >= TopologyNodeCap {
			truncated = true
			continue
		}
		nodeSet[res.ID] = &res
		distance[res.ID] = 0
	}

	nodes := buildTopologyNodes(nodeSet, distance, nil, 0, detectDatabaseTopology(nodeSet), nil, nil)
	return &model.TopologyResponse{
		Depth:              query.Depth,
		Direction:          query.Direction,
		Nodes:              nodes,
		Edges:              []model.TopologyEdge{},
		Groups:             buildTopologyGroups(nodeSet),
		IsDatabaseTopology: detectDatabaseTopology(nodeSet),
		Truncated:          truncated,
		Problems:           buildProblemSummaries(nodes),
	}, nil
}

func sortTopologyRelations(relations []model.ResourceRelation) {
	sort.Slice(relations, func(i, j int) bool {
		if relations[i].RelationType != relations[j].RelationType {
			return relations[i].RelationType < relations[j].RelationType
		}
		if relations[i].FromResourceID != relations[j].FromResourceID {
			return relations[i].FromResourceID < relations[j].FromResourceID
		}
		if relations[i].ToResourceID != relations[j].ToResourceID {
			return relations[i].ToResourceID < relations[j].ToResourceID
		}
		return relations[i].ID < relations[j].ID
	})
}

func isTopologyCandidate(res *model.Resource) bool {
	switch model.HealthStatus(res.HealthStatus) {
	case model.HealthStatusWarning, model.HealthStatusCritical:
		return true
	}
	switch res.ResourceType {
	case model.ResourceTypeService, model.ResourceTypeDatabaseCluster, model.ResourceTypeDatabaseProxy:
		return true
	default:
		return false
	}
}

func validateTopologyQuery(q model.TopologyQuery) error {
	if q.Depth < 0 {
		return ErrInvalidDepth
	}
	switch q.Direction {
	case model.TopologyDirectionBoth, model.TopologyDirectionUpstream, model.TopologyDirectionDownstream:
	default:
		return ErrInvalidDirection
	}
	return nil
}

func contains(slice []uint64, s uint64) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func detectDatabaseTopology(nodeSet map[uint64]*model.Resource) bool {
	for _, res := range nodeSet {
		if res.ResourceType == model.ResourceTypeDatabaseCluster || res.ResourceType == model.ResourceTypeDatabaseInstance {
			return true
		}
	}
	return false
}

type replicationEntry struct {
	depth    int
	parentID *uint64
}

func computeReplicationChain(edgeSet map[uint64]model.ResourceRelation) map[uint64]replicationEntry {
	forward := map[uint64][]uint64{}
	incoming := map[uint64]bool{}
	for _, rel := range edgeSet {
		if rel.RelationType == model.RelationTypeReplicatesTo {
			forward[rel.FromResourceID] = append(forward[rel.FromResourceID], rel.ToResourceID)
			incoming[rel.ToResourceID] = true
		}
	}

	result := map[uint64]replicationEntry{}
	for source := range forward {
		if incoming[source] {
			continue
		}
		type queueItem struct {
			id     uint64
			depth  int
			parent *uint64
		}
		queue := []queueItem{{id: source, depth: 0, parent: nil}}
		for len(queue) > 0 {
			item := queue[0]
			queue = queue[1:]
			if _, exists := result[item.id]; exists {
				continue
			}
			result[item.id] = replicationEntry{depth: item.depth, parentID: item.parent}
			for _, target := range forward[item.id] {
				parentID := item.id
				queue = append(queue, queueItem{id: target, depth: item.depth + 1, parent: &parentID})
			}
		}
	}
	return result
}

func computeClusterGroupKeys(nodeSet map[uint64]*model.Resource, edgeSet map[uint64]model.ResourceRelation) map[uint64]string {
	clusterKeys := map[uint64]string{}
	instanceGroups := map[uint64]string{}
	for id, res := range nodeSet {
		if res.ResourceType == model.ResourceTypeDatabaseCluster {
			clusterKeys[id] = fmt.Sprintf("cluster:%d", id)
		}
	}
	for _, rel := range edgeSet {
		if rel.RelationType == model.RelationTypeMemberOf {
			if clusterKey, ok := clusterKeys[rel.ToResourceID]; ok {
				instanceGroups[rel.FromResourceID] = clusterKey
			}
		}
	}
	return instanceGroups
}

func classifyNodeRole(res *model.Resource, edgeSet map[uint64]model.ResourceRelation) model.TopologyRole {
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

func classifyVisualImportance(_ *model.Resource, role model.TopologyRole, isRoot bool) int {
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
	case model.TopologyRoleEntry, model.TopologyRoleService:
		return 6
	case model.TopologyRoleReplicaIntermediate:
		return 5
	case model.TopologyRoleReplica, model.TopologyRoleProxyStandby:
		return 4
	case model.TopologyRoleControlPlane:
		return 3
	case model.TopologyRoleHost:
		return 2
	default:
		return 1
	}
}

func classifyEdgeSemanticType(rel model.ResourceRelation, nodeSet map[uint64]*model.Resource) model.EdgeSemanticType {
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
		if from, ok := nodeSet[rel.FromResourceID]; ok {
			if from.ResourceType == model.ResourceTypeDatabaseProxy && from.Labels != nil && from.Labels["role"] == "standby" {
				return model.EdgeSemanticFailover
			}
		}
		return model.EdgeSemanticTraffic
	case model.RelationTypeManages:
		if from, ok := nodeSet[rel.FromResourceID]; ok {
			if from.ResourceSubtype == "ha_monitor" {
				return model.EdgeSemanticMonitoring
			}
		}
		return model.EdgeSemanticManagement
	default:
		return model.EdgeSemanticDependency
	}
}

func buildTopologyNodes(
	nodeSet map[uint64]*model.Resource,
	distance map[uint64]int,
	edgeSet map[uint64]model.ResourceRelation,
	rootID uint64,
	isDatabaseTopology bool,
	replicationInfo map[uint64]replicationEntry,
	clusterGroupKeys map[uint64]string,
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

		if gk, ok := clusterGroupKeys[id]; ok {
			node.GroupKey = gk
		}
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

func buildTopologyEdges(edgeSet map[uint64]model.ResourceRelation, nodeSet map[uint64]*model.Resource) []model.TopologyEdge {
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

func buildTopologyGroups(nodeSet map[uint64]*model.Resource) []model.TopologyGroup {
	typeMap := map[model.ResourceType][]uint64{}
	for id, res := range nodeSet {
		typeMap[res.ResourceType] = append(typeMap[res.ResourceType], id)
	}

	type groupEntry struct {
		resourceType model.ResourceType
		label        string
		nodeIDs      []uint64
	}
	entries := make([]groupEntry, 0, len(typeMap))
	for rt, ids := range typeMap {
		label := resourceTypeLabel(rt)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		entries = append(entries, groupEntry{resourceType: rt, label: label, nodeIDs: ids})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].resourceType < entries[j].resourceType
	})

	groups := make([]model.TopologyGroup, 0, len(entries))
	for index, e := range entries {
		groups = append(groups, model.TopologyGroup{
			ID:           uint64(index + 1),
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
		problems = append(problems, model.TopologyProblem{Severity: "critical", Code: "health_critical", Message: "Resource health is critical"})
	case model.HealthStatusWarning:
		problems = append(problems, model.TopologyProblem{Severity: "warning", Code: "health_warning", Message: "Resource health is degraded"})
	}

	switch model.LifecycleStatus(res.LifecycleStatus) {
	case model.LifecycleStatusStopped:
		problems = append(problems, model.TopologyProblem{Severity: "critical", Code: "lifecycle_stopped", Message: "Resource is stopped"})
	case model.LifecycleStatusProvisioning:
		problems = append(problems, model.TopologyProblem{Severity: "warning", Code: "lifecycle_provisioning", Message: "Resource is provisioning"})
	case model.LifecycleStatusDecommissioning:
		problems = append(problems, model.TopologyProblem{Severity: "warning", Code: "lifecycle_decommissioning", Message: "Resource is being decommissioned"})
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
