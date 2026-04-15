//go:build integration

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
		ResourceType: string(model.ResourceTypeHost),
		Page:         1,
		PageSize:     100,
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
		ResourceType:    string(model.ResourceTypeHost),
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
	svc := service.NewResourceService(repo)
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
	svc := service.NewResourceService(repo)

	_, err := svc.Archive(context.Background(), "nonexistent-id", model.ArchiveRequest{})
	if err == nil {
		t.Fatal("expected error for nonexistent resource")
	}
	if err.Error() != "resource not found" {
		t.Fatalf("expected 'resource not found', got %q", err.Error())
	}
}
