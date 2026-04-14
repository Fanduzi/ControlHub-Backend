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

	nodes := buildTopologyNodes(nodeSet, distance, root.ID)
	edges := buildTopologyEdges(edgeSet)
	groups := buildTopologyGroups(nodeSet)

	return &model.TopologyResponse{
		RootResourceID: root.ID,
		Depth:          query.Depth,
		Direction:      query.Direction,
		Nodes:          nodes,
		Edges:         edges,
		Groups:        groups,
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

func buildTopologyNodes(nodeSet map[string]*model.Resource, distance map[string]int, rootID string) []model.TopologyNode {
	nodes := make([]model.TopologyNode, 0, len(nodeSet))
	for id, res := range nodeSet {
		nodes = append(nodes, model.TopologyNode{
			ID:              id,
			ResourceType:    res.ResourceType,
			ResourceSubtype: res.ResourceSubtype,
			Name:            res.Name,
			DisplayName:     res.DisplayName,
			EnvironmentID:   res.EnvironmentID,
			OwnerID:         res.OwnerID,
			LifecycleStatus: res.LifecycleStatus,
			HealthStatus:    res.HealthStatus,
			IsRoot:          id == rootID,
			Distance:        distance[id],
		})
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

func buildTopologyEdges(edgeSet map[string]model.ResourceRelation) []model.TopologyEdge {
	edges := make([]model.TopologyEdge, 0, len(edgeSet))
	for _, rel := range edgeSet {
		edges = append(edges, model.TopologyEdge{
			ID:             rel.ID,
			FromResourceID: rel.FromResourceID,
			ToResourceID:   rel.ToResourceID,
			RelationType:   rel.RelationType,
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
