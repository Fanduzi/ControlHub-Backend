//go:build integration

// Package integration runs real-MySQL integration tests.
// input: database/sql, encoding/json, strings, testing, internal/model, internal/repository/mysql
// output: Inventory resource/profile/relationship audit atomicity and observed/manual/effective value persistence tests
// pos: Proves inventory changes, relationship rules, observations, overrides, and provenance against disposable MySQL
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestInventoryAuditUpdateSuccess(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	created, err := repo.CreateResource(ctx, inventoryAuditResource("inventory-audit-success"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	displayName := "Audited display name"
	labels := map[string]string{"team": "platform", "tier": "critical"}
	changes := []model.AuditChange{
		{Field: "identity.displayName", Operation: model.AuditChangeUpdate, Before: created.DisplayName, After: displayName},
		{Field: "labels.team", Operation: model.AuditChangeUpdate, Before: "operations", After: "platform"},
		{Field: "labels.tier", Operation: model.AuditChangeAdd, After: "critical"},
	}

	updated, err := repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{
		DisplayName: &displayName,
		Labels:      &labels,
	}, ownerDBA, "inventory.resource.updated")
	if err != nil {
		t.Fatalf("update resource with audit: %v", err)
	}
	if updated.DisplayName != displayName {
		t.Fatalf("display name = %q, want %q", updated.DisplayName, displayName)
	}

	var actorID, targetID uint64
	var eventType, result, rawChanges string
	err = db.QueryRowContext(ctx, `select actor_user_id, target_resource_id, event_type, result, changes
		from audit_events where target_resource_id = ? order by id desc limit 1`, created.ID).
		Scan(&actorID, &targetID, &eventType, &result, &rawChanges)
	if err != nil {
		t.Fatalf("read audit event: %v", err)
	}
	if actorID != ownerDBA || targetID != created.ID || eventType != "inventory.resource.updated" || result != "success" {
		t.Fatalf("audit identity = (%d, %d, %q, %q)", actorID, targetID, eventType, result)
	}
	var got []model.AuditChange
	if err := json.Unmarshal([]byte(rawChanges), &got); err != nil {
		t.Fatalf("decode audit changes: %v", err)
	}
	if len(got) != len(changes) {
		t.Fatalf("audit changes = %#v, want %#v", got, changes)
	}
	for i := range changes {
		if got[i].Field != changes[i].Field || got[i].Operation != changes[i].Operation || got[i].Before != changes[i].Before || got[i].After != changes[i].After {
			t.Fatalf("audit change %d = %#v, want %#v", i, got[i], changes[i])
		}
	}
}

func TestObservedValuesManualOverrideAndEffectiveProvenance(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	resource, err := repo.CreateResource(ctx, inventoryAuditResource("effective-values"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	if err := repo.PutObservedValues(ctx, resource.ID, "discovery", map[string]any{"displayName": "discovered"}); err != nil {
		t.Fatalf("write discovery observation: %v", err)
	}
	if err := repo.PutObservedValues(ctx, resource.ID, "terraform", map[string]any{"displayName": "terraform"}); err != nil {
		t.Fatalf("write terraform observation: %v", err)
	}
	if _, err := repo.SetManualOverrideWithAudit(ctx, resource.ID, "displayName", "operator", 0, ownerDBA); err != nil {
		t.Fatalf("set override: %v", err)
	}
	if _, err := repo.SetManualOverrideWithAudit(ctx, resource.ID, "displayName", "stale writer", 0, ownerDBA); !errors.Is(err, service.ErrResourceConflict) {
		t.Fatalf("stale override error = %v, want resource conflict", err)
	}
	if err := repo.PutObservedValues(ctx, resource.ID, "discovery", map[string]any{"displayName": "latest discovery"}); err != nil {
		t.Fatalf("refresh discovery observation: %v", err)
	}

	got, err := repo.GetEffectiveValues(ctx, resource.ID)
	if err != nil {
		t.Fatalf("get overridden effective value: %v", err)
	}
	if value := got["displayName"]; value.Value != "operator" || value.Provenance.Kind != "manual_override" {
		t.Fatalf("overridden effective value = %#v", value)
	}
	if err := repo.ClearManualOverrideWithAudit(ctx, resource.ID, "displayName", 1, ownerDBA); err != nil {
		t.Fatalf("clear override: %v", err)
	}
	got, err = repo.GetEffectiveValues(ctx, resource.ID)
	if err != nil {
		t.Fatalf("get observed effective value: %v", err)
	}
	if value := got["displayName"]; value.Value != "latest discovery" || value.Provenance.Kind != "observed" || value.Provenance.Source != "discovery" {
		t.Fatalf("cleared effective value = %#v", value)
	}

	var sources int
	if err := db.QueryRowContext(ctx, `select count(*) from resource_observed_values where resource_id = ? and field_name = 'displayName'`, resource.ID).Scan(&sources); err != nil {
		t.Fatalf("count observations: %v", err)
	}
	if sources != 2 {
		t.Fatalf("observation sources = %d, want 2", sources)
	}
}

func TestInventoryAuditUpdateRollsBackOnAuditFailure(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	created, err := repo.CreateResource(ctx, inventoryAuditResource("inventory-audit-rollback"))
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}

	displayName := "Must roll back"
	_, err = repo.UpdateResourceWithAudit(ctx, created.ID, model.ResourceUpdateInput{DisplayName: &displayName}, ownerDBA,
		strings.Repeat("x", 65))
	if err == nil {
		t.Fatal("expected audit insert failure")
	}

	got, err := repo.GetResource(created.ID)
	if err != nil {
		t.Fatalf("get resource after rollback: %v", err)
	}
	if got.DisplayName != created.DisplayName {
		t.Fatalf("display name = %q after audit failure, want rollback to %q", got.DisplayName, created.DisplayName)
	}
	var count int
	if err := db.QueryRowContext(ctx, `select count(*) from audit_events where target_resource_id = ?`, created.ID).Scan(&count); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if count != 0 {
		t.Fatalf("audit event count = %d, want 0", count)
	}
}

func TestInventoryAuditProfileAndRelationshipUseFieldDiffContract(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	resources := mysql.NewResourceRepository(db)
	from, err := resources.CreateResource(ctx, inventoryAuditResource("inventory-audit-relation-from"))
	if err != nil {
		t.Fatalf("create from resource: %v", err)
	}
	to, err := resources.CreateResource(ctx, inventoryAuditResource("inventory-audit-relation-to"))
	if err != nil {
		t.Fatalf("create to resource: %v", err)
	}

	err = resources.PutProfileWithAudit(ctx, from.ID, model.ResourceTypeService, map[string]any{
		"systemName": "orders", "repositoryUrl": "https://example.invalid/orders", "runtimeEnv": "go",
	}, ownerDBA, "inventory.profile.updated")
	if err != nil {
		t.Fatalf("put profile with audit: %v", err)
	}
	assertLatestAuditChange(t, db, from.ID, "profile.repositoryUrl", model.AuditChangeAdd)

	relations := mysql.NewRelationRepository(db)
	_, err = relations.CreateRelationWithAudit(ctx, model.RelationCreateInput{
		FromResourceID: from.ID,
		ToResourceID:   to.ID,
		RelationType:   model.RelationTypeDependsOn,
	}, ownerDBA, "inventory.relationship.created")
	if err != nil {
		t.Fatalf("create relationship with audit: %v", err)
	}
	assertLatestAuditChange(t, db, from.ID, "relationships.depends_on", model.AuditChangeAdd)
	assertLatestAuditChange(t, db, to.ID, "relationships.depends_on", model.AuditChangeAdd)
}

func TestInventoryRelationshipMatrixAndAuditAtomicity(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	resources := mysql.NewResourceRepository(db)
	createResource := func(name string, resourceType model.ResourceType, environmentID uint64) *model.Resource {
		t.Helper()
		input := inventoryAuditResource(name)
		input.ResourceType = resourceType
		input.EnvironmentID = environmentID
		created, err := resources.CreateResource(ctx, input)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return created
	}

	instance := createResource("matrix-instance", model.ResourceTypeDatabaseInstance, envProd)
	cluster := createResource("matrix-cluster", model.ResourceTypeDatabaseCluster, envProd)
	stagingCluster := createResource("matrix-staging-cluster", model.ResourceTypeDatabaseCluster, envStaging)
	rollbackCluster := createResource("matrix-rollback-cluster", model.ResourceTypeDatabaseCluster, envProd)
	relations := service.NewRelationService(mysql.NewRelationRepository(db))

	created, err := relations.CreateInventory(ctx, ownerDBA, instance.ID, model.RelationCreateInput{
		ToResourceID: cluster.ID,
		RelationType: model.RelationTypeMemberOf,
	})
	if err != nil {
		t.Fatalf("create allowed member_of: %v", err)
	}
	assertLatestAuditChange(t, db, instance.ID, "relationships.member_of", model.AuditChangeAdd)
	assertLatestAuditChange(t, db, cluster.ID, "relationships.member_of", model.AuditChangeAdd)

	_, err = relations.CreateInventory(ctx, ownerDBA, instance.ID, model.RelationCreateInput{
		ToResourceID: stagingCluster.ID,
		RelationType: model.RelationTypeMemberOf,
	})
	if !errors.Is(err, service.ErrValidationFailed) {
		t.Fatalf("cross-environment member_of error = %v, want ErrValidationFailed", err)
	}
	assertRelationAndAuditCounts(t, db, instance.ID, stagingCluster.ID, 0, 0)

	if _, err := db.Exec(`create trigger ch77_force_inventory_relation_audit_fail
		before insert on audit_events for each row
		signal sqlstate '45000' set message_text = 'forced audit failure'`); err != nil {
		t.Fatalf("create audit failure trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists ch77_force_inventory_relation_audit_fail`) })

	_, err = relations.CreateInventory(ctx, ownerDBA, instance.ID, model.RelationCreateInput{
		ToResourceID: rollbackCluster.ID,
		RelationType: model.RelationTypeMemberOf,
	})
	if err == nil {
		t.Fatal("expected relationship create to roll back when audit insert fails")
	}
	assertRelationAndAuditCounts(t, db, instance.ID, rollbackCluster.ID, 0, 0)

	if err := relations.DeleteInventory(ctx, ownerDBA, created.ID); err == nil {
		t.Fatal("expected relationship delete to roll back when audit insert fails")
	}
	assertRelationAndAuditCounts(t, db, instance.ID, cluster.ID, 1, 1)

	if _, err := db.Exec(`drop trigger ch77_force_inventory_relation_audit_fail`); err != nil {
		t.Fatalf("drop audit failure trigger: %v", err)
	}
	if err := relations.DeleteInventory(ctx, ownerDBA, created.ID); err != nil {
		t.Fatalf("delete allowed member_of: %v", err)
	}
	assertLatestAuditChange(t, db, instance.ID, "relationships.member_of", model.AuditChangeRemove)
	assertLatestAuditChange(t, db, cluster.ID, "relationships.member_of", model.AuditChangeRemove)
}

func assertRelationAndAuditCounts(t *testing.T, db *sql.DB, fromID, toID uint64, wantRelations, wantAudits int) {
	t.Helper()
	var relationCount, auditCount int
	if err := db.QueryRow(`select count(*) from resource_relations where from_resource_id = ? and to_resource_id = ?`, fromID, toID).Scan(&relationCount); err != nil {
		t.Fatalf("count relationships: %v", err)
	}
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ?`, toID).Scan(&auditCount); err != nil {
		t.Fatalf("count relationship audits: %v", err)
	}
	if relationCount != wantRelations || auditCount != wantAudits {
		t.Fatalf("relationship/audit counts = %d/%d, want %d/%d", relationCount, auditCount, wantRelations, wantAudits)
	}
}

func assertLatestAuditChange(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, targetID uint64, field string, operation model.AuditChangeOperation) {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(context.Background(), `select changes from audit_events where target_resource_id = ? order by id desc limit 1`, targetID).Scan(&raw); err != nil {
		t.Fatalf("read latest audit change: %v", err)
	}
	var changes []model.AuditChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatalf("decode latest audit change: %v", err)
	}
	if len(changes) == 0 || changes[0].Field != field || changes[0].Operation != operation {
		t.Fatalf("latest audit changes = %#v, want first %s %s", changes, operation, field)
	}
}

func TestInventoryAuditDomainNameAndVirtualIPProfiles(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	resources := mysql.NewResourceRepository(db)

	domain, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDomainName,
		ResourceSubtype: "dns",
		Name:            "inventory-audit-domain",
		DisplayName:     "Inventory Audit Domain",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create domain name: %v", err)
	}
	err = resources.PutProfileWithAudit(ctx, domain.ID, model.ResourceTypeDomainName, map[string]any{
		"fqdn": "orders.example.com",
	}, ownerDBA, "inventory.profile.updated")
	if err != nil {
		t.Fatalf("put domain name profile with audit: %v", err)
	}
	assertLatestAuditChange(t, db, domain.ID, "profile.fqdn", model.AuditChangeAdd)
	got, err := resources.GetResourceProfile(domain.ID)
	if err != nil {
		t.Fatalf("get domain name profile: %v", err)
	}
	if got.Profile["fqdn"] != "orders.example.com" {
		t.Fatalf("domain name profile = %#v, want fqdn orders.example.com", got.Profile)
	}

	vip, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeVirtualIP,
		ResourceSubtype: "floating",
		Name:            "inventory-audit-vip",
		DisplayName:     "Inventory Audit VIP",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create virtual ip: %v", err)
	}
	err = resources.PutProfileWithAudit(ctx, vip.ID, model.ResourceTypeVirtualIP, map[string]any{
		"ipAddress": "10.0.0.10",
	}, ownerDBA, "inventory.profile.updated")
	if err != nil {
		t.Fatalf("put virtual ip profile with audit: %v", err)
	}
	assertLatestAuditChange(t, db, vip.ID, "profile.ipAddress", model.AuditChangeAdd)
	got, err = resources.GetResourceProfile(vip.ID)
	if err != nil {
		t.Fatalf("get virtual ip profile: %v", err)
	}
	if got.Profile["ipAddress"] != "10.0.0.10" {
		t.Fatalf("virtual ip profile = %#v, want ipAddress 10.0.0.10", got.Profile)
	}
}

func inventoryAuditResource(name string) model.ResourceCreateInput {
	return model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: "api",
		Name:            name,
		DisplayName:     name,
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"team": "operations"},
	}
}

func TestInventoryAuditDatabaseProxyAndControlPlaneProfiles(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	resources := mysql.NewResourceRepository(db)

	proxy, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseProxy,
		ResourceSubtype: "proxysql",
		Name:            "inventory-audit-proxy",
		DisplayName:     "Inventory Audit Proxy",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create database proxy: %v", err)
	}
	err = resources.PutProfileWithAudit(ctx, proxy.ID, model.ResourceTypeDatabaseProxy, map[string]any{
		"technologySubtype": "proxysql",
		"host":              "proxy-prod-01",
		"port":              6033,
		"role":              "active",
		"version":           "2.5.5",
	}, ownerDBA, "inventory.profile.updated")
	if err != nil {
		t.Fatalf("put database proxy profile with audit: %v", err)
	}
	assertAuditChangePresent(t, db, proxy.ID, "profile.host", model.AuditChangeAdd)
	got, err := resources.GetResourceProfile(proxy.ID)
	if err != nil {
		t.Fatalf("get database proxy profile: %v", err)
	}
	if got.Profile["host"] != "proxy-prod-01" || got.Profile["role"] != "active" {
		t.Fatalf("database proxy profile = %#v, want host/role identity", got.Profile)
	}

	control, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeControlPlaneComponent,
		ResourceSubtype: "ha_monitor",
		Name:            "inventory-audit-ha-monitor",
		DisplayName:     "Inventory Audit HA Monitor",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create control plane component: %v", err)
	}
	err = resources.PutProfileWithAudit(ctx, control.ID, model.ResourceTypeControlPlaneComponent, map[string]any{
		"componentSubtype": "ha_monitor",
		"endpoint":         "http://ha-monitor:10008",
		"role":             "standby",
	}, ownerDBA, "inventory.profile.updated")
	if err != nil {
		t.Fatalf("put control plane profile with audit: %v", err)
	}
	assertAuditChangePresent(t, db, control.ID, "profile.endpoint", model.AuditChangeAdd)
	got, err = resources.GetResourceProfile(control.ID)
	if err != nil {
		t.Fatalf("get control plane profile: %v", err)
	}
	if got.Profile["componentSubtype"] != "ha_monitor" || got.Profile["endpoint"] != "http://ha-monitor:10008" {
		t.Fatalf("control plane profile = %#v, want ha_monitor endpoint", got.Profile)
	}

	var haCount int
	if err := db.QueryRowContext(ctx, `select count(*) from resources where resource_type = 'control_plane_component' and resource_subtype = 'ha'`).Scan(&haCount); err != nil {
		t.Fatalf("count ambiguous ha subtypes: %v", err)
	}
	if haCount != 0 {
		t.Fatalf("ambiguous ha subtype remains after migration, count=%d", haCount)
	}
}

func assertAuditChangePresent(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, targetID uint64, field string, operation model.AuditChangeOperation) {
	t.Helper()
	var raw string
	if err := db.QueryRowContext(context.Background(), `select changes from audit_events where target_resource_id = ? order by id desc limit 1`, targetID).Scan(&raw); err != nil {
		t.Fatalf("read latest audit change: %v", err)
	}
	var changes []model.AuditChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatalf("decode latest audit change: %v", err)
	}
	for _, change := range changes {
		if change.Field == field && change.Operation == operation {
			return
		}
	}
	t.Fatalf("latest audit changes = %#v, want %s %s", changes, operation, field)
}
