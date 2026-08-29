//go:build integration

// Package integration tests atomic bulk resource confirmation against MySQL.
// input: context, encoding/json, testing, time, internal/model, internal/repository/mysql, internal/service
// output: TestConfirmBulkResourceMutationMySQL
// pos: Real-MySQL proof for bulk mutation commit, rollback, and reviewed-preview drift rejection
// note: if this file changes, update this header and README.md.
package integration

import (
	"context"
	"encoding/json"
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

	t.Run("drift requires repreview", func(t *testing.T) {
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
}

func bulkReviewedFingerprint(t *testing.T, request service.BulkResourceMutationRequest, resource model.Resource) string {
	t.Helper()
	preview, err := service.PreviewBulkResourceMutation(request, []service.ResourceMutationSnapshot{{
		ID: resource.ID, Version: resource.UpdatedAt.UTC().Format(time.RFC3339Nano),
		Fields: map[string]any{
			"name": resource.Name, "resourceSubtype": resource.ResourceSubtype,
			"displayName": resource.DisplayName, "environmentId": resource.EnvironmentID,
			"ownerId": resource.OwnerID, "lifecycleStatus": resource.LifecycleStatus,
			"healthStatus": resource.HealthStatus, "externalId": resource.ExternalID,
		},
		Labels: resource.Labels,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return preview.Fingerprint
}
