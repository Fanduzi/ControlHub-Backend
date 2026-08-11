// Package main tests the test/CI-only fixture provisioning command's seams:
// auth-compatible hashing, mandatory fixture credentials, the explicit
// test-mode capability, the disposable-DSN isolation gate, migration-00016
// verification, legacy-seed refusal, and password-safe reporting.
// input: testing, bytes, strings, context, database/sql, os
// output: TestHashPassword_*, TestResolveFixtureConfig_*, TestParseDisposableDSN_*,
// TestVerifyFixtureDatabase_*, TestPrintReport_* unit tests
// pos: Locks the command contract: SHA-256 hashing stays login-compatible, every
// fixture credential and the test-mode capability are mandatory with no
// defaults, the metadata DSN must be a loopback disposable *_e2e database,
// migration 00016 must be applied with retired seeds inactive, the published
// 0002 seed identities are refused, and neither password nor hash ever reaches
// output.
// note: if this file changes, update header and README.md
package main

import (
	"bytes"
	"context"
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

func TestResolveFixtureConfig_RequiresExplicitTestMode(t *testing.T) {
	for _, mode := range []string{"", "   ", "0", "yes", "true", "2"} {
		t.Run("mode="+mode, func(t *testing.T) {
			t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", mode)
			stubFixtureCredentials(t)
			if _, err := resolveFixtureConfig(); err == nil {
				t.Fatal("expected refusal without CONTROLHUB_E2E_FIXTURE_MODE=1")
			}
		})
	}
}

func TestResolveFixtureConfig_RequiresDedicatedE2EDSN(t *testing.T) {
	t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "")
	stubFixtureCredentials(t)
	if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), "E2E_FIXTURE_DATABASE_DSN") {
		t.Fatalf("expected dedicated-DSN error, got %v", err)
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
			t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
			t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
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

func TestResolveFixtureConfig_NormalizesEmailsOnly(t *testing.T) {
	t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "  E2E.Admin@Fixture.INVALID  ")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "  e2e.Editor@Fixture.Invalid  ")
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
	cfg, err := resolveFixtureConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Fixtures.Admin.Email != "e2e.admin@fixture.invalid" {
		t.Fatalf("admin email = %q, want lowercased and trimmed", cfg.Fixtures.Admin.Email)
	}
	if cfg.Fixtures.Editor.Email != "e2e.editor@fixture.invalid" {
		t.Fatalf("editor email = %q, want lowercased and trimmed", cfg.Fixtures.Editor.Email)
	}
	if cfg.Fixtures.Admin.Password != "admin-pw" || cfg.Fixtures.Editor.Password != "editor-pw" {
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
			t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
			t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
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
	t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
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
	t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", password)
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")

	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
	if _, err := resolveFixtureConfig(); err == nil || strings.Contains(err.Error(), password) {
		t.Fatalf("missing-email error leaks the password: %v", err)
	}
}

func TestParseDisposableDSN_AcceptsLoopbackDisposableDatabase(t *testing.T) {
	for _, dsn := range []string{
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true",
		"controlhub:pass@tcp(localhost:3306)/controlhub_issue15_e2e",
		"controlhub:pass@tcp([::1]:3306)/ops_e2e",
		"controlhub:pass@tcp(127.0.0.1)/controlhub_e2e",
	} {
		t.Run(dsn, func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err != nil {
				t.Fatalf("expected valid disposable DSN, got %v", err)
			}
		})
	}
}

func TestParseDisposableDSN_RejectsEmptyOrMalformed(t *testing.T) {
	for _, dsn := range []string{"", "not-a-dsn", "controlhub:pass@tcp(127.0.0.1:3306)/"} {
		t.Run(dsn, func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err == nil {
				t.Fatal("expected rejection of empty/malformed DSN")
			}
		})
	}
}

func TestParseDisposableDSN_RejectsRemoteOrProductionLikeHosts(t *testing.T) {
	for _, dsn := range []string{
		"controlhub:pass@tcp(db.example.com:3306)/controlhub_e2e",
		"controlhub:pass@tcp(10.0.0.5:3306)/controlhub_e2e",
		"controlhub:pass@tcp(192.168.1.10:3306)/controlhub_e2e",
		"controlhub:pass@tcp(172.17.0.2:3306)/controlhub_e2e",
		"controlhub:pass@tcp(host.docker.internal:3306)/controlhub_e2e",
	} {
		t.Run(dsn, func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err == nil {
				t.Fatal("expected rejection of remote/production-like host")
			}
		})
	}
}

func TestParseDisposableDSN_RejectsDefaultOrNonDisposableDatabaseNames(t *testing.T) {
	for _, dsn := range []string{
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub",
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub_test",
		"controlhub:pass@tcp(127.0.0.1:3306)/production",
		"controlhub:pass@tcp(127.0.0.1:3306)/ControlHub_E2E",
	} {
		t.Run(dsn, func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err == nil {
				t.Fatal("expected rejection of non-disposable database name")
			}
		})
	}
}

// fakeProbe implements fixtureProbe with canned per-query results.
type fakeProbe struct {
	rows map[string]fakeRow
}

type fakeRow struct {
	values []any
	err    error
}

func (f *fakeProbe) QueryRowContext(_ context.Context, query string, args ...any) rowScanner {
	key := query
	for _, a := range args {
		key += "|" + strings.ToLower(a.(string))
	}
	row := f.rows[key]
	return &fakeRow{values: row.values, err: row.err}
}

func (f *fakeRow) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	for i, d := range dest {
		switch v := d.(type) {
		case *int64:
			*v = f.values[i].(int64)
		case *int:
			*v = f.values[i].(int)
		default:
			panic("unhandled scan type")
		}
	}
	return nil
}

func TestVerifyFixtureDatabase_RejectsUnmigratedDatabase(t *testing.T) {
	probe := &fakeProbe{rows: map[string]fakeRow{
		"select max(version_id) from goose_db_version": {values: []any{int64(15)}},
	}}
	err := verifyFixtureDatabase(context.Background(), probe)
	if err == nil || !strings.Contains(err.Error(), "00016") {
		t.Fatalf("expected migration-00016 refusal, got %v", err)
	}
}

func TestVerifyFixtureDatabase_RejectsActiveRetiredSeeds(t *testing.T) {
	probe := &fakeProbe{rows: map[string]fakeRow{
		"select max(version_id) from goose_db_version":                   {values: []any{int64(16)}},
		"select is_active from users where email = ?|admin@example.com":  {values: []any{1}},
		"select is_active from users where email = ?|editor@example.com": {values: []any{0}},
	}}
	err := verifyFixtureDatabase(context.Background(), probe)
	if err == nil || !strings.Contains(err.Error(), "admin@example.com") {
		t.Fatalf("expected active-seed refusal, got %v", err)
	}
}

func TestVerifyFixtureDatabase_AcceptsMigratedDatabaseWithInactiveSeeds(t *testing.T) {
	probe := &fakeProbe{rows: map[string]fakeRow{
		"select max(version_id) from goose_db_version":                   {values: []any{int64(16)}},
		"select is_active from users where email = ?|admin@example.com":  {values: []any{0}},
		"select is_active from users where email = ?|editor@example.com": {values: []any{0}},
	}}
	if err := verifyFixtureDatabase(context.Background(), probe); err != nil {
		t.Fatalf("expected acceptance, got %v", err)
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

func stubFixtureCredentials(t *testing.T) {
	t.Helper()
	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "admin@fixture.invalid")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
}
