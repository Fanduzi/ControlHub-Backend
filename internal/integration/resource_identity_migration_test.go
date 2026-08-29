//go:build integration

// Package integration provides fail-loud migration coverage for CMDB identity.
// input: testing, strings, pressly/goose
// output: TestResourceIdentityMigration_DuplicateLegacyExternalIDFailsBeforeSchemaChange
// pos: Proves unsafe legacy externalId data aborts migration 19 without dropping source data
// note: if this file changes, update header and README.md
package integration

import (
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestResourceIdentityMigration_DuplicateLegacyExternalIDFailsBeforeSchemaChange(t *testing.T) {
	admin := setupTestDB(t)
	const databaseName = "controlhub_identity_76_failure"
	dropDatabase(t, admin, databaseName)
	createDatabase(t, admin, databaseName)
	t.Cleanup(func() { dropDatabase(t, admin, databaseName) })
	db := openNamedTestDB(t, databaseName)

	goose.SetDialect("mysql")
	if err := goose.UpTo(db, resolveMigrationsDir(), 18); err != nil {
		t.Fatalf("migrate fixture to 18: %v", err)
	}
	if _, err := db.Exec(`insert into resources
		(resource_type, resource_subtype, name, display_name, environment_id, owner_id, lifecycle_status, health_status, labels, source, external_id)
		values
		('service', 'api', 'identity-dup-a', 'A', 1, 2, 'running', 'healthy', '{}', 'manual', 'duplicate-76'),
		('host', 'vm', 'identity-dup-b', 'B', 2, 2, 'running', 'healthy', '{}', 'manual', 'duplicate-76')`); err != nil {
		t.Fatalf("seed duplicate legacy IDs: %v", err)
	}

	err := goose.UpTo(db, resolveMigrationsDir(), 19)
	if err == nil || !strings.Contains(err.Error(), "cannot migrate duplicate resources.external_id values") {
		t.Fatalf("migration error = %v, want duplicate externalId failure", err)
	}
	if !columnExists(t, db, "resources", "external_id") || columnExists(t, db, "resources", "origin") {
		t.Fatal("failed migration changed resource identity columns")
	}
	if tableExists(t, db, "resource_external_identifiers") || tableExists(t, db, "resource_aliases") {
		t.Fatal("failed migration created identity tables")
	}
}
