//go:build integration

package integration

import (
	"context"
	"slices"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestQueryTargetRepository_DerivesConnectionContextFromSeed verifies the
// read-model JOIN against real seed data: every target is a database_instance
// with resolved environment and owner names, and a known seed anchor resolves
// engine/port/cluster.
func TestQueryTargetRepository_DerivesConnectionContextFromSeed(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	targets, err := repo.ListQueryTargets(ctx, model.QueryTargetListQuery{})
	if err != nil {
		t.Fatalf("list query targets: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected seed query targets, got none")
	}

	// WHY: a blank environment or owner name would mean the JOIN is broken and
	// the workbench would render an empty connection card.
	for _, target := range targets {
		if target.ResourceType != model.ResourceTypeDatabaseInstance {
			t.Errorf("target %d resourceType = %s, want database_instance", target.ResourceID, target.ResourceType)
		}
		if target.ConnectionContext.Environment == "" {
			t.Errorf("target %d has empty environment name (join broken)", target.ResourceID)
		}
		if target.ConnectionContext.Owner == "" {
			t.Errorf("target %d has empty owner name (join broken)", target.ResourceID)
		}
	}

	// Known seed anchor: user-redis-primary-prod -> engine redis, port 6379,
	// member_of user-redis-cluster-prod.
	redis := findSeedTargetByName(t, targets, "user-redis-primary-prod")
	if redis.ConnectionContext.Engine != "redis" {
		t.Fatalf("engine = %q, want redis", redis.ConnectionContext.Engine)
	}
	if redis.ConnectionContext.Port != 6379 {
		t.Fatalf("port = %d, want 6379", redis.ConnectionContext.Port)
	}
	if redis.ConnectionContext.ClusterName == "" {
		t.Fatal("expected resolved cluster name for member instance")
	}
}

// TestQueryTargetRepository_EngineFilter verifies the server-side engine filter.
func TestQueryTargetRepository_EngineFilter(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	targets, err := repo.ListQueryTargets(ctx, model.QueryTargetListQuery{Engine: "redis"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected redis targets from seed")
	}
	for _, target := range targets {
		if target.ConnectionContext.Engine != "redis" {
			t.Errorf("engine filter leaked non-redis target %q", target.ConnectionContext.Engine)
		}
	}
}

// TestQueryTargetRepository_InstanceWithoutProfileIsMissingConnection
// exercises the LEFT JOIN: a database_instance with no profile row must still
// surface as a missing_connection target rather than disappearing.
func TestQueryTargetRepository_InstanceWithoutProfileIsMissingConnection(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	qtRepo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	created, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qt-missing-profile-prod",
		DisplayName:     "Query Target Missing Profile",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		ExternalID:      "",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	svc := service.NewQueryTargetService(qtRepo)
	targets, err := svc.List(ctx, model.QueryTargetListQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var target *model.QueryTarget
	for i := range targets {
		if targets[i].ResourceID == created.ID {
			target = &targets[i]
			break
		}
	}
	if target == nil {
		t.Fatalf("expected target for newly created instance %d", created.ID)
	}
	// WHY: no profile means host/port are absent — the target must report
	// missing_connection (a config gap), not unsupported_engine.
	if target.Readiness != model.ReadinessMissingConnection {
		t.Fatalf("readiness = %q, want missing_connection", target.Readiness)
	}
	if target.Governance.SafetyState != model.SafetyStateConnectionIncomplete {
		t.Fatalf("safetyState = %q, want connection_incomplete", target.Governance.SafetyState)
	}
	// WHY: the engine is still identifiable from resource_subtype, so the
	// target reports mysql capability and only the connection gaps (host/port)
	// — never an "engine" gap, and it must remain visible under ?engine=mysql.
	if target.ConnectionContext.Engine != "mysql" {
		t.Fatalf("engine = %q, want mysql (subtype fallback)", target.ConnectionContext.Engine)
	}
	if target.Capability.QueryKind != model.QueryKindSQL {
		t.Fatalf("queryKind = %q, want sql", target.Capability.QueryKind)
	}
	if slices.Contains(target.MissingFields, "engine") {
		t.Fatalf("missingFields must not contain engine when subtype identifies it, got %v", target.MissingFields)
	}
	if !slices.Contains(target.MissingFields, "host") || !slices.Contains(target.MissingFields, "port") {
		t.Fatalf("missingFields = %v, want host and port", target.MissingFields)
	}
}

// TestQueryTargetRepository_EngineFilterIncludesInstanceWithoutProfile
// verifies the engine filter matches a database_instance whose engine is
// identifiable only via resource_subtype (no profile row). Such targets must
// not disappear from the catalog when filtered by their engine.
func TestQueryTargetRepository_EngineFilterIncludesInstanceWithoutProfile(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	repo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	created, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qt-no-profile-filter-prod",
		DisplayName:     "Query Target No Profile Filter",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		ExternalID:      "",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	targets, err := repo.ListQueryTargets(ctx, model.QueryTargetListQuery{Engine: "mysql"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var found bool
	for _, target := range targets {
		if target.ResourceID == created.ID {
			found = true
			if target.ConnectionContext.Engine != "mysql" {
				t.Errorf("engine = %q, want mysql", target.ConnectionContext.Engine)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected no-profile mysql instance to be returned by engine=mysql filter")
	}
}

// TestQueryTargetService_CompleteSeedTargetIsCredentialRequired verifies that
// a fully-connected seed target derives to credential_required (no read-only
// credential metadata exists in Phase 36) with execution disabled.
func TestQueryTargetService_CompleteSeedTargetIsCredentialRequired(t *testing.T) {
	db := setupTestDB(t)
	svc := service.NewQueryTargetService(mysql.NewQueryTargetRepository(db))
	ctx := context.Background()

	targets, err := svc.List(ctx, model.QueryTargetListQuery{Engine: "redis"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	redis := findSeedTargetByName(t, targets, "user-redis-primary-prod")

	if redis.Readiness != model.ReadinessCredentialRequired {
		t.Fatalf("readiness = %q, want credential_required", redis.Readiness)
	}
	if redis.Capability.QueryKind != model.QueryKindRedis {
		t.Fatalf("queryKind = %q, want redis", redis.Capability.QueryKind)
	}
	if redis.Governance.ExecutionEnabled {
		t.Fatal("executionEnabled must be false — Phase 36 never enables execution")
	}
	if redis.AvailableActions.Run || redis.AvailableActions.Explain {
		t.Fatalf("availableActions must all be false, got %+v", redis.AvailableActions)
	}
}

func findSeedTargetByName(t *testing.T, targets []model.QueryTarget, name string) *model.QueryTarget {
	t.Helper()
	for i := range targets {
		if targets[i].ResourceName == name {
			return &targets[i]
		}
	}
	t.Fatalf("expected seed target %q in %d targets", name, len(targets))
	return nil
}
