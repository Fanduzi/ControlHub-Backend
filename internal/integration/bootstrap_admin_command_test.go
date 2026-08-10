//go:build integration

// Package integration exercises the bootstrap-admin command against real MySQL.
// input: os/exec, environment credentials, and the shared migrated MySQL database
// output: TestBootstrapAdminCommandCreatesAndReactivatesAdmin
// pos: Proves the operator CLI creates an authentication-compatible admin and idempotently reactivates it
// note: if this file changes, update header and README.md
package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestBootstrapAdminCommandCreatesAndReactivatesAdmin(t *testing.T) {
	// Given
	db := setupTestDB(t)
	email := fmt.Sprintf("bootstrap-admin-%d@example.invalid", os.Getpid())
	firstPassword := "bootstrap-first-password-13"
	secondPassword := "bootstrap-second-password-13"
	var userID uint64
	t.Cleanup(func() {
		if userID != 0 {
			deleteAuthzTestUser(t, db, userID)
			return
		}
		if _, err := db.Exec(`delete from users where email = ?`, email); err != nil {
			t.Errorf("cleanup bootstrap user %s: %v", email, err)
		}
	})

	// When
	firstOutput := runBootstrapAdminCommand(t, email, firstPassword)

	// Then
	if !strings.Contains(firstOutput, "email:   "+email) || !strings.Contains(firstOutput, "outcome: created") {
		t.Fatalf("bootstrap-admin create output missing email/outcome:\n%s", firstOutput)
	}
	if strings.Contains(firstOutput, firstPassword) {
		t.Fatalf("bootstrap-admin create output leaks password:\n%s", firstOutput)
	}
	var firstActive int
	var firstVersion uint64
	var roleName string
	err := db.QueryRow(`
		select users.id, users.is_active, users.authorization_version, roles.name
		from users join roles on roles.id = users.role_id
		where users.email = ?`, email).Scan(&userID, &firstActive, &firstVersion, &roleName)
	if err != nil {
		t.Fatalf("read created bootstrap user: %v", err)
	}
	if firstActive != 1 || roleName != "admin" {
		t.Fatalf("created user active/role = %d/%q, want 1/admin", firstActive, roleName)
	}
	auth := service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret)
	if _, err := auth.Login(email, firstPassword); err != nil {
		t.Fatalf("created password did not authenticate: %v", err)
	}

	// Given
	if _, err := db.Exec(`update users set is_active = 0 where id = ?`, userID); err != nil {
		t.Fatalf("disable bootstrap user before rerun: %v", err)
	}

	// When
	secondOutput := runBootstrapAdminCommand(t, email, secondPassword)

	// Then
	if !strings.Contains(secondOutput, "email:   "+email) || !strings.Contains(secondOutput, "outcome: reactivated") {
		t.Fatalf("bootstrap-admin rerun output missing email/outcome:\n%s", secondOutput)
	}
	if strings.Contains(secondOutput, secondPassword) {
		t.Fatalf("bootstrap-admin rerun output leaks password:\n%s", secondOutput)
	}
	var count int
	var secondActive int
	var secondVersion uint64
	err = db.QueryRow(`select count(*) from users where email = ?`, email).Scan(&count)
	if err != nil {
		t.Fatalf("count bootstrap users: %v", err)
	}
	if count != 1 {
		t.Fatalf("bootstrap user count = %d, want 1", count)
	}
	err = db.QueryRow(`select is_active, authorization_version from users where id = ?`, userID).Scan(&secondActive, &secondVersion)
	if err != nil {
		t.Fatalf("read reactivated bootstrap user: %v", err)
	}
	if secondActive != 1 {
		t.Fatalf("reactivated user is_active = %d, want 1", secondActive)
	}
	if secondVersion <= firstVersion {
		t.Fatalf("authorization_version = %d after rerun, want greater than %d", secondVersion, firstVersion)
	}
	if _, err := auth.Login(email, firstPassword); err != service.ErrInvalidCredentials {
		t.Fatalf("old password login error = %v, want %v", err, service.ErrInvalidCredentials)
	}
	if _, err := auth.Login(email, secondPassword); err != nil {
		t.Fatalf("rotated password did not authenticate: %v", err)
	}
}

func runBootstrapAdminCommand(t *testing.T, email, password string) string {
	t.Helper()
	root := filepath.Dir(resolveMigrationsDir())
	cmd := exec.Command("go", "run", "./cmd/bootstrap-admin")
	cmd.Dir = root
	cmd.Env = commandEnvironment(email, password)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap-admin command: %v\noutput:\n%s", err, output)
	}
	return string(output)
}

func commandEnvironment(email, password string) []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "DATABASE_DSN=") ||
			strings.HasPrefix(entry, "BOOTSTRAP_ADMIN_EMAIL=") ||
			strings.HasPrefix(entry, "BOOTSTRAP_ADMIN_PASSWORD=") {
			continue
		}
		env = append(env, entry)
	}
	return append(env,
		"DATABASE_DSN="+globalEnv.dsn,
		"BOOTSTRAP_ADMIN_EMAIL="+email,
		"BOOTSTRAP_ADMIN_PASSWORD="+password,
	)
}
