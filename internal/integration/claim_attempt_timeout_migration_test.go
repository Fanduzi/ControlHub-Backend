//go:build integration

// Package integration provides real-MySQL evidence for migration 00032.
// input: database/sql, strings, testing, pressly/goose, migration 00032
// output: TestClaimAttemptTimeoutMigration* — up adds the nullable per-attempt budget column; down refuses only while a stored budget would be destroyed
// pos: Guards the T10-C running/unknown derivation threshold at the schema layer (issue #118)
// note: if this file changes, update header and README.md
package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func claimAttemptTimeoutColumn(t *testing.T, db *sql.DB) (colType, nullable string, found bool) {
	t.Helper()
	err := db.QueryRow(`SELECT COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'query_execution_claims'
		AND COLUMN_NAME = 'attempt_timeout_ms'`).Scan(&colType, &nullable)
	if err == sql.ErrNoRows {
		return "", "", false
	}
	if err != nil {
		t.Fatalf("attempt_timeout_ms shape: %v", err)
	}
	return colType, nullable, true
}

func TestClaimAttemptTimeoutMigrationUpDownEmpty(t *testing.T) {
	db := freshClaimMigrationDB(t, "controlhub_claim_timeout_up", 31)

	if err := goose.UpTo(db, resolveMigrationsDir(), 32); err != nil {
		t.Fatalf("migration 00032 up: %v", err)
	}
	colType, nullable, found := claimAttemptTimeoutColumn(t, db)
	if !found {
		t.Fatal("attempt_timeout_ms missing after 00032 up")
	}
	// Nullable: pre-T10-C claims carry NULL and derive running only — no
	// invented budget is backfilled for them.
	if colType != "bigint unsigned" || nullable != "YES" {
		t.Fatalf("attempt_timeout_ms = %q/%q, want bigint unsigned/YES", colType, nullable)
	}

	// Down on an empty claim ledger is permitted: nothing is destroyed.
	if err := goose.DownTo(db, resolveMigrationsDir(), 31); err != nil {
		t.Fatalf("down on empty claims = %v, want success", err)
	}
	if _, _, found := claimAttemptTimeoutColumn(t, db); found {
		t.Fatal("attempt_timeout_ms still present after down")
	}
}

func TestClaimAttemptTimeoutMigrationDownGuard(t *testing.T) {
	// A recorded budget is per-attempt evidence: dropping the column would
	// destroy it, so the rollback refuses before any DDL runs — even when the
	// row is otherwise unlinked.
	db := freshClaimMigrationDB(t, "controlhub_claim_timeout_down", 32)
	mustExec(t, db, `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, attempt_timeout_ms, claimed_at)
		VALUES (1, 'budgeted', 7, 'db', 'app', ?, 3000, NOW(6))`,
		strings.Repeat("a", 64))

	if err := goose.DownTo(db, resolveMigrationsDir(), 31); err == nil || !strings.Contains(err.Error(), "45000") {
		t.Fatalf("down with recorded budget = %v, want 45000 refusal", err)
	}
	if v := claimVersion(t, db); v != 32 {
		t.Fatalf("version after refused down = %d, want 32", v)
	}
	if _, _, found := claimAttemptTimeoutColumn(t, db); !found {
		t.Fatal("attempt_timeout_ms dropped despite refusal")
	}

	// A claim with no recorded budget does not block the rollback: NULL
	// carries no budget evidence to destroy.
	db2 := freshClaimMigrationDB(t, "controlhub_claim_timeout_down_null", 32)
	mustExec(t, db2, `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, claimed_at)
		VALUES (1, 'legacy', 7, 'db', 'app', ?, NOW(6))`,
		strings.Repeat("b", 64))
	if err := goose.DownTo(db2, resolveMigrationsDir(), 31); err != nil {
		t.Fatalf("down with NULL budget = %v, want success", err)
	}
}
