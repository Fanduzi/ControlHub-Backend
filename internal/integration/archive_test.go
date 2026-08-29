//go:build integration

// Package integration provides real-MySQL coverage for archive service writes.
// input: context, testing, time, internal/model, internal/repository/mysql, internal/service
// output: TestServiceArchive_RejectsUpdateAfterArchive, TestServiceUnarchive_AllowsUpdateAfterUnarchive
// pos: Proves archive/unarchive service writes against real MySQL
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestArchiveResource_SetsArchivedFields(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "archive-target",
		DisplayName:     "Archive Target",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	archived, err := repo.ArchiveResource(ctx, created.ID, "decommissioned")
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	if archived.ArchivedAt == nil {
		t.Fatal("expected archivedAt to be set")
	}
	if archived.ArchiveReason == nil || *archived.ArchiveReason != "decommissioned" {
		t.Fatalf("expected archiveReason 'decommissioned', got %v", archived.ArchiveReason)
	}

	// Re-fetch to confirm persistence.
	fetched, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get after archive: %v", err)
	}
	if fetched.ArchivedAt == nil {
		t.Fatal("expected persisted archivedAt")
	}
	if fetched.ArchiveReason == nil || *fetched.ArchiveReason != "decommissioned" {
		t.Fatalf("expected persisted archiveReason, got %v", fetched.ArchiveReason)
	}
}

func TestArchiveResource_Idempotent(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "idempotent-archive",
		DisplayName:     "Idempotent Archive",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	first, err := repo.ArchiveResource(ctx, created.ID, "retired")
	if err != nil {
		t.Fatalf("first archive: %v", err)
	}
	firstAt := *first.ArchivedAt

	// Small sleep to ensure NOW(6) would differ if re-written.
	time.Sleep(10 * time.Millisecond)

	// Second archive — repo uses WHERE archived_at IS NULL, so it won't re-update.
	_, err = repo.ArchiveResource(ctx, created.ID, "retired again")
	if err != nil {
		t.Fatalf("second archive: %v", err)
	}

	second, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get after second archive: %v", err)
	}

	// archivedAt must not change (idempotent at repo level).
	if !second.ArchivedAt.Equal(firstAt) {
		t.Fatalf("archivedAt changed on second archive: first=%v, second=%v", firstAt, second.ArchivedAt)
	}
	// archiveReason must be original (repo skipped the UPDATE).
	if *second.ArchiveReason != "retired" {
		t.Fatalf("archiveReason changed: got %q, want %q", *second.ArchiveReason, "retired")
	}
}

func TestListResources_ExcludesArchived(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	// Create two resources — archive one.
	r1, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "list-active",
		DisplayName:     "List Active",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create r1: %v", err)
	}

	_, err = repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "list-to-archive",
		DisplayName:     "List To Archive",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create r2: %v", err)
	}

	_, _ = repo.ArchiveResource(ctx, r1.ID, "cleanup")

	// Default list should not include r1.
	items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes: []string{string(model.ResourceTypeHost)},
		Page:          1,
		PageSize:      100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, item := range items {
		if item.ID == r1.ID {
			t.Fatal("archived resource appeared in default list")
		}
	}
}

func TestListResources_IncludeArchived(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	r1, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "inc-arch-target",
		DisplayName:     "Inc Arch Target",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, _ = repo.ArchiveResource(ctx, r1.ID, "retired")

	items, total, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes:   []string{string(model.ResourceTypeHost)},
		IncludeArchived: true,
		Page:            1,
		PageSize:        100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	found := false
	for _, item := range items {
		if item.ID == r1.ID {
			found = true
			if item.ArchivedAt == nil {
				t.Fatal("expected archivedAt on archived resource")
			}
		}
	}
	if !found {
		t.Fatalf("archived resource not found in includeArchived list (total=%d)", total)
	}
}

func TestServiceArchive_RejectsUpdateAfterArchive(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	ctx := context.Background()

	created, err := svc.Create(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "svc-archive-target",
		DisplayName:     "Svc Archive Target",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
		Profile:         map[string]any{"hostname": "svc-archive-target.internal", "ipAddress": "10.0.0.21"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svc.Archive(ctx, created.ID, model.ArchiveRequest{})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Attempt update — should be rejected.
	newName := "Should Not Work"
	_, err = svc.Update(ctx, created.ID, model.ResourcePatchRequest{DisplayName: &newName})
	if err == nil {
		t.Fatal("expected error when updating archived resource")
	}
	if err.Error() != "resource archived" {
		t.Fatalf("expected 'resource archived', got %q", err.Error())
	}
}

func TestServiceArchive_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))

	_, err := svc.Archive(context.Background(), 999999999, model.ArchiveRequest{})
	if err == nil {
		t.Fatal("expected error for nonexistent resource")
	}
	if err.Error() != "resource not found" {
		t.Fatalf("expected 'resource not found', got %q", err.Error())
	}
}

// --- Phase 12.2: Unarchive + ArchivedOnly ---

func TestUnarchiveResource_ClearsArchivedFields(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "unarchive-target",
		DisplayName:     "Unarchive Target",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = repo.ArchiveResource(ctx, created.ID, "temporary")
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	unarchived, err := repo.UnarchiveResource(ctx, created.ID)
	if err != nil {
		t.Fatalf("unarchive: %v", err)
	}

	if unarchived.ArchivedAt != nil {
		t.Fatal("expected archivedAt to be nil after unarchive")
	}
	if unarchived.ArchiveReason != nil {
		t.Fatal("expected archiveReason to be nil after unarchive")
	}
	if unarchived.ArchivedBy != nil {
		t.Fatal("expected archivedBy to be nil after unarchive")
	}

	// Re-fetch to confirm persistence.
	fetched, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get after unarchive: %v", err)
	}
	if fetched.ArchivedAt != nil {
		t.Fatal("expected persisted archivedAt to be nil")
	}
}

func TestUnarchiveResource_IdempotentForActive(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "unarchive-active",
		DisplayName:     "Unarchive Active",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Unarchive on an already-active resource should succeed (repo WHERE is no-op).
	unarchived, err := repo.UnarchiveResource(ctx, created.ID)
	if err != nil {
		t.Fatalf("unarchive active: %v", err)
	}
	if unarchived.ArchivedAt != nil {
		t.Fatal("expected archivedAt to remain nil for active resource")
	}
}

func TestUnarchivedResource_ReappearsInDefaultList(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "reappear-host",
		DisplayName:     "Reappear Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, _ = repo.ArchiveResource(ctx, created.ID, "temp")

	// Should NOT appear in default list.
	items, _, _ := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes: []string{string(model.ResourceTypeHost)},
		Page:          1,
		PageSize:      100,
	})
	for _, item := range items {
		if item.ID == created.ID {
			t.Fatal("archived resource appeared in default list before unarchive")
		}
	}

	_, _ = repo.UnarchiveResource(ctx, created.ID)

	// Should reappear after unarchive.
	items2, _, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes: []string{string(model.ResourceTypeHost)},
		Page:          1,
		PageSize:      100,
	})
	if err != nil {
		t.Fatalf("list after unarchive: %v", err)
	}
	found := false
	for _, item := range items2 {
		if item.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("unarchived resource not found in default list")
	}
}

func TestListResources_ArchivedOnly(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	r1, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "archonly-archived",
		DisplayName:     "ArchOnly Archived",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create r1: %v", err)
	}

	_, err = repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "archonly-active",
		DisplayName:     "ArchOnly Active",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create r2: %v", err)
	}

	_, _ = repo.ArchiveResource(ctx, r1.ID, "cleanup")

	items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes: []string{string(model.ResourceTypeHost)},
		ArchivedOnly:  true,
		Page:          1,
		PageSize:      100,
	})
	if err != nil {
		t.Fatalf("list archivedOnly: %v", err)
	}

	foundR1 := false
	for _, item := range items {
		if item.ID == r1.ID {
			foundR1 = true
		}
		if item.Name == "archonly-active" {
			t.Fatal("active resource appeared in archivedOnly list")
		}
	}
	if !foundR1 {
		t.Fatal("archived resource r1 not found in archivedOnly list")
	}
}

func TestListResources_ArchivedOnlyTakesPrecedenceOverIncludeArchived(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	r1, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "prec-archived",
		DisplayName:     "Prec Archived",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create r1: %v", err)
	}

	_, err = repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "prec-active",
		DisplayName:     "Prec Active",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create r2: %v", err)
	}

	_, _ = repo.ArchiveResource(ctx, r1.ID, "done")

	// Both ArchivedOnly=true and IncludeArchived=true set — ArchivedOnly wins.
	items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes:   []string{string(model.ResourceTypeHost)},
		ArchivedOnly:    true,
		IncludeArchived: true,
		Page:            1,
		PageSize:        100,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	// r1 must be present; active resource must be absent.
	foundR1 := false
	for _, item := range items {
		if item.ID == r1.ID {
			foundR1 = true
		}
		if item.Name == "prec-active" {
			t.Fatal("active resource appeared in archivedOnly list")
		}
	}
	if !foundR1 {
		t.Fatal("archived resource r1 not found in archivedOnly list")
	}
}

func TestServiceUnarchive_AllowsUpdateAfterUnarchive(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	ctx := context.Background()

	created, err := svc.Create(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "svc-unarchive-target",
		DisplayName:     "Svc Unarchive Target",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
		Profile:         map[string]any{"hostname": "svc-unarchive-target.internal", "ipAddress": "10.0.0.22"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = svc.Archive(ctx, created.ID, model.ArchiveRequest{})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	_, err = svc.Unarchive(ctx, created.ID)
	if err != nil {
		t.Fatalf("unarchive: %v", err)
	}

	// Update should now succeed.
	newName := "Restored Name"
	updated, err := svc.Update(ctx, created.ID, model.ResourcePatchRequest{DisplayName: &newName})
	if err != nil {
		t.Fatalf("update after unarchive: %v", err)
	}
	if updated.DisplayName != "Restored Name" {
		t.Fatalf("display name = %q, want %q", updated.DisplayName, "Restored Name")
	}
}
