//go:build integration

// Package integration provides real-MySQL migration rollback evidence.
// input: testing, strings, database/sql, pressly/goose, migration 00027
// output: TestCollectorScanMigrationDownGuard
// pos: Proves migration 00027 cannot erase collector idempotency or Missing evidence during downgrade
// note: if this file changes, update this header and module README.md.
package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestCollectorScanMigrationDownGuard(t *testing.T) {
	tests := []struct {
		name     string
		database string
		insert   string
		table    string
	}{
		{
			name:     "preserves completed scan idempotency evidence",
			database: "controlhub_collector_scan_down_ledger",
			insert: `INSERT INTO collector_scan_ledger
				(machine_principal_id, collector_scan_id, payload_hash, result, completed_at)
				VALUES (1, 'scan-1', UNHEX(SHA2('payload', 256)), 'complete', NOW(6))`,
			table: "collector_scan_ledger",
		},
		{
			name:     "preserves per-CI Missing lifecycle evidence",
			database: "controlhub_collector_scan_down_state",
			insert: `INSERT INTO collector_ci_scan_states
				(machine_principal_id, resource_id, last_seen_collector_scan_id)
				VALUES (1, 1, 'scan-1')`,
			table: "collector_ci_scan_states",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupMigration27TestDB(t, tt.database)
			mustExec(t, db, tt.insert)

			assertMigration27DownBlocked(t, db)
			if got := tableRowCount(t, db, tt.table); got != 1 {
				t.Fatalf("%s rows after blocked downgrade = %d, want 1", tt.table, got)
			}

			mustExec(t, db, "DELETE FROM "+tt.table)
			assertMigration27DownAllowed(t, db)
		})
	}

	t.Run("allows empty lifecycle storage", func(t *testing.T) {
		db := setupMigration27TestDB(t, "controlhub_collector_scan_down_empty")
		assertMigration27DownAllowed(t, db)
	})
}

func setupMigration27TestDB(t *testing.T, databaseName string) *sql.DB {
	t.Helper()
	admin := setupTestDB(t)
	dropDatabase(t, admin, databaseName)
	createDatabase(t, admin, databaseName)
	t.Cleanup(func() { dropDatabase(t, admin, databaseName) })
	db := openNamedTestDB(t, databaseName)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 27); err != nil {
		t.Fatalf("migrate fixture to 27: %v", err)
	}
	return db
}

func assertMigration27DownBlocked(t *testing.T, db *sql.DB) {
	t.Helper()
	err := goose.DownTo(db, resolveMigrationsDir(), 26)
	if err == nil || !strings.Contains(err.Error(), "cannot roll back migration 00027 while collector scan lifecycle data exists") {
		t.Fatalf("migration down error = %v, want explicit data-preservation refusal", err)
	}
	if !tableExists(t, db, "collector_scan_ledger") || !tableExists(t, db, "collector_ci_scan_states") {
		t.Fatal("blocked downgrade changed migration 00027 schema")
	}
}

func assertMigration27DownAllowed(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := goose.DownTo(db, resolveMigrationsDir(), 26); err != nil {
		t.Fatalf("empty migration 00027 down: %v", err)
	}
	if tableExists(t, db, "collector_scan_ledger") || tableExists(t, db, "collector_ci_scan_states") {
		t.Fatal("empty downgrade retained migration 00027 schema")
	}
}
