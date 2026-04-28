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

const (
	testResourceCreatedID uint64 = 100
	testRelationCreatedID uint64 = 200
	testResource1ID      uint64 = 101
	testResource2ID      uint64 = 102
	testMissingID        uint64 = 999999
	testEnvID            uint64 = 1
	testOwnerID          uint64 = 2
)

type fakeResourceWriteRepo struct {
	resources        map[uint64]model.Resource
	createErr        error
	updateErr        error
	getProfileResult *model.ResourceProfileResponse
}

func (f *fakeResourceWriteRepo) ListResources(_ context.Context, _ model.ResourceListQuery) ([]model.Resource, int, error) {
	items := make([]model.Resource, 0)
	for _, item := range f.resources {
		items = append(items, item)
	}
	return items, len(items), nil
}

func (f *fakeResourceWriteRepo) GetResource(id uint64) (*model.Resource, error) {
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	copy := item
	return &copy, nil
}

func (f *fakeResourceWriteRepo) GetResourceProfile(_ uint64) (*model.ResourceProfileResponse, error) {
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
		ID:              testResourceCreatedID,
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
		f.resources = map[uint64]model.Resource{}
	}
	f.resources[created.ID] = created
	return &created, nil
}

func (f *fakeResourceWriteRepo) UpdateResource(_ context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	if input.Name != nil {
		item.Name = *input.Name
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

func (f *fakeResourceWriteRepo) ArchiveResource(_ context.Context, id uint64, reason string) (*model.Resource, error) {
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

func (f *fakeResourceWriteRepo) UnarchiveResource(_ context.Context, id uint64) (*model.Resource, error) {
	item, ok := f.resources[id]
	if !ok {
		return nil, ErrResourceNotFound
	}
	item.ArchivedAt = nil
	item.ArchivedBy = nil
	item.ArchiveReason = nil
	f.resources[id] = item
	copy := item
	return &copy, nil
}

type fakeRelationWriteRepo struct {
	resources map[uint64]model.Resource
	relations map[uint64]model.ResourceRelation
	createErr error
	deleteErr error
}

func (f *fakeRelationWriteRepo) ListByResourceID(resourceID uint64) ([]model.ResourceRelation, error) {
	items := make([]model.ResourceRelation, 0)
	for _, item := range f.relations {
		if item.FromResourceID == resourceID || item.ToResourceID == resourceID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (f *fakeRelationWriteRepo) GetResource(id uint64) (*model.Resource, error) {
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
		ID:             testRelationCreatedID,
		FromResourceID: input.FromResourceID,
		ToResourceID:   input.ToResourceID,
		RelationType:   input.RelationType,
		CreatedAt:      time.Now().UTC(),
	}
	if f.relations == nil {
		f.relations = map[uint64]model.ResourceRelation{}
	}
	f.relations[created.ID] = created
	return &created, nil
}

func (f *fakeRelationWriteRepo) DeleteRelation(_ context.Context, relationID uint64) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.relations[relationID]; !ok {
		return ErrRelationNotFound
	}
	delete(f.relations, relationID)
	return nil
}

func (f *fakeRelationWriteRepo) ListRelationViewsByResourceID(_ uint64) ([]model.ResourceRelationView, error) {
	return []model.ResourceRelationView{}, nil
}

func (f *fakeRelationWriteRepo) ListClusterMembers(_ uint64) ([]model.ClusterMemberView, error) {
	return []model.ClusterMemberView{}, nil
}

func TestResourceServiceCreate(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "order-mysql-02-prod",
		DisplayName:     "Order MySQL 02 Prod",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
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
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
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
		ResourceSubtype: "mysql",
		Name:            "missing-env",
		DisplayName:     "Missing Env",
		EnvironmentID:   testMissingID,
		OwnerID:         testOwnerID,
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
		ResourceSubtype: "mysql",
		Name:            "missing-owner",
		DisplayName:     "Missing Owner",
		EnvironmentID:   testEnvID,
		OwnerID:         testMissingID,
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
		ResourceSubtype: "mysql",
		Name:            "order-mysql-prod",
		DisplayName:     "Duplicate",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
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
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
	}}}
	svc := NewResourceService(repo)
	rt := model.ResourceTypeHost

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{ResourceType: &rt})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestRelationServiceCreate(t *testing.T) {
	repo := &fakeRelationWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: {ID: testResource1ID, Name: "order-api-prod"},
			testResource2ID: {ID: testResource2ID, Name: "order-mysql-prod"},
		},
	}
	svc := NewRelationService(repo)

	created, err := svc.Create(context.Background(), testResource1ID, model.RelationCreateInput{
		ToResourceID: testResource2ID,
		RelationType: model.RelationTypeDependsOn,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if created.FromResourceID != testResource1ID {
		t.Fatalf("expected fromResourceID %d, got %d", testResource1ID, created.FromResourceID)
	}
}

func TestRelationServiceCreateRejectsMissingTarget(t *testing.T) {
	repo := &fakeRelationWriteRepo{resources: map[uint64]model.Resource{
		testResource1ID: {ID: testResource1ID, Name: "order-api-prod"},
	}}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), testResource1ID, model.RelationCreateInput{
		ToResourceID: testMissingID,
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestRelationServiceCreateRejectsUnsupportedRelationType(t *testing.T) {
	repo := &fakeRelationWriteRepo{resources: map[uint64]model.Resource{
		testResource1ID: {ID: testResource1ID, Name: "order-api-prod"},
		testResource2ID: {ID: testResource2ID, Name: "order-mysql-prod"},
	}}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), testResource1ID, model.RelationCreateInput{
		ToResourceID: testResource2ID,
		RelationType: model.RelationType("unsupported"),
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestRelationServiceCreateRejectsDuplicate(t *testing.T) {
	repo := &fakeRelationWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: {ID: testResource1ID, Name: "order-api-prod"},
			testResource2ID: {ID: testResource2ID, Name: "order-mysql-prod"},
		},
		createErr: ErrRelationConflict,
	}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), testResource1ID, model.RelationCreateInput{
		ToResourceID: testResource2ID,
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrRelationConflict) {
		t.Fatalf("expected ErrRelationConflict, got %v", err)
	}
}

func TestRelationServiceDelete(t *testing.T) {
	repo := &fakeRelationWriteRepo{relations: map[uint64]model.ResourceRelation{
		testRelationCreatedID: {ID: testRelationCreatedID, FromResourceID: testResource1ID, ToResourceID: testResource2ID, RelationType: model.RelationTypeDependsOn},
	}}
	svc := NewRelationService(repo)

	if err := svc.Delete(context.Background(), testRelationCreatedID); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := repo.relations[testRelationCreatedID]; ok {
		t.Fatal("expected relation to be removed")
	}
}

func TestRelationServiceDeleteRejectsNotFound(t *testing.T) {
	svc := NewRelationService(&fakeRelationWriteRepo{relations: map[uint64]model.ResourceRelation{}})

	err := svc.Delete(context.Background(), testMissingID)
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
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}}}
	svc := NewResourceService(repo)

	archived, err := svc.Archive(context.Background(), testResource1ID, model.ArchiveRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("expected archivedAt to be set")
	}
}

func TestResourceServiceArchiveNotFound(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	_, err := svc.Archive(context.Background(), testMissingID, model.ArchiveRequest{})
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestResourceServiceArchiveIdempotent(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
		ArchivedAt:    &archivedAt,
	}}}
	svc := NewResourceService(repo)

	result, err := svc.Archive(context.Background(), testResource1ID, model.ArchiveRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ArchivedAt != &archivedAt {
		t.Fatal("expected same archivedAt (idempotent)")
	}
}

func TestResourceServiceArchiveRejectsBlankReason(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
	}}}
	svc := NewResourceService(repo)
	blank := "  "

	_, err := svc.Archive(context.Background(), testResource1ID, model.ArchiveRequest{Reason: &blank})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestResourceServiceUpdateRejectsArchived(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
		ArchivedAt:    &archivedAt,
	}}}
	svc := NewResourceService(repo)
	displayName := "New Name"

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{DisplayName: &displayName})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
}

func TestResourceServiceUnarchive(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	reason := "decommissioned"
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
		ArchivedAt:    &archivedAt,
		ArchiveReason: &reason,
		CreatedAt:     now,
		UpdatedAt:     now,
	}}}
	svc := NewResourceService(repo)

	unarchived, err := svc.Unarchive(context.Background(), testResource1ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if unarchived.ArchivedAt != nil {
		t.Fatal("expected archivedAt to be nil after unarchive")
	}
	if unarchived.ArchiveReason != nil {
		t.Fatal("expected archiveReason to be nil after unarchive")
	}
}

func TestResourceServiceUnarchiveNotFound(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	_, err := svc.Unarchive(context.Background(), testMissingID)
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestResourceServiceUnarchiveIdempotentForActive(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID:            testResource1ID,
		ResourceType:  model.ResourceTypeDatabaseInstance,
		Name:          "order-mysql-prod",
		DisplayName:   "Order MySQL Prod",
		EnvironmentID: testEnvID,
		OwnerID:       testOwnerID,
		Source:        "manual",
		Labels:        map[string]string{},
		CreatedAt:     now,
		UpdatedAt:     now,
	}}}
	svc := NewResourceService(repo)

	result, err := svc.Unarchive(context.Background(), testResource1ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ArchivedAt != nil {
		t.Fatal("expected archivedAt to remain nil for active resource")
	}
}

func TestRelationServiceCreateRejectsArchivedSource(t *testing.T) {
	now := time.Now().UTC()
	archivedAt := now.Add(-1 * time.Hour)
	repo := &fakeRelationWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: {ID: testResource1ID, Name: "source", ArchivedAt: &archivedAt},
			testResource2ID: {ID: testResource2ID, Name: "target"},
		},
	}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), testResource1ID, model.RelationCreateInput{
		ToResourceID: testResource2ID,
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
		resources: map[uint64]model.Resource{
			testResource1ID: {ID: testResource1ID, Name: "source"},
			testResource2ID: {ID: testResource2ID, Name: "target", ArchivedAt: &archivedAt},
		},
	}
	svc := NewRelationService(repo)

	_, err := svc.Create(context.Background(), testResource1ID, model.RelationCreateInput{
		ToResourceID: testResource2ID,
		RelationType: model.RelationTypeDependsOn,
	})
	if !errors.Is(err, ErrResourceArchived) {
		t.Fatalf("expected ErrResourceArchived, got %v", err)
	}
}
