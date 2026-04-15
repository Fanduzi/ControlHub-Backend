// Package service provides tests for resource and relation write flows.
// input: internal/service write APIs, internal/model, testing
// output: TestResourceService* and TestRelationService* functions
// pos: Validates write-side business rules before repository persistence
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

type fakeResourceWriteRepo struct {
	resources        map[string]model.Resource
	createErr        error
	updateErr        error
	getProfileResult *model.ResourceProfileResponse
}

func (f *fakeResourceWriteRepo) ListResources(_ context.Context, q model.ResourceListQuery) ([]model.Resource, int, error) {
	items := make([]model.Resource, 0)
	for _, item := range f.resources {
		items = append(items, item)
	}
	return items, len(items), nil
}

func (f *fakeResourceWriteRepo) GetResource(id string) (*model.Resource, error) {
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	copy := item
	return &copy, nil
}

func (f *fakeResourceWriteRepo) GetResourceProfile(_ string) (*model.ResourceProfileResponse, error) {
	if f.getProfileResult == nil {
		return &model.ResourceProfileResponse{Profile: map[string]any{}}, nil
	}
	return f.getProfileResult, nil
}

func (f *fakeResourceWriteRepo) CreateResource(_ context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	created := model.Resource{
		ID:              "created-resource",
		ResourceType:    input.ResourceType,
		ResourceSubtype: input.ResourceSubtype,
		Name:            input.Name,
		DisplayName:     input.DisplayName,
		EnvironmentID:   input.EnvironmentID,
		OwnerID:         input.OwnerID,
		LifecycleStatus: string(input.LifecycleStatus),
		HealthStatus:    string(input.HealthStatus),
		Source:          input.Source,
		ExternalID:      input.ExternalID,
		Labels:          cloneLabels(input.Labels),
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if f.resources == nil {
		f.resources = map[string]model.Resource{}
	}
	f.resources[created.ID] = created
	return &created, nil
}

func (f *fakeResourceWriteRepo) UpdateResource(_ context.Context, id string, input model.ResourceUpdateInput) (*model.Resource, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	if input.ResourceSubtype != nil {
		item.ResourceSubtype = *input.ResourceSubtype
	}
	if input.DisplayName != nil {
		item.DisplayName = *input.DisplayName
	}
	if input.EnvironmentID != nil {
		item.EnvironmentID = *input.EnvironmentID
	}
	if input.OwnerID != nil {
		item.OwnerID = *input.OwnerID
	}
	if input.LifecycleStatus != nil {
		item.LifecycleStatus = string(*input.LifecycleStatus)
	}
	if input.HealthStatus != nil {
		item.HealthStatus = string(*input.HealthStatus)
	}
	if input.Source != nil {
		item.Source = *input.Source
	}
	if input.ExternalID != nil {
		item.ExternalID = *input.ExternalID
	}
	if input.Labels != nil {
		item.Labels = cloneLabels(*input.Labels)
	}
	item.UpdatedAt = time.Now().UTC()
	f.resources[id] = item
	copy := item
	return &copy, nil
}

func (f *fakeResourceWriteRepo) ArchiveResource(_ context.Context, id string, reason string) (*model.Resource, error) {
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	now := time.Now().UTC()
	item.ArchivedAt = &now
	item.ArchiveReason = &reason
	f.resources[id] = item
	copy := item
	return &copy, nil
}

type fakeRelationWriteRepo struct {
	resources map[string]model.Resource
	relations map[string]model.ResourceRelation
	createErr error
	deleteErr error
}

func (f *fakeRelationWriteRepo) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	items := make([]model.ResourceRelation, 0)
	for _, item := range f.relations {
		if item.FromResourceID == resourceID || item.ToResourceID == resourceID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeRelationWriteRepo) GetResource(id string) (*model.Resource, error) {
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	copy := item
	return &copy, nil
}

func (f *fakeRelationWriteRepo) CreateRelation(_ context.Context, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	created := model.ResourceRelation{
		ID:             "rel-created",
		FromResourceID: input.FromResourceID,
		ToResourceID:   input.ToResourceID,
		RelationType:   input.RelationType,
		CreatedAt:      time.Now().UTC(),
	}
	if f.relations == nil {
		f.relations = map[string]model.ResourceRelation{}
	}
	f.relations[created.ID] = created
	return &created, nil
}

func (f *fakeRelationWriteRepo) DeleteRelation(_ context.Context, relationID string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.relations[relationID]; !ok {
		return ErrRelationNotFound
	}
	delete(f.relations, relationID)
	return nil
}

func TestResourceServiceCreate(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{}}
	svc := NewResourceService(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "order-mysql-02-prod",
		DisplayName:     "Order MySQL 02 Prod",
		EnvironmentID:   "10000000-0000-0000-0000-000000000001",
		OwnerID:         "20000000-0000-0000-0000-000000000002",
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		ExternalID:      "order-mysql-02-prod",
		Labels:          map[string]string{"team": "order"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if created.Name != "order-mysql-02-prod" {
		t.Fatalf("expected created resource name, got %s", created.Name)
	}
}

func TestResourceServiceCreateRejectsUnsupportedResourceType(t *testing.T) {
	svc := NewResourceService(&fakeResourceWriteRepo{})

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceType("unsupported"),
		Name:            "bad-resource",
		DisplayName:     "Bad Resource",
		EnvironmentID:   "10000000-0000-0000-0000-000000000001",
		OwnerID:         "20000000-0000-0000-0000-000000000002",
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestResourceServiceCreateRejectsMissingEnvironment(t *testing.T) {
	svc := NewResourceService(&fakeResourceWriteRepo{})

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		Name:            "missing-env",
		DisplayName:     "Missing Env",
		EnvironmentID:   "missing-env",
		OwnerID:         "20000000-0000-0000-0000-000000000002",
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("expected ErrEnvironmentNotFound, got %v", err)
	}
}

func TestResourceServiceCreateRejectsMissingOwner(t *testing.T) {
	svc := NewResourceService(&fakeResourceWriteRepo{})

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		Name:            "missing-owner",
		DisplayName:     "Missing Owner",
		EnvironmentID:   "10000000-0000-0000-0000-000000000001",
		OwnerID:         "missing-owner",
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if !errors.Is(err, ErrOwnerNotFound) {
		t.Fatalf("expected ErrOwnerNotFound, got %v", err)
	}
}

func TestResourceServiceCreateRejectsDuplicateNameWithinEnvironment(t *testing.T) {
	repo := &fakeResourceWriteRepo{createErr: ErrResourceConflict}
	svc := NewResourceService(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		Name:            "order-mysql-prod",
		DisplayName:     "Duplicate",
		EnvironmentID:   "10000000-0000-0000-0000-000000000001",
		OwnerID:         "20000000-0000-0000-0000-000000000002",
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if !errors.Is(err, ErrResourceConflict) {
		t.Fatalf("expected ErrResourceConflict, got %v", err)
	}
}

func TestResourceServiceUpdateRejectsImmutableFields(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{"res-1": {
		ID:            "res-1",
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: "10000000-0000-0000-0000-000000000001",
		OwnerID:       "20000000-0000-0000-0000-000000000002",
		Source:        "manual",
		Labels:        map[string]string{},
	}}}
	svc := NewResourceService(repo)
	name := "new-name"

	_, err := svc.Update(context.Background(), "res-1", model.ResourcePatchRequest{Name: &name})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestRelationServiceCreate(t *testing.T) {
	repo := &fakeRelationWriteRepo{
		resources: map[string]model.Resource{
			"res-1": {ID: "res-1", Name: "order-api-prod"},
			"res-2": {ID: "res-2", Name: "order-mysql-prod"},
		},
	}
	svc := NewRelationService(repo)

	created, err := svc.Create(context.Background(), "res-1", model.RelationCreateInput{
		ToResourceID: "res-2",
		RelationType: model.RelationTypeDependsOn,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if created.FromResourceID != "res-1" {
		t.Fatalf("expected fromResourceID res-1, got %s", created.FromResourceID)
	}
}

func TestRelationServiceCreateRejectsMissingTarget(t *testing.T) {
	repo := &fakeRelationWriteRepo{resources: map[string]model.Resource{
		"res-1": {ID: "res-1", Name: "order-api-prod"},
	}}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), "res-1", model.RelationCreateInput{
		ToResourceID: "missing",
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestRelationServiceCreateRejectsUnsupportedRelationType(t *testing.T) {
	repo := &fakeRelationWriteRepo{resources: map[string]model.Resource{
		"res-1": {ID: "res-1", Name: "order-api-prod"},
		"res-2": {ID: "res-2", Name: "order-mysql-prod"},
	}}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), "res-1", model.RelationCreateInput{
		ToResourceID: "res-2",
		RelationType: model.RelationType("unsupported"),
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestRelationServiceCreateRejectsDuplicate(t *testing.T) {
	repo := &fakeRelationWriteRepo{
		resources: map[string]model.Resource{
			"res-1": {ID: "res-1", Name: "order-api-prod"},
			"res-2": {ID: "res-2", Name: "order-mysql-prod"},
		},
		createErr: ErrRelationConflict,
	}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), "res-1", model.RelationCreateInput{
		ToResourceID: "res-2",
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrRelationConflict) {
		t.Fatalf("expected ErrRelationConflict, got %v", err)
	}
}

func TestRelationServiceDelete(t *testing.T) {
	repo := &fakeRelationWriteRepo{relations: map[string]model.ResourceRelation{
		"rel-1": {ID: "rel-1", FromResourceID: "res-1", ToResourceID: "res-2", RelationType: model.RelationTypeDependsOn},
	}}
	svc := NewRelationService(repo)

	if err := svc.Delete(context.Background(), "rel-1"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := repo.relations["rel-1"]; ok {
		t.Fatal("expected relation to be removed")
	}
}

func TestRelationServiceDeleteRejectsNotFound(t *testing.T) {
	svc := NewRelationService(&fakeRelationWriteRepo{relations: map[string]model.ResourceRelation{}})

	err := svc.Delete(context.Background(), "missing")
	if !errors.Is(err, ErrRelationNotFound) {
		t.Fatalf("expected ErrRelationNotFound, got %v", err)
	}
}

func cloneLabels(labels map[string]string) map[string]string {
	cloned := make(map[string]string, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func TestResourceServiceArchive(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{"res-1": {
		ID:            "res-1",
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: "10000000-0000-0000-0000-000000000001",
		OwnerID:       "20000000-0000-0000-0000-000000000002",
		Source:        "manual",
		Labels:        map[string]string{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}}}
	svc := NewResourceService(repo)

	archived, err := svc.Archive(context.Background(), "res-1", model.ArchiveRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("expected archivedAt to be set")
	}
}

func TestResourceServiceArchiveNotFound(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{}}
	svc := NewResourceService(repo)

	_, err := svc.Archive(context.Background(), "missing", model.ArchiveRequest{})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestResourceServiceArchiveIdempotent(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{"res-1": {
		ID:            "res-1",
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: "10000000-0000-0000-0000-000000000001",
		OwnerID:       "20000000-0000-0000-0000-000000000002",
		Source:        "manual",
		Labels:        map[string]string{},
		ArchivedAt:    &archivedAt,
	}}}
	svc := NewResourceService(repo)

	result, err := svc.Archive(context.Background(), "res-1", model.ArchiveRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ArchivedAt != &archivedAt {
		t.Fatal("expected same archivedAt (idempotent)")
	}
}

func TestResourceServiceArchiveRejectsBlankReason(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{"res-1": {
		ID:            "res-1",
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: "10000000-0000-0000-0000-000000000001",
		OwnerID:       "20000000-0000-0000-0000-000000000002",
		Source:        "manual",
		Labels:        map[string]string{},
	}}}
	svc := NewResourceService(repo)
	blank := "  "

	_, err := svc.Archive(context.Background(), "res-1", model.ArchiveRequest{Reason: &blank})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestResourceServiceUpdateRejectsArchived(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeResourceWriteRepo{resources: map[string]model.Resource{"res-1": {
		ID:            "res-1",
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: "10000000-0000-0000-0000-000000000001",
		OwnerID:       "20000000-0000-0000-0000-000000000002",
		Source:        "manual",
		Labels:        map[string]string{},
		ArchivedAt:    &archivedAt,
	}}}
	svc := NewResourceService(repo)
	displayName := "New Name"

	_, err := svc.Update(context.Background(), "res-1", model.ResourcePatchRequest{DisplayName: &displayName})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
}

func TestRelationServiceCreateRejectsArchivedSource(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeRelationWriteRepo{
		resources: map[string]model.Resource{
			"res-1": {ID: "res-1", Name: "source", ArchivedAt: &archivedAt},
			"res-2": {ID: "res-2", Name: "target"},
		},
	}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), "res-1", model.RelationCreateInput{
		ToResourceID: "res-2",
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
}

func TestRelationServiceCreateRejectsArchivedTarget(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeRelationWriteRepo{
		resources: map[string]model.Resource{
			"res-1": {ID: "res-1", Name: "source"},
			"res-2": {ID: "res-2", Name: "target", ArchivedAt: &archivedAt},
		},
	}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), "res-1", model.RelationCreateInput{
		ToResourceID: "res-2",
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
}
