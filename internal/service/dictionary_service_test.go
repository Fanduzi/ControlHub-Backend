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
