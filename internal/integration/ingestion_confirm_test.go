//go:build integration

// Package integration runs real-MySQL ingestion confirmation tests.
// input: database/sql, errors, testing, internal/model, internal/repository/mysql, internal/service
// output: TestIngestionConfirmation* atomic confirmation and shared relation-lock cases
// pos: Proves issue #83 confirmation semantics and relation serialization against disposable MySQL
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const ingestionActor uint64 = 1

func TestIngestionConfirmationSuccessAndIdempotentNoOp(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	existing, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: "api",
		Name:            "ingest-existing-success",
		DisplayName:     "Old Service",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Aliases:         []string{"ingest-old-success"},
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create existing: %v", err)
	}
	if err := repo.PutProfileWithAudit(ctx, existing.ID, model.ResourceTypeService, map[string]any{
		"systemName": "old-system", "repositoryUrl": "https://example.invalid/old", "runtimeEnv": "go",
	}, ingestionActor, "inventory.profile.updated"); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	target, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "ingest-target-success",
		DisplayName:     "Target Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}

	rows := []service.IngestionRow{
		{
			EnvironmentID:  envProd,
			CIType:         model.ResourceTypeService,
			Name:           "ingest-existing-success",
			DisplayName:    "New Service",
			Aliases:        []string{"ingest-new-success"},
			Profile:        map[string]any{"systemName": "new-system", "repositoryUrl": "https://example.invalid/new", "runtimeEnv": "go"},
			ObservedValues: map[string]service.ObservedValueInput{"displayName": {Source: "discovery", Value: "Observed Service"}},
			Relations:      []service.IngestionRelation{{Type: model.RelationTypeRunsOn, TargetID: target.ID}},
		},
		{
			EnvironmentID:       envProd,
			CIType:              model.ResourceTypeHost,
			Name:                "ingest-created-success",
			DisplayName:         "Created Host",
			Aliases:             []string{"ingest-created-alias"},
			ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: "asset", Value: "created-success"}},
			Profile:             map[string]any{"hostname": "ingest-created-success", "ipAddress": "10.83.0.1", "osName": "linux"},
			ObservedValues:      map[string]service.ObservedValueInput{"displayName": {Source: "discovery", Value: "Observed Created"}},
		},
	}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := repo.ConfirmIngestion(ctx, rows, preview.Fingerprint, ingestionActor); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	updated, err := repo.GetResource(existing.ID)
	if err != nil {
		t.Fatalf("get updated: %v", err)
	}
	if updated.DisplayName != "New Service" || len(updated.Aliases) != 1 || updated.Aliases[0] != "ingest-new-success" {
		t.Fatalf("updated identity = %+v", updated)
	}
	if got := resourceIDByName(t, db, "ingest-created-success"); got == 0 {
		t.Fatal("created resource missing")
	} else {
		created, err := repo.GetResource(got)
		if err != nil {
			t.Fatalf("get created: %v", err)
		}
		if created.OwnerID != ingestionActor {
			t.Fatalf("created owner = %d, want confirming actor %d", created.OwnerID, ingestionActor)
		}
		assertObservedValue(t, db, got, "displayName", "discovery")
	}
	if got := relationCount(t, db, existing.ID, target.ID, model.RelationTypeRunsOn); got != 1 {
		t.Fatalf("relation count = %d, want 1", got)
	}
	assertObservedValue(t, db, existing.ID, "displayName", "discovery")

	auditsBefore := ingestionAuditCount(t, db)
	repreview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("repreview: %v", err)
	}
	if !repreview.Confirmable {
		t.Fatalf("repreview not confirmable: %+v", repreview)
	}
	if _, err := repo.ConfirmIngestion(ctx, rows, repreview.Fingerprint, ingestionActor); err != nil {
		t.Fatalf("idempotent confirm: %v", err)
	}
	if auditsAfter := ingestionAuditCount(t, db); auditsAfter != auditsBefore {
		t.Fatalf("audit count after no-op confirm = %d, want %d", auditsAfter, auditsBefore)
	}
	if got := relationCount(t, db, existing.ID, target.ID, model.RelationTypeRunsOn); got != 1 {
		t.Fatalf("relation count after no-op = %d, want 1", got)
	}
}

func TestIngestionConfirmationConflictFingerprintDriftAndAuditRollback(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	left := mustCreateIngestionHost(t, repo, "ingest-conflict-left", "Conflict Left")
	right := mustCreateIngestionHost(t, repo, "ingest-conflict-right", "Conflict Right")
	if _, err := repo.UpdateResource(ctx, left.ID, model.ResourceUpdateInput{Aliases: &[]string{"ingest-shared-conflict"}}); err != nil {
		t.Fatalf("seed alias: %v", err)
	}

	conflictRows := []service.IngestionRow{{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: right.Name, Aliases: []string{"ingest-shared-conflict"}}}
	conflictPreview, err := repo.PreviewIngestion(ctx, conflictRows)
	if err != nil {
		t.Fatalf("conflict preview: %v", err)
	}
	if _, err := repo.ConfirmIngestion(ctx, conflictRows, conflictPreview.Fingerprint, ingestionActor); !errors.Is(err, service.ErrIngestionConflict) {
		t.Fatalf("conflict confirm error = %v", err)
	}
	if got, _ := repo.GetResource(right.ID); got.DisplayName != right.DisplayName {
		t.Fatal("conflict confirm wrote resource")
	}

	driftRows := []service.IngestionRow{{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: right.Name, DisplayName: "After Drift"}}
	driftPreview, err := repo.PreviewIngestion(ctx, driftRows)
	if err != nil {
		t.Fatalf("drift preview: %v", err)
	}
	otherName := "Changed Elsewhere"
	if _, err := repo.UpdateResource(ctx, right.ID, model.ResourceUpdateInput{DisplayName: &otherName}); err != nil {
		t.Fatalf("drift update: %v", err)
	}
	if _, err := repo.ConfirmIngestion(ctx, driftRows, driftPreview.Fingerprint, ingestionActor); !errors.Is(err, service.ErrIngestionFingerprintMismatch) {
		t.Fatalf("drift confirm error = %v", err)
	}
	if got, _ := repo.GetResource(right.ID); got.DisplayName != otherName {
		t.Fatalf("drift confirm display = %q, want %q", got.DisplayName, otherName)
	}

	rollbackRows := []service.IngestionRow{
		{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: right.Name, DisplayName: "Rolled Back"},
		{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: "ingest-rollback-created", DisplayName: "Rollback Created"},
	}
	rollbackPreview, err := repo.PreviewIngestion(ctx, rollbackRows)
	if err != nil {
		t.Fatalf("rollback preview: %v", err)
	}
	if _, err := db.Exec(`drop trigger if exists ch83_force_ingestion_audit_fail`); err != nil {
		t.Fatalf("drop stale trigger: %v", err)
	}
	if _, err := db.Exec(`create trigger ch83_force_ingestion_audit_fail
		before insert on audit_events for each row
		begin
			if new.event_type = 'inventory.ingestion.confirmed' then
				signal sqlstate '45000' set message_text = 'forced ingestion audit failure';
			end if;
		end`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists ch83_force_ingestion_audit_fail`) })
	if _, err := repo.ConfirmIngestion(ctx, rollbackRows, rollbackPreview.Fingerprint, ingestionActor); err == nil {
		t.Fatal("expected forced audit failure")
	}
	if got, _ := repo.GetResource(right.ID); got.DisplayName != otherName {
		t.Fatalf("rollback display = %q, want %q", got.DisplayName, otherName)
	}
	if got := resourceIDByName(t, db, "ingest-rollback-created"); got != 0 {
		t.Fatalf("rollback create persisted id %d", got)
	}
}

func TestIngestionConfirmationPreservesManualOverride(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	resource := mustCreateIngestionHost(t, repo, "ingest-manual-override", "Manual Override Base")
	if _, err := repo.SetManualOverrideWithAudit(ctx, resource.ID, "displayName", "Operator Name", 0, ingestionActor); err != nil {
		t.Fatalf("set override: %v", err)
	}
	rows := []service.IngestionRow{{
		EnvironmentID:  envProd,
		CIType:         model.ResourceTypeHost,
		Name:           resource.Name,
		DisplayName:    "Ingested Name",
		ObservedValues: map[string]service.ObservedValueInput{"displayName": {Source: "discovery", Value: "Observed Name"}},
	}}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := repo.ConfirmIngestion(ctx, rows, preview.Fingerprint, ingestionActor); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	values, err := repo.GetEffectiveValues(ctx, resource.ID)
	if err != nil {
		t.Fatalf("effective values: %v", err)
	}
	if got := values["displayName"]; got.Value != "Operator Name" || got.Provenance.Kind != "manual_override" {
		t.Fatalf("effective displayName = %+v, want manual override", got)
	}
	var count int
	if err := db.QueryRowContext(ctx, `select count(*) from resource_manual_overrides where resource_id = ? and field_name = 'displayName'`, resource.ID).Scan(&count); err != nil {
		t.Fatalf("count overrides: %v", err)
	}
	if count != 1 {
		t.Fatalf("override rows = %d, want 1", count)
	}
}

func TestIngestionConfirmationRejectsExternalIdentifierCITypeConflict(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	resource := mustCreateIngestionHost(t, repo, "ingest-type-conflict", "Type Conflict")
	identifiers := []model.ResourceExternalIdentifier{{System: "asset", Value: "immutable-type"}}
	if _, err := repo.UpdateResource(ctx, resource.ID, model.ResourceUpdateInput{ExternalIdentifiers: &identifiers}); err != nil {
		t.Fatalf("seed external identifier: %v", err)
	}
	rows := []service.IngestionRow{{
		EnvironmentID: envProd,
		CIType:        model.ResourceTypeService,
		Name:          "different-service",
		ExternalIdentifiers: []model.ResourceExternalIdentifier{{
			System: "asset", Value: "immutable-type",
		}},
	}}

	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Confirmable || preview.Rows[0].Action != service.PreviewConflict {
		t.Fatalf("immutable ciType mismatch must conflict: %+v", preview)
	}
	if _, err := repo.ConfirmIngestion(ctx, rows, preview.Fingerprint, ingestionActor); !errors.Is(err, service.ErrIngestionConflict) {
		t.Fatalf("confirm error = %v, want ingestion conflict", err)
	}
	got, err := repo.GetResource(resource.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if got.ResourceType != model.ResourceTypeHost {
		t.Fatalf("resource type = %q, want host", got.ResourceType)
	}
}

func TestIngestionConfirmationObservedValuesAreAdditive(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	resource := mustCreateIngestionHost(t, repo, "ingest-observed-additive", "Observed Additive")
	if err := repo.PutObservedValues(ctx, resource.ID, "agent", map[string]any{
		"displayName": "Agent Name", "lifecycleStatus": "running",
	}); err != nil {
		t.Fatalf("seed agent observations: %v", err)
	}
	if err := repo.PutObservedValues(ctx, resource.ID, "discovery", map[string]any{
		"healthStatus": "healthy",
	}); err != nil {
		t.Fatalf("seed discovery observation: %v", err)
	}
	rows := []service.IngestionRow{{
		EnvironmentID: envProd,
		CIType:        model.ResourceTypeHost,
		Name:          resource.Name,
		DisplayName:   resource.DisplayName,
		ObservedValues: map[string]service.ObservedValueInput{
			"displayName": {Source: "cmdb", Value: "CMDB Name"},
		},
	}}

	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if diff := preview.Rows[0].Diff.Observed; len(diff) != 1 || diff["displayName"].After == nil {
		t.Fatalf("observed diff must contain only submitted fields: %+v", diff)
	}
	if _, err := repo.ConfirmIngestion(ctx, rows, preview.Fingerprint, ingestionActor); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `select count(*) from resource_observed_values where resource_id = ?`, resource.ID).Scan(&count); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if count != 4 {
		t.Fatalf("observation rows = %d, want 4 preserved sources/fields", count)
	}
}

func TestIngestionConfirmationRelationMutationsUseSnapshotLocks(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	resourceRepo := mysql.NewResourceRepository(db)
	source, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType: model.ResourceTypeService, ResourceSubtype: "api", Name: "ingest-lock-source",
		DisplayName: "Lock Source", EnvironmentID: envProd, OwnerID: ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy,
		Origin: model.ResourceOriginManual, Labels: map[string]string{},
	})
	if err != nil {
		t.Fatalf("create source: %v", err)
	}
	target := mustCreateIngestionHost(t, resourceRepo, "ingest-lock-target", "Lock Target")
	relationRepo := mysql.NewRelationRepository(db)
	input := model.RelationCreateInput{FromResourceID: source.ID, ToResourceID: target.ID, RelationType: model.RelationTypeRunsOn}

	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin create blocker: %v", err)
	}
	if err := blocker.QueryRowContext(ctx, `select id from resources where id = ? for update`, source.ID).Scan(new(uint64)); err != nil {
		t.Fatalf("lock source: %v", err)
	}
	blockedCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, blockedErr := relationRepo.CreateRelationWithAudit(blockedCtx, input, ingestionActor, "inventory.relationship.created")
	cancel()
	if blockedErr == nil {
		t.Fatal("relation create interleaved with resource snapshot lock")
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release create blocker: %v", err)
	}
	if got := relationCount(t, db, source.ID, target.ID, model.RelationTypeRunsOn); got != 0 {
		t.Fatalf("blocked create relation count = %d, want 0", got)
	}
	relation, err := relationRepo.CreateRelationWithAudit(ctx, input, ingestionActor, "inventory.relationship.created")
	if err != nil {
		t.Fatalf("create after lock release: %v", err)
	}

	blocker, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin delete blocker: %v", err)
	}
	if err := blocker.QueryRowContext(ctx, `select id from resources where id = ? for update`, target.ID).Scan(new(uint64)); err != nil {
		t.Fatalf("lock target: %v", err)
	}
	blockedCtx, cancel = context.WithTimeout(ctx, 500*time.Millisecond)
	blockedErr = relationRepo.DeleteRelationWithAudit(blockedCtx, relation.ID, ingestionActor, "inventory.relationship.deleted")
	cancel()
	if blockedErr == nil {
		t.Fatal("relation delete interleaved with resource snapshot lock")
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatalf("release delete blocker: %v", err)
	}
	if got := relationCount(t, db, source.ID, target.ID, model.RelationTypeRunsOn); got != 1 {
		t.Fatalf("blocked delete relation count = %d, want 1", got)
	}
	if err := relationRepo.DeleteRelationWithAudit(ctx, relation.ID, ingestionActor, "inventory.relationship.deleted"); err != nil {
		t.Fatalf("delete after lock release: %v", err)
	}
}

func mustCreateIngestionHost(t *testing.T, repo *mysql.ResourceRepository, name, displayName string) *model.Resource {
	t.Helper()
	resource, err := repo.CreateResource(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     displayName,
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return resource
}

func resourceIDByName(t *testing.T, db *sql.DB, name string) uint64 {
	t.Helper()
	var id uint64
	err := db.QueryRow(`select id from resources where name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	if err != nil {
		t.Fatalf("find resource %s: %v", name, err)
	}
	return id
}

func relationCount(t *testing.T, db *sql.DB, fromID, toID uint64, relationType model.RelationType) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`select count(*) from resource_relations where from_resource_id = ? and to_resource_id = ? and relation_type = ?`, fromID, toID, relationType).Scan(&count); err != nil {
		t.Fatalf("count relation: %v", err)
	}
	return count
}

func ingestionAuditCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`select count(*) from audit_events where event_type = 'inventory.ingestion.confirmed'`).Scan(&count); err != nil {
		t.Fatalf("count ingestion audits: %v", err)
	}
	return count
}

func assertObservedValue(t *testing.T, db *sql.DB, resourceID uint64, field, source string) {
	t.Helper()
	var gotSource string
	if err := db.QueryRow(`select source from resource_observed_values where resource_id = ? and field_name = ?`, resourceID, field).Scan(&gotSource); err != nil {
		t.Fatalf("read observed value for %d/%s: %v", resourceID, field, err)
	}
	if gotSource != source {
		t.Fatalf("observed source = %q, want %q", gotSource, source)
	}
}

func TestIngestionConfirmationServiceDelegatesToRepository(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	rows := []service.IngestionRow{{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: "ingest-service-delegate", DisplayName: "Service Delegate"}}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	svc := service.NewResourceService(repo)
	if _, err := svc.ConfirmIngestion(ctx, ingestionActor, rows, preview.Fingerprint); err != nil {
		t.Fatalf("service confirm: %v", err)
	}
	if id := resourceIDByName(t, db, "ingest-service-delegate"); id == 0 {
		t.Fatal("service-created resource missing")
	}
}
