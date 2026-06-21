// Package model provides tests for the QueryTargetSafetyState enum chain.
// input: slices, strings, testing
// output: TestQueryTargetSafetyState_*, TestQueryTargetSafetyStateDictionary_*
// pos: Validates the Phase 37 readonly_sandbox_enabled enum value + dictionary + fail-closed Validate
// note: if this file changes, update header and README.md
package model

import (
	"slices"
	"testing"
)

func TestQueryTargetSafetyState_AllKnownValidate(t *testing.T) {
	t.Parallel()
	for _, s := range []QueryTargetSafetyState{
		SafetyStateCredentialMissing,
		SafetyStateExecutionDisabled,
		SafetyStateUnsupportedEngine,
		SafetyStateConnectionIncomplete,
		SafetyStateReadonlySandboxEnabled,
	} {
		if err := s.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", s, err)
		}
	}
}

func TestQueryTargetSafetyState_ReadonlySandboxEnabledValidates(t *testing.T) {
	t.Parallel()
	// WHY: the ready target serializes safetyState = readonly_sandbox_enabled,
	// so that exact value must be a declared, validating member of the enum.
	if err := SafetyStateReadonlySandboxEnabled.Validate(); err != nil {
		t.Fatalf("readonly_sandbox_enabled Validate = %v, want nil", err)
	}
	if string(SafetyStateReadonlySandboxEnabled) != "readonly_sandbox_enabled" {
		t.Fatalf("string value = %q, want readonly_sandbox_enabled", SafetyStateReadonlySandboxEnabled)
	}
}

func TestQueryTargetSafetyState_UnknownRejects(t *testing.T) {
	t.Parallel()
	// WHY: the service must never emit an undeclared safety string. Unknown and
	// near-miss values must fail closed.
	for _, s := range []QueryTargetSafetyState{
		"", "ready", "sandbox_enabled", "READONLY_SANDBOX_ENABLED", "credential-required", "locked",
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want error", s)
		}
	}
}

func TestQueryTargetSafetyStateDictionary_ContainsAllKnownStates(t *testing.T) {
	t.Parallel()
	got := QueryTargetSafetyStateDictionary()
	keys := make([]string, 0, len(got))
	for _, item := range got {
		keys = append(keys, item.Key)
	}
	for _, want := range []string{
		"credential_missing", "execution_disabled", "unsupported_engine",
		"connection_incomplete", "readonly_sandbox_enabled",
	} {
		if !slices.Contains(keys, want) {
			t.Errorf("dictionary missing key %q: got %v", want, keys)
		}
	}
}

func TestQueryTargetSafetyStateDictionary_CloneIsIndependent(t *testing.T) {
	t.Parallel()
	first := QueryTargetSafetyStateDictionary()
	originalFirst := first[0].Key
	first[0] = DictionaryItem{Key: "mutated"}
	second := QueryTargetSafetyStateDictionary()
	// WHY: the dictionary is shared static state; callers must get an independent
	// clone so one mutation cannot corrupt the canonical list.
	if second[0].Key != originalFirst {
		t.Fatalf("dictionary clone was not independent: second[0]=%q, want %q", second[0].Key, originalFirst)
	}
}