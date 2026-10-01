//go:build integration

// Package integration provides real-MySQL evidence for migration 00029.
// input: testing, strings, database/sql, pressly/goose, migration 00029
// output: TestPGQueryConnectionsMigration*
// pos: Proves migration 00029 applies composite connection identity + claims, keeps legacy ” defaults, and refuses a downgrade that would erase PostgreSQL-scoped data
// note: if this file changes, update this header and module README.md.
package integration

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestPGQueryConnectionsMigrationUpCreatesContractShape(t *testing.T) {
	db := setupMigration29TestDB(t, "controlhub_pg_conn_up")

	for _, tc := range []struct{ table, column string }{
		{"query_target_credentials", "database_name"},
		{"query_target_credentials", "default_schema"},
		{"query_executions", "database_name"},
		{"query_executions", "schema_name"},
		{"query_executions", "backend_pid"},
		{"query_executions", "remote_state"},
		{"query_executions", "client_execution_id"},
		{"query_saved_statements", "database_name"},
		{"query_saved_statements", "schema_name"},
		{"query_result_disclosure_policies", "schema_name"},
	} {
		if !columnExists(t, db, tc.table, tc.column) {
			t.Fatalf("migration 00029 up: %s.%s missing", tc.table, tc.column)
		}
	}
	if !tableExists(t, db, "query_execution_claims") {
		t.Fatal("migration 00029 up: query_execution_claims missing")
	}
	if !indexExists(t, db, "query_executions", "uq_query_executions_client_execution") {
		t.Fatal("migration 00029 up: uq_query_executions_client_execution missing")
	}
	if !indexExists(t, db, "query_execution_claims", "ix_claim_actor") {
		t.Fatal("migration 00029 up: ix_claim_actor missing")
	}
}

func TestPGQueryConnectionsMigrationUpPreservesLegacyDefaults(t *testing.T) {
	admin := setupTestDB(t)
	const name = "controlhub_pg_conn_defaults"
	dropDatabase(t, admin, name)
	createDatabase(t, admin, name)
	t.Cleanup(func() { dropDatabase(t, admin, name) })
	db := openNamedTestDB(t, name)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 28); err != nil {
		t.Fatalf("migrate fixture to 28: %v", err)
	}

	// Seed schema-28 rows, then apply 00029: every new column must default to
	// the legacy-neutral value so existing MySQL/TiDB behavior is unchanged.
	mustExec(t, db, `INSERT INTO query_target_credentials
		(resource_id, engine, credential_ref, enabled, environment_policy)
		VALUES (1, 'mysql', 'vault://legacy', 1, 'non_prod_only')`)
	mustExec(t, db, `INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status)
		VALUES (1, 1, 'mysql', 'digest', 'preview', 'success')`)
	mustExec(t, db, `INSERT INTO query_saved_statements
		(target_resource_id, owner_user_id, name, statement, scope)
		VALUES (1, 1, 'legacy', 'SELECT 1', 'personal')`)
	mustExec(t, db, `INSERT INTO query_result_disclosure_policies
		(target_resource_id, database_name, object_name, column_name, mode)
		VALUES (1, 'db1', 't1', 'c1', 'raw_copy_allowed')`)

	if err := goose.UpTo(db, resolveMigrationsDir(), 29); err != nil {
		t.Fatalf("migration 00029 up: %v", err)
	}

	var dbName, defSchema string
	if err := db.QueryRow(`SELECT database_name, default_schema FROM query_target_credentials WHERE resource_id = 1`).
		Scan(&dbName, &defSchema); err != nil || dbName != "" || defSchema != "" {
		t.Fatalf("credential defaults = %q/%q, %v; want ''/''", dbName, defSchema, err)
	}
	var execDB, execSchema, remote string
	var backendPID, clientID any
	if err := db.QueryRow(`SELECT database_name, schema_name, backend_pid, remote_state, client_execution_id
		FROM query_executions WHERE target_resource_id = 1`).
		Scan(&execDB, &execSchema, &backendPID, &remote, &clientID); err != nil {
		t.Fatalf("read execution defaults: %v", err)
	}
	if execDB != "" || execSchema != "" || remote != "" || backendPID != nil || clientID != nil {
		t.Fatalf("execution defaults = %q/%q/%v/%q/%v; want ''/''/nil/''/nil", execDB, execSchema, backendPID, remote, clientID)
	}
	var stmtDB, stmtSchema string
	if err := db.QueryRow(`SELECT database_name, schema_name FROM query_saved_statements WHERE id = 1`).
		Scan(&stmtDB, &stmtSchema); err != nil || stmtDB != "" || stmtSchema != "" {
		t.Fatalf("saved statement defaults = %q/%q, %v; want ''/''", stmtDB, stmtSchema, err)
	}
	var polSchema string
	if err := db.QueryRow(`SELECT schema_name FROM query_result_disclosure_policies WHERE id = 1`).
		Scan(&polSchema); err != nil || polSchema != "" {
		t.Fatalf("policy schema default = %q, %v; want ''", polSchema, err)
	}
}

func TestPGQueryConnectionsMigrationCompositeCredentialKey(t *testing.T) {
	db := setupMigration29TestDB(t, "controlhub_pg_conn_uq")

	// The widened key must allow one row per database and keep rejecting
	// duplicates within the same (resource_id, database_name) scope.
	mustExec(t, db, `INSERT INTO query_target_credentials
		(resource_id, database_name, engine, credential_ref, enabled, environment_policy)
		VALUES (1, '', 'mysql', 'vault://a', 1, 'non_prod_only'),
		       (1, 'pgdb', 'postgresql', 'vault://b', 1, 'non_prod_only')`)
	if _, err := db.Exec(`INSERT INTO query_target_credentials
		(resource_id, database_name, engine, credential_ref, enabled, environment_policy)
		VALUES (1, '', 'mysql', 'vault://dup', 1, 'non_prod_only')`); err == nil {
		t.Fatal("duplicate (resource_id, '') credential accepted; want unique-key rejection")
	}
	if _, err := db.Exec(`INSERT INTO query_target_credentials
		(resource_id, database_name, engine, credential_ref, enabled, environment_policy)
		VALUES (1, 'pgdb', 'postgresql', 'vault://dup2', 1, 'non_prod_only')`); err == nil {
		t.Fatal("duplicate (resource_id, 'pgdb') credential accepted; want unique-key rejection")
	}
}

func TestPGQueryConnectionsMigrationClaimsContract(t *testing.T) {
	db := setupMigration29TestDB(t, "controlhub_pg_conn_claims")

	// Composite PK: one claimant per (target, client_execution_id); NULL
	// execution_id marks an unfinished attempt.
	mustExec(t, db, `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, request_digest, claimed_at)
		VALUES (1, 'exec-1', 7, REPEAT('a', 64), NOW(6))`)
	if _, err := db.Exec(`INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_machine_principal_id, request_digest, claimed_at)
		VALUES (1, 'exec-1', 9, REPEAT('b', 64), NOW(6))`); err == nil {
		t.Fatal("duplicate (target, client_execution_id) claim accepted; want PK rejection")
	}
	mustExec(t, db, `INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_machine_principal_id, request_digest, claimed_at)
		VALUES (1, 'exec-2', 9, REPEAT('c', 64), NOW(6))`)
	var execID any
	if err := db.QueryRow(`SELECT execution_id FROM query_execution_claims
		WHERE target_resource_id = 1 AND client_execution_id = 'exec-1'`).Scan(&execID); err != nil || execID != nil {
		t.Fatalf("claim execution_id = %v, %v; want NULL while unfinished", execID, err)
	}
}

func TestPGQueryConnectionsMigrationClientExecutionIDUniqueness(t *testing.T) {
	db := setupMigration29TestDB(t, "controlhub_pg_conn_ceid")

	// NULL keys coexist (MySQL unique-index semantics): legacy keyless rows
	// stay insertable, while a real key admits exactly one execution.
	mustExec(t, db, `INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status)
		VALUES (1, 1, 'mysql', 'd1', 'p1', 'success'),
		       (1, 1, 'mysql', 'd2', 'p2', 'success')`)
	mustExec(t, db, `INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, client_execution_id)
		VALUES (1, 1, 'postgresql', 'd3', 'p3', 'success', 'exec-1')`)
	if _, err := db.Exec(`INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, client_execution_id)
		VALUES (1, 1, 'postgresql', 'd4', 'p4', 'success', 'exec-1')`); err == nil {
		t.Fatal("duplicate (target, client_execution_id) execution accepted; want unique-key rejection")
	}
}

func TestPGQueryConnectionsMigrationDownGuard(t *testing.T) {
	// Each new-dimension datum must independently block the downgrade.
	cases := []struct {
		name  string
		seed  string
		clean string
	}{
		{"claim row",
			`INSERT INTO query_execution_claims (target_resource_id, client_execution_id, actor_user_id, request_digest, claimed_at)
			 VALUES (1, 'exec-1', 1, REPEAT('a', 64), NOW(6))`,
			`DELETE FROM query_execution_claims`},
		{"pg credential",
			`INSERT INTO query_target_credentials (resource_id, database_name, engine, credential_ref, enabled, environment_policy)
			 VALUES (1, 'pgdb', 'postgresql', 'vault://a', 1, 'non_prod_only')`,
			`DELETE FROM query_target_credentials`},
		{"pinned default_schema",
			`INSERT INTO query_target_credentials (resource_id, default_schema, engine, credential_ref, enabled, environment_policy)
			 VALUES (1, 'app', 'postgresql', 'vault://a', 1, 'non_prod_only')`,
			`DELETE FROM query_target_credentials`},
		{"execution context",
			`INSERT INTO query_executions (target_resource_id, actor_user_id, engine, database_name, schema_name, statement_digest, statement_preview, status)
			 VALUES (1, 1, 'postgresql', 'pgdb', 'app', 'd', 'p', 'success')`,
			`DELETE FROM query_executions`},
		{"execution evidence",
			`INSERT INTO query_executions (target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, backend_pid, remote_state)
			 VALUES (1, 1, 'postgresql', 'd', 'p', 'success', 4242, 'stopped')`,
			`DELETE FROM query_executions`},
		{"execution claim key",
			`INSERT INTO query_executions (target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, client_execution_id)
			 VALUES (1, 1, 'postgresql', 'd', 'p', 'success', 'exec-1')`,
			`DELETE FROM query_executions`},
		{"saved statement context",
			`INSERT INTO query_saved_statements (target_resource_id, owner_user_id, database_name, name, statement, scope)
			 VALUES (1, 1, 'pgdb', 's', 'SELECT 1', 'personal')`,
			`DELETE FROM query_saved_statements`},
		{"schema-scoped disclosure policy",
			`INSERT INTO query_result_disclosure_policies (target_resource_id, database_name, schema_name, object_name, column_name, mode)
			 VALUES (1, 'pgdb', 'app', 't', 'c', 'raw_copy_allowed')`,
			`DELETE FROM query_result_disclosure_policies`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupMigration29TestDB(t, "controlhub_pg_conn_down_"+strings.NewReplacer(" ", "_", "-", "_").Replace(tc.name))
			mustExec(t, db, tc.seed)

			err := goose.DownTo(db, resolveMigrationsDir(), 28)
			if err == nil || !strings.Contains(err.Error(), "cannot roll back migration 00029") {
				t.Fatalf("down error = %v, want explicit data-preservation refusal", err)
			}
			if !columnExists(t, db, "query_executions", "client_execution_id") || !tableExists(t, db, "query_execution_claims") {
				t.Fatal("blocked downgrade changed migration 00029 schema")
			}

			mustExec(t, db, tc.clean)
			if err := goose.DownTo(db, resolveMigrationsDir(), 28); err != nil {
				t.Fatalf("down after cleanup: %v", err)
			}
			if tableExists(t, db, "query_execution_claims") ||
				columnExists(t, db, "query_executions", "client_execution_id") ||
				columnExists(t, db, "query_target_credentials", "database_name") ||
				columnExists(t, db, "query_result_disclosure_policies", "schema_name") {
				t.Fatal("clean downgrade retained migration 00029 schema")
			}
		})
	}
}

func TestPGQueryConnectionsMigrationDownGuardAllowsLegacyOnly(t *testing.T) {
	db := setupMigration29TestDB(t, "controlhub_pg_conn_down_legacy")

	// Rows that only carry legacy '' defaults are preserved by the rollback:
	// the guard refuses data loss, not the schema change itself.
	mustExec(t, db, `INSERT INTO query_target_credentials
		(resource_id, engine, credential_ref, enabled, environment_policy)
		VALUES (1, 'mysql', 'vault://legacy', 1, 'non_prod_only')`)
	mustExec(t, db, `INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status)
		VALUES (1, 1, 'mysql', 'd', 'p', 'success')`)

	if err := goose.DownTo(db, resolveMigrationsDir(), 28); err != nil {
		t.Fatalf("down with legacy-only rows: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("executions after downgrade = %d, %v; want 1 row preserved", n, err)
	}
}

func setupMigration29TestDB(t *testing.T, databaseName string) *sql.DB {
	t.Helper()
	admin := setupTestDB(t)
	dropDatabase(t, admin, databaseName)
	createDatabase(t, admin, databaseName)
	t.Cleanup(func() { dropDatabase(t, admin, databaseName) })
	db := openNamedTestDB(t, databaseName)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 29); err != nil {
		t.Fatalf("migrate fixture to 29: %v", err)
	}
	return db
}
