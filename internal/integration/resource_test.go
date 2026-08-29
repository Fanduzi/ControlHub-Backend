//go:build integration

// Package integration provides real-MySQL coverage for repository, API, and
// migration behavior against disposable Testcontainers databases.
// input: database/sql, testing, time, internal/model, internal/repository/mysql, internal/service
// output: TestResource* and TestResourceService* integration cases, including observation-derived cluster rollups
// pos: Proves resource CRUD, filtering, effective-health rollups, and create-with-profile atomicity against real MySQL
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
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

func TestResourceRepository_ObservationDerivedClusterOperationalSummary(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	var clusterID, memberID uint64
	if err := db.QueryRow(`select id from resources where name = 'analytics-ch-cluster-prod'`).Scan(&clusterID); err != nil {
		t.Fatalf("find cluster: %v", err)
	}
	if err := db.QueryRow(`select id from resources where name = 'analytics-ch-node-01-prod'`).Scan(&memberID); err != nil {
		t.Fatalf("find member: %v", err)
	}
	if _, err := db.Exec(`update resources set health_status = null where id = ?`, memberID); err != nil {
		t.Fatalf("clear manual override: %v", err)
	}
	if err := repo.UpsertHealthObservation(ctx, memberID, model.HealthObservation{
		Status:     model.HealthStatusCritical,
		ObservedAt: time.Now(),
		Observer:   "cluster-rollup-test",
	}); err != nil {
		t.Fatalf("record observation: %v", err)
	}

	cluster, err := repo.GetResource(clusterID)
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	summary := cluster.DatabaseOperationalSummary
	if summary == nil {
		t.Fatal("expected database operational summary")
	}
	if summary.CriticalMemberCount != 2 {
		t.Fatalf("CriticalMemberCount = %d, want 2", summary.CriticalMemberCount)
	}
	if summary.WorstMemberID == nil || *summary.WorstMemberID != int64(memberID) {
		t.Fatalf("WorstMemberID = %v, want %d", summary.WorstMemberID, memberID)
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

// TestResourceServiceCreate_ProfileWriteFailureLeavesNoResource pins the
// create-with-profile atomicity contract against real MySQL: the resource row
// is inserted, the profile write fails (hostname exceeds varchar(255)), and
// the create must neither report success nor leave a resource with a lost
// profile behind.
func TestResourceServiceCreate_ProfileWriteFailureLeavesNoResource(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo)
	ctx := context.Background()

	name := fmt.Sprintf("atomicity-fail-%d", time.Now().UnixNano())
	input := model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "Atomicity Fail Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Profile: map[string]any{
			"hostname":  strings.Repeat("h", 300), // exceeds resource_profiles_host.hostname varchar(255)
			"ipAddress": "10.0.0.1",
			"osName":    "Ubuntu 22.04",
		},
	}

	created, err := svc.Create(ctx, input)
	var ve *service.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected validation error when the initial profile write fails; got %v (created id=%v)", err, created)
	}
	if ve.Fields["hostname"] == "" {
		t.Fatalf("expected field-level detail for hostname, got %#v", ve.Fields)
	}

	var resourceRows int
	if err := db.QueryRowContext(ctx, "select count(*) from resources where name = ?", name).Scan(&resourceRows); err != nil {
		t.Fatalf("count resources: %v", err)
	}
	if resourceRows != 0 {
		t.Fatalf("profile write failure must not leave a resource row behind, found %d", resourceRows)
	}

	var profileRows int
	if err := db.QueryRowContext(ctx,
		"select count(*) from resource_profiles_host where resource_id in (select id from resources where name = ?)", name,
	).Scan(&profileRows); err != nil {
		t.Fatalf("count host profiles: %v", err)
	}
	if profileRows != 0 {
		t.Fatalf("profile write failure must not leave a profile row behind, found %d", profileRows)
	}
}

func TestResourceRepositoryCreateWithProfile_PersistsResourceAndProfile(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	name := fmt.Sprintf("atomicity-ok-%d", time.Now().UnixNano())
	created, err := repo.CreateResourceWithProfile(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "Atomicity Ok Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Labels:          map[string]string{},
	}, map[string]any{
		"hostname":  name,
		"ipAddress": "10.0.0.9",
		"osName":    "Ubuntu 22.04",
	})
	if err != nil {
		t.Fatalf("create resource with profile: %v", err)
	}

	var profileHostname string
	if err := db.QueryRowContext(ctx,
		"select hostname from resource_profiles_host where resource_id = ?", created.ID,
	).Scan(&profileHostname); err != nil {
		t.Fatalf("fetch host profile: %v", err)
	}
	if profileHostname != name {
		t.Fatalf("hostname = %q, want %q", profileHostname, name)
	}
}

// TestResourceRepositoryCreateWithProfile_FailureRollsBack proves at the
// storage layer that a failed initial-profile write rolls the resource insert
// back: no resource row and no profile row may survive.
func TestResourceRepositoryCreateWithProfile_FailureRollsBack(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	name := fmt.Sprintf("atomicity-repo-fail-%d", time.Now().UnixNano())
	_, err := repo.CreateResourceWithProfile(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "Atomicity Repo Fail",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Labels:          map[string]string{},
	}, map[string]any{
		"hostname":  strings.Repeat("h", 300), // exceeds varchar(255) → MySQL 1406
		"ipAddress": "10.0.0.1",
		"osName":    "Ubuntu 22.04",
	})
	if err == nil {
		t.Fatal("expected error when the profile write fails")
	}

	var resourceRows int
	if err := db.QueryRowContext(ctx, "select count(*) from resources where name = ?", name).Scan(&resourceRows); err != nil {
		t.Fatalf("count resources: %v", err)
	}
	if resourceRows != 0 {
		t.Fatalf("profile write failure must not leave a resource row behind, found %d", resourceRows)
	}

	var profileRows int
	if err := db.QueryRowContext(ctx,
		"select count(*) from resource_profiles_host where resource_id in (select id from resources where name = ?)", name,
	).Scan(&profileRows); err != nil {
		t.Fatalf("count host profiles: %v", err)
	}
	if profileRows != 0 {
		t.Fatalf("profile write failure must not leave a profile row behind, found %d", profileRows)
	}
}

// TestResourceAPI_CreateWithProfileFailureRollsBack drives the real HTTP
// handler against real MySQL: when the initial profile write fails at the
// storage layer, the API must not report success and no resource row may be
// left behind.
func TestResourceAPI_CreateWithProfileFailureRollsBack(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	profileService := service.NewProfileService(resourceRepo, resourceRepo)
	relationRepo := mysql.NewRelationRepository(db)
	dictRepo := mysql.NewDictionaryRepository(db)
	qtRepo := mysql.NewQueryTargetRepository(db)
	router := api.NewRouter(api.Dependencies{
		ResourceService:        service.NewResourceService(resourceRepo),
		ProfileService:         profileService,
		RelationService:        service.NewRelationService(relationRepo),
		TopologyService:        service.NewTopologyService(relationRepo),
		AuditService:           service.NewAuditService(mysql.NewAuditRepository(db)),
		AuthService:            service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret),
		EnvironmentService:     service.NewEnvironmentService(dictRepo),
		OwnerService:           service.NewOwnerService(dictRepo),
		RoleService:            service.NewRoleService(dictRepo),
		ResourceTypeService:    service.NewResourceTypeService(dictRepo),
		RelationTypeService:    service.NewRelationTypeService(dictRepo),
		LifecycleStatusService: service.NewLifecycleStatusService(dictRepo),
		HealthStatusService:    service.NewHealthStatusService(dictRepo),
		ResourceSubtypeService: service.NewResourceSubtypeService(),
		QueryTargetService:     service.NewQueryTargetService(qtRepo),
		QueryExecutionService:  &boundaryExecStub{},
		QueryExplainService:    &boundaryExplainStub{},
		QuerySchemaService:     &boundarySchemaStub{},
		QueryCredentialService: &authzCredStub{},
		QueryDisclosureService: &boundaryDisclosureStub{},
		QuerySavedStatementService: service.NewQuerySavedStatementService(
			mysql.NewQuerySavedStatementRepository(db),
			mysql.NewQuerySavedStatementRepository(db),
			qtRepo,
			service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		),
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
	})

	email := fmt.Sprintf("atomicity-api-%d@example.com", time.Now().UnixNano())
	userID := insertAuthzTestUser(t, db, email, "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })
	token := mustLogin(t, router, email, "secret123")

	name := fmt.Sprintf("atomicity-api-fail-%d", time.Now().UnixNano())
	body := fmt.Sprintf(`{
		"resourceType":"host",
		"resourceSubtype":"vm",
		"name":%q,
		"displayName":"Atomicity API Fail",
		"environmentId":%d,
		"ownerId":%d,
		"lifecycleStatus":"provisioning",
		"healthStatus":"unknown",
		"source":"manual",
		"profile":{"hostname":%q,"ipAddress":"10.0.0.1","osName":"Ubuntu 22.04"}
	}`, name, envProd, ownerDBA, strings.Repeat("h", 300))

	rec := doBearerWithBody(t, router, http.MethodPost, "/resources", token, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected controlled 400 validation error when the profile write fails, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("expected validation_failed error code, got body=%s", rec.Body.String())
	}

	var resourceRows int
	if err := db.QueryRowContext(context.Background(),
		"select count(*) from resources where name = ?", name,
	).Scan(&resourceRows); err != nil {
		t.Fatalf("count resources: %v", err)
	}
	if resourceRows != 0 {
		t.Fatalf("profile write failure must not leave a resource row behind, found %d", resourceRows)
	}
}

// TestResourceRepositoryCreateWithProfile_EmptyProfileObjectWritesEmptyRow
// pins the submitted-empty semantics on a supported type: an explicit empty
// profile object persists an empty typed profile row, matching the PUT
// /resources/{id}/profile empty-body upsert behavior.
func TestResourceRepositoryCreateWithProfile_EmptyProfileObjectWritesEmptyRow(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	name := fmt.Sprintf("atomicity-empty-host-%d", time.Now().UnixNano())
	created, err := repo.CreateResourceWithProfile(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "Atomicity Empty Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Labels:          map[string]string{},
	}, map[string]any{})
	if err != nil {
		t.Fatalf("create host with empty profile object: %v", err)
	}

	var hostname string
	if err := db.QueryRowContext(ctx,
		"select hostname from resource_profiles_host where resource_id = ?", created.ID,
	).Scan(&hostname); err != nil {
		t.Fatalf("fetch host profile: %v", err)
	}
	if hostname != "" {
		t.Fatalf("expected empty hostname in profile row, got %q", hostname)
	}
}

// TestProfileServicePatch_PreservesOmittedFieldsOnMySQL proves the PATCH
// partial-merge contract against real MySQL: a partial update must not clear
// fields that were not submitted.
func TestProfileServicePatch_PreservesOmittedFieldsOnMySQL(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	profileSvc := service.NewProfileService(repo, repo)
	ctx := context.Background()

	name := fmt.Sprintf("patch-merge-%d", time.Now().UnixNano())
	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "Patch Merge Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if err := repo.UpsertHostProfile(ctx, created.ID, "original-host", "10.0.0.7", "Ubuntu 22.04"); err != nil {
		t.Fatalf("seed host profile: %v", err)
	}

	if err := profileSvc.PatchProfile(ctx, created.ID, map[string]any{"hostname": "patched-host"}); err != nil {
		t.Fatalf("patch profile: %v", err)
	}

	profile, err := repo.GetResourceProfile(created.ID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if profile.Profile["hostname"] != "patched-host" {
		t.Fatalf("expected patched hostname, got %#v", profile.Profile)
	}
	if profile.Profile["ipAddress"] != "10.0.0.7" {
		t.Fatalf("expected omitted ipAddress preserved as 10.0.0.7, got %#v", profile.Profile)
	}
	if profile.Profile["osName"] != "Ubuntu 22.04" {
		t.Fatalf("expected omitted osName preserved as Ubuntu 22.04, got %#v", profile.Profile)
	}
}

// TestProfileServicePutProfile_OverlongRejectedOnMySQL proves the length
// validation contract against real MySQL: an overlong field is rejected with
// a controlled validation error before any database write (never a 500).
func TestProfileServicePutProfile_OverlongRejectedOnMySQL(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	profileSvc := service.NewProfileService(repo, repo)
	ctx := context.Background()

	name := fmt.Sprintf("overlong-%d", time.Now().UnixNano())
	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "Overlong Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	err = profileSvc.PutProfile(ctx, created.ID, map[string]any{
		"hostname": strings.Repeat("h", 256),
	})
	var ve *service.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for overlong hostname, got %v", err)
	}
	if ve.Fields["hostname"] == "" {
		t.Fatalf("expected field-level detail for hostname, got %#v", ve.Fields)
	}
}

// TestResourceRepositoryPatchProfile_Int64PortPersisted pins the int64 port
// conversion at the repository seam: a validated int64 port must be written
// as its value, never coerced to zero.
func TestResourceRepositoryPatchProfile_Int64PortPersisted(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	name := fmt.Sprintf("patch-int64-port-%d", time.Now().UnixNano())
	created, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            name,
		DisplayName:     "Patch Int64 Port",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusProvisioning,
		HealthStatus:    model.HealthStatusUnknown,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if err := repo.PatchProfile(ctx, created.ID, model.ResourceTypeDatabaseInstance, map[string]any{
		"port": int64(3306),
	}); err != nil {
		t.Fatalf("patch profile: %v", err)
	}

	var port int
	if err := db.QueryRowContext(ctx,
		"select port from resource_profiles_database_instance where resource_id = ?", created.ID,
	).Scan(&port); err != nil {
		t.Fatalf("fetch port: %v", err)
	}
	if port != 3306 {
		t.Fatalf("expected port 3306 persisted, got %d", port)
	}
}
