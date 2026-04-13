// Package service provides tests for dictionary services.
// input: internal/service (all dictionary services), internal/model
// output: TestResourceTypeServiceList, TestRelationTypeServiceList, TestLifecycleStatusServiceList, TestHealthStatusServiceList
// pos: Validates dictionary service listing with fake repos
// note: if this file changes, update header and README.md
package service

import (
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type fakeResourceTypeRepo struct {
	items []model.DictionaryItem
}

func (f fakeResourceTypeRepo) ListResourceTypes() ([]model.DictionaryItem, error) {
	return f.items, nil
}

type fakeRelationTypeRepo struct {
	items []model.DictionaryItem
}

func (f fakeRelationTypeRepo) ListRelationTypes() ([]model.DictionaryItem, error) {
	return f.items, nil
}

func TestResourceTypeServiceList(t *testing.T) {
	expected := []model.DictionaryItem{
		{Key: string(model.ResourceTypeHost), Label: "Host", Description: "Infrastructure carrier resource."},
	}

	svc := NewResourceTypeService(fakeResourceTypeRepo{items: expected})

	items, err := svc.List()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if items[0].Key != expected[0].Key {
		t.Fatalf("expected key %s, got %s", expected[0].Key, items[0].Key)
	}
}

func TestRelationTypeServiceList(t *testing.T) {
	expected := []model.DictionaryItem{
		{Key: string(model.RelationTypeDependsOn), Label: "Depends On", Description: "Dependency edge."},
	}

	svc := NewRelationTypeService(fakeRelationTypeRepo{items: expected})

	items, err := svc.List()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if items[0].Key != expected[0].Key {
		t.Fatalf("expected key %s, got %s", expected[0].Key, items[0].Key)
	}
}

type fakeLifecycleStatusRepo struct {
	items []model.DictionaryItem
}

func (f fakeLifecycleStatusRepo) ListLifecycleStatuses() ([]model.DictionaryItem, error) {
	return f.items, nil
}

type fakeHealthStatusRepo struct {
	items []model.DictionaryItem
}

func (f fakeHealthStatusRepo) ListHealthStatuses() ([]model.DictionaryItem, error) {
	return f.items, nil
}

func TestLifecycleStatusServiceList(t *testing.T) {
	expected := []model.DictionaryItem{
		{Key: string(model.LifecycleStatusRunning), Label: "Running", Description: "Active resource."},
	}

	svc := NewLifecycleStatusService(fakeLifecycleStatusRepo{items: expected})

	items, err := svc.List()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if items[0].Key != expected[0].Key {
		t.Fatalf("expected key %s, got %s", expected[0].Key, items[0].Key)
	}
}

func TestHealthStatusServiceList(t *testing.T) {
	expected := []model.DictionaryItem{
		{Key: string(model.HealthStatusHealthy), Label: "Healthy", Description: "No issues."},
	}

	svc := NewHealthStatusService(fakeHealthStatusRepo{items: expected})

	items, err := svc.List()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	if items[0].Key != expected[0].Key {
		t.Fatalf("expected key %s, got %s", expected[0].Key, items[0].Key)
	}
}
