//go:build integration

// Package integration provides real-MySQL schema proofs for goose migrations.
// input: database/sql, Testcontainers database, and schema migrations
// output: migration version, tables, columns, constraints, typed-profile identity, and seed regression tests
// pos: real-MySQL schema contract coverage, including governed identity and all typed-profile tables
// note: if this file changes, update header and README.md
package integration

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestGooseCleanMigration(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	// Verify seed patch truth remains visible after all migrations using business keys.
	assertResourceDisplayNameByName(t, db,
		"payment-mysql-replica-01-prod",
		"Payment MySQL Replica 01",
	)
	assertResourceDisplayNameByName(t, db,
		"notification-service-prod",
		"Notification Delivery Service",
	)
	assertResourceExistsByName(t, db, "payment-proxysql-02-prod")
	assertRelationExistsByBusinessKeys(t, db, "payment-proxysql-02-prod", "payment-mysql-cluster-prod", "fronts")
	assertRelationExistsByBusinessKeys(t, db, "payment-mysql-primary-prod", "payment-mysql-replica-01-prod", "replicates_to")
}

func TestSchemaUsesBigintPrimaryKeysWithoutForeignKeys(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	expectedUnsignedBigintIDs := map[string][]string{
		"roles":                            {"id"},
		"users":                            {"id", "role_id", "authorization_version"},
		"environments":                     {"id"},
		"owners":                           {"id"},
		"resources":                        {"id", "environment_id", "owner_id", "archived_by"},
		"resource_aliases":                 {"id", "resource_id", "environment_id"},
		"resource_external_identifiers":    {"id", "resource_id"},
		"resource_relations":               {"id", "from_resource_id", "to_resource_id"},
		"query_saved_statements":           {"id", "target_resource_id", "owner_user_id"},
		"query_saved_statement_parameters": {"id", "statement_id"},
		"audit_events":                     {"id", "actor_user_id", "target_resource_id"},
	}
	for tableName, columns := range expectedUnsignedBigintIDs {
		for _, columnName := range columns {
			assertUnsignedBigintColumn(t, db, tableName, columnName)
		}
		assertPrimaryKeyColumns(t, db, tableName, "id")
	}

	for _, tableName := range []string{
		"roles",
		"users",
		"environments",
		"owners",
		"resources",
		"resource_aliases",
		"resource_external_identifiers",
		"resource_relations",
		"resource_profiles_host",
		"resource_profiles_database_instance",
		"resource_profiles_database_cluster",
		"resource_profiles_service",
		"resource_profiles_domain_name",
		"resource_profiles_virtual_ip",
		"resource_profiles_database_proxy",
		"resource_profiles_control_plane_component",
		"query_saved_statements",
		"query_saved_statement_parameters",
		"audit_events",
	} {
		assertNoForeignKeys(t, db, tableName)
	}
}

func TestProfileTablesUseUniqueResourceIDInsteadOfPrimaryKeyResourceID(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	profileTables := []string{
		"resource_profiles_host",
		"resource_profiles_database_instance",
		"resource_profiles_database_cluster",
		"resource_profiles_service",
		"resource_profiles_domain_name",
		"resource_profiles_virtual_ip",
		"resource_profiles_database_proxy",
		"resource_profiles_control_plane_component",
	}
	for _, tableName := range profileTables {
		assertUnsignedBigintColumn(t, db, tableName, "id")
		assertUnsignedBigintColumn(t, db, tableName, "resource_id")
		assertPrimaryKeyColumns(t, db, tableName, "id")
		assertUniqueIndexOnSingleColumn(t, db, tableName, "resource_id")
	}
}

func assertSchemaChainBaseline(t *testing.T, db *sql.DB) {
	t.Helper()

	// goose_db_version must exist after migration.
	var count int
	err := db.QueryRow("SELECT count(*) FROM goose_db_version").Scan(&count)
	if err != nil {
		t.Fatalf("goose_db_version table missing: %v", err)
	}
	if count == 0 {
		t.Fatal("goose_db_version has no rows — migrations not recorded")
	}

	// Latest version should be current.
	var maxVersion int64
	err = db.QueryRow("SELECT max(version_id) FROM goose_db_version WHERE is_applied = true").Scan(&maxVersion)
	if err != nil {
		t.Fatalf("query max version: %v", err)
	}
	if maxVersion < 19 {
		t.Fatalf("expected at least 19 migrations applied, got version %d", maxVersion)
	}

	expectedTables := []string{
		"roles", "users", "environments", "owners", "resources",
		"resource_aliases", "resource_external_identifiers",
		"resource_relations",
		"resource_profiles_host",
		"resource_profiles_database_instance",
		"resource_profiles_database_cluster",
		"resource_profiles_service",
		"resource_profiles_domain_name",
		"resource_profiles_virtual_ip",
		"resource_profiles_database_proxy",
		"resource_profiles_control_plane_component",
		"audit_events",
	}
	for _, table := range expectedTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q not found", table)
		}
	}

	if !indexExists(t, db, "resources", "idx_resources_lifecycle") {
		t.Error("expected index idx_resources_lifecycle on resources")
	}
	if !indexExists(t, db, "resources", "uq_resource_name_env_type") {
		t.Error("expected unique index uq_resource_name_env_type on resources")
	}
	for _, col := range []string{"archived_at", "archived_by", "archive_reason"} {
		if !columnExists(t, db, "resources", col) {
			t.Errorf("expected column %q on resources", col)
		}
	}
	if !indexExists(t, db, "resources", "idx_resources_archived_at") {
		t.Error("expected index idx_resources_archived_at on resources")
	}
	if uniqueIndexOnColumnOnly(t, db, "resources", "name") {
		t.Error("resources should not have a global unique index on name alone")
	}
	if !columnExists(t, db, "resources", "origin") || columnExists(t, db, "resources", "source") || columnExists(t, db, "resources", "external_id") {
		t.Error("resources must use origin and normalized external identifiers")
	}
	if !indexExists(t, db, "resource_aliases", "uq_resource_alias_env") {
		t.Error("expected per-environment alias uniqueness")
	}
	if !indexExists(t, db, "resource_external_identifiers", "uq_resource_external_identifier") {
		t.Error("expected global external identifier uniqueness")
	}
	if !indexExists(t, db, "resource_relations", "uq_relation") {
		t.Error("expected unique index uq_relation on resource_relations")
	}
}

func tableExists(t *testing.T, db *sql.DB, tableName string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
		tableName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check table existence: %v", err)
	}
	return count > 0
}

func indexExists(t *testing.T, db *sql.DB, tableName, indexName string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		"SELECT count(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
		tableName, indexName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check index existence: %v", err)
	}
	return count > 0
}

func columnExists(t *testing.T, db *sql.DB, tableName, columnName string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(
		"SELECT count(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		tableName, columnName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check column existence: %v", err)
	}
	return count > 0
}

// uniqueIndexOnColumnOnly checks if there is a unique index on exactly one column.
func uniqueIndexOnColumnOnly(t *testing.T, db *sql.DB, tableName, columnName string) bool {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.statistics s1
		WHERE s1.table_schema = DATABASE()
		  AND s1.table_name = ?
		  AND s1.column_name = ?
		  AND s1.non_unique = 0
		  AND s1.index_name != 'PRIMARY'
		  AND (
		    SELECT count(*)
		    FROM information_schema.statistics s2
		    WHERE s2.table_schema = s1.table_schema
		      AND s2.table_name = s1.table_name
		      AND s2.index_name = s1.index_name
		  ) = 1`,
		tableName, columnName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("check unique index on column: %v", err)
	}
	return count > 0
}

func assertUnsignedBigintColumn(t *testing.T, db *sql.DB, tableName, columnName string) {
	t.Helper()
	var dataType, columnType string
	err := db.QueryRow(`
		SELECT data_type, column_type
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
		tableName, columnName,
	).Scan(&dataType, &columnType)
	if err != nil {
		t.Fatalf("query column type for %s.%s: %v", tableName, columnName, err)
	}
	if dataType != "bigint" {
		t.Fatalf("expected %s.%s data_type bigint, got %q", tableName, columnName, dataType)
	}
	if !strings.Contains(strings.ToLower(columnType), "unsigned") {
		t.Fatalf("expected %s.%s column_type to include unsigned, got %q", tableName, columnName, columnType)
	}
}

func assertPrimaryKeyColumns(t *testing.T, db *sql.DB, tableName string, wantColumns ...string) {
	t.Helper()
	rows, err := db.Query(`
		SELECT column_name
		FROM information_schema.key_column_usage
		WHERE table_schema = DATABASE() AND table_name = ? AND constraint_name = 'PRIMARY'
		ORDER BY ordinal_position`, tableName)
	if err != nil {
		t.Fatalf("query primary key for %s: %v", tableName, err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var columnName string
		if scanErr := rows.Scan(&columnName); scanErr != nil {
			t.Fatalf("scan primary key for %s: %v", tableName, scanErr)
		}
		got = append(got, columnName)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate primary key for %s: %v", tableName, err)
	}
	if strings.Join(got, ",") != strings.Join(wantColumns, ",") {
		t.Fatalf("primary key for %s = %v, want %v", tableName, got, wantColumns)
	}
}

func assertNoForeignKeys(t *testing.T, db *sql.DB, tableName string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.table_constraints
		WHERE table_schema = DATABASE()
		  AND table_name = ?
		  AND constraint_type = 'FOREIGN KEY'`, tableName).Scan(&count)
	if err != nil {
		t.Fatalf("query foreign keys for %s: %v", tableName, err)
	}
	if count != 0 {
		t.Fatalf("expected no foreign keys on %s, found %d", tableName, count)
	}
}

func assertUniqueIndexOnSingleColumn(t *testing.T, db *sql.DB, tableName, columnName string) {
	t.Helper()
	var count int
	err := db.QueryRow(`
		SELECT count(*)
		FROM information_schema.statistics s1
		WHERE s1.table_schema = DATABASE()
		  AND s1.table_name = ?
		  AND s1.column_name = ?
		  AND s1.non_unique = 0
		  AND s1.index_name != 'PRIMARY'
		  AND (
		    SELECT count(*)
		    FROM information_schema.statistics s2
		    WHERE s2.table_schema = s1.table_schema
		      AND s2.table_name = s1.table_name
		      AND s2.index_name = s1.index_name
		  ) = 1`,
		tableName, columnName,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query unique index on %s.%s: %v", tableName, columnName, err)
	}
	if count == 0 {
		t.Fatalf("expected unique index on %s.%s", tableName, columnName)
	}
}

func assertResourceDisplayNameByName(t *testing.T, db *sql.DB, resourceName, want string) {
	t.Helper()
	var got string
	err := db.QueryRow("SELECT display_name FROM resources WHERE name = ?", resourceName).Scan(&got)
	if err != nil {
		t.Fatalf("query display_name for resource %s: %v", resourceName, err)
	}
	if got != want {
		t.Fatalf("display_name for resource %s = %q, want %q", resourceName, got, want)
	}
}

func assertResourceExistsByName(t *testing.T, db *sql.DB, resourceName string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT count(*) FROM resources WHERE name = ?", resourceName).Scan(&count)
	if err != nil {
		t.Fatalf("query resource %s: %v", resourceName, err)
	}
	if count != 1 {
		t.Fatalf("expected resource %s to exist exactly once, got %d", resourceName, count)
	}
}

func assertRelationExistsByBusinessKeys(t *testing.T, db *sql.DB, fromResourceName, toResourceName, relationType string) {
	t.Helper()
	query := fmt.Sprintf(`
		SELECT count(*)
		FROM resource_relations rel
		JOIN resources src ON src.id = rel.from_resource_id
		JOIN resources dst ON dst.id = rel.to_resource_id
		WHERE src.name = ? AND dst.name = ? AND rel.relation_type = ?`)
	var count int
	err := db.QueryRow(query, fromResourceName, toResourceName, relationType).Scan(&count)
	if err != nil {
		t.Fatalf("query relation %s -> %s (%s): %v", fromResourceName, toResourceName, relationType, err)
	}
	if count != 1 {
		t.Fatalf("expected relation %s -> %s (%s) to exist exactly once, got %d", fromResourceName, toResourceName, relationType, count)
	}
}

func lookupResourceIDByName(t *testing.T, db *sql.DB, resourceName string) uint64 {
	t.Helper()
	var id uint64
	err := db.QueryRow("SELECT id FROM resources WHERE name = ?", resourceName).Scan(&id)
	if err != nil {
		t.Fatalf("query resource id for %s: %v", resourceName, err)
	}
	return id
}
