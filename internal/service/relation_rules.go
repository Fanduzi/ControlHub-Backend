// Package service provides server-owned business rules for resource relationships.
// input: internal/model resource taxonomy and RelationService repository lookup
// output: shared relationship validation and source-specific RelationshipRulesResponse discovery data
// pos: Single authority for allowed source/target pairs, environment policy, and console discovery
// note: if this file changes, update this header and module README.md.
package service

import (
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

type relationshipRule struct {
	sources         []model.ResourceType
	targets         []model.ResourceType
	sameEnvironment bool
}

type RelationshipRule struct {
	RelationType        model.RelationType   `json:"relationType"`
	TargetResourceTypes []model.ResourceType `json:"targetResourceTypes"`
	SameEnvironment     bool                 `json:"sameEnvironment"`
}

type RelationshipRulesResponse struct {
	SourceResourceID    uint64             `json:"sourceResourceId"`
	SourceEnvironmentID uint64             `json:"sourceEnvironmentId"`
	Rules               []RelationshipRule `json:"rules"`
}

var allRelationshipResourceTypes = []model.ResourceType{
	model.ResourceTypeHost,
	model.ResourceTypeDatabaseInstance,
	model.ResourceTypeDatabaseCluster,
	model.ResourceTypeService,
	model.ResourceTypeDomainName,
	model.ResourceTypeVirtualIP,
	model.ResourceTypeDatabaseProxy,
	model.ResourceTypeControlPlaneComponent,
}

var relationshipRules = map[model.RelationType]relationshipRule{
	model.RelationTypeMemberOf: {
		sources:         []model.ResourceType{model.ResourceTypeDatabaseInstance},
		targets:         []model.ResourceType{model.ResourceTypeDatabaseCluster},
		sameEnvironment: true,
	},
	model.RelationTypeRunsOn: {
		sources: []model.ResourceType{
			model.ResourceTypeService,
			model.ResourceTypeDatabaseInstance,
			model.ResourceTypeDatabaseProxy,
			model.ResourceTypeControlPlaneComponent,
		},
		targets:         []model.ResourceType{model.ResourceTypeHost},
		sameEnvironment: true,
	},
	model.RelationTypePointsTo: {
		sources: []model.ResourceType{model.ResourceTypeDomainName},
		targets: []model.ResourceType{
			model.ResourceTypeVirtualIP,
			model.ResourceTypeService,
			model.ResourceTypeDatabaseProxy,
			model.ResourceTypeDatabaseCluster,
			model.ResourceTypeDatabaseInstance,
		},
	},
	model.RelationTypeFronts: {
		sources: []model.ResourceType{model.ResourceTypeVirtualIP, model.ResourceTypeDatabaseProxy},
		targets: []model.ResourceType{
			model.ResourceTypeDatabaseProxy,
			model.ResourceTypeDatabaseCluster,
			model.ResourceTypeDatabaseInstance,
		},
		sameEnvironment: true,
	},
	model.RelationTypeManages: {
		sources: []model.ResourceType{model.ResourceTypeControlPlaneComponent},
		targets: []model.ResourceType{model.ResourceTypeDatabaseCluster, model.ResourceTypeDatabaseInstance},
	},
	model.RelationTypeReplicatesTo: {
		sources:         []model.ResourceType{model.ResourceTypeDatabaseInstance},
		targets:         []model.ResourceType{model.ResourceTypeDatabaseInstance},
		sameEnvironment: true,
	},
	model.RelationTypeDependsOn: {
		sources: allRelationshipResourceTypes,
		targets: allRelationshipResourceTypes,
	},
}

var relationshipRuleOrder = []model.RelationType{
	model.RelationTypeMemberOf,
	model.RelationTypeRunsOn,
	model.RelationTypePointsTo,
	model.RelationTypeFronts,
	model.RelationTypeManages,
	model.RelationTypeReplicatesTo,
	model.RelationTypeDependsOn,
}

func (s *RelationService) Rules(resourceID uint64) (*RelationshipRulesResponse, error) {
	if resourceID == 0 {
		return nil, fmt.Errorf("%w: resource id is required", ErrValidationFailed)
	}
	resource, err := s.repo.GetResource(resourceID)
	if err != nil {
		return nil, err
	}
	response := &RelationshipRulesResponse{
		SourceResourceID:    resourceID,
		SourceEnvironmentID: resource.EnvironmentID,
		Rules:               []RelationshipRule{},
	}
	for _, relationType := range relationshipRuleOrder {
		rule := relationshipRules[relationType]
		if containsRelationshipResourceType(rule.sources, resource.ResourceType) {
			response.Rules = append(response.Rules, RelationshipRule{
				RelationType:        relationType,
				TargetResourceTypes: append([]model.ResourceType(nil), rule.targets...),
				SameEnvironment:     rule.sameEnvironment,
			})
		}
	}
	return response, nil
}

func validateRelationshipRule(from, to model.Resource, relationType model.RelationType) error {
	rule, ok := relationshipRules[relationType]
	if !ok || !containsRelationshipResourceType(rule.sources, from.ResourceType) || !containsRelationshipResourceType(rule.targets, to.ResourceType) {
		return fmt.Errorf("%w: relationType is not allowed for these resource types", ErrValidationFailed)
	}
	if from.ID == to.ID {
		return fmt.Errorf("%w: self-relations are not supported", ErrValidationFailed)
	}
	if rule.sameEnvironment && from.EnvironmentID != to.EnvironmentID {
		return fmt.Errorf("%w: relationType requires resources in the same environment", ErrValidationFailed)
	}
	return nil
}

func containsRelationshipResourceType(items []model.ResourceType, want model.ResourceType) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
