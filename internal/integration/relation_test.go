//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// createTestResources is a helper that creates two resources and returns their IDs.
func createTestResources(t *testing.T, repo *mysql.ResourceRepository, ctx context.Context, namePrefix string) (uint64, uint64) {
	t.Helper()

	resA, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            namePrefix + "-a",
		DisplayName:     namePrefix + " A",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource A: %v", err)
	}

	resB, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: "api",
		Name:            namePrefix + "-b",
		DisplayName:     namePrefix + " B",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource B: %v", err)
	}

	return resA.ID, resB.ID
}

func TestRelationRepository_CreateAndFetch(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	idA, idB := createTestResources(t, resRepo, ctx, "rel-create")

	rel, err := relRepo.CreateRelation(ctx, model.RelationCreateInput{
		FromResourceID: idA,
		ToResourceID:   idB,
		RelationType:   model.RelationTypeDependsOn,
	})
	if err != nil {
		t.Fatalf("create relation: %v", err)
	}
	if rel.ID == 0 {
		t.Fatal("expected auto-increment relation ID")
	}
	if rel.FromResourceID != idA || rel.ToResourceID != idB {
		t.Fatalf("relation ids mismatch: from=%d to=%d", rel.FromResourceID, rel.ToResourceID)
	}

	// Fetch by resource ID.
	rels, err := relRepo.ListByResourceID(idA)
	if err != nil {
		t.Fatalf("list relations: %v", err)
	}
	if len(rels) == 0 {
		t.Fatal("expected at least one relation for resource A")
	}
}

func TestRelationRepository_DuplicateRelation_Conflict(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	idA, idB := createTestResources(t, resRepo, ctx, "rel-dup")

	_, err := relRepo.CreateRelation(ctx, model.RelationCreateInput{
		FromResourceID: idA,
		ToResourceID:   idB,
		RelationType:   model.RelationTypeRunsOn,
	})
	if err != nil {
		t.Fatalf("first relation: %v", err)
	}

	// Same (from, to, type) should conflict.
	_, err = relRepo.CreateRelation(ctx, model.RelationCreateInput{
		FromResourceID: idA,
		ToResourceID:   idB,
		RelationType:   model.RelationTypeRunsOn,
	})
	if !errors.Is(err, service.ErrRelationConflict) {
		t.Fatalf("duplicate relation err = %v, want ErrRelationConflict", err)
	}
}

func TestRelationRepository_DeleteRelation(t *testing.T) {
	db := setupTestDB(t)
	resRepo := mysql.NewResourceRepository(db)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	idA, idB := createTestResources(t, resRepo, ctx, "rel-del")

	rel, err := relRepo.CreateRelation(ctx, model.RelationCreateInput{
		FromResourceID: idA,
		ToResourceID:   idB,
		RelationType:   model.RelationTypeManages,
	})
	if err != nil {
		t.Fatalf("create relation: %v", err)
	}

	err = relRepo.DeleteRelation(ctx, rel.ID)
	if err != nil {
		t.Fatalf("delete relation: %v", err)
	}

	// Verify it's gone.
	rels, err := relRepo.ListByResourceID(idA)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, r := range rels {
		if r.ID == rel.ID {
			t.Fatal("relation still exists after delete")
		}
	}
}

func TestRelationRepository_DeleteUnknown_NotFound(t *testing.T) {
	db := setupTestDB(t)
	relRepo := mysql.NewRelationRepository(db)
	ctx := context.Background()

	err := relRepo.DeleteRelation(ctx, 999999999999)
	if !errors.Is(err, service.ErrRelationNotFound) {
		t.Fatalf("delete unknown err = %v, want ErrRelationNotFound", err)
	}
}
