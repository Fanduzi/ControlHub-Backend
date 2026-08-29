//go:build integration

// Package integration tests atomic bulk resource confirmation against MySQL.
// input: context, database/sql, encoding/json, errors, testing, time, internal/model, internal/repository/mysql, internal/service
// output: TestConfirmBulkResourceMutationMySQL
// pos: Real-MySQL proof for stable previews, externalId audit, multi-target rollback, and archived/concurrent lock rejection
// note: if this file changes, update this header and README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestConfirmBulkResourceMutationMySQL(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)

	t.Run("success", func(t *testing.T) {
		created, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-confirm-success"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"displayName": "bulk confirmed"},
			Labels:     service.LabelOperations{Add: map[string]string{"tier": "critical"}},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *created)
		if _, err := repo.ConfirmBulkResourceMutation(ctx, request, fingerprint, ownerDBA); err != nil {
			t.Fatalf("confirm bulk mutation: %v", err)
		}
		got, err := repo.GetResource(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DisplayName != "bulk confirmed" || got.Labels["tier"] != "critical" {
			t.Fatalf("resource after confirm = %#v", got)
		}
		var raw string
		if err := db.QueryRowContext(ctx, `select changes from audit_events where target_resource_id = ? and event_type = 'inventory.resource.bulk_updated'`, created.ID).Scan(&raw); err != nil {
			t.Fatalf("read bulk audit: %v", err)
		}
		var changes []model.AuditChange
		if err := json.Unmarshal([]byte(raw), &changes); err != nil || len(changes) != 2 {
			t.Fatalf("bulk audit changes = %#v, err = %v", changes, err)
		}
	})

	t.Run("idempotent preview", func(t *testing.T) {
		created, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-preview-idempotent"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"displayName": "preview only"},
		}
		first, err := repo.PreviewBulkResourceMutation(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		second, err := repo.PreviewBulkResourceMutation(ctx, request)
		if err != nil || !first.Confirmable || first.Fingerprint != second.Fingerprint {
			t.Fatalf("previews = %#v / %#v, err = %v", first, second, err)
		}
		got, err := repo.GetResource(created.ID)
		if err != nil || got.DisplayName != created.DisplayName {
			t.Fatalf("resource after previews = %#v, err = %v", got, err)
		}
	})

	t.Run("externalId commits with field audit", func(t *testing.T) {
		input := inventoryAuditResource("bulk-external-id")
		input.ExternalID = "bulk-external-before"
		created, err := repo.CreateResource(ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"externalId": "bulk-external-after"},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *created)
		if _, err := repo.ConfirmBulkResourceMutation(ctx, request, fingerprint, ownerDBA); err != nil {
			t.Fatalf("confirm externalId mutation: %v", err)
		}
		got, err := repo.GetResource(created.ID)
		if err != nil || got.ExternalID != "bulk-external-after" {
			t.Fatalf("resource externalId = %q, err = %v", got.ExternalID, err)
		}
		var raw string
		if err := db.QueryRowContext(ctx, `select changes from audit_events where target_resource_id = ? and event_type = 'inventory.resource.bulk_updated' order by id desc limit 1`, created.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var changes []struct {
			Field     string                             `json:"field"`
			Operation model.AuditChangeOperation         `json:"operation"`
			Before    []model.ResourceExternalIdentifier `json:"before"`
			After     []model.ResourceExternalIdentifier `json:"after"`
		}
		if err := json.Unmarshal([]byte(raw), &changes); err != nil || len(changes) != 1 ||
			changes[0].Field != "identity.externalIdentifiers" || changes[0].Operation != model.AuditChangeUpdate ||
			len(changes[0].Before) != 1 || changes[0].Before[0].Value != "bulk-external-before" ||
			len(changes[0].After) != 1 || changes[0].After[0].Value != "bulk-external-after" {
			t.Fatalf("externalId audit changes = %#v, err = %v", changes, err)
		}
	})

	t.Run("second target write failure rolls back whole batch", func(t *testing.T) {
		first, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-mid-batch-first"))
		if err != nil {
			t.Fatal(err)
		}
		second, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-mid-batch-second"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets: []service.BulkResourceMutationTarget{
				{ResourceID: first.ID, ExpectedVersion: first.UpdatedAt.UTC().Format(time.RFC3339Nano)},
				{ResourceID: second.ID, ExpectedVersion: second.UpdatedAt.UTC().Format(time.RFC3339Nano)},
			},
			FieldPatch: map[string]any{"name": "bulk-mid-batch-conflict"},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *first, *second)
		if _, err := repo.ConfirmBulkResourceMutation(ctx, request, fingerprint, ownerDBA); !errors.Is(err, service.ErrResourceNameConflict) {
			t.Fatalf("confirm error = %v, want name conflict", err)
		}
		gotFirst, err := repo.GetResource(first.ID)
		if err != nil {
			t.Fatal(err)
		}
		gotSecond, err := repo.GetResource(second.ID)
		if err != nil {
			t.Fatal(err)
		}
		if gotFirst.Name != first.Name || gotSecond.Name != second.Name {
			t.Fatalf("resources after rollback = %q / %q", gotFirst.Name, gotSecond.Name)
		}
		var audits int
		if err := db.QueryRowContext(ctx, `select count(*) from audit_events where event_type = 'inventory.resource.bulk_updated' and target_resource_id in (?, ?)`, first.ID, second.ID).Scan(&audits); err != nil || audits != 0 {
			t.Fatalf("bulk audits after rollback = %d, err = %v", audits, err)
		}
	})

	t.Run("audit failure rolls back", func(t *testing.T) {
		created, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-confirm-rollback"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"displayName": "must roll back"},
			Labels:     service.LabelOperations{Add: map[string]string{"rollback": "must-not-persist"}},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *created)
		const trigger = "bulk_confirm_fail_audit"
		if _, err := db.ExecContext(ctx, "drop trigger if exists "+trigger); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, "create trigger "+trigger+" before insert on audit_events for each row signal sqlstate '45000' set message_text = 'injected audit failure'"); err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = db.ExecContext(context.Background(), "drop trigger if exists "+trigger) }()
		if _, err := repo.ConfirmBulkResourceMutation(ctx, request, fingerprint, ownerDBA); err == nil {
			t.Fatal("expected injected audit failure")
		}
		got, err := repo.GetResource(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DisplayName != created.DisplayName {
			t.Fatalf("display name after rollback = %q, want %q", got.DisplayName, created.DisplayName)
		}
		if _, exists := got.Labels["rollback"]; exists {
			t.Fatalf("labels after rollback = %#v", got.Labels)
		}
	})

	t.Run("conflict requires repreview", func(t *testing.T) {
		created, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-confirm-drift"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"displayName": "reviewed value"},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *created)
		drifted := "concurrent value"
		if _, err := repo.UpdateResource(ctx, created.ID, model.ResourceUpdateInput{DisplayName: &drifted}); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ConfirmBulkResourceMutation(ctx, request, fingerprint, ownerDBA); err == nil {
			t.Fatal("expected drift rejection")
		}
		got, err := repo.GetResource(created.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.DisplayName != drifted {
			t.Fatalf("display name after rejected confirm = %q, want %q", got.DisplayName, drifted)
		}
	})

	t.Run("archived resource is rejected under lock", func(t *testing.T) {
		created, err := repo.CreateResource(ctx, inventoryAuditResource("bulk-confirm-archived"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"displayName": "must not commit"},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *created)
		archived, err := repo.ArchiveResource(ctx, created.ID, "integration lock validation")
		if err != nil {
			t.Fatal(err)
		}
		if !archived.UpdatedAt.Equal(created.UpdatedAt) {
			t.Fatalf("archive changed reviewed version: %s != %s", archived.UpdatedAt, created.UpdatedAt)
		}

		preview, err := repo.ConfirmBulkResourceMutation(ctx, request, fingerprint, ownerDBA)
		if !errors.Is(err, service.ErrBulkResourceMutationConflict) || len(preview.Items) != 1 ||
			preview.Items[0].Conflict || len(preview.Items[0].Errors) == 0 {
			t.Fatalf("confirm preview = %#v, err = %v", preview, err)
		}
		got, err := repo.GetResource(created.ID)
		if err != nil || got.ArchivedAt == nil || got.DisplayName != created.DisplayName {
			t.Fatalf("archived resource after rejected confirm = %#v, err = %v", got, err)
		}
		var audits int
		if err := db.QueryRowContext(ctx, `select count(*) from audit_events where target_resource_id = ? and event_type = 'inventory.resource.bulk_updated'`, created.ID).Scan(&audits); err != nil || audits != 0 {
			t.Fatalf("bulk audits after archived rejection = %d, err = %v", audits, err)
		}
	})

	t.Run("concurrent two-connection drift waits for lock", func(t *testing.T) {
		writerDB := setupTestDB(t)
		confirmerDB := setupTestDB(t)
		observerDB := setupTestDB(t)
		confirmer := mysql.NewResourceRepository(confirmerDB)
		created, err := confirmer.CreateResource(ctx, inventoryAuditResource("bulk-confirm-lock-drift"))
		if err != nil {
			t.Fatal(err)
		}
		request := service.BulkResourceMutationRequest{
			Targets:    []service.BulkResourceMutationTarget{{ResourceID: created.ID, ExpectedVersion: created.UpdatedAt.UTC().Format(time.RFC3339Nano)}},
			FieldPatch: map[string]any{"displayName": "reviewed value"},
		}
		fingerprint := bulkReviewedFingerprint(t, request, *created)

		writerTx, err := writerDB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer writerTx.Rollback()
		const drifted = "uncommitted concurrent value"
		if _, err := writerTx.ExecContext(ctx, `update resources set display_name = ?, updated_at = updated_at + interval 1 second where id = ?`, drifted, created.ID); err != nil {
			t.Fatal(err)
		}

		confirmCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := confirmer.ConfirmBulkResourceMutation(confirmCtx, request, fingerprint, ownerDBA)
			result <- err
		}()
		waitForMySQLDataLockWait(t, observerDB)
		if err := writerTx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := <-result; !errors.Is(err, service.ErrBulkResourceMutationConflict) {
			t.Fatalf("confirm error = %v, want drift conflict", err)
		}
		got, err := confirmer.GetResource(created.ID)
		if err != nil || got.DisplayName != drifted {
			t.Fatalf("resource after concurrent drift = %#v, err = %v", got, err)
		}
	})
}

func bulkReviewedFingerprint(t *testing.T, request service.BulkResourceMutationRequest, resources ...model.Resource) string {
	t.Helper()
	snapshots := make([]service.ResourceMutationSnapshot, 0, len(resources))
	for _, resource := range resources {
		snapshots = append(snapshots, service.ResourceMutationSnapshot{
			ID: resource.ID, Version: resource.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Fields: map[string]any{
				"name": resource.Name, "resourceSubtype": resource.ResourceSubtype,
				"displayName": resource.DisplayName, "environmentId": resource.EnvironmentID,
				"ownerId": resource.OwnerID, "lifecycleStatus": resource.LifecycleStatus,
				"healthStatus": resource.HealthStatus, "externalId": resource.ExternalID,
			},
			Labels: resource.Labels,
		})
	}
	preview, err := service.PreviewBulkResourceMutation(request, snapshots)
	if err != nil {
		t.Fatal(err)
	}
	return preview.Fingerprint
}

func waitForMySQLDataLockWait(t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var waits int
		if err := db.QueryRowContext(context.Background(), `select count(*) from performance_schema.data_lock_waits`).Scan(&waits); err != nil {
			t.Fatal(err)
		}
		if waits != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("confirm did not wait on the concurrent resource lock")
}
