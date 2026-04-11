package model

import "testing"

func TestResourceTypeValidation(t *testing.T) {
	valid := []ResourceType{
		ResourceTypeHost,
		ResourceTypeDatabaseInstance,
		ResourceTypeDatabaseCluster,
		ResourceTypeService,
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
