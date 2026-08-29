// Package model provides domain entities for the resource management system.
// input: testing and time
// output: machine-principal scope and credential-expiry contract tests
// pos: Pure security-boundary regression coverage for machine credentials
// note: if this file changes, update this header and module README.md.
package model

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeMachineScopesAcceptsOnlyClosedSet(t *testing.T) {
	got, err := NormalizeMachineScopes([]MachineScope{
		MachineScopeNamedViewsRead,
		MachineScopeInventoryRead,
		MachineScopeRelationsRead,
		MachineScopeGovernedSelect,
		MachineScopeAuditRead,
	})
	if err != nil {
		t.Fatalf("NormalizeMachineScopes: %v", err)
	}
	want := []MachineScope{
		MachineScopeInventoryRead,
		MachineScopeRelationsRead,
		MachineScopeGovernedSelect,
		MachineScopeAuditRead,
		MachineScopeNamedViewsRead,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}

	for name, scopes := range map[string][]MachineScope{
		"empty":     nil,
		"unknown":   {"inventory:write"},
		"duplicate": {MachineScopeInventoryRead, MachineScopeInventoryRead},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NormalizeMachineScopes(scopes); err == nil {
				t.Fatal("expected closed-scope validation error")
			}
		})
	}
}

func TestResolveMachineCredentialExpiryDefaultsAndCapsLifetime(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

	got, err := ResolveMachineCredentialExpiry(now, nil)
	if err != nil {
		t.Fatalf("default expiry: %v", err)
	}
	if want := now.Add(30 * 24 * time.Hour); !got.Equal(want) {
		t.Fatalf("default expiry = %v, want %v", got, want)
	}

	max := now.Add(90 * 24 * time.Hour)
	if got, err := ResolveMachineCredentialExpiry(now, &max); err != nil || !got.Equal(max) {
		t.Fatalf("maximum expiry = %v, %v; want %v, nil", got, err, max)
	}

	for name, expiry := range map[string]time.Time{
		"not future": now,
		"over max":   max.Add(time.Microsecond),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveMachineCredentialExpiry(now, &expiry); err == nil {
				t.Fatal("expected expiry validation error")
			}
		})
	}
}
