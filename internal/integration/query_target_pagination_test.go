//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

func TestQueryTargetRepository_SearchTreatsPercentAndUnderscoreLiterally(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	qtRepo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	pctRes, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "pct-search-100pct-prod",
		DisplayName:     "PCT Search 100%",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create pct resource: %v", err)
	}

	_, err = resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "pct-search-100xyz-prod",
		DisplayName:     "PCT Search 100xyz",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create wildcard-match resource: %v", err)
	}

	underscoreRes, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "underscore-search-a_b-prod",
		DisplayName:     "Underscore Search a_b",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create underscore resource: %v", err)
	}

	_, err = resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "underscore-search-axb-prod",
		DisplayName:     "Underscore Search axb",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create underscore-wildcard resource: %v", err)
	}

	targets, _, err := qtRepo.ListQueryTargets(ctx, model.QueryTargetListQuery{Q: "100%"})
	if err != nil {
		t.Fatalf("list with %% query: %v", err)
	}
	var foundPct bool
	for _, tgt := range targets {
		if tgt.ResourceID == pctRes.ID {
			foundPct = true
		}
		if tgt.ResourceName == "pct-search-100xyz-prod" {
			t.Fatalf("search for '100%%' matched 'pct-search-100xyz-prod' - %% treated as wildcard")
		}
	}
	if !foundPct {
		t.Fatalf("search for '100%%' did not match resource with literal %% in name (id %d)", pctRes.ID)
	}

	targets, _, err = qtRepo.ListQueryTargets(ctx, model.QueryTargetListQuery{Q: "a_b"})
	if err != nil {
		t.Fatalf("list with _ query: %v", err)
	}
	var foundUnderscore bool
	for _, tgt := range targets {
		if tgt.ResourceID == underscoreRes.ID {
			foundUnderscore = true
		}
		if tgt.ResourceName == "underscore-search-axb-prod" {
			t.Fatalf("search for 'a_b' matched 'underscore-search-axb-prod' - _ treated as wildcard")
		}
	}
	if !foundUnderscore {
		t.Fatalf("search for 'a_b' did not match resource with literal _ in name (id %d)", underscoreRes.ID)
	}
}

func TestQueryTargetRepository_Pagination1000Targets(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	qtRepo := mysql.NewQueryTargetRepository(db)
	ctx := context.Background()

	const batchSize = 200
	const totalTargets = 1000
	prefix := "qt-page1k-" + t.Name()

	for batch := 0; batch < totalTargets; batch += batchSize {
		end := batch + batchSize
		if end > totalTargets {
			end = totalTargets
		}
		for i := batch; i < end; i++ {
			_, err := resourceRepo.CreateResource(ctx, model.ResourceCreateInput{
				ResourceType:    model.ResourceTypeDatabaseInstance,
				ResourceSubtype: "mysql",
				Name:            fmt.Sprintf("%s-%04d", prefix, i),
				DisplayName:     fmt.Sprintf("Page1K Target %04d", i),
				EnvironmentID:   envProd,
				OwnerID:         ownerDBA,
				LifecycleStatus: model.LifecycleStatusRunning,
				HealthStatus:    model.HealthStatusHealthy,
				Source:          "manual",
				Labels:          map[string]string{},
			})
			if err != nil {
				t.Fatalf("create target %d: %v", i, err)
			}
		}
	}

	targets, total, err := qtRepo.ListQueryTargets(ctx, model.QueryTargetListQuery{
		Q:        prefix,
		Page:     1,
		PageSize: 25,
	})
	if err != nil {
		t.Fatalf("list query targets: %v", err)
	}

	if len(targets) != 25 {
		t.Fatalf("returned %d targets, want exactly 25", len(targets))
	}
	if total < totalTargets {
		t.Fatalf("total = %d, want >= %d", total, totalTargets)
	}
	if targets[0].ResourceName != fmt.Sprintf("%s-0000", prefix) {
		t.Fatalf("first target = %q, want %q", targets[0].ResourceName, fmt.Sprintf("%s-0000", prefix))
	}
}
