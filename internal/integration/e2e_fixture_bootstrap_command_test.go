//go:build integration

// Package integration exercises the e2e-fixture-bootstrap CLI against real MySQL.
// input: os/exec, database/sql, testing, context, fmt, strings, os, path/filepath, github.com/pressly/goose/v3, github.com/fan/controlhub/internal/repository/mysql, github.com/fan/controlhub/internal/service
// output: TestE2EFixtureBootstrapCommandProvisionsAndRollsBackAgainstMySQL
// pos: Proves the test/CI-only fixture CLI provisions BOTH fixture operators on a
// disposable controlhub_*_e2e database, keeps the retired 0002 seeds inactive,
// reactivates idempotently with authorization_version rotation, never leaks
// secrets, and rolls back the whole transaction when one identity cannot be
// provisioned.
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestE2EFixtureBootstrapCommandProvisionsAndRollsBackAgainstMySQL runs the
// real `go run ./cmd/e2e-fixture-bootstrap` CLI against a dedicated
// disposable `controlhub_*_e2e` database (literal loopback DSN) and verifies:
// creation of admin+editor fixtures, retired seeds stay inactive, idempotent
// reactivation with authorization_version rotation, secret-free output, and
// whole-transaction rollback when the editor identity cannot be provisioned.
func TestE2EFixtureBootstrapCommandProvisionsAndRollsBackAgainstMySQL(t *testing.T) {
	ctx := context.Background()

	// Dedicated disposable database inside the shared Testcontainers MySQL.
	dbName := fmt.Sprintf("controlhub_issue15_%d_e2e", os.Getpid())
	port, err := globalEnv.container.MappedPort(ctx, "3306")
	if err != nil {
		t.Fatalf("get container port: %v", err)
	}
	// Literal loopback (not a hostname) so the fixture DSN gate accepts it.
	fixtureDSN := fmt.Sprintf("root:test@tcp(127.0.0.1:%s)/%s?parseTime=true&charset=utf8mb4", port.Port(), dbName)

	if _, err := globalEnv.db.ExecContext(ctx, "create database "+dbName); err != nil {
		t.Fatalf("create disposable e2e database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := globalEnv.db.ExecContext(context.Background(), "drop database if exists "+dbName); err != nil {
			t.Errorf("drop disposable e2e database %s: %v", dbName, err)
		}
	})

	fxdb, err := sql.Open("mysql", fixtureDSN)
	if err != nil {
		t.Fatalf("open disposable e2e database: %v", err)
	}
	t.Cleanup(func() { fxdb.Close() })
	if err := fxdb.PingContext(ctx); err != nil {
		t.Fatalf("ping disposable e2e database: %v", err)
	}
	if err := goose.UpContext(ctx, fxdb, resolveMigrationsDir()); err != nil {
		t.Fatalf("migrate disposable e2e database: %v", err)
	}

	auth := service.NewAuthService(mysql.NewUserRepository(fxdb), authzIntegrationSecret)

	adminEmail := fmt.Sprintf("e2e-admin-%d@controlhub-e2e.invalid", os.Getpid())
	editorEmail := fmt.Sprintf("e2e-editor-%d@controlhub-e2e.invalid", os.Getpid())
	adminPw1 := fmt.Sprintf("e2e-admin-pw-%d", os.Getpid())
	editorPw1 := fmt.Sprintf("e2e-editor-pw-%d", os.Getpid())

	// When: first provision
	out, err := runE2EFixtureCommand(t, fixtureDSN, adminEmail, adminPw1, editorEmail, editorPw1)
	if err != nil {
		t.Fatalf("e2e-fixture-bootstrap create: %v\noutput:\n%s", err, out)
	}

	// Then: both fixtures created, output is secret-free
	for _, want := range []string{"email:   " + adminEmail, "email:   " + editorEmail, "outcome: created"} {
		if !strings.Contains(out, want) {
			t.Fatalf("create output missing %q:\n%s", want, out)
		}
	}
	assertFixtureReportHasNoSecrets(t, out, fixtureDSN, adminPw1, editorPw1)

	var adminID, editorID uint64
	assertFixtureUser(t, fxdb, adminEmail, "admin", &adminID)
	assertFixtureUser(t, fxdb, editorEmail, "editor", &editorID)
	assertRetiredSeedsInactive(t, fxdb)
	for _, c := range []struct{ email, pw string }{{adminEmail, adminPw1}, {editorEmail, editorPw1}} {
		if _, err := auth.Login(c.email, c.pw); err != nil {
			t.Fatalf("fixture %s did not authenticate: %v", c.email, err)
		}
	}

	adminVer1 := fixtureAuthorizationVersion(t, fxdb, adminEmail)
	editorVer1 := fixtureAuthorizationVersion(t, fxdb, editorEmail)

	// When: re-provision with rotated credentials
	adminPw2 := fmt.Sprintf("e2e-admin-pw2-%d", os.Getpid())
	editorPw2 := fmt.Sprintf("e2e-editor-pw2-%d", os.Getpid())
	out, err = runE2EFixtureCommand(t, fixtureDSN, adminEmail, adminPw2, editorEmail, editorPw2)
	if err != nil {
		t.Fatalf("e2e-fixture-bootstrap reactivate: %v\noutput:\n%s", err, out)
	}

	// Then: both reactivated, versions rotated, old passwords dead, seeds still inactive
	for _, want := range []string{"email:   " + adminEmail, "email:   " + editorEmail, "outcome: reactivated"} {
		if !strings.Contains(out, want) {
			t.Fatalf("reactivate output missing %q:\n%s", want, out)
		}
	}
	assertFixtureReportHasNoSecrets(t, out, fixtureDSN, adminPw2, editorPw2)
	var adminActive, editorActive int
	if err := fxdb.QueryRow(`select is_active from users where id = ?`, adminID).Scan(&adminActive); err != nil {
		t.Fatalf("read admin active state: %v", err)
	}
	if err := fxdb.QueryRow(`select is_active from users where id = ?`, editorID).Scan(&editorActive); err != nil {
		t.Fatalf("read editor active state: %v", err)
	}
	if adminActive != 1 || editorActive != 1 {
		t.Fatalf("reactivated fixtures active = %d/%d, want 1/1", adminActive, editorActive)
	}
	if v := fixtureAuthorizationVersion(t, fxdb, adminEmail); v <= adminVer1 {
		t.Fatalf("admin authorization_version = %d after rerun, want > %d", v, adminVer1)
	}
	if v := fixtureAuthorizationVersion(t, fxdb, editorEmail); v <= editorVer1 {
		t.Fatalf("editor authorization_version = %d after rerun, want > %d", v, editorVer1)
	}
	for _, c := range []struct{ email, pw string }{{adminEmail, adminPw1}, {editorEmail, editorPw1}} {
		if _, err := auth.Login(c.email, c.pw); err != service.ErrInvalidCredentials {
			t.Fatalf("old password for %s error = %v, want %v", c.email, err, service.ErrInvalidCredentials)
		}
	}
	for _, c := range []struct{ email, pw string }{{adminEmail, adminPw2}, {editorEmail, editorPw2}} {
		if _, err := auth.Login(c.email, c.pw); err != nil {
			t.Fatalf("rotated password for %s did not authenticate: %v", c.email, err)
		}
	}
	assertRetiredSeedsInactive(t, fxdb)
	adminVer2 := fixtureAuthorizationVersion(t, fxdb, adminEmail)

	// When: the editor identity cannot be provisioned (role lookup fails)
	rollbackAdmin := fmt.Sprintf("e2e-admin-%d-rollback@controlhub-e2e.invalid", os.Getpid())
	rollbackEditor := fmt.Sprintf("e2e-editor-%d-rollback@controlhub-e2e.invalid", os.Getpid())
	if _, err := fxdb.Exec(`update roles set name = 'editor_disabled_test' where name = 'editor'`); err != nil {
		t.Fatalf("disable editor role for rollback scenario: %v", err)
	}
	t.Cleanup(func() {
		if _, err := fxdb.Exec(`update roles set name = 'editor' where name = 'editor_disabled_test'`); err != nil {
			t.Errorf("restore editor role: %v", err)
		}
	})
	out, err = runE2EFixtureCommand(t, fixtureDSN, rollbackAdmin, adminPw2, rollbackEditor, editorPw2)

	// Then: command fails loudly and the whole transaction rolled back
	if err == nil {
		t.Fatalf("expected rollback-scenario failure, command succeeded:\n%s", out)
	}
	assertFixtureReportHasNoSecrets(t, out, fixtureDSN, adminPw2, editorPw2)
	var partial int
	if err := fxdb.QueryRow(`select count(*) from users where email in (?, ?)`, rollbackAdmin, rollbackEditor).Scan(&partial); err != nil {
		t.Fatalf("count partial fixtures: %v", err)
	}
	if partial != 0 {
		t.Fatalf("rollback left %d partial fixture rows behind", partial)
	}
	if v := fixtureAuthorizationVersion(t, fxdb, adminEmail); v != adminVer2 {
		t.Fatalf("existing admin authorization_version changed after rolled-back run: %d, want %d", v, adminVer2)
	}
}

func runE2EFixtureCommand(t *testing.T, dsn, adminEmail, adminPw, editorEmail, editorPw string) (string, error) {
	t.Helper()
	root := filepath.Dir(resolveMigrationsDir())
	cmd := exec.Command("go", "run", "./cmd/e2e-fixture-bootstrap")
	cmd.Dir = root
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "E2E_FIXTURE_") ||
			strings.HasPrefix(entry, "CONTROLHUB_E2E_FIXTURE_MODE=") ||
			strings.HasPrefix(entry, "DATABASE_DSN=") {
			continue
		}
		env = append(env, entry)
	}
	cmd.Env = append(env,
		"CONTROLHUB_E2E_FIXTURE_MODE=1",
		"E2E_FIXTURE_DATABASE_DSN="+dsn,
		"E2E_FIXTURE_ADMIN_EMAIL="+adminEmail,
		"E2E_FIXTURE_ADMIN_PASSWORD="+adminPw,
		"E2E_FIXTURE_EDITOR_EMAIL="+editorEmail,
		"E2E_FIXTURE_EDITOR_PASSWORD="+editorPw,
	)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func assertFixtureUser(t *testing.T, db *sql.DB, email, wantRole string, id *uint64) {
	t.Helper()
	var active int
	var roleName string
	if err := db.QueryRow(`
		select users.id, users.is_active, roles.name
		from users join roles on roles.id = users.role_id
		where users.email = ?`, email).Scan(id, &active, &roleName); err != nil {
		t.Fatalf("read fixture %s: %v", email, err)
	}
	if active != 1 || roleName != wantRole {
		t.Fatalf("fixture %s active/role = %d/%q, want 1/%s", email, active, roleName, wantRole)
	}
}

func assertRetiredSeedsInactive(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, email := range []string{"admin@example.com", "editor@example.com"} {
		var active int
		if err := db.QueryRow(`select is_active from users where email = ?`, email).Scan(&active); err != nil {
			t.Fatalf("read retired seed %s: %v", email, err)
		}
		if active != 0 {
			t.Fatalf("retired seed %s is active after fixture provisioning", email)
		}
	}
}

func fixtureAuthorizationVersion(t *testing.T, db *sql.DB, email string) uint64 {
	t.Helper()
	var version uint64
	if err := db.QueryRow(`select authorization_version from users where email = ?`, email).Scan(&version); err != nil {
		t.Fatalf("read authorization_version for %s: %v", email, err)
	}
	return version
}

func assertFixtureReportHasNoSecrets(t *testing.T, output, dsn, adminPw, editorPw string) {
	t.Helper()
	for _, needle := range []string{dsn, adminPw, editorPw, "root:test", "password", "secret", "hash"} {
		if strings.Contains(output, needle) {
			t.Fatalf("fixture command output leaks %q:\n%s", needle, output)
		}
	}
}
