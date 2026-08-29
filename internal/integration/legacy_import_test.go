//go:build integration

// Package integration provides real-MySQL coverage for legacy cutover import.
// input: context, database/sql, fmt, strings, testing, time, go-sql-driver/mysql, pressly/goose, internal/cutover
// output: TestImportLegacyData_* integration cases
// pos: Proves UUID-to-bigint cutover import against real MySQL: NULL audit actor preservation and fail-loud unknown actor mapping with no partial import
// note: if this file changes, update this header and README.md
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	gosqlmysql "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"

	"github.com/fan/controlhub/internal/cutover"
)

func TestImportLegacyData_MigratesUUIDDataIntoBigintSchema(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	sourceDBName := uniqueImportDBName("legacy_src")
	targetDBName := uniqueImportDBName("legacy_dst")

	adminDB := setupTestDB(t)
	createDatabase(t, adminDB, sourceDBName)
	createDatabase(t, adminDB, targetDBName)
	t.Cleanup(func() {
		dropDatabase(t, adminDB, sourceDBName)
		dropDatabase(t, adminDB, targetDBName)
	})

	sourceDB := openNamedTestDB(t, sourceDBName)
	targetDB := openNamedTestDB(t, targetDBName)
	seedLegacySourceData(t, sourceDB)
	applyMigrations(t, targetDB)
	truncateBusinessTables(t, targetDB)

	err := cutover.ImportLegacyData(ctx, cutover.ImportConfig{
		SourceDSN: dsnForDatabase(sourceDBName),
		TargetDSN: dsnForDatabase(targetDBName),
	})
	if err != nil {
		t.Fatalf("import legacy data: %v", err)
	}

	assertCount(t, targetDB, "roles", 1)
	assertCount(t, targetDB, "users", 1)
	assertCount(t, targetDB, "environments", 1)
	assertCount(t, targetDB, "owners", 1)
	assertCount(t, targetDB, "resources", 2)
	assertCount(t, targetDB, "resource_profiles_host", 1)
	assertCount(t, targetDB, "resource_relations", 1)
	assertCount(t, targetDB, "audit_events", 1)

	resourceOneID := lookupResourceIDByName(t, targetDB, "legacy-host-01")
	resourceTwoID := lookupResourceIDByName(t, targetDB, "legacy-service-01")
	if resourceOneID == 0 || resourceTwoID == 0 {
		t.Fatal("expected imported resources to receive bigint ids")
	}
	if resourceOneID == resourceTwoID {
		t.Fatal("expected distinct bigint ids for imported resources")
	}

	assertHostProfileResourceID(t, targetDB, resourceOneID)
	assertImportedRelation(t, targetDB, resourceTwoID, resourceOneID)
	assertImportedAuditEvent(t, targetDB, resourceTwoID)
	assertImportedArchiveFields(t, targetDB, resourceTwoID)
}

func TestImportLegacyData_RejectsNonEmptyTargetDatabase(t *testing.T) {
	ctx := context.Background()
	sourceDBName := uniqueImportDBName("legacy_src")
	targetDBName := uniqueImportDBName("legacy_dst")

	adminDB := setupTestDB(t)
	createDatabase(t, adminDB, sourceDBName)
	createDatabase(t, adminDB, targetDBName)
	t.Cleanup(func() {
		dropDatabase(t, adminDB, sourceDBName)
		dropDatabase(t, adminDB, targetDBName)
	})

	sourceDB := openNamedTestDB(t, sourceDBName)
	targetDB := openNamedTestDB(t, targetDBName)
	seedLegacySourceData(t, sourceDB)
	applyMigrations(t, targetDB)

	err := cutover.ImportLegacyData(ctx, cutover.ImportConfig{
		SourceDSN: dsnForDatabase(sourceDBName),
		TargetDSN: dsnForDatabase(targetDBName),
	})
	if err == nil {
		t.Fatal("expected non-empty target database to be rejected")
	}
	if !strings.Contains(err.Error(), "target table roles must be empty") {
		t.Fatalf("unexpected non-empty target error: %v", err)
	}
}

func TestImportLegacyData_PreservesNullAuditActor(t *testing.T) {
	// Anonymous authentication audit events (migration 00017 makes the target
	// actor_user_id nullable) must import with a NULL actor while preserving
	// the fixed event/result/target/created-at metadata. A source row with no
	// actor is valid security history; it must never be fabricated attribution
	// nor fail the import.
	ctx := context.Background()
	sourceDBName := uniqueImportDBName("legacy_src")
	targetDBName := uniqueImportDBName("legacy_dst")

	adminDB := setupTestDB(t)
	createDatabase(t, adminDB, sourceDBName)
	createDatabase(t, adminDB, targetDBName)
	t.Cleanup(func() {
		dropDatabase(t, adminDB, sourceDBName)
		dropDatabase(t, adminDB, targetDBName)
	})

	sourceDB := openNamedTestDB(t, sourceDBName)
	targetDB := openNamedTestDB(t, targetDBName)
	seedLegacySourceData(t, sourceDB)
	// The legacy source permits anonymous auth outcomes: nullable actor column
	// plus an auth.bearer/rejected event with no verified actor and no target.
	execSQL(t, sourceDB, `alter table audit_events modify actor_user_id char(36) default null`)
	execSQL(t, sourceDB, `insert into audit_events (id, actor_user_id, target_resource_id, event_type, result, created_at) values
		('audit-anon-0000000000000000000000001', null, null, 'auth.bearer', 'rejected', '2026-01-05 00:00:00.000000')`)
	applyMigrations(t, targetDB)
	truncateBusinessTables(t, targetDB)

	err := cutover.ImportLegacyData(ctx, cutover.ImportConfig{
		SourceDSN: dsnForDatabase(sourceDBName),
		TargetDSN: dsnForDatabase(targetDBName),
	})
	if err != nil {
		t.Fatalf("import legacy data with anonymous audit event: %v", err)
	}

	var actorID sql.NullInt64
	var targetID sql.NullInt64
	var eventType, result string
	var createdAt time.Time
	if err := targetDB.QueryRow(`select actor_user_id, target_resource_id, event_type, result, created_at from audit_events where event_type = ?`, "auth.bearer").Scan(&actorID, &targetID, &eventType, &result, &createdAt); err != nil {
		t.Fatalf("query imported anonymous audit event: %v", err)
	}
	if actorID.Valid {
		t.Fatalf("anonymous audit actor = %v, want NULL", actorID.Int64)
	}
	if targetID.Valid {
		t.Fatalf("anonymous audit target = %v, want NULL", targetID.Int64)
	}
	if eventType != "auth.bearer" || result != "rejected" {
		t.Fatalf("anonymous audit event = %s/%s, want auth.bearer/rejected", eventType, result)
	}
	wantCreatedAt := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	if !createdAt.Equal(wantCreatedAt) {
		t.Fatalf("anonymous audit created_at = %v, want %v", createdAt, wantCreatedAt)
	}
	// The mapped non-NULL audit event must still import alongside it.
	assertCount(t, targetDB, "audit_events", 2)
}

func TestImportLegacyData_UnknownAuditActorFailsLoudWithoutPartialImport(t *testing.T) {
	// A non-NULL source actor that cannot be mapped to a target user must stop
	// the import loudly: mapped identity history is never silently corrupted.
	// The whole import runs in one target transaction, so a loud failure must
	// leave no partial rows in any business table.
	ctx := context.Background()
	sourceDBName := uniqueImportDBName("legacy_src")
	targetDBName := uniqueImportDBName("legacy_dst")

	adminDB := setupTestDB(t)
	createDatabase(t, adminDB, sourceDBName)
	createDatabase(t, adminDB, targetDBName)
	t.Cleanup(func() {
		dropDatabase(t, adminDB, sourceDBName)
		dropDatabase(t, adminDB, targetDBName)
	})

	sourceDB := openNamedTestDB(t, sourceDBName)
	targetDB := openNamedTestDB(t, targetDBName)
	seedLegacySourceData(t, sourceDB)
	execSQL(t, sourceDB, `insert into audit_events (id, actor_user_id, target_resource_id, event_type, result, created_at) values
		('audit-unknown-0000000000000000000001', 'user-ghost-0000000000000000000000001', null, 'auth.login', 'succeeded', '2026-01-05 00:00:00.000000')`)
	applyMigrations(t, targetDB)
	truncateBusinessTables(t, targetDB)

	err := cutover.ImportLegacyData(ctx, cutover.ImportConfig{
		SourceDSN: dsnForDatabase(sourceDBName),
		TargetDSN: dsnForDatabase(targetDBName),
	})
	if err == nil {
		t.Fatal("expected unknown audit actor to fail import loudly")
	}
	if !strings.Contains(err.Error(), "missing actor user mapping for audit event auth.login") {
		t.Fatalf("unexpected unknown audit actor error: %v", err)
	}
	for _, tableName := range []string{
		"audit_events",
		"resource_relations",
		"resource_profiles_service",
		"resource_profiles_database_cluster",
		"resource_profiles_database_instance",
		"resource_profiles_host",
		"resources",
		"users",
		"owners",
		"environments",
		"roles",
	} {
		assertCount(t, targetDB, tableName, 0)
	}
}

func execSQL(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatalf("exec %q: %v", shortSQL(statement), err)
	}
}

func TestImportLegacyData_RequiresParseTime(t *testing.T) {
	ctx := context.Background()
	sourceDBName := uniqueImportDBName("legacy_src")
	targetDBName := uniqueImportDBName("legacy_dst")

	adminDB := setupTestDB(t)
	createDatabase(t, adminDB, sourceDBName)
	createDatabase(t, adminDB, targetDBName)
	t.Cleanup(func() {
		dropDatabase(t, adminDB, sourceDBName)
		dropDatabase(t, adminDB, targetDBName)
	})

	sourceDB := openNamedTestDB(t, sourceDBName)
	targetDB := openNamedTestDB(t, targetDBName)
	seedLegacySourceData(t, sourceDB)
	applyMigrations(t, targetDB)
	truncateBusinessTables(t, targetDB)

	err := cutover.ImportLegacyData(ctx, cutover.ImportConfig{
		SourceDSN: dsnWithoutParseTime(sourceDBName),
		TargetDSN: dsnForDatabase(targetDBName),
	})
	if err == nil {
		t.Fatal("expected source dsn without parseTime to be rejected")
	}
	if !strings.Contains(err.Error(), "source dsn must set parseTime=true") {
		t.Fatalf("unexpected parseTime validation error: %v", err)
	}
}

func uniqueImportDBName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func dsnForDatabase(name string) string {
	cfg, err := gosqlmysql.ParseDSN(globalEnv.dsn)
	if err != nil {
		panic(fmt.Sprintf("parse dsn: %v", err))
	}
	cfg.DBName = name
	return cfg.FormatDSN()
}

func dsnWithoutParseTime(name string) string {
	cfg, err := gosqlmysql.ParseDSN(globalEnv.dsn)
	if err != nil {
		panic(fmt.Sprintf("parse dsn: %v", err))
	}
	cfg.DBName = name
	cfg.ParseTime = false
	return cfg.FormatDSN()
}

func openNamedTestDB(t *testing.T, name string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsnForDatabase(name))
	if err != nil {
		t.Fatalf("open named db %s: %v", name, err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping named db %s: %v", name, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createDatabase(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
		t.Fatalf("create database %s: %v", name, err)
	}
}

func dropDatabase(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec("DROP DATABASE IF EXISTS `" + name + "`"); err != nil {
		t.Fatalf("drop database %s: %v", name, err)
	}
}

func applyMigrations(t *testing.T, db *sql.DB) {
	t.Helper()
	goose.SetDialect("mysql")
	if err := goose.Up(db, resolveMigrationsDir()); err != nil {
		t.Fatalf("goose up target db: %v", err)
	}
}

func truncateBusinessTables(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := []string{
		"audit_events",
		"resource_relations",
		"resource_external_identifiers",
		"resource_aliases",
		"resource_profiles_service",
		"resource_profiles_database_cluster",
		"resource_profiles_database_instance",
		"resource_profiles_host",
		"resources",
		"users",
		"owners",
		"environments",
		"roles",
	}
	for _, tableName := range tables {
		if _, err := db.Exec("TRUNCATE TABLE " + tableName); err != nil {
			t.Fatalf("truncate %s: %v", tableName, err)
		}
	}
}

func seedLegacySourceData(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE roles (
			id char(36) NOT NULL PRIMARY KEY,
			name varchar(64) NOT NULL,
			description text NOT NULL,
			created_at datetime(6) NOT NULL,
			updated_at datetime(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE environments (
			id char(36) NOT NULL PRIMARY KEY,
			name varchar(128) NOT NULL,
			slug varchar(64) NOT NULL,
			description text NOT NULL,
			created_at datetime(6) NOT NULL,
			updated_at datetime(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE owners (
			id char(36) NOT NULL PRIMARY KEY,
			name varchar(128) NOT NULL,
			email varchar(255) NOT NULL,
			created_at datetime(6) NOT NULL,
			updated_at datetime(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE users (
			id char(36) NOT NULL PRIMARY KEY,
			email varchar(255) NOT NULL,
			password_hash varchar(255) NOT NULL,
			display_name varchar(255) NOT NULL,
			role_id char(36) NOT NULL,
			created_at datetime(6) NOT NULL,
			updated_at datetime(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE resources (
			id char(36) NOT NULL PRIMARY KEY,
			resource_type varchar(64) NOT NULL,
			resource_subtype varchar(64) NOT NULL,
			name varchar(255) NOT NULL,
			display_name varchar(255) NOT NULL,
			environment_id char(36) NOT NULL,
			owner_id char(36) NOT NULL,
			lifecycle_status varchar(64) NOT NULL,
			health_status varchar(64) NOT NULL,
			labels json NOT NULL,
			source varchar(64) NOT NULL,
			external_id varchar(255) NOT NULL,
			created_at datetime(6) NOT NULL,
			updated_at datetime(6) NOT NULL,
			archived_at datetime(6) DEFAULT NULL,
			archived_by char(36) DEFAULT NULL,
			archive_reason text DEFAULT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE resource_profiles_host (
			resource_id char(36) NOT NULL PRIMARY KEY,
			hostname varchar(255) NOT NULL,
			ip_address varchar(64) NOT NULL,
			os_name varchar(128) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE resource_relations (
			id char(36) NOT NULL PRIMARY KEY,
			from_resource_id char(36) NOT NULL,
			to_resource_id char(36) NOT NULL,
			relation_type varchar(64) NOT NULL,
			created_at datetime(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE audit_events (
			id char(36) NOT NULL PRIMARY KEY,
			actor_user_id char(36) NOT NULL,
			target_resource_id char(36) DEFAULT NULL,
			event_type varchar(128) NOT NULL,
			result varchar(64) NOT NULL,
			created_at datetime(6) NOT NULL
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`INSERT INTO roles (id, name, description, created_at, updated_at) VALUES
			('role-admin-000000000000000000000001', 'admin', 'Full access', '2026-01-01 00:00:00.000000', '2026-01-01 00:00:00.000000')`,
		`INSERT INTO environments (id, name, slug, description, created_at, updated_at) VALUES
			('env-prod-000000000000000000000001', 'Production', 'prod', 'Production environment', '2026-01-01 00:00:00.000000', '2026-01-01 00:00:00.000000')`,
		`INSERT INTO owners (id, name, email, created_at, updated_at) VALUES
			('owner-app-000000000000000000000001', 'App Team', 'app@example.com', '2026-01-01 00:00:00.000000', '2026-01-01 00:00:00.000000')`,
		`INSERT INTO users (id, email, password_hash, display_name, role_id, created_at, updated_at) VALUES
			('user-admin-000000000000000000000001', 'admin@example.com', 'hash', 'Admin User', 'role-admin-000000000000000000000001', '2026-01-01 00:00:00.000000', '2026-01-01 00:00:00.000000')`,
		`INSERT INTO resources (
			id, resource_type, resource_subtype, name, display_name, environment_id, owner_id,
			lifecycle_status, health_status, labels, source, external_id, created_at, updated_at,
			archived_at, archived_by, archive_reason
		) VALUES
			('res-host-000000000000000000000001', 'host', 'vm', 'legacy-host-01', 'Legacy Host 01', 'env-prod-000000000000000000000001', 'owner-app-000000000000000000000001', 'running', 'healthy', '{"team":"platform"}', 'manual', 'legacy-host-ext', '2026-01-01 00:00:00.000000', '2026-01-01 00:00:00.000000', NULL, NULL, NULL),
			('res-svc-000000000000000000000001', 'service', 'api', 'legacy-service-01', 'Legacy Service 01', 'env-prod-000000000000000000000001', 'owner-app-000000000000000000000001', 'running', 'warning', '{"team":"platform"}', 'manual', 'legacy-service-ext', '2026-01-01 00:00:00.000000', '2026-01-02 00:00:00.000000', '2026-01-03 00:00:00.000000', 'user-admin-000000000000000000000001', 'legacy cleanup')`,
		`INSERT INTO resource_profiles_host (resource_id, hostname, ip_address, os_name) VALUES
			('res-host-000000000000000000000001', 'legacy-host-01.internal', '10.0.0.10', 'Ubuntu 24.04')`,
		`INSERT INTO resource_relations (id, from_resource_id, to_resource_id, relation_type, created_at) VALUES
			('rel-000000000000000000000000000001', 'res-svc-000000000000000000000001', 'res-host-000000000000000000000001', 'depends_on', '2026-01-04 00:00:00.000000')`,
		`INSERT INTO audit_events (id, actor_user_id, target_resource_id, event_type, result, created_at) VALUES
			('audit-0000000000000000000000000001', 'user-admin-000000000000000000000001', 'res-svc-000000000000000000000001', 'resource.archived', 'success', '2026-01-03 00:00:00.000000')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed legacy source using %q: %v", shortSQL(statement), err)
		}
	}
}

func shortSQL(statement string) string {
	trimmed := strings.TrimSpace(statement)
	parts := strings.Fields(trimmed)
	if len(parts) > 4 {
		parts = parts[:4]
	}
	return strings.Join(parts, " ")
}

func assertCount(t *testing.T, db *sql.DB, tableName string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT count(*) FROM " + tableName).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("count %s = %d, want %d", tableName, got, want)
	}
}

func assertHostProfileResourceID(t *testing.T, db *sql.DB, resourceID uint64) {
	t.Helper()
	var got uint64
	if err := db.QueryRow("SELECT resource_id FROM resource_profiles_host WHERE hostname = ?", "legacy-host-01.internal").Scan(&got); err != nil {
		t.Fatalf("query host profile resource_id: %v", err)
	}
	if got != resourceID {
		t.Fatalf("host profile resource_id = %d, want %d", got, resourceID)
	}
}

func assertImportedRelation(t *testing.T, db *sql.DB, fromID, toID uint64) {
	t.Helper()
	var gotFrom, gotTo uint64
	if err := db.QueryRow("SELECT from_resource_id, to_resource_id FROM resource_relations WHERE relation_type = ?", "depends_on").Scan(&gotFrom, &gotTo); err != nil {
		t.Fatalf("query imported relation: %v", err)
	}
	if gotFrom != fromID || gotTo != toID {
		t.Fatalf("imported relation = (%d,%d), want (%d,%d)", gotFrom, gotTo, fromID, toID)
	}
}

func assertImportedAuditEvent(t *testing.T, db *sql.DB, targetResourceID uint64) {
	t.Helper()
	var actorID uint64
	var gotTarget sql.NullInt64
	if err := db.QueryRow("SELECT actor_user_id, target_resource_id FROM audit_events WHERE event_type = ?", "resource.archived").Scan(&actorID, &gotTarget); err != nil {
		t.Fatalf("query imported audit event: %v", err)
	}
	if actorID == 0 {
		t.Fatal("expected imported audit event actor id to be bigint")
	}
	if !gotTarget.Valid || uint64(gotTarget.Int64) != targetResourceID {
		t.Fatalf("imported audit target = %v, want %d", gotTarget, targetResourceID)
	}
}

func assertImportedArchiveFields(t *testing.T, db *sql.DB, resourceID uint64) {
	t.Helper()
	var archivedBy sql.NullInt64
	var archiveReason sql.NullString
	var archivedAt sql.NullTime
	if err := db.QueryRow("SELECT archived_at, archived_by, archive_reason FROM resources WHERE id = ?", resourceID).Scan(&archivedAt, &archivedBy, &archiveReason); err != nil {
		t.Fatalf("query imported archive fields: %v", err)
	}
	if !archivedAt.Valid {
		t.Fatal("expected archived_at to be preserved")
	}
	if !archivedBy.Valid || archivedBy.Int64 == 0 {
		t.Fatal("expected archived_by to be translated to bigint user id")
	}
	if !archiveReason.Valid || archiveReason.String != "legacy cleanup" {
		t.Fatalf("archive_reason = %v, want legacy cleanup", archiveReason)
	}
}
