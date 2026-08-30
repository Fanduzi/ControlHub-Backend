//go:build integration

// Package integration provides real-MySQL migration rollback evidence.
// input: testing, strings, database/sql, pressly/goose, migration 00028
// output: TestQueryWorkspaceMigrationDownGuard*
// pos: Proves migration 00028 cannot erase workspace drafts or reusable full SQL during downgrade
// note: if this file changes, update this header and module README.md.
package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestQueryWorkspaceMigrationDownGuardPreservesWorkspaceDrafts(t *testing.T) {
	db := setupMigration28TestDB(t, "controlhub_query_workspace_down_drafts")
	mustExec(t, db, `INSERT INTO query_workspaces (owner_user_id, worksheets, version) VALUES (1, JSON_ARRAY(), 1)`)

	assertMigration28DownBlocked(t, db)
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_workspaces`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("workspace rows after blocked downgrade = %d, %v; want 1, nil", rows, err)
	}

	mustExec(t, db, `DELETE FROM query_workspaces`)
	assertMigration28DownAllowed(t, db)
}

func TestQueryWorkspaceMigrationDownGuardPreservesAnyNonNullFullStatement(t *testing.T) {
	db := setupMigration28TestDB(t, "controlhub_query_workspace_down_statement")
	mustExec(t, db, `INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, full_statement, status)
		VALUES (1, 1, 'mysql', 'digest', 'preview', '', 'success')`)

	assertMigration28DownBlocked(t, db)
	if !columnExists(t, db, "query_executions", "full_statement") {
		t.Fatal("blocked downgrade dropped query_executions.full_statement")
	}

	mustExec(t, db, `UPDATE query_executions SET full_statement = NULL`)
	assertMigration28DownAllowed(t, db)
}

func TestQueryWorkspaceMigrationDownGuardAllowsEmptyStorage(t *testing.T) {
	db := setupMigration28TestDB(t, "controlhub_query_workspace_down_empty")
	assertMigration28DownAllowed(t, db)
}

func setupMigration28TestDB(t *testing.T, databaseName string) *sql.DB {
	t.Helper()
	admin := setupTestDB(t)
	dropDatabase(t, admin, databaseName)
	createDatabase(t, admin, databaseName)
	t.Cleanup(func() { dropDatabase(t, admin, databaseName) })
	db := openNamedTestDB(t, databaseName)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 28); err != nil {
		t.Fatalf("migrate fixture to 28: %v", err)
	}
	return db
}

func assertMigration28DownBlocked(t *testing.T, db *sql.DB) {
	t.Helper()
	err := goose.DownTo(db, resolveMigrationsDir(), 27)
	if err == nil || !strings.Contains(err.Error(), "cannot roll back migration 00028 while query workspace or full statement data exists") {
		t.Fatalf("migration down error = %v, want explicit data-preservation refusal", err)
	}
	if !tableExists(t, db, "query_workspaces") || !columnExists(t, db, "query_executions", "full_statement") {
		t.Fatal("blocked downgrade changed migration 00028 schema")
	}
}

func assertMigration28DownAllowed(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.DownTo(db, resolveMigrationsDir(), 27); err != nil {
		t.Fatalf("empty migration 00028 down: %v", err)
	}
	if tableExists(t, db, "query_workspaces") || columnExists(t, db, "query_executions", "full_statement") {
		t.Fatal("empty downgrade retained migration 00028 schema")
	}
}
