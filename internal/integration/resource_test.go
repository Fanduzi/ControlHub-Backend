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

// Seed reference IDs from migration 0002.
const (
	envProd    = "10000000-0000-0000-0000-000000000001"
	envStaging = "10000000-0000-0000-0000-000000000002"
	ownerDBA   = "20000000-0000-0000-0000-000000000002"
)

func TestResourceRepository_CreateAndGet(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	input := model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "test-host-001",
		DisplayName:     "Test Host 001",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		ExternalID:      "",
		Labels:          map[string]string{"team": "test"},
	}

	created, err := repo.CreateResource(ctx, input)
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty ID after create")
	}
	if created.Name != "test-host-001" {
		t.Fatalf("name = %q, want %q", created.Name, "test-host-001")
	}

	// Fetch by ID.
	fetched, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if fetched.Name != "test-host-001" {
		t.Fatalf("fetched name = %q, want %q", fetched.Name, "test-host-001")
	}
	if fetched.Labels["team"] != "test" {
		t.Fatalf("fetched labels[team] = %q, want %q", fetched.Labels["team"], "test")
	}
}

func TestResourceRepository_PatchMutableFields(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	input := model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: "api",
		Name:            "patch-svc-001",
		DisplayName:     "Original Name",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	}

	created, err := repo.CreateResource(ctx, input)
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	newDisplay := "Updated Name"
	newStatus := model.LifecycleStatusDegraded
	updated, err := repo.UpdateResource(ctx, created.ID, model.ResourceUpdateInput{
		DisplayName:     &newDisplay,
		LifecycleStatus: &newStatus,
	})
	if err != nil {
		t.Fatalf("update resource: %v", err)
	}
	if updated.DisplayName != "Updated Name" {
		t.Fatalf("display name = %q, want %q", updated.DisplayName, "Updated Name")
	}
	if updated.LifecycleStatus != string(model.LifecycleStatusDegraded) {
		t.Fatalf("lifecycle status = %q, want %q", updated.LifecycleStatus, model.LifecycleStatusDegraded)
	}
	// Immutable fields should be unchanged.
	if updated.Name != "patch-svc-001" {
		t.Fatalf("name changed unexpectedly: %q", updated.Name)
	}
}

func TestResourceRepository_DuplicateNameSameEnv_Conflict(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	input := model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "dup-host-same-env",
		DisplayName:     "First",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	}

	_, err := repo.CreateResource(ctx, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Second create with same name + same environment should conflict.
	input.DisplayName = "Second"
	_, err = repo.CreateResource(ctx, input)
	if !errors.Is(err, service.ErrResourceConflict) {
		t.Fatalf("second create: err = %v, want ErrResourceConflict", err)
	}
}

func TestResourceRepository_SameNameDifferentEnv_Succeeds(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	base := model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "shared-host-name",
		DisplayName:     "Host in Prod",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	}

	_, err := repo.CreateResource(ctx, base)
	if err != nil {
		t.Fatalf("create in prod: %v", err)
	}

	// Same name in staging should succeed.
	base.EnvironmentID = envStaging
	base.DisplayName = "Host in Staging"
	staging, err := repo.CreateResource(ctx, base)
	if err != nil {
		t.Fatalf("create in staging: %v", err)
	}
	if staging.Name != "shared-host-name" {
		t.Fatalf("staging name = %q, want %q", staging.Name, "shared-host-name")
	}
}

func TestResourceRepository_MySQL1062MapsToConflict(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	input := model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseCluster,
		ResourceSubtype: "mysql",
		Name:            "1062-test-cluster",
		DisplayName:     "Cluster One",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	}

	_, err := repo.CreateResource(ctx, input)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// This must trigger MySQL 1062 and the repository must map it.
	input.DisplayName = "Cluster Two"
	_, err = repo.CreateResource(ctx, input)
	if !errors.Is(err, service.ErrResourceConflict) {
		t.Fatalf("duplicate create err = %v (%T), want ErrResourceConflict", err, err)
	}
}
