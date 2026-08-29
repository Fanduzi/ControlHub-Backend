// Package model provides tests for taxonomy subtype dictionaries and validation.
// input: testing, ResourceSubtypeDictionary, ValidateResourceSubtype
// output: TestResourceSubtypeValidate*, TestResourceSubtypeDictionary*
// pos: Pins Domain Name dns, Virtual IP floating, and service worker taxonomy; rejects unknown subtypes
// output: TestResourceSubtype* functions
// pos: Pins Database Proxy technology subtypes and Control Plane ha_monitor, and rejects ambiguous ha
// note: if this file changes, update header and README.md
package model

import "testing"

func TestResourceSubtypeValidate_DatabaseProxyAndControlPlane(t *testing.T) {
	valid := []struct {
		resourceType string
		subtype      string
	}{
		{"database_proxy", "proxysql"},
		{"database_proxy", "chproxy"},
		{"database_proxy", "haproxy"},
		{"database_proxy", "maxscale"},
		{"control_plane_component", "orchestrator"},
		{"control_plane_component", "ha_monitor"},
		{"control_plane_component", "backup_manager"},
	}
	for _, tt := range valid {
		if err := ValidateResourceSubtype(tt.resourceType, tt.subtype); err != nil {
			t.Errorf("ValidateResourceSubtype(%q, %q) = %v, want nil", tt.resourceType, tt.subtype, err)
		}
	}

	rejected := []struct {
		resourceType string
		subtype      string
	}{
		{"database_proxy", "ha"},
		{"database_proxy", ""},
		{"control_plane_component", "ha"},
		{"control_plane_component", ""},
	}
	for _, tt := range rejected {
		if err := ValidateResourceSubtype(tt.resourceType, tt.subtype); err == nil {
			t.Errorf("ValidateResourceSubtype(%q, %q) = nil, want controlled rejection of ambiguous ha subtype", tt.resourceType, tt.subtype)
		}
	}

	proxy := ResourceSubtypeDictionary("database_proxy")
	if len(proxy) != 4 {
		t.Errorf("ResourceSubtypeDictionary(database_proxy) = %#v, want 4 technology subtypes", proxy)
	}
	control := ResourceSubtypeDictionary("control_plane_component")
	if len(control) != 3 {
		t.Errorf("ResourceSubtypeDictionary(control_plane_component) = %#v, want 3 component subtypes", control)
	}
	for _, item := range control {
		if item.Key == "ha" {
			t.Fatal("ambiguous control_plane_component subtype ha must not remain in the dictionary; use ha_monitor")
		}
	}
}

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
		{"service", "worker"},
		{"database_proxy", "proxysql"},
		{"control_plane_component", "orchestrator"},
		{"domain_name", "dns"},
		{"virtual_ip", "floating"},
		{"control_plane_component", "ha_monitor"},
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

func TestResourceSubtypeValidate_DomainNameAndVirtualIPRejectUnknown(t *testing.T) {
	cases := []struct {
		resourceType string
		subtype      string
	}{
		{"domain_name", "anything"},
		{"domain_name", ""},
		{"virtual_ip", "cidr"},
		{"virtual_ip", ""},
	}
	for _, tt := range cases {
		if err := ValidateResourceSubtype(tt.resourceType, tt.subtype); err == nil {
			t.Errorf("ValidateResourceSubtype(%q, %q) = nil, want controlled rejection", tt.resourceType, tt.subtype)
		}
	}
}

func TestResourceSubtypeValidate_ServiceUnknownRejected(t *testing.T) {
	if err := ValidateResourceSubtype("service", "ha"); err == nil {
		t.Fatal("unknown service subtype must be rejected")
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

func TestResourceSubtypeDictionary_DomainNameAndVirtualIP(t *testing.T) {
	domain := ResourceSubtypeDictionary("domain_name")
	if len(domain) != 1 || domain[0].Key != "dns" {
		t.Errorf("ResourceSubtypeDictionary(domain_name) = %#v, want [dns]", domain)
	}
	vip := ResourceSubtypeDictionary("virtual_ip")
	if len(vip) != 1 || vip[0].Key != "floating" {
		t.Errorf("ResourceSubtypeDictionary(virtual_ip) = %#v, want [floating]", vip)
	}
}

func TestResourceSubtypeDictionary_ServiceIncludesWorker(t *testing.T) {
	items := ResourceSubtypeDictionary("service")
	keys := make(map[string]bool, len(items))
	for _, item := range items {
		keys[item.Key] = true
	}
	if !keys["worker"] {
		t.Fatalf("service dictionary missing worker subtype, got %#v", keys)
	}
}
