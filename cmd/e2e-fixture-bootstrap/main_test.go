// Package main tests the test/CI-only fixture provisioning command's seams:
// auth-compatible hashing, mandatory fixture credentials, legacy-seed refusal,
// and password-safe reporting.
// input: testing, bytes, strings, os
// output: TestHashPassword_*, TestResolveFixtureConfig_*, TestLegacySeedRefusal_*, TestPrintReport_* unit tests
// pos: Locks the command contract: SHA-256 hashing stays login-compatible, every
// fixture credential is mandatory with no defaults, the published 0002 seed
// identities are refused, and neither password nor hash ever reaches output.
// note: if this file changes, update header and README.md
package main

import (
	"bytes"
	"strings"
	"testing"
)

// seedAdminPasswordHash is the SHA-256 hex of "secret123" published by
// migrations/0002_seed_reference_data.sql for the seeded admin. The fixture
// command hashes with the same scheme so provisioned identities authenticate
// against AuthService.Login.
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

func TestResolveFixtureConfig_RequiresAllFourCredentials(t *testing.T) {
	cases := []struct {
		name           string
		adminEmail     string
		adminPassword  string
		editorEmail    string
		editorPassword string
	}{
		{"missing admin email", "", "pw-admin", "editor@fixture.invalid", "pw-editor"},
		{"blank admin email", "   ", "pw-admin", "editor@fixture.invalid", "pw-editor"},
		{"missing admin password", "admin@fixture.invalid", "", "editor@fixture.invalid", "pw-editor"},
		{"blank admin password", "admin@fixture.invalid", "   ", "editor@fixture.invalid", "pw-editor"},
		{"missing editor email", "admin@fixture.invalid", "pw-admin", "", "pw-editor"},
		{"blank editor email", "admin@fixture.invalid", "pw-admin", "   ", "pw-editor"},
		{"missing editor password", "admin@fixture.invalid", "pw-admin", "editor@fixture.invalid", ""},
		{"blank editor password", "admin@fixture.invalid", "pw-admin", "editor@fixture.invalid", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", tc.adminEmail)
			t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", tc.adminPassword)
			t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", tc.editorEmail)
			t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", tc.editorPassword)
			if _, err := resolveFixtureConfig(); err == nil {
				t.Fatal("expected error when a fixture credential is missing or blank")
			}
		})
	}
}

func TestResolveFixtureConfig_RejectsNonInvalidFixtureEmails(t *testing.T) {
	cases := []struct {
		name  string
		email string
	}{
		{"real-looking operator email", "ops@example.com"},
		{"production-looking email", "admin@controlhub.io"},
		{"no tld", "e2e-admin@localhost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", tc.email)
			t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
			t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
			t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
			if _, err := resolveFixtureConfig(); err == nil {
				t.Fatal("expected refusal of a non-.invalid fixture email")
			}
		})
	}
}

func TestResolveFixtureConfig_NormalizesEmailsOnly(t *testing.T) {
	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "  E2E.Admin@Fixture.INVALID  ")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "  e2e.Editor@Fixture.Invalid  ")
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
	cfg, err := resolveFixtureConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Admin.Email != "e2e.admin@fixture.invalid" {
		t.Fatalf("admin email = %q, want lowercased and trimmed", cfg.Admin.Email)
	}
	if cfg.Editor.Email != "e2e.editor@fixture.invalid" {
		t.Fatalf("editor email = %q, want lowercased and trimmed", cfg.Editor.Email)
	}
	if cfg.Admin.Password != "admin-pw" || cfg.Editor.Password != "editor-pw" {
		t.Fatal("passwords must be preserved byte-for-byte")
	}
}

func TestResolveFixtureConfig_RefusesLegacySeedEmails(t *testing.T) {
	for _, email := range []string{
		"admin@example.com",
		"editor@example.com",
		"ADMIN@example.com",
		"  editor@example.com ",
	} {
		t.Run(email, func(t *testing.T) {
			t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", email)
			t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
			t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
			t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
			if _, err := resolveFixtureConfig(); err == nil {
				t.Fatal("expected refusal of the published seed email")
			}
		})
	}
}

func TestResolveFixtureConfig_RefusesLegacySeedPassword(t *testing.T) {
	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "admin@fixture.invalid")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "secret123")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
	if _, err := resolveFixtureConfig(); err == nil {
		t.Fatal("expected refusal of the published seed password")
	}
}

func TestResolveFixtureConfig_ErrorsNeverLeakPasswords(t *testing.T) {
	password := "hunter2-very-secret"
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", password)
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")

	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
	if _, err := resolveFixtureConfig(); err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("missing-email error leaks the password: %v", err)
	}
}

func TestPrintReport_NeverContainsPasswordOrHash(t *testing.T) {
	var buf bytes.Buffer
	printReport(&buf, fixtureSet{
		Admin:  fixtureCredential{Email: "admin@fixture.invalid", Role: "admin"},
		Editor: fixtureCredential{Email: "editor@fixture.invalid", Role: "editor"},
	}, map[string]bootstrapOutcome{
		"admin@fixture.invalid":  outcomeCreated,
		"editor@fixture.invalid": outcomeReactivated,
	})
	out := buf.String()

	for _, needle := range []string{"admin@fixture.invalid", "editor@fixture.invalid", "admin", "editor", "created", "reactivated"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("output missing %q:\n%s", needle, out)
		}
	}
	for _, bad := range []string{"password", "secret", "hash", "E2E_FIXTURE", "hunter2"} {
		if strings.Contains(out, bad) {
			t.Fatalf("output leaks %q:\n%s", bad, out)
		}
	}
}
