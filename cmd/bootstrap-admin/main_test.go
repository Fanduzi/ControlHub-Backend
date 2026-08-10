// Package main tests the bootstrap-admin command's unit-testable seams:
// auth-compatible hashing, required-credentials validation, and password-safe
// reporting.
// input: testing, bytes, strings, os
// output: TestHashPassword_*, TestResolveBootstrapConfig_*, TestPrintReport_* unit tests
// pos: Locks the command contract: SHA-256 hashing stays login-compatible, both
// credentials are mandatory, and neither password nor hash ever reaches output.
// note: if this file changes, update header and README.md
package main

import (
	"bytes"
	"strings"
	"testing"
)

// seedAdminPasswordHash is the SHA-256 hex of "secret123" published by
// migrations/0002_seed_reference_data.sql for the seeded admin. If bootstrap
// hashing ever diverges from the login algorithm, this test fails — a
// bootstrap-created credential must authenticate against AuthService.Login.
const seedAdminPasswordHash = "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"

func TestHashPassword_MatchesAuthCompatibleSHA256(t *testing.T) {
	got := hashPassword("secret123")
	if got != seedAdminPasswordHash {
		t.Fatalf("hashPassword(secret123) = %s, want %s (published 0002 seed hash)", got, seedAdminPasswordHash)
	}
}

func TestHashPassword_IsNotTheRawPassword(t *testing.T) {
	if hashPassword("hunter2") == "hunter2" {
		t.Fatal("hash must never equal the plaintext password")
	}
}

func TestResolveBootstrapConfig_RequiresBothCredentials(t *testing.T) {
	cases := []struct {
		name     string
		email    string
		password string
	}{
		{"missing email", "", "pw"},
		{"blank email", "   ", "pw"},
		{"missing password", "ops@example.com", ""},
		{"blank password", "ops@example.com", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("BOOTSTRAP_ADMIN_EMAIL", tc.email)
			t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", tc.password)
			if _, err := resolveBootstrapConfig(); err == nil {
				t.Fatal("expected error when a credential is missing or blank")
			}
		})
	}
}

func TestResolveBootstrapConfig_NormalizesEmailOnly(t *testing.T) {
	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "  Ops.Admin@Example.COM  ")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "s3cret")
	cfg, err := resolveBootstrapConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.email != "ops.admin@example.com" {
		t.Fatalf("email = %q, want lowercased and trimmed", cfg.email)
	}
	// The password is validated for presence only; its exact bytes are stored.
	if cfg.password != "s3cret" {
		t.Fatalf("password = %q, want untouched", cfg.password)
	}
}

func TestResolveBootstrapConfig_ErrorsNeverLeakPassword(t *testing.T) {
	password := "hunter2-very-secret"
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", password)

	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "")
	if _, err := resolveBootstrapConfig(); err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("missing-email error leaks the password: %v", err)
	}

	t.Setenv("BOOTSTRAP_ADMIN_EMAIL", "ops@example.com")
	t.Setenv("BOOTSTRAP_ADMIN_PASSWORD", "")
	if _, err := resolveBootstrapConfig(); err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("missing-password error carries a password value: %v", err)
	}
}

func TestPrintReport_NeverContainsPasswordOrHash(t *testing.T) {
	var buf bytes.Buffer
	printReport(&buf, "ops@example.com", outcomeReactivated)
	out := buf.String()

	for _, needle := range []string{"ops@example.com", "admin", "reactivated"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("output missing %q:\n%s", needle, out)
		}
	}
	for _, bad := range []string{"password", "secret", "hash", "BOOTSTRAP_ADMIN_PASSWORD", "hunter2"} {
		if strings.Contains(out, bad) {
			t.Fatalf("output leaks %q:\n%s", bad, out)
		}
	}
}
