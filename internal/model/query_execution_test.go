// Package model provides tests for query execution domain validators.
// input: strings, testing
// output: TestQueryEnvironmentPolicy_*, TestValidateCredentialRef_*
// pos: Unit tests for environment-policy and credential_ref fail-closed validators
// note: if this file changes, update header and README.md
package model

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
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

func TestEncodeDecodeCursor_Roundtrip(t *testing.T) {
	t.Parallel()
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	hash := "abc123"
	encoded, err := EncodeCursor(ts, 42, hash)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	p, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("DecodeCursor() error = %v", err)
	}
	if p.Version != CursorVersion {
		t.Errorf("Version = %d, want %d", p.Version, CursorVersion)
	}
	if p.ID != "42" {
		t.Errorf("ID = %q, want %q", p.ID, "42")
	}
	if p.QueryHash != hash {
		t.Errorf("QueryHash = %q, want %q", p.QueryHash, hash)
	}
	if !p.CreatedAt.Equal(ts) {
		t.Errorf("CreatedAt = %v, want %v", p.CreatedAt, ts)
	}
}

func TestDecodeCursor_RejectsInvalidInputs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"not_base64", "!!!not-valid-base64!!!"},
		{"not_json", base64.RawURLEncoding.EncodeToString([]byte("not json"))},
		{"wrong_version", func() string {
			raw, _ := EncodeCursor(time.Now(), 1, "h")
			b, _ := base64.RawURLEncoding.DecodeString(raw)
			s := strings.Replace(string(b), `"v":1`, `"v":99`, 1)
			return base64.RawURLEncoding.EncodeToString([]byte(s))
		}()},
		{"oversized", base64.RawURLEncoding.EncodeToString(make([]byte, 2048))},
		{"tampered_id", func() string {
			raw, _ := EncodeCursor(time.Now(), 1, "h")
			b, _ := base64.RawURLEncoding.DecodeString(raw)
			s := strings.Replace(string(b), `"id":"1"`, `"id":"abc"`, 1)
			return base64.RawURLEncoding.EncodeToString([]byte(s))
		}()},
		{"missing_created_at", func() string {
			raw, _ := EncodeCursor(time.Now(), 1, "h")
			b, _ := base64.RawURLEncoding.DecodeString(raw)
			s := strings.Replace(string(b), `"t":`, `"t_x":`, 1)
			return base64.RawURLEncoding.EncodeToString([]byte(s))
		}()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeCursor(tt.raw); err == nil {
				t.Errorf("DecodeCursor(%q) = nil, want error", tt.name)
			}
		})
	}
}

func TestComputeQueryHash_Deterministic(t *testing.T) {
	t.Parallel()
	status := QueryExecutionSuccess
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	h1 := ComputeQueryHash(10, &status, &from, &to, "self")
	h2 := ComputeQueryHash(10, &status, &from, &to, "self")
	if h1 != h2 {
		t.Errorf("ComputeQueryHash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA256 hex)", len(h1))
	}
}

func TestComputeQueryHash_DifferentInputsProduceDifferentHashes(t *testing.T) {
	t.Parallel()
	s1 := QueryExecutionSuccess
	s2 := QueryExecutionFailed
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	h1 := ComputeQueryHash(10, &s1, &from, &to, "self")
	h2 := ComputeQueryHash(10, &s2, &from, &to, "self")
	if h1 == h2 {
		t.Error("different status should produce different hashes")
	}
}

// TestComputeQueryHash_FractionalSecondTimestampsProduceDifferentHashes proves
// Oracle Finding 1 is fixed: two timestamps in the same second but with
// different fractional parts must produce different hashes, so a cursor
// encoded under one fractional-second filter cannot be replayed under another.
func TestComputeQueryHash_FractionalSecondTimestampsProduceDifferentHashes(t *testing.T) {
	t.Parallel()
	status := QueryExecutionSuccess
	from1 := time.Date(2025, 6, 1, 12, 0, 0, 100_000_000, time.UTC)
	from2 := time.Date(2025, 6, 1, 12, 0, 0, 900_000_000, time.UTC)
	h1 := ComputeQueryHash(10, &status, &from1, nil, "self")
	h2 := ComputeQueryHash(10, &status, &from2, nil, "self")
	if h1 == h2 {
		t.Fatal("fractional-second timestamps in the same second must produce different hashes")
	}
}

func TestValidateStatus_ValidPasses(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"success", "rejected", "failed", "timeout"} {
		if err := ValidateStatus(s); err != nil {
			t.Errorf("ValidateStatus(%q) = %v, want nil", s, err)
		}
	}
}

func TestValidateStatus_RejectsInvalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "SUCCESS", "Success", "pending", "ok", " error"} {
		if err := ValidateStatus(s); err == nil {
			t.Errorf("ValidateStatus(%q) = nil, want error", s)
		}
	}
}

func TestParseTimestamp_ValidRFC3339(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"utc_z", "2025-06-01T12:00:00Z"},
		{"with_offset", "2025-06-01T12:00:00+05:30"},
		{"nanos", "2025-06-01T12:00:00.123456789Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseTimestamp(tt.input); err != nil {
				t.Errorf("ParseTimestamp(%q) = %v, want nil", tt.input, err)
			}
		})
	}
}

func TestParseTimestamp_RejectsInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"date_only", "2025-06-01"},
		{"no_timezone", "2025-06-01T12:00:00"},
		{"empty", ""},
		{"garbage", "not-a-date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseTimestamp(tt.input); err == nil {
				t.Errorf("ParseTimestamp(%q) = nil, want error", tt.input)
			}
		})
	}
}

func TestValidateTimeWindow_ValidPasses(t *testing.T) {
	t.Parallel()
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateTimeWindow(&from, &to); err != nil {
		t.Errorf("ValidateTimeWindow() = %v, want nil", err)
	}
}

func TestValidateTimeWindow_RejectsFromAfterTo(t *testing.T) {
	t.Parallel()
	from := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateTimeWindow(&from, &to); err == nil {
		t.Error("ValidateTimeWindow(from > to) = nil, want error")
	}
}

func TestValidateTimeWindow_RejectsEqualFromTo(t *testing.T) {
	t.Parallel()
	ts := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateTimeWindow(&ts, &ts); err == nil {
		t.Error("ValidateTimeWindow(from == to) = nil, want error")
	}
}

func TestValidateTimeWindow_NilFromOrToPasses(t *testing.T) {
	t.Parallel()
	ts := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := ValidateTimeWindow(nil, &ts); err != nil {
		t.Errorf("ValidateTimeWindow(nil, to) = %v, want nil", err)
	}
	if err := ValidateTimeWindow(&ts, nil); err != nil {
		t.Errorf("ValidateTimeWindow(from, nil) = %v, want nil", err)
	}
	if err := ValidateTimeWindow(nil, nil); err != nil {
		t.Errorf("ValidateTimeWindow(nil, nil) = %v, want nil", err)
	}
}

func TestValidateCursor_ValidPasses(t *testing.T) {
	t.Parallel()
	hash := "testhash123"
	encoded, err := EncodeCursor(time.Now(), 1, hash)
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if err := ValidateCursor(encoded, hash); err != nil {
		t.Errorf("ValidateCursor() = %v, want nil", err)
	}
}

func TestValidateCursor_RejectsHashMismatch(t *testing.T) {
	t.Parallel()
	encoded, err := EncodeCursor(time.Now(), 1, "hash_a")
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}
	if err := ValidateCursor(encoded, "hash_b"); err == nil {
		t.Error("ValidateCursor(mismatched hash) = nil, want error")
	}
}

func TestValidateCursor_RejectsInvalidFormat(t *testing.T) {
	t.Parallel()
	if err := ValidateCursor("not-valid-cursor", "hash"); err == nil {
		t.Error("ValidateCursor(invalid) = nil, want error")
	}
}

func TestNormalizeFilters_ProducesCanonicalString(t *testing.T) {
	t.Parallel()
	status := QueryExecutionSuccess
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	s1 := NormalizeFilters(&status, &from, &to)
	s2 := NormalizeFilters(&status, &from, &to)
	if s1 != s2 {
		t.Errorf("NormalizeFilters not deterministic: %q != %q", s1, s2)
	}
	if !strings.Contains(s1, "success") {
		t.Errorf("NormalizeFilters should contain status, got %q", s1)
	}
}

func TestNormalizeFilters_NilValues(t *testing.T) {
	t.Parallel()
	s := NormalizeFilters(nil, nil, nil)
	if s != "||" {
		t.Errorf("NormalizeFilters(nil,nil,nil) = %q, want %q", s, "||")
	}
}
