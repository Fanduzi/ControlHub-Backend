// Package model provides domain entities for the resource management system.
// input: internal/model (all types and dictionaries), testing
// output: Test* functions
// pos: Validates taxonomy constants and dictionary completeness
// note: if this file changes, update header and README.md
package model

import "testing"

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
