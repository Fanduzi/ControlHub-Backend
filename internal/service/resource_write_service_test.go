// Package service provides tests for resource and relation write flows.
// input: internal/service write APIs, internal/model, testing
// output: TestResourceService* and TestRelationService* functions
// pos: Validates write-side business rules before repository persistence
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

const (
	testResourceCreatedID uint64 = 100
	testRelationCreatedID uint64 = 200
	testResource1ID       uint64 = 101
	testResource2ID       uint64 = 102
	testMissingID         uint64 = 999999
	testEnvID             uint64 = 1
	testOwnerID           uint64 = 2
)

type fakeResourceWriteRepo struct {
	resources        map[uint64]model.Resource
	createErr        error
	updateErr        error
	profileErr       error          // injected failure for create-with-profile
	profile          map[string]any // last profile written via CreateResourceWithProfile
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

// CreateResourceWithProfile simulates the repository-level transaction: when
// the profile write fails, neither the resource nor the profile is recorded.
func (f *fakeResourceWriteRepo) CreateResourceWithProfile(_ context.Context, input model.ResourceCreateInput, profile map[string]any) (*model.Resource, error) {
	if f.profileErr != nil {
		return nil, f.profileErr
	}
	created, err := f.CreateResource(context.Background(), input)
	if err != nil {
		return nil, err
	}
	f.profile = profile
	return created, nil
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

func TestResourceServiceArchiveRejectsReasonTooLong(t *testing.T) {
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
	long := strings.Repeat("a", model.MaxArchiveReasonLength+1)

	_, err := svc.Archive(context.Background(), testResource1ID, model.ArchiveRequest{Reason: &long})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestResourceServiceArchiveAcceptsMaxLengthReason(t *testing.T) {
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
	maxReason := strings.Repeat("a", model.MaxArchiveReasonLength)

	_, err := svc.Archive(context.Background(), testResource1ID, model.ArchiveRequest{Reason: &maxReason})
	if err != nil {
		t.Fatalf("expected no error for max-length reason, got %v", err)
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

func TestResourceServiceCreateRejectsLabelControlCharacters(t *testing.T) {
	svc := NewResourceService(&fakeResourceWriteRepo{})

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "order-mysql-02-prod",
		DisplayName:     "Order MySQL 02 Prod",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"team": "or\x00der"},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for control char in label value, got %v", err)
	}
}

func TestResourceServiceCreateRejectsLabelKeyControlCharacters(t *testing.T) {
	svc := NewResourceService(&fakeResourceWriteRepo{})

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "order-mysql-02-prod",
		DisplayName:     "Order MySQL 02 Prod",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"tea\x03m": "order"},
	})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for control char in label key, got %v", err)
	}
}

func TestResourceServiceCreateAcceptsValidLabels(t *testing.T) {
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
		Labels:          map[string]string{"team": "order", "tier": "data", "env": "prod"},
	})
	if err != nil {
		t.Fatalf("expected no error for valid labels, got %v", err)
	}
	if created.Labels["team"] != "order" {
		t.Fatalf("expected label team=order, got %s", created.Labels["team"])
	}
}

func TestResourceServicePatchRejectsLabelControlCharacters(t *testing.T) {
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
	labels := map[string]string{"team": "or\x00der"}

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{Labels: &labels})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for control char in patch label value, got %v", err)
	}
}

func TestResourceServicePatchRejectsLabelKeyControlCharacters(t *testing.T) {
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
	labels := map[string]string{"tea\x03m": "order"}

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{Labels: &labels})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for control char in patch label key, got %v", err)
	}
}

func TestResourceServicePatchRejectsSensitiveLabelKeys(t *testing.T) {
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
	labels := map[string]string{"apiToken": "must-not-enter-inventory"}

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{Labels: &labels})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed for sensitive label key, got %v", err)
	}
}

func TestResourceServicePatchAcceptsValidLabels(t *testing.T) {
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
	labels := map[string]string{"team": "order", "pci": "true"}

	updated, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{Labels: &labels})
	if err != nil {
		t.Fatalf("expected no error for valid patch labels, got %v", err)
	}
	if updated.Labels["team"] != "order" {
		t.Fatalf("expected label team=order, got %s", updated.Labels["team"])
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

// TestResourceServiceCreate_ProfileWriteFailureReturnsErrorAndNoResource pins
// the atomicity contract: when the initial profile write fails, Create must
// return an error and must not leave a resource with a lost profile behind.
func TestResourceServiceCreate_ProfileWriteFailureReturnsErrorAndNoResource(t *testing.T) {
	repo := &fakeResourceWriteRepo{
		resources:  map[uint64]model.Resource{},
		profileErr: errors.New("profile write failed"),
	}
	svc := NewResourceService(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "atomicity-host-01",
		DisplayName:     "Atomicity Host 01",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile: map[string]any{
			"hostname":  "atomicity-host-01",
			"ipAddress": "10.0.0.1",
			"osName":    "Ubuntu 22.04",
		},
	})
	if err == nil {
		t.Fatal("expected error when the initial profile write fails; got success")
	}
	if len(repo.resources) != 0 {
		t.Fatalf("expected no resource left behind after profile write failure, found %d", len(repo.resources))
	}
}

// TestResourceServiceCreate_WithProfilePersistsProfile pins the success side
// of the contract: a valid embedded profile is written together with the
// resource through the transactional repository seam.
func TestResourceServiceCreate_WithProfilePersistsProfile(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "atomicity-host-02",
		DisplayName:     "Atomicity Host 02",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile: map[string]any{
			"hostname":  "atomicity-host-02",
			"ipAddress": "10.0.0.2",
			"osName":    "Ubuntu 22.04",
		},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if created == nil || created.ID == 0 {
		t.Fatal("expected created resource")
	}
	if repo.profile == nil || repo.profile["hostname"] != "atomicity-host-02" {
		t.Fatalf("expected embedded profile to be written through the repo, got %#v", repo.profile)
	}
	if len(repo.resources) != 1 {
		t.Fatalf("expected exactly one resource, found %d", len(repo.resources))
	}
}

// TestResourceServiceCreate_EmptyProfileObjectGoesThroughAtomicPath pins the
// nil-versus-empty distinction: a submitted empty profile object is still a
// profile write request and must reach the transactional repository seam
// (where unsupported types are rejected) instead of being silently dropped.
func TestResourceServiceCreate_EmptyProfileObjectGoesThroughAtomicPath(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "atomicity-empty-profile-01",
		DisplayName:     "Atomicity Empty Profile 01",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile:         map[string]any{},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if created == nil || created.ID == 0 {
		t.Fatal("expected created resource")
	}
	if repo.profile == nil {
		t.Fatal("expected the empty profile object to reach the atomic repository seam, got nil")
	}
	if len(repo.resources) != 1 {
		t.Fatalf("expected exactly one resource, found %d", len(repo.resources))
	}
}

func TestResourceServiceCreateRejectsInvalidProfileFields(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "atomicity-invalid-profile-01",
		DisplayName:     "Atomicity Invalid Profile 01",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile: map[string]any{
			"hostname":  strings.Repeat("h", 256),
			"bogus":     "x",
			"ipAddress": "10.0.0.1",
		},
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for invalid profile fields, got %v", err)
	}
	if ve.Fields["hostname"] == "" || ve.Fields["bogus"] == "" {
		t.Fatalf("expected field-level details for hostname and bogus, got %#v", ve.Fields)
	}
	if len(repo.resources) != 0 {
		t.Fatalf("expected no resource left behind after validation failure, found %d", len(repo.resources))
	}
}
