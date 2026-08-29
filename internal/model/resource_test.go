// Package model provides domain entities for the resource management system.
// input: internal/model (all types and dictionaries), testing
// output: Test* functions
// pos: Validates taxonomy constants and dictionary completeness
// note: if this file changes, update header and README.md
package model

import (
	"encoding/json"
	"testing"
)

func TestResourceTypeValidation(t *testing.T) {
	valid := []ResourceType{
		ResourceTypeHost,
		ResourceTypeDatabaseInstance,
		ResourceTypeDatabaseCluster,
		ResourceTypeService,
		ResourceTypeDomainName,
		ResourceTypeVirtualIP,
		ResourceTypeDatabaseProxy,
		ResourceTypeControlPlaneComponent,
	}

	for _, item := range valid {
		if err := item.Validate(); err != nil {
			t.Fatalf("expected %s to be valid: %v", item, err)
		}
	}

	if err := ResourceType("unknown").Validate(); err == nil {
		t.Fatal("expected invalid resource type to fail validation")
	}
}

func TestRelationTypeValidation(t *testing.T) {
	valid := []RelationType{
		RelationTypeDependsOn,
		RelationTypeMemberOf,
		RelationTypeRunsOn,
		RelationTypePointsTo,
		RelationTypeFronts,
		RelationTypeManages,
		RelationTypeReplicatesTo,
	}

	for _, item := range valid {
		if err := item.Validate(); err != nil {
			t.Fatalf("expected %s to be valid: %v", item, err)
		}
	}

	if err := RelationType("unknown").Validate(); err == nil {
		t.Fatal("expected invalid relation type to fail validation")
	}
}

func TestResourceTypeDictionaryItems(t *testing.T) {
	items := ResourceTypeDictionary()

	if len(items) != 8 {
		t.Fatalf("expected 8 resource type items, got %d", len(items))
	}

	if items[0].Key != string(ResourceTypeHost) {
		t.Fatalf("expected first resource type key host, got %s", items[0].Key)
	}

	last := items[len(items)-1]
	if last.Key != string(ResourceTypeControlPlaneComponent) {
		t.Fatalf("expected last resource type key control_plane_component, got %s", last.Key)
	}

	for _, item := range items {
		if item.Label == "" {
			t.Fatalf("expected label for resource type %s", item.Key)
		}
		if item.Description == "" {
			t.Fatalf("expected description for resource type %s", item.Key)
		}
	}
}

func TestRelationTypeDictionaryItems(t *testing.T) {
	items := RelationTypeDictionary()

	if len(items) != 7 {
		t.Fatalf("expected 7 relation type items, got %d", len(items))
	}

	if items[0].Key != string(RelationTypeDependsOn) {
		t.Fatalf("expected first relation type key depends_on, got %s", items[0].Key)
	}

	last := items[len(items)-1]
	if last.Key != string(RelationTypeReplicatesTo) {
		t.Fatalf("expected last relation type key replicates_to, got %s", last.Key)
	}

	for _, item := range items {
		if item.Label == "" {
			t.Fatalf("expected label for relation type %s", item.Key)
		}
		if item.Description == "" {
			t.Fatalf("expected description for relation type %s", item.Key)
		}
	}
}

func TestLifecycleStatusValidation(t *testing.T) {
	valid := []LifecycleStatus{
		LifecycleStatusProvisioning,
		LifecycleStatusRunning,
		LifecycleStatusStopped,
		LifecycleStatusDegraded,
		LifecycleStatusDecommissioning,
	}

	for _, item := range valid {
		if err := item.Validate(); err != nil {
			t.Fatalf("expected %s to be valid: %v", item, err)
		}
	}

	if err := LifecycleStatus("unknown").Validate(); err == nil {
		t.Fatal("expected invalid lifecycle status to fail validation")
	}
}

func TestHealthStatusValidation(t *testing.T) {
	valid := []HealthStatus{
		HealthStatusHealthy,
		HealthStatusWarning,
		HealthStatusCritical,
		HealthStatusUnknown,
	}

	for _, item := range valid {
		if err := item.Validate(); err != nil {
			t.Fatalf("expected %s to be valid: %v", item, err)
		}
	}

	if err := HealthStatus("unknown_status").Validate(); err == nil {
		t.Fatal("expected invalid health status to fail validation")
	}
}

func TestLifecycleStatusDictionaryItems(t *testing.T) {
	items := LifecycleStatusDictionary()

	if len(items) != 5 {
		t.Fatalf("expected 5 lifecycle status items, got %d", len(items))
	}

	if items[0].Key != string(LifecycleStatusProvisioning) {
		t.Fatalf("expected first lifecycle status key provisioning, got %s", items[0].Key)
	}

	last := items[len(items)-1]
	if last.Key != string(LifecycleStatusDecommissioning) {
		t.Fatalf("expected last lifecycle status key decommissioning, got %s", last.Key)
	}

	for _, item := range items {
		if item.Label == "" {
			t.Fatalf("expected label for lifecycle status %s", item.Key)
		}
		if item.Description == "" {
			t.Fatalf("expected description for lifecycle status %s", item.Key)
		}
	}
}

func TestHealthStatusDictionaryItems(t *testing.T) {
	items := HealthStatusDictionary()

	if len(items) != 4 {
		t.Fatalf("expected 4 health status items, got %d", len(items))
	}

	if items[0].Key != string(HealthStatusHealthy) {
		t.Fatalf("expected first health status key healthy, got %s", items[0].Key)
	}

	last := items[len(items)-1]
	if last.Key != string(HealthStatusUnknown) {
		t.Fatalf("expected last health status key unknown, got %s", last.Key)
	}

	for _, item := range items {
		if item.Label == "" {
			t.Fatalf("expected label for health status %s", item.Key)
		}
		if item.Description == "" {
			t.Fatalf("expected description for health status %s", item.Key)
		}
	}
}

func TestResourceJSONUsesNumericIDs(t *testing.T) {
	payload := []byte(`{"id":101,"resourceType":"host","resourceSubtype":"linux","name":"host-1","displayName":"Host 1","environmentId":202,"ownerId":303,"lifecycleStatus":"running","healthStatus":"healthy","source":"discovery","externalId":"ext-1","labels":{"tier":"prod"}}`)

	var resource Resource
	if err := json.Unmarshal(payload, &resource); err != nil {
		t.Fatalf("expected JSON unmarshal to succeed: %v", err)
	}

	if resource.ID != 101 {
		t.Fatalf("expected id 101, got %v", resource.ID)
	}
	if resource.EnvironmentID != 202 {
		t.Fatalf("expected environmentId 202, got %v", resource.EnvironmentID)
	}
	if resource.OwnerID != 303 {
		t.Fatalf("expected ownerId 303, got %v", resource.OwnerID)
	}

	legacyPayload := []byte(`{"id":"101","resourceType":"host","resourceSubtype":"linux","name":"host-1","displayName":"Host 1","environmentId":"202","ownerId":"303","lifecycleStatus":"running","healthStatus":"healthy","source":"discovery","externalId":"ext-1","labels":{"tier":"prod"}}`)
	if err := json.Unmarshal(legacyPayload, &resource); err == nil {
		t.Fatal("expected legacy string-form resource ids to fail unmarshal")
	}
}

func TestResourceIdentityJSON(t *testing.T) {
	payload := []byte(`{"id":76,"resourceType":"service","name":"orders-api","origin":"imported","aliases":["orders","order-api"],"externalIdentifiers":[{"system":"servicenow","value":"CI-76"}]}`)

	var resource Resource
	if err := json.Unmarshal(payload, &resource); err != nil {
		t.Fatalf("unmarshal resource identity: %v", err)
	}
	if resource.Origin != ResourceOriginImported {
		t.Fatalf("origin = %q, want imported", resource.Origin)
	}
	if len(resource.Aliases) != 2 || resource.Aliases[0] != "orders" {
		t.Fatalf("aliases = %#v", resource.Aliases)
	}
	if len(resource.ExternalIdentifiers) != 1 || resource.ExternalIdentifiers[0].System != "servicenow" || resource.ExternalIdentifiers[0].Value != "CI-76" {
		t.Fatalf("external identifiers = %#v", resource.ExternalIdentifiers)
	}
}

func TestResourceOriginValidation(t *testing.T) {
	for _, origin := range []ResourceOrigin{ResourceOriginManual, ResourceOriginImported, ResourceOriginDiscovered} {
		if err := origin.Validate(); err != nil {
			t.Fatalf("origin %q should be valid: %v", origin, err)
		}
	}
	if err := ResourceOrigin("crawler").Validate(); err == nil {
		t.Fatal("unsupported origin should fail validation")
	}
}

func TestRelationCreateInputJSONUsesNumericIDs(t *testing.T) {
	payload := []byte(`{"toResourceId":404,"relationType":"depends_on"}`)

	var input RelationCreateInput
	if err := json.Unmarshal(payload, &input); err != nil {
		t.Fatalf("expected JSON unmarshal to succeed: %v", err)
	}

	if input.ToResourceID != 404 {
		t.Fatalf("expected toResourceId 404, got %v", input.ToResourceID)
	}

	legacyPayload := []byte(`{"toResourceId":"404","relationType":"depends_on"}`)
	if err := json.Unmarshal(legacyPayload, &input); err == nil {
		t.Fatal("expected legacy string-form relation ids to fail unmarshal")
	}
}
