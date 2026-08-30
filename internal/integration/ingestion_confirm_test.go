//go:build integration

// Package integration runs real-MySQL ingestion confirmation tests.
// input: database/sql, errors, testing, internal/model, internal/repository/mysql, internal/service
// output: TestIngestionConfirmation* and TestCollectorIngestionConfirmation* atomic persistence proofs
// pos: Proves User and collector confirmation semantics against disposable MySQL
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const ingestionActor uint64 = 1

const collectorPrincipal uint64 = 870001

func TestCollectorIngestionConfirmationCompletePersistsMachineOwnedScan(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	rows := []service.IngestionRow{{
		EnvironmentID: envProd,
		CIType:        model.ResourceTypeHost,
		Name:          "collector-complete-create",
		DisplayName:   "Collector Complete Create",
		ExternalIdentifiers: []model.ResourceExternalIdentifier{{
			System: "collector", Value: "complete-create",
		}},
	}}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := svc.ConfirmCollectorIngestion(ctx, collectorPrincipal, rows, preview.Fingerprint, service.CollectorIngestionMetadata{
		ScanID: "collector-complete-1", ScanResult: model.CollectorScanResultComplete,
	}); err != nil {
		t.Fatalf("confirm collector ingestion: %v", err)
	}

	resourceID := resourceIDByName(t, db, "collector-complete-create")
	resource, err := repo.GetResource(resourceID)
	if err != nil {
		t.Fatalf("get collector-created resource: %v", err)
	}
	if resource.OwnerID != 0 || resource.Origin != model.ResourceOriginDiscovered {
		t.Fatalf("collector-created owner/origin = %d/%q, want 0/%q", resource.OwnerID, resource.Origin, model.ResourceOriginDiscovered)
	}
	if resource.ArchivedAt != nil {
		t.Fatal("collector-created resource was archived")
	}

	var ledgerCount, omissions int
	var lastSeen string
	if err := db.QueryRowContext(ctx, `select count(*) from collector_scan_ledger
		where machine_principal_id = ? and collector_scan_id = ? and result = 'complete'`, collectorPrincipal, "collector-complete-1").Scan(&ledgerCount); err != nil {
		t.Fatalf("read collector ledger: %v", err)
	}
	if ledgerCount != 1 {
		t.Fatalf("collector ledger rows = %d, want 1", ledgerCount)
	}
	if err := db.QueryRowContext(ctx, `select consecutive_complete_scan_omissions, last_seen_collector_scan_id
		from collector_ci_scan_states where machine_principal_id = ? and resource_id = ?`, collectorPrincipal, resourceID).Scan(&omissions, &lastSeen); err != nil {
		t.Fatalf("read collector state: %v", err)
	}
	if omissions != 0 || lastSeen != "collector-complete-1" {
		t.Fatalf("collector state omissions/lastSeen = %d/%q", omissions, lastSeen)
	}

	var actorUserID, actorMachineID sql.NullInt64
	if err := db.QueryRowContext(ctx, `select actor_user_id, actor_machine_principal_id from audit_events
		where target_resource_id = ? and event_type = 'inventory.ingestion.confirmed'`, resourceID).Scan(&actorUserID, &actorMachineID); err != nil {
		t.Fatalf("read collector audit actor: %v", err)
	}
	if actorUserID.Valid || !actorMachineID.Valid || uint64(actorMachineID.Int64) != collectorPrincipal {
		t.Fatalf("collector audit actors user/machine = %v/%v", actorUserID, actorMachineID)
	}
}

func TestCollectorIngestionConfirmationExactRetryIsNoOp(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	rows := []service.IngestionRow{{
		EnvironmentID: envProd,
		CIType:        model.ResourceTypeHost,
		Name:          "collector-exact-retry",
		DisplayName:   "Collector Exact Retry",
	}}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	metadata := service.CollectorIngestionMetadata{ScanID: "collector-retry-1", ScanResult: model.CollectorScanResultComplete}
	if _, err := svc.ConfirmCollectorIngestion(ctx, collectorPrincipal, rows, preview.Fingerprint, metadata); err != nil {
		t.Fatalf("first confirm: %v", err)
	}
	resourceID := resourceIDByName(t, db, "collector-exact-retry")
	beforeLedger := tableRowCount(t, db, "collector_scan_ledger")
	beforeAudits := ingestionAuditCount(t, db)
	var beforeOmissions int
	var beforeLastSeen string
	if err := db.QueryRowContext(ctx, `select consecutive_complete_scan_omissions, last_seen_collector_scan_id
		from collector_ci_scan_states where machine_principal_id = ? and resource_id = ?`, collectorPrincipal, resourceID).Scan(&beforeOmissions, &beforeLastSeen); err != nil {
		t.Fatalf("read state before retry: %v", err)
	}

	if _, err := svc.ConfirmCollectorIngestion(ctx, collectorPrincipal, rows, preview.Fingerprint, metadata); err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	if got := tableRowCount(t, db, "collector_scan_ledger"); got != beforeLedger {
		t.Fatalf("ledger rows after retry = %d, want %d", got, beforeLedger)
	}
	if got := ingestionAuditCount(t, db); got != beforeAudits {
		t.Fatalf("audit rows after retry = %d, want %d", got, beforeAudits)
	}
	var afterOmissions int
	var afterLastSeen string
	if err := db.QueryRowContext(ctx, `select consecutive_complete_scan_omissions, last_seen_collector_scan_id
		from collector_ci_scan_states where machine_principal_id = ? and resource_id = ?`, collectorPrincipal, resourceID).Scan(&afterOmissions, &afterLastSeen); err != nil {
		t.Fatalf("read state after retry: %v", err)
	}
	if afterOmissions != beforeOmissions || afterLastSeen != beforeLastSeen {
		t.Fatalf("state after retry = %d/%q, want %d/%q", afterOmissions, afterLastSeen, beforeOmissions, beforeLastSeen)
	}
	changedRows := append([]service.IngestionRow(nil), rows...)
	changedRows[0].DisplayName = "Changed Retry Payload"
	if _, err := svc.ConfirmCollectorIngestion(ctx, collectorPrincipal, changedRows, preview.Fingerprint, metadata); !errors.Is(err, service.ErrCollectorScanConflict) {
		t.Fatalf("changed payload retry error = %v, want collector scan conflict", err)
	}
	conflicting := metadata
	conflicting.ScanResult = model.CollectorScanResultIncomplete
	if _, err := svc.ConfirmCollectorIngestion(ctx, collectorPrincipal, rows, preview.Fingerprint, conflicting); !errors.Is(err, service.ErrCollectorScanConflict) {
		t.Fatalf("conflicting retry error = %v, want collector scan conflict", err)
	}
	if _, err := svc.ConfirmCollectorIngestion(ctx, collectorPrincipal, rows, strings.Repeat("0", 64), metadata); !errors.Is(err, service.ErrCollectorScanConflict) {
		t.Fatalf("conflicting fingerprint error = %v, want collector scan conflict", err)
	}
}

func TestCollectorIngestionConfirmationConcurrentExactRetryIsNoOp(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	principalID := collectorPrincipal + 3
	rows := []service.IngestionRow{{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: "collector-concurrent-retry", DisplayName: "Collector Concurrent Retry"}}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	metadata := service.CollectorIngestionMetadata{ScanID: "collector-concurrent-retry-1", ScanResult: model.CollectorScanResultComplete}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.ConfirmCollectorIngestion(ctx, principalID, rows, preview.Fingerprint, metadata)
			results <- err
		}()
	}
	close(start)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent exact retry %d: %v", attempt, err)
		}
	}

	resourceID := resourceIDByName(t, db, rows[0].Name)
	var ledgerCount, auditCount, stateCount int
	if err := db.QueryRow(`select count(*) from collector_scan_ledger where machine_principal_id = ? and collector_scan_id = ?`, principalID, metadata.ScanID).Scan(&ledgerCount); err != nil {
		t.Fatalf("count concurrent ledger: %v", err)
	}
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ? and event_type = 'inventory.ingestion.confirmed'`, resourceID).Scan(&auditCount); err != nil {
		t.Fatalf("count concurrent audit: %v", err)
	}
	if err := db.QueryRow(`select count(*) from collector_ci_scan_states where machine_principal_id = ? and resource_id = ?`, principalID, resourceID).Scan(&stateCount); err != nil {
		t.Fatalf("count concurrent state: %v", err)
	}
	if ledgerCount != 1 || auditCount != 1 || stateCount != 1 {
		t.Fatalf("concurrent ledger/audit/state counts = %d/%d/%d, want 1/1/1", ledgerCount, auditCount, stateCount)
	}
}

func TestCollectorIngestionConfirmationMissingLifecycleNeverArchives(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	principalID := collectorPrincipal + 1
	missingRow := service.IngestionRow{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: "collector-missing-lifecycle", DisplayName: "Collector Missing Lifecycle"}
	presentRow := service.IngestionRow{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: "collector-present-lifecycle", DisplayName: "Collector Present Lifecycle"}

	confirmCollectorScan(t, ctx, repo, svc, principalID, "lifecycle-initial", model.CollectorScanResultComplete, []service.IngestionRow{missingRow, presentRow})
	missingID := resourceIDByName(t, db, missingRow.Name)
	confirmCollectorScan(t, ctx, repo, svc, principalID, "lifecycle-incomplete", model.CollectorScanResultIncomplete, []service.IngestionRow{presentRow})
	assertCollectorState(t, db, principalID, missingID, 0, false, "lifecycle-initial")
	confirmCollectorScan(t, ctx, repo, svc, principalID, "lifecycle-failed", model.CollectorScanResultFailed, []service.IngestionRow{presentRow})
	assertCollectorState(t, db, principalID, missingID, 0, false, "lifecycle-initial")

	for omission := 1; omission <= 3; omission++ {
		scanID := fmt.Sprintf("lifecycle-complete-omission-%d", omission)
		confirmCollectorScan(t, ctx, repo, svc, principalID, scanID, model.CollectorScanResultComplete, []service.IngestionRow{presentRow})
		assertCollectorState(t, db, principalID, missingID, omission, omission == 3, "lifecycle-initial")
	}
	resource, err := repo.GetResource(missingID)
	if err != nil {
		t.Fatalf("get missing resource: %v", err)
	}
	if resource.ArchivedAt != nil {
		t.Fatal("Missing resource was automatically archived")
	}

	confirmCollectorScan(t, ctx, repo, svc, principalID, "lifecycle-recovered", model.CollectorScanResultComplete, []service.IngestionRow{missingRow, presentRow})
	assertCollectorState(t, db, principalID, missingID, 0, false, "lifecycle-recovered")
	if got := resourceIDByName(t, db, missingRow.Name); got != missingID {
		t.Fatalf("recovered resource ID = %d, want stable ID %d", got, missingID)
	}
	resource, err = repo.GetResource(missingID)
	if err != nil || resource.ArchivedAt != nil {
		t.Fatalf("recovered resource archived/deleted: resource=%+v err=%v", resource, err)
	}
}

func TestCollectorIngestionConfirmationFingerprintAndLedgerFailureRollback(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	principalID := collectorPrincipal + 2
	existing := mustCreateIngestionHost(t, repo, "collector-rollback-existing", "Before Collector Rollback")
	rows := []service.IngestionRow{
		{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: existing.Name, DisplayName: "After Collector Rollback"},
		{EnvironmentID: envProd, CIType: model.ResourceTypeHost, Name: "collector-rollback-created", DisplayName: "Collector Rollback Created"},
	}
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	metadata := service.CollectorIngestionMetadata{ScanID: "collector-rollback-1", ScanResult: model.CollectorScanResultComplete}
	if _, err := svc.ConfirmCollectorIngestion(ctx, principalID, rows, strings.Repeat("0", 64), metadata); !errors.Is(err, service.ErrIngestionFingerprintMismatch) {
		t.Fatalf("unverified fingerprint error = %v, want fingerprint mismatch", err)
	}
	if got, _ := repo.GetResource(existing.ID); got.DisplayName != existing.DisplayName {
		t.Fatalf("unverified fingerprint updated display name to %q", got.DisplayName)
	}
	if got := resourceIDByName(t, db, rows[1].Name); got != 0 {
		t.Fatalf("unverified fingerprint persisted resource %d", got)
	}

	beforeLedger := tableRowCount(t, db, "collector_scan_ledger")
	beforeAudits := ingestionAuditCount(t, db)
	if _, err := db.Exec(`create trigger issue87_force_collector_ledger_fail
		before insert on collector_scan_ledger for each row
		signal sqlstate '45000' set message_text = 'forced collector ledger failure'`); err != nil {
		t.Fatalf("create ledger failure trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists issue87_force_collector_ledger_fail`) })
	if _, err := svc.ConfirmCollectorIngestion(ctx, principalID, rows, preview.Fingerprint, metadata); err == nil {
		t.Fatal("expected forced collector ledger failure")
	}
	if got, _ := repo.GetResource(existing.ID); got.DisplayName != existing.DisplayName {
		t.Fatalf("ledger failure persisted display name %q, want %q", got.DisplayName, existing.DisplayName)
	}
	if got := resourceIDByName(t, db, rows[1].Name); got != 0 {
		t.Fatalf("ledger failure persisted resource %d", got)
	}
	if got := tableRowCount(t, db, "collector_scan_ledger"); got != beforeLedger {
		t.Fatalf("ledger rows after rollback = %d, want %d", got, beforeLedger)
	}
	if got := ingestionAuditCount(t, db); got != beforeAudits {
		t.Fatalf("machine audit rows after rollback = %d, want %d", got, beforeAudits)
	}
}

func confirmCollectorScan(t *testing.T, ctx context.Context, repo *mysql.ResourceRepository, svc *service.ResourceService, principalID uint64, scanID string, result model.CollectorScanResult, rows []service.IngestionRow) {
	t.Helper()
	preview, err := repo.PreviewIngestion(ctx, rows)
	if err != nil {
		t.Fatalf("preview %s: %v", scanID, err)
	}
	if _, err := svc.ConfirmCollectorIngestion(ctx, principalID, rows, preview.Fingerprint, service.CollectorIngestionMetadata{ScanID: scanID, ScanResult: result}); err != nil {
		t.Fatalf("confirm %s: %v", scanID, err)
	}
}

func assertCollectorState(t *testing.T, db *sql.DB, principalID, resourceID uint64, wantOmissions int, wantMissing bool, wantLastSeen string) {
	t.Helper()
	var omissions int
	var lastSeen string
	var missingSince sql.NullTime
	if err := db.QueryRow(`select consecutive_complete_scan_omissions, last_seen_collector_scan_id, missing_since
		from collector_ci_scan_states where machine_principal_id = ? and resource_id = ?`, principalID, resourceID).Scan(&omissions, &lastSeen, &missingSince); err != nil {
		t.Fatalf("read collector state: %v", err)
	}
	if omissions != wantOmissions || missingSince.Valid != wantMissing || lastSeen != wantLastSeen {
		t.Fatalf("collector state omissions/missing/lastSeen = %d/%t/%q, want %d/%t/%q", omissions, missingSince.Valid, lastSeen, wantOmissions, wantMissing, wantLastSeen)
	}
}

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
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	if _, err := svc.ConfirmIngestion(ctx, ingestionActor, rows, preview.Fingerprint); err != nil {
		t.Fatalf("service confirm: %v", err)
	}
	if id := resourceIDByName(t, db, "ingest-service-delegate"); id == 0 {
		t.Fatal("service-created resource missing")
	}
}
