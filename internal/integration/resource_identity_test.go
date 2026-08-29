//go:build integration

// Package integration provides real-MySQL coverage for CMDB resource identity.
// input: context, errors, testing, internal/model, internal/repository/mysql, internal/service
// output: TestResourceIdentityRepository_CreateNormalizesAndEnforcesIdentity
// pos: Proves normalized identity persistence and explicit conflict classification against MySQL
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestResourceIdentityRepository_CreateNormalizesAndEnforcesIdentity(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()

	first, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: "api",
		Name:            "shared-ci-name",
		DisplayName:     "Identity Service",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginImported,
		Aliases:         []string{" Orders-API ", "orders-api"},
		ExternalIdentifiers: []model.ResourceExternalIdentifier{
			{System: "servicenow", Value: "CI-000076"},
		},
		Labels: map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource identity: %v", err)
	}
	if first.Origin != model.ResourceOriginImported {
		t.Fatalf("origin = %q, want imported", first.Origin)
	}
	if len(first.Aliases) != 1 || first.Aliases[0] != "orders-api" {
		t.Fatalf("aliases = %#v, want normalized unique alias", first.Aliases)
	}

	sameNameOtherType, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm", Name: "shared-ci-name",
		DisplayName: "Same name host", EnvironmentID: envProd, OwnerID: ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy,
		Origin: model.ResourceOriginManual, Labels: map[string]string{},
	})
	if err != nil || sameNameOtherType.ID == 0 {
		t.Fatalf("same name in same environment for another CI type should succeed: %v", err)
	}
	_, err = repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType: model.ResourceTypeService, ResourceSubtype: "api", Name: "shared-ci-name",
		DisplayName: "Duplicate service", EnvironmentID: envProd, OwnerID: ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy,
		Origin: model.ResourceOriginManual, Labels: map[string]string{},
	})
	if !errors.Is(err, service.ErrResourceNameConflict) {
		t.Fatalf("duplicate scoped name error = %v, want ErrResourceNameConflict", err)
	}

	_, err = repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "alias-conflict-host",
		DisplayName:     "Same name, different CI type",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Aliases:         []string{"ORDERS-API"},
		Labels:          map[string]string{},
	})
	if !errors.Is(err, service.ErrResourceAliasConflict) {
		t.Fatalf("duplicate normalized alias error = %v, want ErrResourceAliasConflict", err)
	}

	_, err = repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "external-id-conflict",
		DisplayName:     "External ID conflict",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginDiscovered,
		ExternalIdentifiers: []model.ResourceExternalIdentifier{
			{System: "servicenow", Value: "CI-000076"},
		},
		Labels: map[string]string{},
	})
	if !errors.Is(err, service.ErrResourceExternalIdentifierConflict) {
		t.Fatalf("duplicate external identifier error = %v, want ErrResourceExternalIdentifierConflict", err)
	}
}

func TestResourceIdentityRepository_UpdateAndAuditAreAtomic(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	resource, err := repo.CreateResource(ctx, identityResourceInput("identity-atomic", []string{"old-alias"}, "CI-ATOMIC"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	_, err = repo.CreateResource(ctx, identityResourceInput("identity-blocker", []string{"reserved-alias"}, "CI-BLOCKER"))
	if err != nil {
		t.Fatalf("create blocker: %v", err)
	}

	displayName := "Must roll back"
	conflictingAliases := []string{"reserved-alias"}
	_, err = repo.UpdateResourceWithAudit(ctx, resource.ID, model.ResourceUpdateInput{
		DisplayName: &displayName, Aliases: &conflictingAliases,
	}, ownerDBA, "inventory.resource.updated")
	if !errors.Is(err, service.ErrResourceAliasConflict) {
		t.Fatalf("conflicting update error = %v", err)
	}
	unchanged, err := repo.GetResource(resource.ID)
	if err != nil {
		t.Fatalf("get resource after rollback: %v", err)
	}
	if unchanged.DisplayName == displayName || len(unchanged.Aliases) != 1 || unchanged.Aliases[0] != "old-alias" {
		t.Fatalf("identity update was not rolled back: %#v", unchanged)
	}

	aliases := []string{"new-alias"}
	identifiers := []model.ResourceExternalIdentifier{{System: "servicenow", Value: "CI-ATOMIC-NEXT"}}
	updated, err := repo.UpdateResourceWithAudit(ctx, resource.ID, model.ResourceUpdateInput{
		Aliases: &aliases, ExternalIdentifiers: &identifiers,
	}, ownerDBA, "inventory.resource.updated")
	if err != nil {
		t.Fatalf("update identity with audit: %v", err)
	}
	if len(updated.Aliases) != 1 || updated.Aliases[0] != "new-alias" || len(updated.ExternalIdentifiers) != 1 {
		t.Fatalf("updated identity = %#v", updated)
	}
	var raw string
	if err := db.QueryRowContext(ctx, `select changes from audit_events where target_resource_id = ? order by id desc limit 1`, resource.ID).Scan(&raw); err != nil {
		t.Fatalf("read identity audit: %v", err)
	}
	var changes []model.AuditChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatalf("decode identity audit: %v", err)
	}
	if len(changes) != 2 || changes[0].Field != "identity.aliases" || changes[1].Field != "identity.externalIdentifiers" {
		t.Fatalf("identity audit changes = %#v", changes)
	}
}

func TestResourceIdentitySchema_RejectsInternalIDAndOriginChanges(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	resource, err := repo.CreateResource(context.Background(), identityResourceInput("identity-immutable", nil, "CI-IMMUTABLE"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	if _, err := db.Exec(`update resources set origin = 'imported' where id = ?`, resource.ID); err == nil {
		t.Fatal("expected origin update to fail")
	}
	if _, err := db.Exec(`update resources set id = id + 1000000 where id = ?`, resource.ID); err == nil {
		t.Fatal("expected internal id update to fail")
	}
}

func identityResourceInput(name string, aliases []string, externalValue string) model.ResourceCreateInput {
	return model.ResourceCreateInput{
		ResourceType: model.ResourceTypeService, ResourceSubtype: "api", Name: name, DisplayName: name,
		EnvironmentID: envProd, OwnerID: ownerDBA, LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus: model.HealthStatusHealthy, Origin: model.ResourceOriginManual, Aliases: aliases,
		ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: "servicenow", Value: externalValue}}, Labels: map[string]string{},
	}
}
