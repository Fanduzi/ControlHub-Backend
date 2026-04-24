//go:build integration

package integration

import (
	"database/sql"
	"testing"
)

func TestGooseCleanMigration(t *testing.T) {
	db := setupTestDB(t)

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
	if maxVersion < 8 {
		t.Fatalf("expected at least 8 migrations applied, got version %d", maxVersion)
	}

	// Verify expected tables exist.
	expectedTables := []string{
		"roles", "users", "environments", "owners", "resources",
		"resource_relations",
		"resource_profiles_host",
		"resource_profiles_database_instance",
		"resource_profiles_database_cluster",
		"resource_profiles_service",
		"audit_events",
	}
	for _, table := range expectedTables {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q not found", table)
		}
	}

	// Verify resources has idx_resources_lifecycle.
	if !indexExists(t, db, "resources", "idx_resources_lifecycle") {
		t.Error("expected index idx_resources_lifecycle on resources")
	}

	// Verify resources has uq_resource_name_env (name, environment_id).
	if !indexExists(t, db, "resources", "uq_resource_name_env") {
		t.Error("expected unique index uq_resource_name_env on resources")
	}

	// Verify archive columns exist on resources.
	for _, col := range []string{"archived_at", "archived_by", "archive_reason"} {
		if !columnExists(t, db, "resources", col) {
			t.Errorf("expected column %q on resources", col)
		}
	}

	// Verify archive index exists.
	if !indexExists(t, db, "resources", "idx_resources_archived_at") {
		t.Error("expected index idx_resources_archived_at on resources")
	}

	// Verify resources does NOT have a global unique index only on name.
	if uniqueIndexOnColumnOnly(t, db, "resources", "name") {
		t.Error("resources should not have a global unique index on name alone")
	}

	// Verify resource_relations has unique (from_resource_id, to_resource_id, relation_type).
	if !indexExists(t, db, "resource_relations", "uq_relation") {
		t.Error("expected unique index uq_relation on resource_relations")
	}

	// Verify seed patch truth remains visible after all migrations.
	assertResourceDisplayName(t, db,
		"41000000-0000-0000-0000-000000000023",
		"Payment MySQL Replica 01",
	)
	assertResourceDisplayName(t, db,
		"41000000-0000-0000-0000-000000000034",
		"Notification Delivery Service",
	)
	assertResourceExists(t, db, "41000000-0000-0000-0000-000000000044")
	assertRelationExists(t, db, "51000000-0000-0000-0000-000000000090")
	assertRelationExists(t, db, "51000000-0000-0000-0000-000000000091")
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

func assertResourceDisplayName(t *testing.T, db *sql.DB, resourceID, want string) {
	t.Helper()
	var got string
	err := db.QueryRow("SELECT display_name FROM resources WHERE id = ?", resourceID).Scan(&got)
	if err != nil {
		t.Fatalf("query display_name for %s: %v", resourceID, err)
	}
	if got != want {
		t.Fatalf("display_name for %s = %q, want %q", resourceID, got, want)
	}
}

func assertResourceExists(t *testing.T, db *sql.DB, resourceID string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT count(*) FROM resources WHERE id = ?", resourceID).Scan(&count)
	if err != nil {
		t.Fatalf("query resource %s: %v", resourceID, err)
	}
	if count != 1 {
		t.Fatalf("expected resource %s to exist exactly once, got %d", resourceID, count)
	}
}

func assertRelationExists(t *testing.T, db *sql.DB, relationID string) {
	t.Helper()
	var count int
	err := db.QueryRow("SELECT count(*) FROM resource_relations WHERE id = ?", relationID).Scan(&count)
	if err != nil {
		t.Fatalf("query relation %s: %v", relationID, err)
	}
	if count != 1 {
		t.Fatalf("expected relation %s to exist exactly once, got %d", relationID, count)
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
