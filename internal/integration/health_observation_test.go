//go:build integration

// Package integration provides real-MySQL coverage for resource health observations.
// input: disposable MySQL, resource repository, health observations, and inventory audit storage
// output: latest-per-observer, effective-health, no-observation-audit, and atomic override tests
// pos: Persistence contract for Issue 81 resource health state
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

func TestHealthObservationsKeepLatestPerObserverAndNeverInventoryAudit(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	created, err := repo.CreateResource(ctx, inventoryAuditResource("health-observation-latest"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if _, err := repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{ClearHealthStatus: true}, ownerDBA, "inventory.resource.health_override.cleared"); err != nil {
		t.Fatalf("clear initial manual override: %v", err)
	}

	beforeAudits := countResourceAudits(t, db, created.ID)
	now := time.Now().UTC().Truncate(time.Microsecond)
	observations := []model.HealthObservation{
		{Status: model.HealthStatusHealthy, ObservedAt: now.Add(-2 * time.Hour), Observer: "prometheus"},
		{Status: model.HealthStatusWarning, ObservedAt: now.Add(-time.Hour), Observer: "prometheus"},
		{Status: model.HealthStatusCritical, ObservedAt: now.Add(-90 * time.Minute), Observer: "synthetic-check"},
		{Status: model.HealthStatusHealthy, ObservedAt: now.Add(-3 * time.Hour), Observer: "prometheus"},
	}
	for _, observation := range observations {
		if err := repo.UpsertHealthObservation(ctx, created.ID, observation); err != nil {
			t.Fatalf("upsert observation from %s: %v", observation.Observer, err)
		}
	}

	got, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if got.HealthStatus != string(model.HealthStatusCritical) || got.HealthFreshness != model.HealthFreshnessFresh {
		t.Fatalf("effective health = (%q, %q), want (critical, fresh)", got.HealthStatus, got.HealthFreshness)
	}
	if got.HealthObservedAt == nil || !got.HealthObservedAt.Equal(now.Add(-90*time.Minute)) || got.HealthObserver != "synthetic-check" {
		t.Fatalf("effective observation = (%v, %q), want (%v, synthetic-check)", got.HealthObservedAt, got.HealthObserver, now.Add(-90*time.Minute))
	}

	var stored int
	if err := db.QueryRowContext(ctx, `select count(*) from resource_health_observations where resource_id = ?`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if stored != 2 {
		t.Fatalf("stored observations = %d, want one latest row per each of 2 observers", stored)
	}
	if afterAudits := countResourceAudits(t, db, created.ID); afterAudits != beforeAudits {
		t.Fatalf("audit count after observations = %d, want unchanged %d", afterAudits, beforeAudits)
	}
}

func TestHealthObservationStaleAndNeverAreNotHealthy(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	created, err := repo.CreateResource(ctx, inventoryAuditResource("health-observation-stale"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if _, err := repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{ClearHealthStatus: true}, ownerDBA, "inventory.resource.health_override.cleared"); err != nil {
		t.Fatalf("clear initial manual override: %v", err)
	}

	got, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get never-observed resource: %v", err)
	}
	if got.HealthStatus == string(model.HealthStatusHealthy) || got.HealthFreshness != model.HealthFreshnessNever {
		t.Fatalf("never-observed health = (%q, %q), want non-healthy/never", got.HealthStatus, got.HealthFreshness)
	}

	observedAt := time.Now().UTC().Add(-25 * time.Hour).Truncate(time.Microsecond)
	if err := repo.UpsertHealthObservation(ctx, created.ID, model.HealthObservation{
		Status: model.HealthStatusHealthy, ObservedAt: observedAt, Observer: "prometheus",
	}); err != nil {
		t.Fatalf("upsert stale observation: %v", err)
	}
	got, err = repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get stale resource: %v", err)
	}
	if got.HealthStatus == string(model.HealthStatusHealthy) || got.HealthFreshness != model.HealthFreshnessStale {
		t.Fatalf("stale health = (%q, %q), want non-healthy/stale", got.HealthStatus, got.HealthFreshness)
	}
}

func TestHealthStatusFilterUsesEffectiveObservation(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	created, err := repo.CreateResource(ctx, inventoryAuditResource("health-observation-filter"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if _, err := repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{ClearHealthStatus: true}, ownerDBA, "inventory.resource.health_override.cleared"); err != nil {
		t.Fatalf("clear initial manual override: %v", err)
	}
	if err := repo.UpsertHealthObservation(ctx, created.ID, model.HealthObservation{
		Status: model.HealthStatusWarning, ObservedAt: time.Now().UTC(), Observer: "prometheus",
	}); err != nil {
		t.Fatalf("upsert warning observation: %v", err)
	}

	items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
		HealthStatuses: []string{string(model.HealthStatusWarning)},
		Page:           1,
		PageSize:       100,
	})
	if err != nil {
		t.Fatalf("list warning resources: %v", err)
	}
	for _, item := range items {
		if item.ID == created.ID {
			return
		}
	}
	t.Fatalf("effective warning resource %d missing from healthStatus filter", created.ID)
}

func TestManualHealthOverrideSetAndClearCommitWithInventoryAudit(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	created, err := repo.CreateResource(ctx, inventoryAuditResource("health-override-atomic"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	critical := model.HealthStatusCritical
	updated, err := repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{HealthStatus: &critical}, ownerDBA, "inventory.resource.health_override.set")
	if err != nil {
		t.Fatalf("set manual override: %v", err)
	}
	if updated.ManualHealthOverride == nil || *updated.ManualHealthOverride != critical {
		t.Fatalf("manual override = %v, want critical", updated.ManualHealthOverride)
	}
	assertLatestHealthOverrideAudit(t, db, created.ID, model.AuditChangeUpdate, string(model.HealthStatusHealthy), string(critical))

	updated, err = repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{ClearHealthStatus: true}, ownerDBA, "inventory.resource.health_override.cleared")
	if err != nil {
		t.Fatalf("clear manual override: %v", err)
	}
	if updated.ManualHealthOverride != nil {
		t.Fatalf("manual override after clear = %v, want nil", updated.ManualHealthOverride)
	}
	assertLatestHealthOverrideAudit(t, db, created.ID, model.AuditChangeRemove, string(critical), nil)

	warning := model.HealthStatusWarning
	if _, err := repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{HealthStatus: &warning}, ownerDBA, strings.Repeat("x", 65)); err == nil {
		t.Fatal("expected audit insertion failure")
	}
	got, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get resource after rollback: %v", err)
	}
	if got.ManualHealthOverride != nil {
		t.Fatalf("manual override after audit rollback = %v, want nil", got.ManualHealthOverride)
	}
}

func countResourceAudits(t *testing.T, db *sql.DB, resourceID uint64) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ?`, resourceID).Scan(&count); err != nil {
		t.Fatalf("count audits: %v", err)
	}
	return count
}

func assertLatestHealthOverrideAudit(t *testing.T, db *sql.DB, resourceID uint64, operation model.AuditChangeOperation, before, after any) {
	t.Helper()
	var raw string
	if err := db.QueryRow(`select changes from audit_events where target_resource_id = ? order by id desc limit 1`, resourceID).Scan(&raw); err != nil {
		t.Fatalf("read health override audit: %v", err)
	}
	var changes []model.AuditChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatalf("decode health override audit: %v", err)
	}
	if len(changes) != 1 || changes[0].Field != "manualHealthOverride" || changes[0].Operation != operation || changes[0].Before != before || changes[0].After != after {
		t.Fatalf("health override audit = %#v", changes)
	}
}
