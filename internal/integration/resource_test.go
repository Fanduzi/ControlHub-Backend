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

// Seed data ClickHouse cluster:
//   analytics-ch-cluster-prod  (database_cluster, clickhouse, healthy, running)
//     ├── analytics-ch-node-01-prod (database_instance, clickhouse, healthy, replica)
//     └── analytics-ch-node-02-prod (database_instance, clickhouse, critical, replica)

// Subtypes present in seed data (migration 0004):
//   mysql, clickhouse, postgresql, redis, vm, physical, api, nginx, haproxy, envoy

// Seed reference IDs from migration 0002.
const (
	envProd    uint64 = 1
	envStaging uint64 = 2
	ownerDBA   uint64 = 2
)

func TestResourceRepositoryCreate_UsesAutoIncrementID(t *testing.T) {
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
	if created.ID == 0 {
		t.Fatal("expected auto-increment ID after create")
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

func TestResourceRepository_FilterByResourceSubtype(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	items, total, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceSubtypes: []string{"mysql"},
		EnvironmentIDs:   []uint64{envProd},
		Page:             1,
		PageSize:         100,
	})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if total == 0 {
		t.Fatal("expected at least one mysql resource in prod seed data")
	}
	for _, item := range items {
		if item.ResourceSubtype != "mysql" {
			t.Errorf("got subtype %q, want mysql", item.ResourceSubtype)
		}
		if item.EnvironmentID != envProd {
			t.Errorf("got env %d, want %d", item.EnvironmentID, envProd)
		}
	}
}

func TestResourceRepository_FilterByMultiResourceSubtype(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	items, total, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceSubtypes: []string{"mysql", "clickhouse"},
		Page:             1,
		PageSize:         100,
	})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if total == 0 {
		t.Fatal("expected at least one mysql or clickhouse resource in seed data")
	}
	for _, item := range items {
		if item.ResourceSubtype != "mysql" && item.ResourceSubtype != "clickhouse" {
			t.Errorf("got subtype %q, want mysql or clickhouse", item.ResourceSubtype)
		}
	}
}

func TestResourceRepository_ResourceSubtypeWithResourceType(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
		ResourceTypes:    []string{string(model.ResourceTypeDatabaseInstance)},
		ResourceSubtypes: []string{"mysql"},
		Page:             1,
		PageSize:         100,
	})
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	for _, item := range items {
		if item.ResourceType != model.ResourceTypeDatabaseInstance {
			t.Errorf("got type %q, want database_instance", item.ResourceType)
		}
		if item.ResourceSubtype != "mysql" {
			t.Errorf("got subtype %q, want mysql", item.ResourceSubtype)
		}
	}
}


func TestResourceRepository_DatabaseClusterOperationalSummary(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	t.Run("GetResource returns rollup for database_cluster", func(t *testing.T) {
		items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
			ResourceTypes:    []string{string(model.ResourceTypeDatabaseCluster)},
			ResourceSubtypes: []string{"clickhouse"},
			EnvironmentIDs:   []uint64{envProd},
			Page:             1,
			PageSize:         100,
		})
		if err != nil {
			t.Fatalf("list clusters: %v", err)
		}
		if len(items) == 0 {
			t.Fatal("expected at least one clickhouse cluster in seed data")
		}

		var clusterID uint64
		for _, item := range items {
			if item.Name == "analytics-ch-cluster-prod" {
				clusterID = item.ID
				break
			}
		}
		if clusterID == 0 {
			t.Fatal("analytics-ch-cluster-prod not found")
		}

		detail, err := repo.GetResource(clusterID)
		if err != nil {
			t.Fatalf("get resource: %v", err)
		}

		summary := detail.DatabaseOperationalSummary
		if summary == nil {
			t.Fatal("expected DatabaseOperationalSummary for database_cluster resource, got nil")
		}
		if summary.MemberCount != 2 {
			t.Errorf("MemberCount = %d, want 2", summary.MemberCount)
		}
		if summary.CriticalMemberCount != 1 {
			t.Errorf("CriticalMemberCount = %d, want 1", summary.CriticalMemberCount)
		}
		if summary.ReplicaMemberCount != 2 {
			t.Errorf("ReplicaMemberCount = %d, want 2", summary.ReplicaMemberCount)
		}
		if summary.PrimaryMemberCount != 0 {
			t.Errorf("PrimaryMemberCount = %d, want 0", summary.PrimaryMemberCount)
		}
		if summary.WorstMemberStatus != "critical" {
			t.Errorf("WorstMemberStatus = %q, want %q", summary.WorstMemberStatus, "critical")
		}
		if summary.WorstMemberName != "Analytics ClickHouse Node 02" {
			t.Errorf("WorstMemberName = %q, want %q", summary.WorstMemberName, "Analytics ClickHouse Node 02")
		}
	})

	t.Run("ListResources includes rollup for database_clusters", func(t *testing.T) {
		items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
			ResourceTypes:    []string{string(model.ResourceTypeDatabaseCluster)},
			ResourceSubtypes: []string{"clickhouse"},
			EnvironmentIDs:   []uint64{envProd},
			Page:             1,
			PageSize:         100,
		})
		if err != nil {
			t.Fatalf("list clusters: %v", err)
		}

		var found bool
		var summary *model.DatabaseOperationalSummary
		for _, item := range items {
			if item.Name == "analytics-ch-cluster-prod" {
				found = true
				summary = item.DatabaseOperationalSummary
				break
			}
		}
		if !found {
			t.Fatal("analytics-ch-cluster-prod not found in list")
		}

		if summary == nil {
			t.Fatal("expected DatabaseOperationalSummary in list response, got nil")
		}
		if summary.MemberCount != 2 {
			t.Errorf("MemberCount = %d, want 2", summary.MemberCount)
		}
		if summary.CriticalMemberCount != 1 {
			t.Errorf("CriticalMemberCount = %d, want 1", summary.CriticalMemberCount)
		}
	})

	t.Run("non-cluster resources have no rollup", func(t *testing.T) {
		items, _, err := repo.ListResources(ctx, model.ResourceListQuery{
			ResourceTypes: []string{string(model.ResourceTypeHost)},
			Page:          1,
			PageSize:      5,
		})
		if err != nil {
			t.Fatalf("list hosts: %v", err)
		}
		for _, item := range items {
			if item.DatabaseOperationalSummary != nil {
				t.Errorf("host %q should not have DatabaseOperationalSummary", item.Name)
			}
		}
	})
}
