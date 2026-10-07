//go:build integration

// Package integration provides real-MySQL evidence for migration 00030.
// input: testing, strings, database/sql, pressly/goose, migration 00030
// output: TestDisclosureNoPadMigration*
// pos: Proves migration 00030 re-collates disclosure name columns to NO PAD, preserves rows and the five-part key, and refuses a downgrade that would merge trailing-space identities
// note: if this file changes, update this header and module README.md.
package integration

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// disclosureNameCollations returns the collation of each canonical name
// segment so tests can assert the store's real comparison semantics.
func disclosureNameCollations(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, col := range []string{"database_name", "schema_name", "object_name", "column_name"} {
		var collation string
		if err := db.QueryRow(`SELECT COLLATION_NAME FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'query_result_disclosure_policies' AND COLUMN_NAME = ?`, col).
			Scan(&collation); err != nil {
			t.Fatalf("collation for %s: %v", col, err)
		}
		out[col] = collation
	}
	return out
}

func TestDisclosureNoPadMigrationUpPreservesDataAndKey(t *testing.T) {
	admin := setupTestDB(t)
	const name = "controlhub_nopad_up"
	dropDatabase(t, admin, name)
	createDatabase(t, admin, name)
	t.Cleanup(func() { dropDatabase(t, admin, name) })
	db := openNamedTestDB(t, name)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 29); err != nil {
		t.Fatalf("migrate fixture to 29: %v", err)
	}

	// Seed under the old PAD SPACE collation: ordinary names plus case- and
	// unicode-distinct values that must survive the re-collation verbatim.
	mustExec(t, db, `INSERT INTO query_result_disclosure_policies
		(target_resource_id, database_name, schema_name, object_name, column_name, mode)
		VALUES (1, 'sales_db', 'app', 'orders', 'id', 'raw_copy_allowed'),
		       (1, 'SalesDB', 'app', 'orders', 'id', 'masked_no_copy'),
		       (1, '销售_db', 'app', 'orders', 'id', 'masked_no_copy'),
		       (2, 'sales_db', '', 'orders', 'id', 'raw_copy_allowed')`)

	if err := goose.UpTo(db, resolveMigrationsDir(), 30); err != nil {
		t.Fatalf("migration 00030 up: %v", err)
	}

	// Rows, ids, modes, and name bytes are preserved verbatim.
	rows, err := db.Query(`SELECT id, target_resource_id, database_name, schema_name, object_name, column_name, mode
		FROM query_result_disclosure_policies ORDER BY id`)
	if err != nil {
		t.Fatalf("read policies: %v", err)
	}
	defer rows.Close()
	type row struct {
		id, rid             uint64
		db, s, obj, col, md string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.rid, &r.db, &r.s, &r.obj, &r.col, &r.md); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("rows after up = %d, want 4", len(got))
	}
	if got[0].db != "sales_db" || got[1].db != "SalesDB" || got[2].db != "销售_db" || got[3].s != "" {
		t.Fatalf("name bytes mutated by up: %+v", got)
	}
	if got[0].md != "raw_copy_allowed" || got[1].md != "masked_no_copy" {
		t.Fatalf("modes mutated by up: %+v", got)
	}

	// The four name columns now compare under a case-sensitive NO PAD binary
	// collation; column shape (VARCHAR(128), NOT NULL, schema default) is kept.
	for col, collation := range disclosureNameCollations(t, db) {
		if collation != "utf8mb4_0900_bin" {
			t.Fatalf("%s collation = %q, want utf8mb4_0900_bin", col, collation)
		}
	}
	var pad string
	if err := db.QueryRow(`SELECT PAD_ATTRIBUTE FROM information_schema.COLLATIONS
		WHERE COLLATION_NAME = 'utf8mb4_0900_bin'`).Scan(&pad); err != nil || pad != "NO PAD" {
		t.Fatalf("utf8mb4_0900_bin PAD_ATTRIBUTE = %q, %v; want NO PAD", pad, err)
	}
	var colType, nullable, dflt string
	if err := db.QueryRow(`SELECT COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_DEFAULT,'<null>') FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'query_result_disclosure_policies' AND COLUMN_NAME = 'schema_name'`).
		Scan(&colType, &nullable, &dflt); err != nil {
		t.Fatalf("schema_name shape: %v", err)
	}
	if colType != "varchar(128)" || nullable != "NO" || dflt != "" {
		t.Fatalf("schema_name shape = %q/%q/%q, want varchar(128)/NO/''", colType, nullable, dflt)
	}
	if !indexExists(t, db, "query_result_disclosure_policies", "uq_disclosure_policy_scope") {
		t.Fatal("uq_disclosure_policy_scope lost by migration 00030")
	}

	// Trailing-space siblings are now distinct identities, while an exact
	// duplicate of the new key still hits the unique index.
	mustExec(t, db, `INSERT INTO query_result_disclosure_policies
		(target_resource_id, database_name, schema_name, object_name, column_name, mode)
		VALUES (1, 'sales_db', 'app', 'orders ', 'id', 'masked_no_copy')`)
	if _, err := db.Exec(`INSERT INTO query_result_disclosure_policies
		(target_resource_id, database_name, schema_name, object_name, column_name, mode)
		VALUES (1, 'sales_db', 'app', 'orders', 'id', 'masked_no_copy')`); err == nil {
		t.Fatal("exact duplicate accepted after up; want unique-key rejection")
	}
}

func TestDisclosureNoPadMigrationDownGuard(t *testing.T) {
	// Each segment pair that differs only by trailing spaces is distinct under
	// NO PAD but collides under PAD SPACE; any of them must block the rollback
	// before any DDL runs.
	cases := []struct {
		name  string
		seed  string
		clean string
	}{
		{"database_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app', 'orders', 'id', 'raw_copy_allowed'),
				       (1, 'db_a ', 'app', 'orders', 'id', 'masked_no_copy')`,
			`DELETE FROM query_result_disclosure_policies WHERE database_name = 'db_a '`},
		{"schema_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app', 'orders', 'id', 'raw_copy_allowed'),
				       (1, 'db_a', 'app ', 'orders', 'id', 'masked_no_copy')`,
			`DELETE FROM query_result_disclosure_policies WHERE schema_name = 'app '`},
		{"object_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app', 'orders', 'id', 'raw_copy_allowed'),
				       (1, 'db_a', 'app', 'orders ', 'id', 'masked_no_copy')`,
			`DELETE FROM query_result_disclosure_policies WHERE object_name = 'orders '`},
		{"column_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app', 'orders', 'id', 'raw_copy_allowed'),
				       (1, 'db_a', 'app', 'orders', 'id ', 'masked_no_copy')`,
			`DELETE FROM query_result_disclosure_policies WHERE column_name = 'id '`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupMigration30TestDB(t, "controlhub_nopad_down_"+strings.ReplaceAll(tc.name, "_", ""))
			mustExec(t, db, tc.seed)

			err := goose.DownTo(db, resolveMigrationsDir(), 29)
			if err == nil || !strings.Contains(err.Error(), "cannot roll back migration 00030") {
				t.Fatalf("down error = %v, want explicit trailing-space refusal", err)
			}
			// Checks ran before DDL: the columns are still NO PAD.
			for col, collation := range disclosureNameCollations(t, db) {
				if collation != "utf8mb4_0900_bin" {
					t.Fatalf("blocked downgrade changed %s collation to %q", col, collation)
				}
			}
			var n int
			if err := db.QueryRow(`SELECT COUNT(*) FROM query_result_disclosure_policies`).Scan(&n); err != nil || n != 2 {
				t.Fatalf("blocked downgrade lost rows: n = %d, %v", n, err)
			}

			mustExec(t, db, tc.clean)
			if err := goose.DownTo(db, resolveMigrationsDir(), 29); err != nil {
				t.Fatalf("down after cleanup: %v", err)
			}
			for col, collation := range disclosureNameCollations(t, db) {
				if collation != "utf8mb4_bin" {
					t.Fatalf("%s collation = %q after clean down, want utf8mb4_bin", col, collation)
				}
			}
		})
	}
}

// TestDisclosureNoPadMigrationDownGuardTrailingSpaceSingleton proves the guard
// protects identity, not just key uniqueness: even a single policy whose name
// carries a trailing U+0020 would silently widen its match identity under PAD
// SPACE (a stored 'orders ' would answer queries for 'orders'), so the
// downgrade must refuse before any DDL — without deleting, merging, or
// rewriting the row.
func TestDisclosureNoPadMigrationDownGuardTrailingSpaceSingleton(t *testing.T) {
	cases := []struct {
		name     string
		seed     string
		segment  string
		padQuery string
		clean    string
	}{
		{"database_name one space",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a ', 'app', 'orders', 'id', 'raw_copy_allowed')`,
			"database_name",
			`SELECT COUNT(*) FROM query_result_disclosure_policies WHERE database_name = 'db_a'`,
			`DELETE FROM query_result_disclosure_policies`},
		{"database_name two spaces",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a  ', 'app', 'orders', 'id', 'raw_copy_allowed')`,
			"database_name",
			`SELECT COUNT(*) FROM query_result_disclosure_policies WHERE database_name = 'db_a'`,
			`DELETE FROM query_result_disclosure_policies`},
		{"schema_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app ', 'orders', 'id', 'raw_copy_allowed')`,
			"schema_name",
			`SELECT COUNT(*) FROM query_result_disclosure_policies WHERE schema_name = 'app'`,
			`DELETE FROM query_result_disclosure_policies`},
		{"object_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app', 'orders ', 'id', 'raw_copy_allowed')`,
			"object_name",
			`SELECT COUNT(*) FROM query_result_disclosure_policies WHERE object_name = 'orders'`,
			`DELETE FROM query_result_disclosure_policies`},
		{"column_name",
			`INSERT INTO query_result_disclosure_policies
				(target_resource_id, database_name, schema_name, object_name, column_name, mode)
				VALUES (1, 'db_a', 'app', 'orders', 'id ', 'raw_copy_allowed')`,
			"column_name",
			`SELECT COUNT(*) FROM query_result_disclosure_policies WHERE column_name = 'id'`,
			`DELETE FROM query_result_disclosure_policies`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupMigration30TestDB(t, "controlhub_nopad_pad_"+strings.ReplaceAll(tc.name, " ", "_"))
			mustExec(t, db, tc.seed)

			// Under NO PAD the trimmed sibling name must NOT match the stored
			// row — the stored identity is byte-exact.
			var n int
			if err := db.QueryRow(tc.padQuery).Scan(&n); err != nil || n != 0 {
				t.Fatalf("%s trimmed-name match count = %d, %v; want 0 under NO PAD", tc.segment, n, err)
			}

			// Down must refuse: under PAD SPACE this row's identity widens to
			// also answer the trimmed name, so rollback is not semantics-safe
			// even though no second row collides.
			err := goose.DownTo(db, resolveMigrationsDir(), 29)
			if err == nil || !strings.Contains(err.Error(), "cannot roll back migration 00030") {
				t.Fatalf("%s down error = %v, want explicit trailing-space refusal", tc.segment, err)
			}

			// The refusal ran before DDL: columns still NO PAD, goose version
			// still 30, and the row is untouched byte-for-byte.
			for col, collation := range disclosureNameCollations(t, db) {
				if collation != "utf8mb4_0900_bin" {
					t.Fatalf("blocked downgrade changed %s collation to %q", col, collation)
				}
			}
			var maxVersion int64
			if err := db.QueryRow(`SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = true`).Scan(&maxVersion); err != nil || maxVersion != 30 {
				t.Fatalf("goose version after blocked down = %d, %v; want 30", maxVersion, err)
			}
			var id uint64
			var mode string
			if err := db.QueryRow(`SELECT id, mode FROM query_result_disclosure_policies`).Scan(&id, &mode); err != nil {
				t.Fatalf("row after blocked down: %v", err)
			}
			if mode != "raw_copy_allowed" {
				t.Fatalf("mode after blocked down = %q, want raw_copy_allowed", mode)
			}
			var stored string
			if err := db.QueryRow(fmt.Sprintf(`SELECT %s FROM query_result_disclosure_policies WHERE id = ?`, tc.segment), id).Scan(&stored); err != nil {
				t.Fatalf("read %s: %v", tc.segment, err)
			}
			if !strings.HasSuffix(stored, " ") {
				t.Fatalf("%s bytes after blocked down = %q, want trailing space preserved", tc.segment, stored)
			}

			// Removing the row that needs NO PAD semantics releases the
			// downgrade.
			mustExec(t, db, tc.clean)
			if err := goose.DownTo(db, resolveMigrationsDir(), 29); err != nil {
				t.Fatalf("down after cleanup: %v", err)
			}
		})
	}
}

func TestDisclosureNoPadMigrationDownAllowsSafeData(t *testing.T) {
	db := setupMigration30TestDB(t, "controlhub_nopad_down_safe")

	// Names without trailing-space ambiguity survive the rollback untouched:
	// the guard refuses data loss, not the schema change itself.
	mustExec(t, db, `INSERT INTO query_result_disclosure_policies
		(target_resource_id, database_name, schema_name, object_name, column_name, mode)
		VALUES (1, 'sales_db', 'app', 'orders', 'id', 'raw_copy_allowed'),
		       (1, 'SalesDB', 'app', 'orders', 'id', 'masked_no_copy'),
		       (1, 'sales_db', '', 'orders', 'id', 'masked_no_copy')`)

	if err := goose.DownTo(db, resolveMigrationsDir(), 29); err != nil {
		t.Fatalf("down with safe rows: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_result_disclosure_policies`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("policies after safe downgrade = %d, %v; want 3 rows preserved", n, err)
	}
	for col, collation := range disclosureNameCollations(t, db) {
		if collation != "utf8mb4_bin" {
			t.Fatalf("%s collation = %q after down, want utf8mb4_bin", col, collation)
		}
	}
}

func setupMigration30TestDB(t *testing.T, databaseName string) *sql.DB {
	t.Helper()
	admin := setupTestDB(t)
	dropDatabase(t, admin, databaseName)
	createDatabase(t, admin, databaseName)
	t.Cleanup(func() { dropDatabase(t, admin, databaseName) })
	db := openNamedTestDB(t, databaseName)
	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 30); err != nil {
		t.Fatalf("migrate fixture to 30: %v", err)
	}
	return db
}
