// Package model provides tests for query execution domain validators.
// input: strings, testing
// output: TestQueryEnvironmentPolicy_*, TestValidateCredentialRef_*
// pos: Unit tests for environment-policy and credential_ref fail-closed validators
// note: if this file changes, update header and README.md
package model

import (
	"strings"
	"testing"
)

func TestQueryEnvironmentPolicy_ValidValuesPass(t *testing.T) {
	t.Parallel()
	for _, p := range []QueryEnvironmentPolicy{
		QueryEnvPolicyDisabled,
		QueryEnvPolicyNonProdOnly,
		QueryEnvPolicyAllEnvironments,
	} {
		if err := p.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", p, err)
		}
	}
}

func TestQueryEnvironmentPolicy_UnknownAndEmptyFailClosed(t *testing.T) {
	t.Parallel()
	// WHY: unknown/empty policy must fail closed — it must never be silently
	// treated as all_environments, because that is the only policy that unlocks
	// production targets. Production safety depends on this rejecting.
	for _, p := range []QueryEnvironmentPolicy{
		"", "production", "all", "non_prod", "ALL_ENVIRONMENTS", "any", "disabled ",
	} {
		if err := p.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want error (fail closed)", p)
		}
	}
}

func TestValidateCredentialRef_ValidPasses(t *testing.T) {
	t.Parallel()
	for _, ref := range []string{
		"A",
		"ABC_123",
		"0",
		"UPPER_ONLY_99",
		strings.Repeat("A", MaxCredentialRefLength),
	} {
		if err := ValidateCredentialRef(ref); err != nil {
			t.Errorf("ValidateCredentialRef(%q) = %v, want nil", ref, err)
		}
	}
}

func TestValidateCredentialRef_RejectsLowercaseDashDotSpaceEmpty(t *testing.T) {
	t.Parallel()
	// WHY: an invalid credential_ref must be rejected before any environment
	// lookup, so the ref charset is constrained to [A-Z0-9_]+. Lowercase,
	// punctuation, whitespace, unicode, empty, and over-length refs are all
	// invalid — failing here prevents constructing a bogus env-var key and
	// keeps the target locked.
	for _, ref := range []string{
		"", "lowercase", "a-b", "a.b", "a b", "A!", "with/slash", "ünïcode",
	} {
		if err := ValidateCredentialRef(ref); err == nil {
			t.Errorf("ValidateCredentialRef(%q) = nil, want error", ref)
		}
	}
	// Length cap: one character over the bound must fail.
	long := strings.Repeat("A", MaxCredentialRefLength+1)
	if err := ValidateCredentialRef(long); err == nil {
		t.Errorf("ValidateCredentialRef(len=%d) = nil, want error", len(long))
	}
}