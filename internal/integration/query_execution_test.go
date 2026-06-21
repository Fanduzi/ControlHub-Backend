//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 37
// read-only query sandbox (repository, service end-to-end, audit/history).
package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

// seedCredentialRow is a test-only fixture that inserts a credential metadata
// row directly. Phase 37 has no credential write API, so product code never
// inserts these rows; this helper exists solely to exercise repository reads and
// the readiness/execute paths against realistic data.
func seedCredentialRow(t *testing.T, db *sql.DB, resourceID uint64, engine, ref string, enabled bool, policy string) {
	t.Helper()
	_, err := db.Exec(
		`insert into query_target_credentials (resource_id, engine, credential_ref, enabled, environment_policy) values (?, ?, ?, ?, ?)`,
		resourceID, engine, ref, enabled, policy,
	)
	if err != nil {
		t.Fatalf("seed credential row for resource %d: %v", resourceID, err)
	}
}

// createQueryTargetResource creates a database_instance resource and returns its
// id so a credential row can be attached to it.
func createQueryTargetResource(t *testing.T, db *sql.DB, namePrefix string) uint64 {
	t.Helper()
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	res, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            namePrefix,
		DisplayName:     namePrefix,
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create query target resource: %v", err)
	}
	return res.ID
}

func TestQueryExecutionRepository_InsertAndListByTarget(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-insert-list")

	for i, status := range []model.QueryExecutionStatus{
		model.QueryExecutionSuccess,
		model.QueryExecutionRejected,
	} {
		if _, err := repo.InsertExecution(ctx, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  "select ?",
			StatementPreview: "select 1",
			Status:           status,
			RowCount:         i,
			DurationMs:       int64(i + 1),
		}); err != nil {
			t.Fatalf("insert execution %d: %v", i, err)
		}
	}

	items, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	// WHY: history is newest-first so operators see the latest attempt first.
	if items[0].RowCount < items[1].RowCount {
		t.Fatalf("history not newest-first: items[0].RowCount=%d items[1].RowCount=%d", items[0].RowCount, items[1].RowCount)
	}
	if items[0].Status != model.QueryExecutionRejected {
		t.Fatalf("newest status = %q, want rejected", items[0].Status)
	}
	if items[0].TargetResourceID != targetID || items[0].ActorUserID != ownerDBA {
		t.Fatalf("history record mismatched ids: %+v", items[0])
	}
}

func TestQueryExecutionRepository_CredentialMetadataReported(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-ready")

	// An enabled credential with a valid ref and non_prod_only policy is the
	// data that lets the service mark a non-production target ready.
	seedCredentialRow(t, db, targetID, "mysql", "ORDER_MYSQL_RO", true, string(model.QueryEnvPolicyNonProdOnly))

	got, err := repo.GetCredentialByResourceID(ctx, targetID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if !got.Enabled {
		t.Fatalf("Enabled = false, want true (ready credential)")
	}
	if got.CredentialRef != "ORDER_MYSQL_RO" {
		t.Fatalf("CredentialRef = %q, want ORDER_MYSQL_RO", got.CredentialRef)
	}
	if got.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly {
		t.Fatalf("EnvironmentPolicy = %q, want non_prod_only", got.EnvironmentPolicy)
	}
	if got.Engine != "mysql" || got.ResourceID != targetID {
		t.Fatalf("credential metadata mismatched: %+v", got)
	}
}

func TestQueryExecutionRepository_DisabledCredentialReported(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-disabled")

	// A disabled credential reads back with Enabled=false; the service treats
	// that as locked. The repo reports the flag faithfully.
	seedCredentialRow(t, db, targetID, "mysql", "ORDER_MYSQL_RO", false, string(model.QueryEnvPolicyDisabled))

	got, err := repo.GetCredentialByResourceID(ctx, targetID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if got.Enabled {
		t.Fatalf("Enabled = true, want false (disabled credential must stay locked)")
	}
	if got.EnvironmentPolicy != model.QueryEnvPolicyDisabled {
		t.Fatalf("EnvironmentPolicy = %q, want disabled", got.EnvironmentPolicy)
	}
}

func TestQueryExecutionRepository_InvalidCredentialRefFailsClosed(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-badref")

	// WHY: an invalid credential_ref in the metadata must never reach the
	// resolver/env lookup. The repo validates on read and fails closed (returns
	// an error, distinct from a simple not-found), keeping the target locked.
	seedCredentialRow(t, db, targetID, "mysql", "bad-ref", true, string(model.QueryEnvPolicyAllEnvironments))

	_, err := repo.GetCredentialByResourceID(ctx, targetID)
	if err == nil {
		t.Fatal("expected error for invalid credential_ref, got nil")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid credential_ref must fail closed with a validation error, not sql.ErrNoRows: %v", err)
	}
}

func TestQueryExecutionRepository_RoundTripsTypedEnvironmentPolicy(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		policy model.QueryEnvironmentPolicy
	}{
		{"disabled", model.QueryEnvPolicyDisabled},
		{"non_prod_only", model.QueryEnvPolicyNonProdOnly},
		{"all_environments", model.QueryEnvPolicyAllEnvironments},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targetID := createQueryTargetResource(t, db, "qe-policy-"+tc.name)
			seedCredentialRow(t, db, targetID, "mysql", "ORDER_MYSQL_RO", true, string(tc.policy))

			got, err := repo.GetCredentialByResourceID(ctx, targetID)
			if err != nil {
				t.Fatalf("get credential: %v", err)
			}
			if got.EnvironmentPolicy != tc.policy {
				t.Fatalf("EnvironmentPolicy = %q, want %q", got.EnvironmentPolicy, tc.policy)
			}
			if err := got.EnvironmentPolicy.Validate(); err != nil {
				t.Fatalf("round-tripped policy failed Validate: %v", err)
			}
		})
	}
}

func TestQueryExecutionRepository_MissingCredentialReturnsNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-missing")

	_, err := repo.GetCredentialByResourceID(ctx, targetID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing credential error = %v, want sql.ErrNoRows", err)
	}
}

func TestQueryExecutionRepository_InsertAuditEvent(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-audit")

	if err := repo.InsertAuditEvent(ctx, ownerDBA, targetID, "query.executed", "success"); err != nil {
		t.Fatalf("insert audit event: %v", err)
	}

	var eventType, result string
	err := db.QueryRow(
		`select event_type, result from audit_events where target_resource_id = ? order by id desc limit 1`,
		targetID,
	).Scan(&eventType, &result)
	if err != nil {
		t.Fatalf("read back audit event: %v", err)
	}
	if eventType != "query.executed" || result != "success" {
		t.Fatalf("audit event = (%q,%q), want (query.executed,success)", eventType, result)
	}
}