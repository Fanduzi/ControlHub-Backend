// Package service tests server-derived resource completeness rules.
// input: each resource type's minimum identity, matrix-valid structural relation, and invalid structural endpoints
// output: exhaustive complete/partial score, missing-key, label-ignore, matrix-validation, and input-immutability checks
// pos: Public pure completeness-rule seam against the relationship matrix
// note: if this file changes, update this header and module README.md.
package service

import (
	"reflect"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestDeriveCompleteness_AllResourceTypes(t *testing.T) {
	tests := []struct {
		name     string
		resource model.Resource
		profile  map[string]any
		relation model.ResourceRelationView // Structural relation from #77.
		identity string
		domain   bool
	}{
		{"host: inbound runs_on from database proxy", resource(model.ResourceTypeHost), map[string]any{"hostname": "host-1", "ipAddress": "10.0.0.1"}, inbound(model.RelationTypeRunsOn, model.ResourceTypeDatabaseProxy), "hostname", false},
		{"database instance: member_of", resource(model.ResourceTypeDatabaseInstance), map[string]any{"engine": "mysql", "host": "db-1", "port": 3306}, outbound(model.RelationTypeMemberOf, model.ResourceTypeDatabaseCluster), "engine", false},
		{"database cluster: inbound member_of", resource(model.ResourceTypeDatabaseCluster), map[string]any{"engine": "mysql", "primaryEndpoint": "db.example.com"}, inbound(model.RelationTypeMemberOf, model.ResourceTypeDatabaseInstance), "engine", false},
		{"service: runs_on", resource(model.ResourceTypeService), map[string]any{"systemName": "orders"}, outbound(model.RelationTypeRunsOn, model.ResourceTypeHost), "systemName", false},
		{"domain name: no structural edge", resource(model.ResourceTypeDomainName), map[string]any{"fqdn": "orders.example.com"}, model.ResourceRelationView{}, "fqdn", true},
		{"virtual IP: fronts database cluster", resource(model.ResourceTypeVirtualIP), map[string]any{"ipAddress": "10.0.0.2"}, outbound(model.RelationTypeFronts, model.ResourceTypeDatabaseCluster), "ipAddress", false},
		{"database proxy: fronts database instance", resource(model.ResourceTypeDatabaseProxy), map[string]any{"technologySubtype": "proxysql", "host": "proxy-1", "port": 6032, "role": "active"}, outbound(model.RelationTypeFronts, model.ResourceTypeDatabaseInstance), "technologySubtype", false},
		{"control plane component: runs_on", resource(model.ResourceTypeControlPlaneComponent), map[string]any{"componentSubtype": "orchestrator", "endpoint": "http://orchestrator", "role": "active"}, outbound(model.RelationTypeRunsOn, model.ResourceTypeHost), "componentSubtype", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := test.resource
			profile := cloneProfile(test.profile)
			relation := test.relation

			relations := []model.ResourceRelationView{relation}
			if test.domain {
				relations = nil
			}
			got := DeriveCompleteness(input, profile, relations)
			if !reflect.DeepEqual(got, model.Completeness{Score: 100, Status: "complete", MissingRequirements: []string{}}) {
				t.Fatalf("complete result = %#v", got)
			}
			if !reflect.DeepEqual(input, test.resource) || !reflect.DeepEqual(profile, test.profile) || !reflect.DeepEqual(relation, test.relation) {
				t.Fatal("DeriveCompleteness changed its input")
			}

			if !test.domain {
				got = DeriveCompleteness(input, profile, nil)
				want := model.Completeness{Score: 85, Status: "partial", MissingRequirements: []string{"structuralRelationship"}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("partial result = %#v, want %#v", got, want)
				}
			}

			profile = cloneProfile(test.profile)
			delete(profile, test.identity)
			got = DeriveCompleteness(input, profile, relations)
			want := model.Completeness{Score: 85, Status: "partial", MissingRequirements: []string{"minimumIdentity"}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("missing identity result = %#v, want %#v", got, want)
			}
		})
	}
}

func TestDeriveCompleteness_MissingRequirementsAndLabels(t *testing.T) {
	resource := model.Resource{ResourceType: model.ResourceTypeHost, Labels: map[string]string{"hostname": "not-identity", "owner": "not-owner"}}
	got := DeriveCompleteness(resource, nil, nil)
	want := model.Completeness{
		Score:  0,
		Status: "partial",
		MissingRequirements: []string{
			"name", "displayName", "environment", "owner", "minimumIdentity", "aliasOrExternalCIIdentifier", "structuralRelationship",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
}

func TestDeriveCompleteness_RequiresMatrixValidStructuralEndpoint(t *testing.T) {
	resource := resource(model.ResourceTypeService)
	profile := map[string]any{"systemName": "orders"}
	invalid := outbound(model.RelationTypeMemberOf, model.ResourceTypeDatabaseCluster)

	got := DeriveCompleteness(resource, profile, []model.ResourceRelationView{invalid})
	want := model.Completeness{Score: 85, Status: "partial", MissingRequirements: []string{"structuralRelationship"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
}

func resource(resourceType model.ResourceType) model.Resource {
	return model.Resource{
		ID: 1, ResourceType: resourceType, Name: "ci-1", DisplayName: "CI 1", EnvironmentID: 1, OwnerID: 1,
		Aliases: []string{"ci-one"}, Labels: map[string]string{"ignored": "label"},
	}
}

func outbound(relationType model.RelationType, relatedType model.ResourceType) model.ResourceRelationView {
	return model.ResourceRelationView{FromResourceID: 1, ToResourceID: 2, RelationType: relationType, RelatedResourceType: string(relatedType)}
}

func inbound(relationType model.RelationType, relatedType model.ResourceType) model.ResourceRelationView {
	return model.ResourceRelationView{FromResourceID: 2, ToResourceID: 1, RelationType: relationType, RelatedResourceType: string(relatedType)}
}

func cloneProfile(profile map[string]any) map[string]any {
	clone := make(map[string]any, len(profile))
	for key, value := range profile {
		clone[key] = value
	}
	return clone
}
