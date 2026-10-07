//go:build integration

// Package integration provides real-MySQL evidence for migration 00031.
// input: database/sql, fmt, strings, testing, pressly/goose, migration 00031
// output: TestClaimCollationMigration* — up preserves rows while making claim/key identity case-exact; down refuses whenever a stored identity would widen under ai_ci
// pos: Guards the G9 execution-claim opaque-key identity at the physical collation layer (issue #118, T10-A)
// note: if this file changes, update header and README.md
package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func claimIdentityCollations(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, spec := range [][2]string{
		{"query_execution_claims", "client_execution_id"},
		{"query_execution_claims", "database_name"},
		{"query_execution_claims", "schema_name"},
		{"query_execution_claims", "request_digest"},
		{"query_executions", "client_execution_id"},
	} {
		var collation string
		if err := db.QueryRow(`SELECT COLLATION_NAME FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, spec[0], spec[1]).
			Scan(&collation); err != nil {
			t.Fatalf("collation for %s.%s: %v", spec[0], spec[1], err)
		}
		out[spec[0]+"."+spec[1]] = collation
	}
	return out
}

func freshClaimMigrationDB(t *testing.T, name string, version int64) *sql.DB {
	t.Helper()
	admin := setupTestDB(t)
	dropDatabase(t, admin, name)
	createDatabase(t, admin, name)
	t.Cleanup(func() { dropDatabase(t, admin, name) })
	db := openNamedTestDB(t, name)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), version); err != nil {
		t.Fatalf("migrate fixture to %d: %v", version, err)
	}
	return db
}

func claimVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var v int64
	if err := db.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied = true`).Scan(&v); err != nil {
		t.Fatalf("goose version: %v", err)
	}
	return v
}

func TestClaimCollationMigrationUpPreservesData(t *testing.T) {
	db := freshClaimMigrationDB(t, "controlhub_claim_up", 30)

	// Seed under ai_ci: case-bearing identity data that must survive the
	// re-collation byte-exact.
	mustExec(t, db, `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, claimed_at)
		VALUES (1, 'KeyA', 7, 'SalesDB', 'App', ?, NOW(6))`,
		strings.Repeat("a", 64))
	mustExec(t, db, `INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, client_execution_id)
		VALUES (1, 7, 'postgresql', 'd', 'p', 'success', 'KeyB')`)

	if err := goose.UpTo(db, resolveMigrationsDir(), 31); err != nil {
		t.Fatalf("migration 00031 up: %v", err)
	}
	for name, collation := range claimIdentityCollations(t, db) {
		if collation != "utf8mb4_0900_bin" {
			t.Fatalf("%s collation = %q, want utf8mb4_0900_bin", name, collation)
		}
	}
	// Row bytes preserved verbatim.
	var key, dbName, schema, digest string
	if err := db.QueryRow(`SELECT client_execution_id, database_name, schema_name, request_digest
		FROM query_execution_claims WHERE target_resource_id = 1`).Scan(&key, &dbName, &schema, &digest); err != nil {
		t.Fatalf("read claim: %v", err)
	}
	if key != "KeyA" || dbName != "SalesDB" || schema != "App" || digest != strings.Repeat("a", 64) {
		t.Fatalf("claim bytes mutated: %q %q %q", key, dbName, schema)
	}
	// Case-distinct keys are now independent occupancies; exact duplicates
	// still collide.
	if _, err := db.Exec(`INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, claimed_at)
		VALUES (1, 'keya', 7, 'salesdb', 'app', ?, NOW(6))`, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("case-distinct key rejected after up: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, claimed_at)
		VALUES (1, 'KeyA', 7, 'SalesDB', 'App', ?, NOW(6))`, strings.Repeat("c", 64)); err == nil {
		t.Fatal("exact duplicate key accepted after up")
	}
	// Column shapes kept.
	var colType, nullable string
	if err := db.QueryRow(`SELECT COLUMN_TYPE, IS_NULLABLE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'query_execution_claims' AND COLUMN_NAME = 'client_execution_id'`).
		Scan(&colType, &nullable); err != nil {
		t.Fatalf("key shape: %v", err)
	}
	if colType != "varchar(64)" || nullable != "NO" {
		t.Fatalf("key shape = %q/%q, want varchar(64)/NO", colType, nullable)
	}
}

func TestClaimCollationMigrationDownGuard(t *testing.T) {
	// ANY claim row or keyed-execution row blocks the rollback before any DDL
	// runs: under ai_ci even a lowercase key widens its match class, so no
	// "safe characters" list can prove a stored identity survives.
	assertRefused := func(t *testing.T, db *sql.DB, why string) {
		t.Helper()
		if err := goose.DownTo(db, resolveMigrationsDir(), 30); err == nil || !strings.Contains(err.Error(), "45000") {
			t.Fatalf("down with %s = %v, want 45000 refusal", why, err)
		}
		if v := claimVersion(t, db); v != 31 {
			t.Fatalf("version after refused down = %d, want 31", v)
		}
		for name, collation := range claimIdentityCollations(t, db) {
			if collation != "utf8mb4_0900_bin" {
				t.Fatalf("%s collation = %q after refused down", name, collation)
			}
		}
	}

	t.Run("single lowercase claim key refuses", func(t *testing.T) {
		db := freshClaimMigrationDB(t, "controlhub_claim_down1", 31)
		mustExec(t, db, `INSERT INTO query_execution_claims
			(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, claimed_at)
			VALUES (1, 'lower-key', 7, 'salesdb', 'app', ?, NOW(6))`, strings.Repeat("a", 64))
		assertRefused(t, db, "lowercase claim")
		var key string
		if err := db.QueryRow(`SELECT client_execution_id FROM query_execution_claims WHERE target_resource_id = 1`).Scan(&key); err != nil || key != "lower-key" {
			t.Fatalf("claim data after refused down: %q %v", key, err)
		}
	})

	t.Run("accented claim key refuses", func(t *testing.T) {
		db := freshClaimMigrationDB(t, "controlhub_claim_down2", 31)
		mustExec(t, db, `INSERT INTO query_execution_claims
			(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, claimed_at)
			VALUES (1, 'clé', 7, 'salesdb', 'app', ?, NOW(6))`, strings.Repeat("a", 64))
		assertRefused(t, db, "accented claim")
	})

	t.Run("finalized claim refuses", func(t *testing.T) {
		db := freshClaimMigrationDB(t, "controlhub_claim_down3", 31)
		res, err := db.Exec(`INSERT INTO query_executions
			(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, client_execution_id)
			VALUES (1, 7, 'postgresql', 'd', 'p', 'success', 'lower-key')`)
		if err != nil {
			t.Fatalf("seed keyed execution: %v", err)
		}
		execID, _ := res.LastInsertId()
		mustExec(t, db, `INSERT INTO query_execution_claims
			(target_resource_id, client_execution_id, actor_user_id, database_name, schema_name, request_digest, execution_id, claimed_at)
			VALUES (1, 'lower-key', 7, 'salesdb', 'app', ?, ?, NOW(6))`, strings.Repeat("a", 64), execID)
		assertRefused(t, db, "finalized claim")
	})

	t.Run("keyed history without claims refuses", func(t *testing.T) {
		db := freshClaimMigrationDB(t, "controlhub_claim_down4", 31)
		mustExec(t, db, `INSERT INTO query_executions
			(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, client_execution_id)
			VALUES (1, 7, 'postgresql', 'd', 'p', 'success', 'keyb')`)
		assertRefused(t, db, "keyed execution history")
	})

	t.Run("empty claims and NULL-key history downgrade cleanly", func(t *testing.T) {
		db := freshClaimMigrationDB(t, "controlhub_claim_down5", 31)
		// Keyless legacy evidence is safe to keep: its identity does not
		// depend on the claim-key collation.
		mustExec(t, db, `INSERT INTO query_executions
			(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status)
			VALUES (1, 7, 'mysql', 'd', 'p', 'failed')`)
		if err := goose.DownTo(db, resolveMigrationsDir(), 30); err != nil {
			t.Fatalf("clean down: %v", err)
		}
		if v := claimVersion(t, db); v != 30 {
			t.Fatalf("version after down = %d, want 30", v)
		}
		if c := claimIdentityCollations(t, db)["query_execution_claims.client_execution_id"]; c != "utf8mb4_0900_ai_ci" {
			t.Fatalf("collation after down = %q, want utf8mb4_0900_ai_ci", c)
		}
		var cnt int
		if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE client_execution_id IS NULL`).Scan(&cnt); err != nil || cnt != 1 {
			t.Fatalf("null-key history after down: %d %v", cnt, err)
		}
		if err := goose.UpTo(db, resolveMigrationsDir(), 31); err != nil {
			t.Fatalf("re-up after clean down: %v", err)
		}
	})
}
