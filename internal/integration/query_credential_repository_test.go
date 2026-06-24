//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 38A
// query credential metadata repository operations (Task B2): product-safe
// get/upsert/delete, in-method validation guard, fail-closed read of an invalid
// stored ref, and the no-DSN-stored invariant.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

// newCredentialRepoTestDB returns a clean DB connection plus a credential
// repository. query_target_credentials has no FK on resource_id, so repository
// tests use synthetic resource ids without provisioning real resources.
func newCredentialRepoTestDB(t *testing.T) (*sql.DB, *mysql.QueryExecutionRepository) {
	t.Helper()
	db := setupTestDB(t)
	return db, mysql.NewQueryExecutionRepository(db)
}

// TestQueryCredentialRepository_UpsertGetDelete proves the product-safe metadata
// lifecycle: a missing row reads as not-found, upsert inserts, a second upsert
// updates in place (idempotent on the unique resource_id), and delete removes the
// row so it reads as not-found again. WHY: the product API relies on these three
// primitives behaving exactly this way.
func TestQueryCredentialRepository_UpsertGetDelete(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 7700000001

	// 1. No metadata row -> not-found sentinel, never a zero-value row.
	if _, err := repo.GetCredentialByResourceID(ctx, rid); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing row: err = %v, want sql.ErrNoRows", err)
	}

	// 2. Insert.
	meta := model.QueryCredentialMetadata{
		ResourceID:        rid,
		Engine:            "mysql",
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	if err := repo.UpsertCredentialMetadata(ctx, meta); err != nil {
		t.Fatalf("upsert insert: %v", err)
	}
	got, err := repo.GetCredentialByResourceID(ctx, rid)
	if err != nil {
		t.Fatalf("get after insert: %v", err)
	}
	if got.CredentialRef != "ORDER_MYSQL_RO" || !got.Enabled || got.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly {
		t.Fatalf("inserted metadata = %+v", got)
	}

	// 3. Update in place (same resource_id, new values) — exactly one row.
	meta.CredentialRef = "ORDER_MYSQL_RO_OVERRIDE"
	meta.Enabled = false
	meta.EnvironmentPolicy = model.QueryEnvPolicyAllEnvironments
	if err := repo.UpsertCredentialMetadata(ctx, meta); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	got, err = repo.GetCredentialByResourceID(ctx, rid)
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if got.CredentialRef != "ORDER_MYSQL_RO_OVERRIDE" || got.Enabled || got.EnvironmentPolicy != model.QueryEnvPolicyAllEnvironments {
		t.Fatalf("updated metadata = %+v", got)
	}
	if credentialRowCount(t, db, rid) != 1 {
		t.Fatal("upsert must keep exactly one row per resource_id")
	}

	// 4. Delete -> not-found again.
	if err := repo.DeleteCredentialByResourceID(ctx, rid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.GetCredentialByResourceID(ctx, rid); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("after delete: err = %v, want sql.ErrNoRows", err)
	}
	if credentialRowCount(t, db, rid) != 0 {
		t.Fatal("after delete, row count must be 0")
	}
}

// TestQueryCredentialRepository_InvalidStoredRefFailsClosed proves a row whose
// credential_ref was bypassed into the table (e.g. legacy/manual data) is never
// surfaced to a resolver: the read re-validates and fails closed. WHY: the
// resolver must never perform an env lookup with an unvalidated key.
func TestQueryCredentialRepository_InvalidStoredRefFailsClosed(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 7700000002

	// Bypass application validation to plant an invalid ref directly.
	mustExec(t, db, `insert into query_target_credentials (resource_id, engine, credential_ref, enabled, environment_policy) values (?, 'mysql', 'lowercase-bad', false, 'non_prod_only')`, rid)

	_, err := repo.GetCredentialByResourceID(ctx, rid)
	if err == nil {
		t.Fatal("read of an invalid stored ref must fail closed, got nil")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatal("read of an invalid stored ref must error, not report not-found")
	}
}

// TestQueryCredentialRepository_UpsertRejectsInvalidRefAndPolicy proves the
// in-method validation guard rejects an invalid credential ref and an invalid
// environment policy before any write, leaving the table untouched. WHY: the
// product write path must validate defense-in-depth, not trust the caller.
func TestQueryCredentialRepository_UpsertRejectsInvalidRefAndPolicy(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()

	cases := []struct {
		name string
		meta model.QueryCredentialMetadata
	}{
		{"invalid ref", model.QueryCredentialMetadata{ResourceID: 7700000003, Engine: "mysql", CredentialRef: "bad-ref!", EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly, Enabled: true}},
		{"empty ref", model.QueryCredentialMetadata{ResourceID: 7700000003, Engine: "mysql", CredentialRef: "", EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly, Enabled: true}},
		{"invalid policy", model.QueryCredentialMetadata{ResourceID: 7700000003, Engine: "mysql", CredentialRef: "OK_REF", EnvironmentPolicy: model.QueryEnvironmentPolicy("prod_plus"), Enabled: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.UpsertCredentialMetadata(ctx, tc.meta); err == nil {
				t.Fatalf("upsert with %s must be rejected", tc.name)
			}
			if credentialRowCount(t, db, tc.meta.ResourceID) != 0 {
				t.Fatalf("rejected upsert must not write a row for %s", tc.name)
			}
		})
	}
}

// TestQueryCredentialRepository_MetadataNeverStoresDSN proves the stored
// metadata columns never carry a DSN/password: a normal upsert stores only the
// opaque ref/engine/policy, and a credential_ref that looks like a DSN is
// rejected by validation before it can be persisted.
func TestQueryCredentialRepository_MetadataNeverStoresDSN(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 7700000004

	if err := repo.UpsertCredentialMetadata(ctx, model.QueryCredentialMetadata{
		ResourceID:        rid,
		Engine:            "mysql",
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// No metadata column may carry a DSN-looking value.
	var engine, ref, policy string
	var enabled bool
	if err := db.QueryRow(
		`select engine, credential_ref, enabled, environment_policy from query_target_credentials where resource_id = ?`,
		rid,
	).Scan(&engine, &ref, &enabled, &policy); err != nil {
		t.Fatalf("read metadata row: %v", err)
	}
	for _, val := range []string{engine, ref, policy} {
		if strings.Contains(val, "tcp(") || strings.Contains(val, "://") || strings.Contains(val, "@") {
			t.Fatalf("metadata column %q looks like a DSN fragment", val)
		}
	}

	// A ref shaped like a DSN is rejected outright, so it can never be stored.
	if err := repo.UpsertCredentialMetadata(ctx, model.QueryCredentialMetadata{
		ResourceID:        rid,
		Engine:            "mysql",
		CredentialRef:     "root:secret@tcp(127.0.0.1:3306)/db",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err == nil {
		t.Fatal("a DSN-shaped credential_ref must be rejected and never stored")
	}
}

// credentialRowCount returns the number of credential metadata rows for a
// resource id.
func credentialRowCount(t *testing.T, db *sql.DB, resourceID uint64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`select count(*) from query_target_credentials where resource_id = ?`, resourceID).Scan(&n); err != nil {
		t.Fatalf("count credential rows: %v", err)
	}
	return n
}

// TestQueryCredentialRepository_UpsertWithAudit_AtomicOnSuccess proves the
// transactional upsert+audit commits BOTH the metadata row and the audit row on
// success. WHY: it is the happy path of the atomic primitive P1-2 requires — the
// repository, not the service, owns the metadata+audit transaction.
func TestQueryCredentialRepository_UpsertWithAudit_AtomicOnSuccess(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 7700000005
	meta := model.QueryCredentialMetadata{
		ResourceID: rid, Engine: "mysql", CredentialRef: "ORDER_MYSQL_RO",
		Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	if err := repo.UpsertCredentialMetadataWithAudit(ctx, meta, 42, "query.credential.updated", "success"); err != nil {
		t.Fatalf("upsert with audit: %v", err)
	}
	if credentialRowCount(t, db, rid) != 1 {
		t.Fatal("metadata row must be committed on success")
	}
	if n := qcAuditCount(t, db, rid, "query.credential.updated"); n != 1 {
		t.Fatalf("audit row must be committed on success, got %d", n)
	}
}

// TestQueryCredentialRepository_UpsertWithAudit_RollsBackOnAuditFailure proves a
// failed audit write rolls back the metadata upsert inside the real MySQL
// transaction. The audit failure is forced by overflowing the varchar(64)
// event_type column (the testcontainer uses MySQL's default STRICT_TRANS_TABLES,
// so this errors with 1406). WHY: "configured but no audit" is forbidden.
func TestQueryCredentialRepository_UpsertWithAudit_RollsBackOnAuditFailure(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 7700000006
	meta := model.QueryCredentialMetadata{
		ResourceID: rid, Engine: "mysql", CredentialRef: "ORDER_MYSQL_RO",
		Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	longEventType := strings.Repeat("x", 65) // exceeds audit_events.event_type varchar(64)
	if err := repo.UpsertCredentialMetadataWithAudit(ctx, meta, 42, longEventType, "success"); err == nil {
		t.Fatal("an overflowing event_type must make the audit insert fail")
	}
	if credentialRowCount(t, db, rid) != 0 {
		t.Fatal("metadata must NOT be committed when the audit insert fails (transaction rolled back)")
	}
}

// TestQueryCredentialRepository_DeleteWithAudit_RollsBackOnAuditFailure proves a
// failed audit write rolls back the metadata delete inside the real MySQL
// transaction. WHY: deleting metadata without an audit row would silently remove
// an attributed change; delete+audit must be one atomic store operation.
func TestQueryCredentialRepository_DeleteWithAudit_RollsBackOnAuditFailure(t *testing.T) {
	db, repo := newCredentialRepoTestDB(t)
	ctx := context.Background()
	const rid uint64 = 7700000007
	meta := model.QueryCredentialMetadata{
		ResourceID: rid, Engine: "mysql", CredentialRef: "ORDER_MYSQL_RO",
		Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	if err := repo.UpsertCredentialMetadata(ctx, meta); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
	longEventType := strings.Repeat("x", 65) // exceeds audit_events.event_type varchar(64)
	if err := repo.DeleteCredentialMetadataWithAudit(ctx, rid, 42, longEventType, "success"); err == nil {
		t.Fatal("an overflowing event_type must make the audit insert fail")
	}
	if credentialRowCount(t, db, rid) != 1 {
		t.Fatal("metadata must still be present when the audit insert fails (transaction rolled back)")
	}
}
