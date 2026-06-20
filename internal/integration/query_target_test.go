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

// NOTE on isolation: TestOpenAPIFuzz (in this same package) exercises write
// endpoints freely against the shared disposable database and mutates seed
// data — including deleting/updating profiles and relations. Therefore
// query-target integration tests must NOT assert on fuzz-mutable seed state
// (a specific instance's profile host/port, cluster relation, etc.). They
// either assert only robust join invariants (environment/owner name
// resolution, which the fuzz cannot break), or create their own fixtures.

// TestQueryTargetRepository_DerivesConnectionContextFromSeed verifies the
// read-model JOIN against real migrated data using only robust invariants:
// every target is a database_instance with a resolved environment and owner
// name. Profile host/port and cluster relations are intentionally not asserted
// here because the co-running fuzz mutates them.
func TestQueryTargetRepository_DerivesConnectionContextFromSeed(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	targets, err := repo.ListQueryTargets(ctx, model.QueryTargetListQuery{})
	if err != nil {
		t.Fatalf("list query targets: %v", err)
	}
	if len(targets) == 0 {
		t.Fatal("expected query targets, got none")
	}

	// WHY: a blank environment or owner name would mean the environments/owners
	// JOIN is broken. This holds even after the fuzz mutates resources because
	// resource writes always reference valid environment/owner ids.
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
}

// TestQueryTargetRepository_EngineFilter verifies the server-side engine filter
// returns only matching engines. Robust: the filter matches the resolved
// engine expression, so every returned target is engine=redis by construction,
// and the redis subtype is stable for seeded instances.
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

// TestQueryTargetService_CompleteConnectionIsCredentialRequired creates a
// database_instance WITH a complete profile (host/port) and asserts it derives
// to credential_required with execution disabled. Self-contained so it does not
// depend on fuzz-mutable seed profiles.
func TestQueryTargetService_CompleteConnectionIsCredentialRequired(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	qtRepo := mysql.NewQueryTargetRepository(db)
	svc := service.NewQueryTargetService(qtRepo)
	ctx := context.Background()

	created, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qt-complete-profile-prod",
		DisplayName:     "Query Target Complete Profile",
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
	if err := resourceRepo.UpsertDatabaseInstanceProfile(ctx, created.ID, "mysql", "8.0.36", "qt-complete-host.internal", 3306, "primary"); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	targets, err := svc.List(ctx, model.QueryTargetListQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	target := findTargetByID(t, targets, created.ID)

	// WHY: complete connection metadata still has no read-only credential in
	// Phase 36, so the target must be credential_required — never ready.
	if target.Readiness != model.ReadinessCredentialRequired {
		t.Fatalf("readiness = %q, want credential_required", target.Readiness)
	}
	if target.Capability.QueryKind != model.QueryKindSQL {
		t.Fatalf("queryKind = %q, want sql", target.Capability.QueryKind)
	}
	if target.Governance.ExecutionEnabled {
		t.Fatal("executionEnabled must be false — Phase 36 never enables execution")
	}
	if target.AvailableActions.Run || target.AvailableActions.Explain {
		t.Fatalf("availableActions must all be false, got %+v", target.AvailableActions)
	}
	if !slices.Contains(target.MissingFields, "readonlyCredential") {
		t.Fatalf("missingFields = %v, want readonlyCredential", target.MissingFields)
	}
	if target.ConnectionContext.Engine != "mysql" || target.ConnectionContext.Host == "" || target.ConnectionContext.Port != 3306 {
		t.Fatalf("connection context not resolved from profile: %+v", target.ConnectionContext)
	}
}

// TestQueryTargetRepository_InstanceWithoutProfileIsMissingConnection
// exercises the engine subtype fallback + LEFT JOIN: a database_instance with
// no profile row must still report its subtype engine and surface as
// missing_connection (host/port gap) rather than disappearing.
func TestQueryTargetRepository_InstanceWithoutProfileIsMissingConnection(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	qtRepo := mysql.NewQueryTargetRepository(db)
	svc := service.NewQueryTargetService(qtRepo)
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

	targets, err := svc.List(ctx, model.QueryTargetListQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	target := findTargetByID(t, targets, created.ID)

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
	// — never an "engine" gap.
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

	target := findTargetByID(t, targets, created.ID)
	if target.ConnectionContext.Engine != "mysql" {
		t.Fatalf("engine = %q, want mysql", target.ConnectionContext.Engine)
	}
}

func findTargetByID(t *testing.T, targets []model.QueryTarget, id uint64) *model.QueryTarget {
	t.Helper()
	for i := range targets {
		if targets[i].ResourceID == id {
			return &targets[i]
		}
	}
	t.Fatalf("expected query target id %d in %d targets", id, len(targets))
	return nil
}
