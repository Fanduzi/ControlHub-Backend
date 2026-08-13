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
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// seedAdminPasswordHash is the SHA-256 hex of "secret123" published by
// migrations/0002_seed_reference_data.sql for the seeded admin. It remains
// here for reference but is no longer produced by hashPassword (now Argon2id).
const seedAdminPasswordHash = "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"

func TestHashPassword_ProducesArgon2id(t *testing.T) {
	got := hashPassword("secret123")
	if !strings.HasPrefix(got, "$argon2id$") {
		t.Fatalf("hashPassword(secret123) = %s, want Argon2id prefix", got)
	}
	// The same password must produce different hashes (random salt).
	got2 := hashPassword("secret123")
	if got == got2 {
		t.Fatal("two hashPassword calls must produce different salts")
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
	// The generic DATABASE_DSN is set to a perfectly valid disposable DSN: a
	// regression to reading it must FAIL this test.
	t.Setenv("DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "")
	stubFixtureCredentials(t)
	if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), "E2E_FIXTURE_DATABASE_DSN") {
		t.Fatalf("expected dedicated-DSN error even with a valid generic DATABASE_DSN present, got %v", err)
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
			if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), "refusing published seed email") {
				t.Fatalf("expected the explicit retired-seed refusal (not the .invalid gate), got %v", err)
			}
		})
	}
}

func TestResolveFixtureConfig_RefusesNonInvalidTLDIndependently(t *testing.T) {
	t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "someone@example.org")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
	if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), ".invalid") || strings.Contains(err.Error(), "refusing published seed") {
		t.Fatalf("expected the .invalid gate to reject independently of the seed guard, got %v", err)
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

func TestResolveFixtureConfig_RefusesNonASCIIEmails(t *testing.T) {
	for _, email := range []string{"e2e-àdmin@fixture.invalid", "e2e-admin@fïxture.invalid", "e2e-admin@fixture.ïnvalid"} {
		t.Run(email, func(t *testing.T) {
			t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
			t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
			t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", email)
			t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
			t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
			t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
			if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), "printable ASCII") {
				t.Fatalf("expected printable-ASCII refusal for %q, got %v", email, err)
			}
		})
	}
}

func TestResolveFixtureConfig_RefusesControlByteEmails(t *testing.T) {
	for _, email := range []string{"e2e-a\x01dmin@fixture.invalid", "e2e-b\x02@fixture.invalid", "e2e-c\x7f@fixture.invalid", "e2e-d @fixture.invalid"} {
		t.Run(fmt.Sprintf("ctrl-%d", len(email)), func(t *testing.T) {
			t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
			t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
			t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", email)
			t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
			t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "editor@fixture.invalid")
			t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
			if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), "printable ASCII") {
				t.Fatalf("expected printable-ASCII refusal for %q, got %v", email, err)
			}
		})
	}
}

func TestResolveFixtureConfig_RefusesIdenticalAdminEditorEmails(t *testing.T) {
	t.Setenv("CONTROLHUB_E2E_FIXTURE_MODE", "1")
	t.Setenv("E2E_FIXTURE_DATABASE_DSN", "controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true")
	t.Setenv("E2E_FIXTURE_ADMIN_EMAIL", "shared@fixture.invalid")
	t.Setenv("E2E_FIXTURE_ADMIN_PASSWORD", "admin-pw")
	t.Setenv("E2E_FIXTURE_EDITOR_EMAIL", "  Shared@Fixture.Invalid  ")
	t.Setenv("E2E_FIXTURE_EDITOR_PASSWORD", "editor-pw")
	if _, err := resolveFixtureConfig(); err == nil || !strings.Contains(err.Error(), "must be distinct") {
		t.Fatalf("expected refusal of identical admin/editor emails, got %v", err)
	}
}

func TestParseDisposableDSN_AcceptsLoopbackDisposableDatabase(t *testing.T) {
	dsns := []string{
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true",
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub_issue15_e2e",
		"controlhub:pass@tcp([::1]:3306)/controlhub_e2e",
		"controlhub:pass@tcp(127.0.0.1)/controlhub_e2e",
	}
	for i, dsn := range dsns {
		t.Run(fmt.Sprintf("loopback-disposable-%d", i), func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err != nil {
				t.Fatalf("expected valid disposable DSN, got %v", err)
			}
		})
	}
}

func TestParseDisposableDSN_RejectsEmptyOrMalformed(t *testing.T) {
	dsns := []string{"", "not-a-dsn-with-sekret-value-42", "controlhub:pass@tcp(127.0.0.1:3306)/"}
	for i, dsn := range dsns {
		t.Run(fmt.Sprintf("malformed-%d", i), func(t *testing.T) {
			_, err := parseDisposableDSN(dsn)
			if err == nil {
				t.Fatal("expected rejection of empty/malformed DSN")
			}
			if strings.Contains(err.Error(), "sekret") {
				t.Fatalf("malformed-DSN error echoes a secret-bearing token: %v", err)
			}
		})
	}
}

func TestParseDisposableDSN_RejectsRemoteOrProductionLikeHosts(t *testing.T) {
	dsns := []string{
		"controlhub:pass@tcp(db.example.com:3306)/controlhub_e2e",
		"controlhub:pass@tcp(10.0.0.5:3306)/controlhub_e2e",
		"controlhub:pass@tcp(192.168.1.10:3306)/controlhub_e2e",
		"controlhub:pass@tcp(172.17.0.2:3306)/controlhub_e2e",
		"controlhub:pass@tcp(host.docker.internal:3306)/controlhub_e2e",
		"controlhub:pass@tcp(localhost:3306)/controlhub_e2e",
	}
	for i, dsn := range dsns {
		t.Run(fmt.Sprintf("non-loopback-host-%d", i), func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err == nil {
				t.Fatal("expected rejection of remote/production-like host")
			}
		})
	}
}

func TestParseDisposableDSN_RejectsDefaultOrNonDisposableDatabaseNames(t *testing.T) {
	dsns := []string{
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub",
		"controlhub:pass@tcp(127.0.0.1:3306)/controlhub_test",
		"controlhub:pass@tcp(127.0.0.1:3306)/production",
		"controlhub:pass@tcp(127.0.0.1:3306)/production_e2e",
		"controlhub:pass@tcp(127.0.0.1:3306)/ops_e2e",
		"controlhub:pass@tcp(127.0.0.1:3306)/ControlHub_E2E",
	}
	for i, dsn := range dsns {
		t.Run(fmt.Sprintf("non-disposable-dbname-%d", i), func(t *testing.T) {
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
		if s, ok := a.(string); ok {
			key += "|" + strings.ToLower(s)
		} else {
			key += "|" + fmt.Sprint(a)
		}
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
		case *uint64:
			*v = f.values[i].(uint64)
		case *int:
			*v = f.values[i].(int)
		default:
			panic("unhandled scan type")
		}
	}
	return nil
}

func TestVerifyFixtureDatabase_RejectsMissingRetiredSeed(t *testing.T) {
	for _, seed := range []struct{ missing, present string }{
		{"admin@example.com", "editor@example.com"},
		{"editor@example.com", "admin@example.com"},
	} {
		t.Run(seed.missing, func(t *testing.T) {
			probe := &fakeProbe{rows: map[string]fakeRow{
				"select count(*) from goose_db_version where version_id = ? and is_applied = 1|16": {values: []any{1}},
				"select is_active from users where email = ?|" + seed.missing:                      {err: sql.ErrNoRows},
				"select is_active from users where email = ?|" + seed.present:                      {values: []any{0}},
			}}
			err := verifyFixtureDatabase(context.Background(), probe)
			if err == nil || !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), seed.missing) {
				t.Fatalf("expected missing-seed refusal for %s, got %v", seed.missing, err)
			}
		})
	}
}

func TestParseDisposableDSN_RejectsNonTCP(t *testing.T) {
	dsns := []string{
		"controlhub:pass@unix(/tmp/mysql.sock)/controlhub_e2e",
	}
	for i, dsn := range dsns {
		t.Run(fmt.Sprintf("non-tcp-%d", i), func(t *testing.T) {
			if _, err := parseDisposableDSN(dsn); err == nil || !strings.Contains(err.Error(), "tcp") {
				t.Fatalf("expected non-TCP refusal, got %v", err)
			}
		})
	}
}

func TestVerifyFixtureDatabase_RejectsUnappliedMigration16(t *testing.T) {
	probe := &fakeProbe{rows: map[string]fakeRow{
		"select count(*) from goose_db_version where version_id = ? and is_applied = 1|16": {values: []any{0}},
	}}
	err := verifyFixtureDatabase(context.Background(), probe)
	if err == nil || !strings.Contains(err.Error(), "00016") {
		t.Fatalf("expected migration-00016 refusal, got %v", err)
	}
}

func TestVerifyFixtureDatabase_RejectsActiveRetiredSeeds(t *testing.T) {
	for _, seed := range []struct{ active, inactive string }{
		{"admin@example.com", "editor@example.com"},
		{"editor@example.com", "admin@example.com"},
	} {
		t.Run(seed.active, func(t *testing.T) {
			probe := &fakeProbe{rows: map[string]fakeRow{
				"select count(*) from goose_db_version where version_id = ? and is_applied = 1|16": {values: []any{1}},
				"select is_active from users where email = ?|" + seed.active:                       {values: []any{1}},
				"select is_active from users where email = ?|" + seed.inactive:                     {values: []any{0}},
			}}
			err := verifyFixtureDatabase(context.Background(), probe)
			if err == nil || !strings.Contains(err.Error(), "is active") || !strings.Contains(err.Error(), seed.active) {
				t.Fatalf("expected active-seed refusal for %s, got %v", seed.active, err)
			}
		})
	}
}

func TestVerifyFixtureDatabase_AcceptsMigratedDatabaseWithInactiveSeeds(t *testing.T) {
	probe := &fakeProbe{rows: map[string]fakeRow{
		"select count(*) from goose_db_version where version_id = ? and is_applied = 1|16": {values: []any{1}},
		"select is_active from users where email = ?|admin@example.com":                    {values: []any{0}},
		"select is_active from users where email = ?|editor@example.com":                   {values: []any{0}},
	}}
	if err := verifyFixtureDatabase(context.Background(), probe); err != nil {
		t.Fatalf("expected acceptance, got %v", err)
	}
}

func TestPrintReport_NeverContainsPasswordOrHash(t *testing.T) {
	adminPw := "report-admin-pw-1"
	editorPw := "report-editor-pw-2"
	var buf bytes.Buffer
	printReport(&buf, fixtureSet{
		Admin:  fixtureCredential{Email: "admin@fixture.invalid", Password: adminPw, Role: "admin"},
		Editor: fixtureCredential{Email: "editor@fixture.invalid", Password: editorPw, Role: "editor"},
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
	for _, bad := range []string{adminPw, editorPw, hashPassword(adminPw), hashPassword(editorPw), "password", "secret", "hash", "E2E_FIXTURE", "hunter2"} {
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

// fakeExecutor records upsert traffic for the transactional provisioning body.
type fakeExecutor struct {
	roleIDs   map[string]uint64
	affected  map[string]int64
	failExec  bool
	execCalls int
}

func (f *fakeExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execCalls++
	if f.failExec {
		return nil, errors.New("injected upsert failure")
	}
	email := args[0].(string)
	return fakeResult{affected: f.affected[email]}, nil
}

func (f *fakeExecutor) QueryRowContext(_ context.Context, query string, args ...any) rowScanner {
	return &fakeRow{values: []any{f.roleIDs[args[0].(string)]}}
}

type fakeResult struct{ affected int64 }

func (r fakeResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeResult) RowsAffected() (int64, error) { return r.affected, nil }

func TestUpsertFixtures_BothIdentitiesInOneUnit(t *testing.T) {
	exec := &fakeExecutor{
		roleIDs:  map[string]uint64{"admin": 1, "editor": 2},
		affected: map[string]int64{"admin@fixture.invalid": 1, "editor@fixture.invalid": 1},
	}
	set := fixtureSet{
		Admin:  fixtureCredential{Email: "admin@fixture.invalid", Password: "pw", Role: "admin"},
		Editor: fixtureCredential{Email: "editor@fixture.invalid", Password: "pw", Role: "editor"},
	}
	outcomes, err := upsertFixtures(context.Background(), exec, set)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if outcomes["admin@fixture.invalid"] != outcomeCreated || outcomes["editor@fixture.invalid"] != outcomeCreated {
		t.Fatalf("unexpected outcomes: %v", outcomes)
	}
	if exec.execCalls != 2 {
		t.Fatalf("expected exactly two upserts, got %d", exec.execCalls)
	}
}

func TestUpsertFixtures_PropagatesFailureWithoutPartialOutcomes(t *testing.T) {
	exec := &fakeExecutor{
		roleIDs:  map[string]uint64{"admin": 1, "editor": 2},
		affected: map[string]int64{"admin@fixture.invalid": 1},
		failExec: true,
	}
	set := fixtureSet{
		Admin:  fixtureCredential{Email: "admin@fixture.invalid", Password: "pw", Role: "admin"},
		Editor: fixtureCredential{Email: "editor@fixture.invalid", Password: "pw", Role: "editor"},
	}
	if _, err := upsertFixtures(context.Background(), exec, set); err == nil {
		t.Fatal("expected the upsert failure to propagate")
	}
	// The caller (runFixtureBootstrap) rolls back on this error, so no
	// partial outcome set is ever returned.
}

// fakeTxStarter records transaction lifecycle for runFixtureBootstrap.
type fakeTxStarter struct {
	exec       *fakeExecutor
	committed  bool
	rolledBack bool
}

func (f *fakeTxStarter) BeginTx(_ context.Context, _ *sql.TxOptions) (fixtureTransaction, error) {
	return &fakeTx{starter: f}, nil
}

type fakeTx struct {
	starter *fakeTxStarter
}

func (t *fakeTx) Commit() error   { t.starter.committed = true; return nil }
func (t *fakeTx) Rollback() error { t.starter.rolledBack = true; return nil }
func (t *fakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.starter.exec.ExecContext(ctx, query, args...)
}
func (t *fakeTx) QueryRowContext(ctx context.Context, query string, args ...any) rowScanner {
	return t.starter.exec.QueryRowContext(ctx, query, args...)
}

func TestRunFixtureBootstrap_CommitsWhenBothUpsertsSucceed(t *testing.T) {
	starter := &fakeTxStarter{exec: &fakeExecutor{
		roleIDs:  map[string]uint64{"admin": 1, "editor": 2},
		affected: map[string]int64{"admin@fixture.invalid": 1, "editor@fixture.invalid": 1},
	}}
	set := fixtureSet{
		Admin:  fixtureCredential{Email: "admin@fixture.invalid", Password: "pw", Role: "admin"},
		Editor: fixtureCredential{Email: "editor@fixture.invalid", Password: "pw", Role: "editor"},
	}
	if _, err := runFixtureBootstrap(context.Background(), starter, set); err != nil {
		t.Fatalf("err: %v", err)
	}
	if !starter.committed || starter.rolledBack {
		t.Fatal("expected commit without rollback on success")
	}
}

func TestRunFixtureBootstrap_RollsBackWhenAnUpsertFails(t *testing.T) {
	starter := &fakeTxStarter{exec: &fakeExecutor{
		roleIDs:  map[string]uint64{"admin": 1, "editor": 2},
		affected: map[string]int64{"admin@fixture.invalid": 1},
		failExec: true,
	}}
	set := fixtureSet{
		Admin:  fixtureCredential{Email: "admin@fixture.invalid", Password: "pw", Role: "admin"},
		Editor: fixtureCredential{Email: "editor@fixture.invalid", Password: "pw", Role: "editor"},
	}
	if _, err := runFixtureBootstrap(context.Background(), starter, set); err == nil {
		t.Fatal("expected the upsert failure to propagate")
	}
	if starter.committed {
		t.Fatal("must not commit after a failed upsert")
	}
	if !starter.rolledBack {
		t.Fatal("expected rollback after a failed upsert — a partial fixture administrator must never persist")
	}
}
