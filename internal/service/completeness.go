// Package service provides server-derived resource completeness rules.
// input: model resource identity, typed profile values, resolved relationship views, and the relationship matrix
// output: DeriveCompleteness returns a read-only completeness score, status, and missing requirement keys
// pos: Pure inventory-quality projection shared by future resource read paths
// note: if this file changes, update this header and module README.md.
package service

import (
	"strings"

	"github.com/fan/controlhub/internal/model"
)

const (
	completenessStatusComplete = "complete"
	completenessStatusPartial  = "partial"
)

// DeriveCompleteness evaluates seven equally weighted, server-owned requirements.
// It deliberately does not validate write inputs: ingestion resources may be incomplete.
func DeriveCompleteness(resource model.Resource, profile map[string]any, relations []model.ResourceRelationView) model.Completeness {
	missing := make([]string, 0, 7)
	if strings.TrimSpace(resource.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(resource.DisplayName) == "" {
		missing = append(missing, "displayName")
	}
	if resource.EnvironmentID == 0 {
		missing = append(missing, "environment")
	}
	if resource.OwnerID == 0 {
		missing = append(missing, "owner")
	}
	if !hasMinimumIdentity(resource.ResourceType, profile) {
		missing = append(missing, "minimumIdentity")
	}
	if !hasAliasOrExternalIdentifier(resource) {
		missing = append(missing, "aliasOrExternalCIIdentifier")
	}
	if !hasStructuralRelation(resource, relations) {
		missing = append(missing, "structuralRelationship")
	}

	if len(missing) == 0 {
		return model.Completeness{Score: 100, Status: completenessStatusComplete, MissingRequirements: []string{}}
	}
	return model.Completeness{
		Score:               (7 - len(missing)) * 100 / 7,
		Status:              completenessStatusPartial,
		MissingRequirements: missing,
	}
}

func hasMinimumIdentity(resourceType model.ResourceType, profile map[string]any) bool {
	for _, spec := range profileFieldSchemas[resourceType] {
		if (spec.identity || spec.required) && !identityValuePresent(spec, profile) {
			return false
		}
	}
	return true
}

func hasAliasOrExternalIdentifier(resource model.Resource) bool {
	for _, alias := range resource.Aliases {
		if strings.TrimSpace(alias) != "" {
			return true
		}
	}
	for _, identifier := range resource.ExternalIdentifiers {
		if strings.TrimSpace(identifier.System) != "" && strings.TrimSpace(identifier.Value) != "" {
			return true
		}
	}
	return false
}

func hasStructuralRelation(resource model.Resource, relations []model.ResourceRelationView) bool {
	if resource.ResourceType == model.ResourceTypeDomainName {
		return true
	}
	for _, relation := range relations {
		rule, ok := relationshipRules[relation.RelationType]
		if !ok {
			continue
		}
		switch relation.RelationType {
		case model.RelationTypeMemberOf, model.RelationTypeRunsOn, model.RelationTypeFronts, model.RelationTypeReplicatesTo:
			if relation.FromResourceID == resource.ID && containsRelationshipResourceType(rule.sources, resource.ResourceType) ||
				relation.ToResourceID == resource.ID && containsRelationshipResourceType(rule.targets, resource.ResourceType) {
				return true
			}
		}
	}
	return false
}
