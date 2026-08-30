//go:build integration

// Package integration provides real-MySQL machine-principal lifecycle proofs.
// input: bytes, context, database/sql, encoding/json, errors, fmt, log, strings, testing, time, internal/model, internal/repository/mysql, internal/service
// output: one-time secret, hash-only persistence, audit absence, expiry, revoke, overlap, last-used, all-seven-scope, and rollback tests
// pos: Real-MySQL security-boundary coverage for machine principals at schema version 25
// note: if this file changes, update this header and module README.md.
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestMachinePrincipalCredentialLifecycle(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewMachinePrincipalRepository(db)
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	svc := service.NewMachinePrincipalService(repo).WithClock(func() time.Time { return now })
	admin := service.AuthenticatedUser{ID: 860001, Role: "admin"}

	var logs bytes.Buffer
	originalLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLogOutput) })

	oldIssue, err := svc.Create(context.Background(), admin, model.MachinePrincipalCreateRequest{
		Name: "issue-86-inventory-agent",
		Scopes: []model.MachineScope{
			model.MachineScopeInventoryRead,
			model.MachineScopeNamedViewsRead,
		},
	})
	if err != nil {
		t.Fatalf("create machine principal: %v", err)
	}
	if oldIssue.Secret == "" {
		t.Fatal("create did not return the one-time plaintext credential")
	}
	assertCredentialHashOnly(t, db, oldIssue)

	identity, err := svc.Authenticate(context.Background(), oldIssue.Secret, model.MachineScopeNamedViewsRead)
	if err != nil {
		t.Fatalf("authenticate old credential: %v", err)
	}
	identityJSON, err := json.Marshal(identity)
	if err != nil {
		t.Fatalf("marshal identity: %v", err)
	}
	if bytes.Contains(identityJSON, []byte(oldIssue.Secret)) {
		t.Fatal("authenticated identity repeated the one-time plaintext secret")
	}
	if _, err := svc.Authenticate(context.Background(), oldIssue.Secret, model.MachineScopeRelationsRead); !errors.Is(err, service.ErrMachineScopeDenied) {
		t.Fatalf("missing scope error = %v, want controlled scope denial", err)
	}
	var lastUsedAt sql.NullTime
	if err := db.QueryRow(`SELECT last_used_at FROM machine_principal_credentials WHERE id = ?`, oldIssue.Credential.ID).Scan(&lastUsedAt); err != nil {
		t.Fatalf("read last_used_at: %v", err)
	}
	if !lastUsedAt.Valid || !lastUsedAt.Time.Equal(now) {
		t.Fatalf("last_used_at = %v, want %v", lastUsedAt, now)
	}

	newIssue, err := svc.Rotate(context.Background(), admin, oldIssue.Credential.ID, model.MachineCredentialRotateRequest{
		Scopes: []model.MachineScope{model.MachineScopeInventoryRead},
	})
	if err != nil {
		t.Fatalf("rotate credential: %v", err)
	}
	for name, token := range map[string]string{"old": oldIssue.Secret, "new": newIssue.Secret} {
		if _, err := svc.Authenticate(context.Background(), token, model.MachineScopeInventoryRead); err != nil {
			t.Fatalf("%s credential during overlap: %v", name, err)
		}
	}
	if err := svc.Revoke(context.Background(), admin, oldIssue.Credential.ID); err != nil {
		t.Fatalf("revoke old credential: %v", err)
	}
	if _, err := svc.Authenticate(context.Background(), oldIssue.Secret, model.MachineScopeInventoryRead); !errors.Is(err, service.ErrMachineCredentialRevoked) {
		t.Fatalf("revoked credential error = %v, want controlled revoke denial", err)
	}
	if _, err := svc.Authenticate(context.Background(), newIssue.Secret, model.MachineScopeInventoryRead); err != nil {
		t.Fatalf("new credential after old revoke: %v", err)
	}

	expiresAt := now.Add(time.Hour)
	expiringIssue, err := svc.Rotate(context.Background(), admin, newIssue.Credential.ID, model.MachineCredentialRotateRequest{
		Scopes: []model.MachineScope{model.MachineScopeAuditRead}, ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("rotate expiring credential: %v", err)
	}
	now = expiresAt
	if _, err := svc.Authenticate(context.Background(), expiringIssue.Secret, model.MachineScopeAuditRead); !errors.Is(err, service.ErrMachineCredentialExpired) {
		t.Fatalf("expired credential error = %v, want controlled expiry denial", err)
	}

	for _, secret := range []string{oldIssue.Secret, newIssue.Secret, expiringIssue.Secret} {
		assertSecretAbsentFromDatabase(t, db, secret)
		if strings.Contains(logs.String(), secret) {
			t.Fatal("plaintext machine credential entered logs")
		}
	}
	assertMachinePrincipalAuditActors(t, db, admin.ID)
}

func TestMachinePrincipalCredentialPersistsAllSevenScopes(t *testing.T) {
	db := setupTestDB(t)
	svc := service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db))
	scopes := []model.MachineScope{
		model.MachineScopeInventoryRead,
		model.MachineScopeRelationsRead,
		model.MachineScopeGovernedSelect,
		model.MachineScopeAuditRead,
		model.MachineScopeNamedViewsRead,
		model.MachineScopeInventoryIngest,
		model.MachineScopeHealthWrite,
	}
	issued, err := svc.Create(context.Background(), service.AuthenticatedUser{ID: 860001, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "all-seven-scope-agent", Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("create all-seven-scope credential: %v", err)
	}
	for _, scope := range scopes {
		if _, err := svc.Authenticate(context.Background(), issued.Secret, scope); err != nil {
			t.Fatalf("authenticate persisted scope %q: %v", scope, err)
		}
	}
}

func TestMachinePrincipalCreateRollsBackWhenAuditFails(t *testing.T) {
	db := setupTestDB(t)
	beforePrincipals := tableRowCount(t, db, "machine_principals")
	beforeCredentials := tableRowCount(t, db, "machine_principal_credentials")
	beforeAudits := tableRowCount(t, db, "audit_events")

	if _, err := db.Exec(`CREATE TRIGGER issue86_force_machine_audit_fail
		BEFORE INSERT ON audit_events FOR EACH ROW
		SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'forced machine audit failure'`); err != nil {
		t.Fatalf("create audit failure trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DROP TRIGGER IF EXISTS issue86_force_machine_audit_fail`) })

	svc := service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db))
	_, err := svc.Create(context.Background(), service.AuthenticatedUser{ID: 860001, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "issue-86-rollback-agent", Scopes: []model.MachineScope{model.MachineScopeInventoryRead},
	})
	if err == nil {
		t.Fatal("expected create failure when administrator audit insert fails")
	}
	if got := tableRowCount(t, db, "machine_principals"); got != beforePrincipals {
		t.Fatalf("machine principals after rollback = %d, want %d", got, beforePrincipals)
	}
	if got := tableRowCount(t, db, "machine_principal_credentials"); got != beforeCredentials {
		t.Fatalf("machine credentials after rollback = %d, want %d", got, beforeCredentials)
	}
	if got := tableRowCount(t, db, "audit_events"); got != beforeAudits {
		t.Fatalf("audit rows after rollback = %d, want %d", got, beforeAudits)
	}
}

func assertCredentialHashOnly(t *testing.T, db *sql.DB, issued model.MachineCredentialIssue) {
	t.Helper()
	var lookupID string
	var hash []byte
	if err := db.QueryRow(`SELECT lookup_id, secret_hash FROM machine_principal_credentials WHERE id = ?`, issued.Credential.ID).Scan(&lookupID, &hash); err != nil {
		t.Fatalf("read stored credential: %v", err)
	}
	if lookupID != issued.Credential.LookupID || len(hash) != 32 || bytes.Equal(hash, []byte(issued.Secret)) {
		t.Fatalf("stored credential is not lookup-id plus 32-byte irreversible hash")
	}
}

func assertSecretAbsentFromDatabase(t *testing.T, db *sql.DB, secret string) {
	t.Helper()
	rows, err := db.Query(`SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = DATABASE()
		AND data_type IN ('char','varchar','tinytext','text','mediumtext','longtext','binary','varbinary','tinyblob','blob','mediumblob','longblob','json')`)
	if err != nil {
		t.Fatalf("list secret-capable columns: %v", err)
	}
	defer rows.Close()
	type column struct{ table, name string }
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.table, &c.name); err != nil {
			t.Fatalf("scan secret-capable column: %v", err)
		}
		columns = append(columns, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate secret-capable columns: %v", err)
	}
	for _, c := range columns {
		query := fmt.Sprintf("SELECT COUNT(*) FROM `%s` WHERE LOCATE(?, CAST(`%s` AS CHAR)) > 0", strings.ReplaceAll(c.table, "`", "``"), strings.ReplaceAll(c.name, "`", "``"))
		var count int
		if err := db.QueryRow(query, secret).Scan(&count); err != nil {
			t.Fatalf("scan %s.%s for plaintext credential: %v", c.table, c.name, err)
		}
		if count != 0 {
			t.Fatalf("plaintext machine credential found in %s.%s", c.table, c.name)
		}
	}
}

func assertMachinePrincipalAuditActors(t *testing.T, db *sql.DB, actorID uint64) {
	t.Helper()
	rows, err := db.Query(`SELECT actor_user_id, target_resource_id, changes FROM audit_events
		WHERE actor_user_id = ? AND event_type IN ('machine_principal.created','machine_credential.rotated','machine_credential.revoked')`, actorID)
	if err != nil {
		t.Fatalf("read machine-principal audits: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var actor uint64
		var target sql.NullInt64
		var changes []byte
		if err := rows.Scan(&actor, &target, &changes); err != nil {
			t.Fatalf("scan machine-principal audit: %v", err)
		}
		if actor != actorID || target.Valid || !json.Valid(changes) {
			t.Fatalf("unsafe machine-principal audit actor=%d target=%v changes=%q", actor, target, changes)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate machine-principal audits: %v", err)
	}
	if count != 4 {
		t.Fatalf("machine-principal audit count = %d, want 4", count)
	}
}

func tableRowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM `" + strings.ReplaceAll(table, "`", "``") + "`").Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
