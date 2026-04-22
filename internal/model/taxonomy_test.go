package model

import "testing"

func TestResourceSubtypeValidate_Valid(t *testing.T) {
	tests := []struct {
		resourceType string
		subtype      string
	}{
		{"database_instance", "mysql"},
		{"database_instance", "postgresql"},
		{"database_instance", "redis"},
		{"database_instance", "clickhouse"},
		{"database_instance", "mongodb"},
		{"database_instance", "tidb"},
		{"database_cluster", "mysql"},
		{"host", "vm"},
		{"host", "physical"},
		{"host", "container"},
		{"service", "api"},
		{"database_proxy", "proxysql"},
		{"control_plane_component", "orchestrator"},
	}
	for _, tt := range tests {
		err := ValidateResourceSubtype(tt.resourceType, tt.subtype)
		if err != nil {
			t.Errorf("ValidateResourceSubtype(%q, %q) = %v, want nil", tt.resourceType, tt.subtype, err)
		}
	}
}

func TestResourceSubtypeValidate_Invalid(t *testing.T) {
	err := ValidateResourceSubtype("database_instance", "invalid_engine")
	if err == nil {
		t.Error("ValidateResourceSubtype(invalid) = nil, want error")
	}
}

func TestResourceSubtypeValidate_NoSubtypes(t *testing.T) {
	err := ValidateResourceSubtype("domain_name", "anything")
	if err != nil {
		t.Errorf("ValidateResourceSubtype(domain_name, anything) should be ignored, got %v", err)
	}
}

func TestResourceSubtypeDictionary(t *testing.T) {
	items := ResourceSubtypeDictionary("database_instance")
	if len(items) != 6 {
		t.Errorf("ResourceSubtypeDictionary(database_instance) returned %d items, want 6", len(items))
	}
	keys := make(map[string]bool)
	for _, item := range items {
		keys[item.Key] = true
	}
	if !keys["mysql"] || !keys["postgresql"] || !keys["redis"] {
		t.Error("missing expected subtypes for database_instance")
	}
}

func TestResourceSubtypeDictionary_Empty(t *testing.T) {
	items := ResourceSubtypeDictionary("domain_name")
	if len(items) != 0 {
		t.Errorf("ResourceSubtypeDictionary(domain_name) returned %d items, want 0", len(items))
	}
}
