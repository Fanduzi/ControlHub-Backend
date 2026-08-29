//go:build integration

// Package integration provides real-MySQL coverage for core CI typed profiles.
// input: context, encoding/json, testing, internal/model, internal/repository/mysql, internal/service
// output: TestTypedProfileManualIdentity*
// pos: Proves core CI typed-profile identity, worker subtype, and T01 profile audit on MySQL
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestTypedProfileManualIdentity_CreateReadEditAndAudit(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	resources := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	profiles := service.NewProfileService(repo, repo)
	ctx := context.Background()
	name := fmt.Sprintf("t02-host-%d", time.Now().UnixNano())

	created, err := resources.Create(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            name,
		DisplayName:     "T02 Host",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"team": "platform"},
		Profile:         map[string]any{"hostname": "t02-host.internal", "ipAddress": "10.8.0.10", "osName": "Ubuntu 22.04"},
	})
	if err != nil {
		t.Fatalf("create host: %v", err)
	}
	if created.Labels["hostname"] != "" {
		t.Fatalf("hostname must live in typed profile, not labels: %#v", created.Labels)
	}

	got, err := resources.GetProfile(created.ID)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if got.Profile["hostname"] != "t02-host.internal" || got.Profile["ipAddress"] != "10.8.0.10" {
		t.Fatalf("profile = %#v", got.Profile)
	}

	if err := profiles.PatchProfileInventory(ctx, ownerDBA, created.ID, map[string]any{"osName": "Ubuntu 24.04"}); err != nil {
		t.Fatalf("patch profile: %v", err)
	}
	after, err := resources.GetProfile(created.ID)
	if err != nil {
		t.Fatalf("get profile after edit: %v", err)
	}
	if after.Profile["osName"] != "Ubuntu 24.04" {
		t.Fatalf("osName after edit = %#v", after.Profile)
	}
	if after.Profile["hostname"] != "t02-host.internal" {
		t.Fatalf("hostname must be preserved across partial edit, got %#v", after.Profile)
	}

	var raw string
	if err := db.QueryRowContext(ctx, `select changes from audit_events where target_resource_id = ? order by id desc limit 1`, created.ID).Scan(&raw); err != nil {
		t.Fatalf("read profile audit: %v", err)
	}
	var changes []model.AuditChange
	if err := json.Unmarshal([]byte(raw), &changes); err != nil {
		t.Fatalf("decode profile audit: %v", err)
	}
	found := false
	for _, change := range changes {
		if change.Field == "profile.osName" && change.Operation == model.AuditChangeUpdate && change.After == "Ubuntu 24.04" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected T01 profile.osName update evidence, got %#v", changes)
	}
}

func TestTypedProfileManualIdentity_RejectsMissingFieldsAndUnknownSubtype(t *testing.T) {
	db := setupTestDB(t)
	svc := service.NewResourceService(mysql.NewResourceRepository(db), mysql.NewRelationRepository(db))
	ctx := context.Background()

	_, err := svc.Create(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            fmt.Sprintf("t02-missing-%d", time.Now().UnixNano()),
		DisplayName:     "Missing Identity",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"hostname": "only-in-labels", "ipAddress": "10.8.0.11"},
	})
	var ve *service.ValidationError
	if !errors.As(err, &ve) || ve.Fields["hostname"] == "" || ve.Fields["ipAddress"] == "" {
		t.Fatalf("expected identity field errors, got %v", err)
	}

	_, err = svc.Create(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeService,
		ResourceSubtype: "ha",
		Name:            fmt.Sprintf("t02-bad-sub-%d", time.Now().UnixNano()),
		DisplayName:     "Bad Subtype",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile:         map[string]any{"systemName": "orders"},
	})
	if !errors.As(err, &ve) || ve.Fields["resourceSubtype"] == "" {
		t.Fatalf("expected unknown subtype field error, got %v", err)
	}
}

func TestTypedProfileManualIdentity_AcceptsAllFourCoreTypes(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewResourceRepository(db)
	svc := service.NewResourceService(repo, mysql.NewRelationRepository(db))
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	cases := []model.ResourceCreateInput{
		{
			ResourceType: model.ResourceTypeHost, ResourceSubtype: "physical",
			Name: fmt.Sprintf("t02-core-host-%d", suffix), DisplayName: "Core Host",
			Profile: map[string]any{"hostname": "core-host.internal", "ipAddress": "10.8.0.12"},
		},
		{
			ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql",
			Name: fmt.Sprintf("t02-core-db-%d", suffix), DisplayName: "Core DB",
			Profile: map[string]any{"engine": "mysql", "host": "core-db.internal", "port": 3306},
		},
		{
			ResourceType: model.ResourceTypeDatabaseCluster, ResourceSubtype: "mysql",
			Name: fmt.Sprintf("t02-core-cluster-%d", suffix), DisplayName: "Core Cluster",
			Profile: map[string]any{"engine": "mysql", "primaryEndpoint": "core-cluster.internal:3306"},
		},
		{
			ResourceType: model.ResourceTypeService, ResourceSubtype: "worker",
			Name: fmt.Sprintf("t02-core-worker-%d", suffix), DisplayName: "Core Worker",
			Profile: map[string]any{"systemName": "core-worker"},
		},
	}

	for _, input := range cases {
		input.EnvironmentID = envProd
		input.OwnerID = ownerDBA
		input.LifecycleStatus = model.LifecycleStatusRunning
		input.HealthStatus = model.HealthStatusHealthy
		input.Source = "manual"
		created, err := svc.Create(ctx, input)
		if err != nil {
			t.Fatalf("create %s: %v", input.ResourceType, err)
		}
		got, err := svc.GetProfile(created.ID)
		if err != nil {
			t.Fatalf("get profile %s: %v", input.ResourceType, err)
		}
		for key, want := range input.Profile {
			if fmt.Sprint(got.Profile[key]) != fmt.Sprint(want) {
				t.Fatalf("%s profile[%s]=%#v, want %#v", input.ResourceType, key, got.Profile[key], want)
			}
		}
	}
}
