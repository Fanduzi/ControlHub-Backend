// Package service provides tests for resource and relation write flows.
// input: internal/service write APIs, internal/model, testing
// output: TestResourceService* and TestRelationService* functions, including the complete relationship matrix
// pos: Validates resource identity and complete relationship rules before repository persistence
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
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
	getProfileCalls  int
	getProfilesCalls int
}

type emptyCompletenessRelationReader struct{}

func (emptyCompletenessRelationReader) ListRelationsByResourceIDs([]uint64) ([]model.ResourceRelation, error) {
	return nil, nil
}

func newResourceServiceForTest(repo ResourceRepository) *ResourceService {
	return NewResourceService(repo, emptyCompletenessRelationReader{})
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
	f.getProfileCalls++
	if f.getProfileResult == nil {
		return &model.ResourceProfileResponse{Profile: map[string]any{}}, nil
	}
	return f.getProfileResult, nil
}

func (f *fakeResourceWriteRepo) GetResourceProfiles(_ context.Context, ids []uint64) (map[uint64]map[string]any, error) {
	f.getProfilesCalls++
	profiles := make(map[uint64]map[string]any, len(ids))
	for _, id := range ids {
		profiles[id] = map[string]any{}
	}
	return profiles, nil
}

func (f *fakeResourceWriteRepo) CreateResource(_ context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	created := model.Resource{
		ID:                  testResourceCreatedID,
		ResourceType:        input.ResourceType,
		ResourceSubtype:     input.ResourceSubtype,
		Name:                input.Name,
		DisplayName:         input.DisplayName,
		EnvironmentID:       input.EnvironmentID,
		OwnerID:             input.OwnerID,
		LifecycleStatus:     string(input.LifecycleStatus),
		HealthStatus:        string(input.HealthStatus),
		Origin:              input.Origin,
		Aliases:             append([]string(nil), input.Aliases...),
		ExternalIdentifiers: append([]model.ResourceExternalIdentifier(nil), input.ExternalIdentifiers...),
		Source:              input.Source,
		ExternalID:          input.ExternalID,
		Labels:              cloneLabels(input.Labels),
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
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
	if input.Aliases != nil {
		item.Aliases = append([]string(nil), (*input.Aliases)...)
	}
	if input.ExternalIdentifiers != nil {
		item.ExternalIdentifiers = append([]model.ResourceExternalIdentifier(nil), (*input.ExternalIdentifiers)...)
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
	svc := newResourceServiceForTest(repo)

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
		Profile:         instanceIdentityProfile(),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if created.Name != "order-mysql-02-prod" {
		t.Fatalf("expected created resource name, got %s", created.Name)
	}
}

func TestResourceServiceCreateRejectsUnsupportedResourceType(t *testing.T) {
	svc := newResourceServiceForTest(&fakeResourceWriteRepo{})

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
	svc := newResourceServiceForTest(&fakeResourceWriteRepo{})

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
		Profile:         instanceIdentityProfile(),
	})
	if !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("expected ErrEnvironmentNotFound, got %v", err)
	}
}

func TestResourceServiceCreateRejectsMissingOwner(t *testing.T) {
	svc := newResourceServiceForTest(&fakeResourceWriteRepo{})

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
		Profile:         instanceIdentityProfile(),
	})
	if !errors.Is(err, ErrOwnerNotFound) {
		t.Fatalf("expected ErrOwnerNotFound, got %v", err)
	}
}

func TestResourceServiceCreateRejectsDuplicateNameWithinEnvironment(t *testing.T) {
	repo := &fakeResourceWriteRepo{createErr: ErrResourceConflict}
	svc := newResourceServiceForTest(repo)

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
		Profile:         instanceIdentityProfile(),
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
	svc := newResourceServiceForTest(repo)
	rt := model.ResourceTypeHost

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{ResourceType: &rt})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("expected ErrValidationFailed, got %v", err)
	}
}

func TestResourceServiceUpdateRejectsImmutableOrigin(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{testResource1ID: {
		ID: testResource1ID, ResourceType: model.ResourceTypeService, Name: "orders-api", Origin: model.ResourceOriginManual,
	}}}
	svc := newResourceServiceForTest(repo)
	origin := model.ResourceOriginImported

	_, err := svc.Update(context.Background(), testResource1ID, model.ResourcePatchRequest{Origin: &origin})
	if !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("immutable origin error = %v, want validation failure", err)
	}
}

func TestNormalizeResourceIdentity(t *testing.T) {
	input := model.ResourceCreateInput{
		ResourceType: model.ResourceTypeService, Name: "orders-api", DisplayName: "Orders API",
		EnvironmentID: 1, OwnerID: 1, LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus: model.HealthStatusHealthy, Origin: model.ResourceOriginImported,
		Aliases:             []string{" Orders-API ", "orders-api", "Public-Orders"},
		ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: " ServiceNow ", Value: " CI-76 "}},
	}

	normalized := normalizeResourceCreateInput(input)
	if len(normalized.Aliases) != 2 || normalized.Aliases[0] != "orders-api" || normalized.Aliases[1] != "public-orders" {
		t.Fatalf("normalized aliases = %#v", normalized.Aliases)
	}
	if normalized.ExternalIdentifiers[0].System != "servicenow" || normalized.ExternalIdentifiers[0].Value != "CI-76" {
		t.Fatalf("normalized external identifiers = %#v", normalized.ExternalIdentifiers)
	}
}

func TestRelationServiceCreate(t *testing.T) {
	repo := &fakeRelationWriteRepo{
		resources: map[uint64]model.Resource{
			testResource1ID: {ID: testResource1ID, ResourceType: model.ResourceTypeService, Name: "order-api-prod"},
			testResource2ID: {ID: testResource2ID, ResourceType: model.ResourceTypeDatabaseInstance, Name: "order-mysql-prod"},
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

func TestRelationServiceCreateEnforcesRelationshipMatrix(t *testing.T) {
	resourceTypes := []model.ResourceType{
		model.ResourceTypeHost,
		model.ResourceTypeDatabaseInstance,
		model.ResourceTypeDatabaseCluster,
		model.ResourceTypeService,
		model.ResourceTypeDomainName,
		model.ResourceTypeVirtualIP,
		model.ResourceTypeDatabaseProxy,
		model.ResourceTypeControlPlaneComponent,
	}
	relationTypes := []model.RelationType{
		model.RelationTypeMemberOf,
		model.RelationTypeRunsOn,
		model.RelationTypePointsTo,
		model.RelationTypeFronts,
		model.RelationTypeManages,
		model.RelationTypeReplicatesTo,
		model.RelationTypeDependsOn,
	}
	allowed := map[model.RelationType]map[model.ResourceType][]model.ResourceType{
		model.RelationTypeMemberOf: {
			model.ResourceTypeDatabaseInstance: {model.ResourceTypeDatabaseCluster},
		},
		model.RelationTypeRunsOn: {
			model.ResourceTypeService:               {model.ResourceTypeHost},
			model.ResourceTypeDatabaseInstance:      {model.ResourceTypeHost},
			model.ResourceTypeDatabaseProxy:         {model.ResourceTypeHost},
			model.ResourceTypeControlPlaneComponent: {model.ResourceTypeHost},
		},
		model.RelationTypePointsTo: {
			model.ResourceTypeDomainName: {
				model.ResourceTypeVirtualIP,
				model.ResourceTypeService,
				model.ResourceTypeDatabaseProxy,
				model.ResourceTypeDatabaseCluster,
				model.ResourceTypeDatabaseInstance,
			},
		},
		model.RelationTypeFronts: {
			model.ResourceTypeVirtualIP: {
				model.ResourceTypeDatabaseProxy,
				model.ResourceTypeDatabaseCluster,
				model.ResourceTypeDatabaseInstance,
			},
			model.ResourceTypeDatabaseProxy: {
				model.ResourceTypeDatabaseProxy,
				model.ResourceTypeDatabaseCluster,
				model.ResourceTypeDatabaseInstance,
			},
		},
		model.RelationTypeManages: {
			model.ResourceTypeControlPlaneComponent: {
				model.ResourceTypeDatabaseCluster,
				model.ResourceTypeDatabaseInstance,
			},
		},
		model.RelationTypeReplicatesTo: {
			model.ResourceTypeDatabaseInstance: {model.ResourceTypeDatabaseInstance},
		},
	}
	sameEnvironment := map[model.RelationType]bool{
		model.RelationTypeMemberOf:     true,
		model.RelationTypeRunsOn:       true,
		model.RelationTypeFronts:       true,
		model.RelationTypeReplicatesTo: true,
	}

	for _, relationType := range relationTypes {
		for _, fromType := range resourceTypes {
			for _, toType := range resourceTypes {
				pairAllowed := relationType == model.RelationTypeDependsOn || containsResourceType(allowed[relationType][fromType], toType)
				for _, crossEnvironment := range []bool{false, true} {
					wantAllowed := pairAllowed && (!crossEnvironment || !sameEnvironment[relationType])
					t.Run(fmt.Sprintf("%s/%s/%s/cross_environment=%t", relationType, fromType, toType, crossEnvironment), func(t *testing.T) {
						repo := &fakeRelationWriteRepo{resources: map[uint64]model.Resource{
							testResource1ID: {ID: testResource1ID, ResourceType: fromType, EnvironmentID: testEnvID},
							testResource2ID: {ID: testResource2ID, ResourceType: toType, EnvironmentID: testEnvID},
						}}
						if crossEnvironment {
							target := repo.resources[testResource2ID]
							target.EnvironmentID++
							repo.resources[testResource2ID] = target
						}

						_, err := NewRelationService(repo).Create(context.Background(), testResource1ID, model.RelationCreateInput{
							ToResourceID: testResource2ID,
							RelationType: relationType,
						})
						if wantAllowed && err != nil {
							t.Fatalf("expected relation to be allowed, got %v", err)
						}
						if !wantAllowed && !errors.Is(err, ErrValidationFailed) {
							t.Fatalf("expected ErrValidationFailed, got %v", err)
						}
					})
				}
			}
		}
	}

	for _, relationType := range relationTypes {
		t.Run(string(relationType)+"/self", func(t *testing.T) {
			repo := &fakeRelationWriteRepo{resources: map[uint64]model.Resource{
				testResource1ID: {ID: testResource1ID, ResourceType: model.ResourceTypeDatabaseInstance, EnvironmentID: testEnvID},
			}}
			_, err := NewRelationService(repo).Create(context.Background(), testResource1ID, model.RelationCreateInput{
				ToResourceID: testResource1ID,
				RelationType: relationType,
			})
			if !errors.Is(err, ErrValidationFailed) {
				t.Fatalf("expected ErrValidationFailed, got %v", err)
			}
		})
	}
}

func containsResourceType(items []model.ResourceType, want model.ResourceType) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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
			testResource1ID: {ID: testResource1ID, ResourceType: model.ResourceTypeService, Name: "order-api-prod"},
			testResource2ID: {ID: testResource2ID, ResourceType: model.ResourceTypeDatabaseInstance, Name: "order-mysql-prod"},
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
	svc := newResourceServiceForTest(repo)

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
	svc := newResourceServiceForTest(repo)

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
	svc := newResourceServiceForTest(repo)

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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)

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
	svc := newResourceServiceForTest(repo)

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
	svc := newResourceServiceForTest(repo)

	result, err := svc.Unarchive(context.Background(), testResource1ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ArchivedAt != nil {
		t.Fatal("expected archivedAt to remain nil for active resource")
	}
}

func TestResourceServiceCreateRejectsLabelControlCharacters(t *testing.T) {
	svc := newResourceServiceForTest(&fakeResourceWriteRepo{})

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
	svc := newResourceServiceForTest(&fakeResourceWriteRepo{})

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
	svc := newResourceServiceForTest(repo)

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
		Profile:         instanceIdentityProfile(),
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)
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
	svc := newResourceServiceForTest(repo)

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
	svc := newResourceServiceForTest(repo)

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

func TestResourceServiceCreate_EmptyHostProfileRejectedForIdentity(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
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
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for missing host identity, got %v", err)
	}
	if ve.Fields["hostname"] == "" || ve.Fields["ipAddress"] == "" {
		t.Fatalf("expected hostname and ipAddress identity errors, got %#v", ve.Fields)
	}
	if len(repo.resources) != 0 {
		t.Fatalf("expected no resource left behind after identity rejection, found %d", len(repo.resources))
	}
}

func instanceIdentityProfile() map[string]any {
	return map[string]any{
		"engine": "mysql",
		"host":   "db-01.internal",
		"port":   3306,
	}
}

func TestResourceServiceCreate_MinimumManualIdentityMatrix(t *testing.T) {
	tests := []struct {
		name       string
		input      model.ResourceCreateInput
		wantOK     bool
		wantFields []string
	}{
		{
			name: "host complete identity",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm",
				Name: "host-ok", DisplayName: "Host OK", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"hostname": "host-01.internal", "ipAddress": "10.0.0.10"},
			},
			wantOK: true,
		},
		{
			name: "host missing hostname",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm",
				Name: "host-no-name", DisplayName: "Host", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"ipAddress": "10.0.0.10"},
			},
			wantFields: []string{"hostname"},
		},
		{
			name: "host whitespace hostname",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm",
				Name: "host-blank", DisplayName: "Host", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"hostname": "   ", "ipAddress": "10.0.0.10"},
			},
			wantFields: []string{"hostname"},
		},
		{
			name: "host identity only in labels",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeHost, ResourceSubtype: "vm",
				Name: "host-labels", DisplayName: "Host", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Labels: map[string]string{"hostname": "host-01.internal", "ipAddress": "10.0.0.10"},
			},
			wantFields: []string{"hostname", "ipAddress"},
		},
		{
			name: "database instance complete identity",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql",
				Name: "db-ok", DisplayName: "DB OK", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: instanceIdentityProfile(),
			},
			wantOK: true,
		},
		{
			name: "database instance missing port",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "mysql",
				Name: "db-no-port", DisplayName: "DB", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"engine": "mysql", "host": "db-01.internal"},
			},
			wantFields: []string{"port"},
		},
		{
			name: "database cluster complete identity",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeDatabaseCluster, ResourceSubtype: "mysql",
				Name: "cluster-ok", DisplayName: "Cluster OK", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"engine": "mysql", "primaryEndpoint": "cluster.internal:3306"},
			},
			wantOK: true,
		},
		{
			name: "database cluster missing primaryEndpoint",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeDatabaseCluster, ResourceSubtype: "mysql",
				Name: "cluster-no-ep", DisplayName: "Cluster", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"engine": "mysql"},
			},
			wantFields: []string{"primaryEndpoint"},
		},
		{
			name: "service worker complete identity",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeService, ResourceSubtype: "worker",
				Name: "worker-ok", DisplayName: "Worker OK", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"systemName": "order-worker"},
			},
			wantOK: true,
		},
		{
			name: "service missing systemName",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeService, ResourceSubtype: "api",
				Name: "svc-no-system", DisplayName: "Service", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
			},
			wantFields: []string{"systemName"},
		},
		{
			name: "service unknown subtype rejected",
			input: model.ResourceCreateInput{
				ResourceType: model.ResourceTypeService, ResourceSubtype: "ha",
				Name: "svc-bad-sub", DisplayName: "Service", EnvironmentID: testEnvID, OwnerID: testOwnerID,
				LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual",
				Profile: map[string]any{"systemName": "orders"},
			},
			wantFields: []string{"resourceSubtype"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
			svc := newResourceServiceForTest(repo)
			created, err := svc.Create(context.Background(), tt.input)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("expected success, got %v", err)
				}
				if created == nil {
					t.Fatal("expected created resource")
				}
				if tt.input.Profile != nil && repo.profile == nil {
					t.Fatal("expected typed profile to be written, not labels")
				}
				return
			}
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected ValidationError, got %v", err)
			}
			for _, field := range tt.wantFields {
				if ve.Fields[field] == "" {
					t.Fatalf("expected field error for %s, got %#v", field, ve.Fields)
				}
			}
			if len(repo.resources) != 0 {
				t.Fatalf("expected no resource after rejection, found %d", len(repo.resources))
			}
		})
	}
}

func TestResourceServiceCreate_KeepsLabelsAsFreeClassification(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: "vm",
		Name:            "host-labeled",
		DisplayName:     "Host Labeled",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"team": "platform", "tier": "core"},
		Profile:         map[string]any{"hostname": "host-labeled.internal", "ipAddress": "10.0.0.11"},
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if created.Labels["hostname"] != "" || created.Labels["ipAddress"] != "" {
		t.Fatalf("known profile attributes must not be copied into labels, got %#v", created.Labels)
	}
	if created.Labels["team"] != "platform" {
		t.Fatalf("expected free-classification label team=platform, got %#v", created.Labels)
	}
	if repo.profile["hostname"] != "host-labeled.internal" {
		t.Fatalf("expected identity in typed profile, got %#v", repo.profile)
	}
}

func TestResourceServiceCreateDomainNameRequiresNormalizedFQDN(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDomainName,
		ResourceSubtype: "dns",
		Name:            "orders-domain",
		DisplayName:     "Orders Domain",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile:         map[string]any{"fqdn": "Orders.Example.COM."},
	})
	if err != nil {
		t.Fatalf("expected domain name create to succeed, got %v", err)
	}
	if created == nil {
		t.Fatal("expected created resource")
	}
	if repo.profile["fqdn"] != "orders.example.com" {
		t.Fatalf("expected persisted fqdn orders.example.com, got %#v", repo.profile)
	}
}

func TestResourceServiceCreateDomainNameRejectsMissingProfile(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDomainName,
		ResourceSubtype: "dns",
		Name:            "orders-domain-missing",
		DisplayName:     "Orders Domain Missing",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for missing domain name identity, got %v", err)
	}
	if ve.Fields["fqdn"] == "" {
		t.Fatalf("expected fqdn field error, got %#v", ve.Fields)
	}
	if len(repo.resources) != 0 {
		t.Fatalf("expected no resource after missing FQDN, found %d", len(repo.resources))
	}
}

func TestResourceServiceCreateVirtualIPRejectsUnknownSubtypeAndCIDR(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeVirtualIP,
		ResourceSubtype: "anycast",
		Name:            "vip-unknown-subtype",
		DisplayName:     "VIP Unknown Subtype",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile:         map[string]any{"ipAddress": "10.0.0.10"},
	})
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Fields["resourceSubtype"] == "" {
		t.Fatalf("expected resourceSubtype rejection, got %v", err)
	}

	_, err = svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeVirtualIP,
		ResourceSubtype: "floating",
		Name:            "vip-cidr",
		DisplayName:     "VIP CIDR",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile:         map[string]any{"ipAddress": "10.0.0.10/24"},
	})
	if !errors.As(err, &ve) || ve.Fields["ipAddress"] == "" {
		t.Fatalf("expected ipAddress rejection for CIDR, got %v", err)
	}
	if len(repo.resources) != 0 {
		t.Fatalf("expected no resource after invalid virtual IP writes, found %d", len(repo.resources))
	}
}

func TestResourceServiceCreateDatabaseProxyRequiresTypedProfile(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseProxy,
		ResourceSubtype: "proxysql",
		Name:            "orders-proxysql-01",
		DisplayName:     "Orders ProxySQL",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile: map[string]any{
			"technologySubtype": "proxysql",
			"host":              "proxy-prod-01",
			"port":              6033,
			"role":              "standby",
		},
	})
	if err != nil {
		t.Fatalf("expected database_proxy create to succeed, got %v", err)
	}
	if created == nil {
		t.Fatal("expected created resource")
	}
	if repo.profile["technologySubtype"] != "proxysql" || repo.profile["role"] != "standby" {
		t.Fatalf("expected persisted proxy profile, got %#v", repo.profile)
	}

	_, err = svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseProxy,
		ResourceSubtype: "proxysql",
		Name:            "orders-proxysql-missing",
		DisplayName:     "Orders ProxySQL Missing",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected ValidationError for missing proxy identity, got %v", err)
	}
	if ve.Fields["host"] == "" || ve.Fields["port"] == "" || ve.Fields["role"] == "" {
		t.Fatalf("expected host/port/role field errors, got %#v", ve.Fields)
	}
}

func TestResourceServiceCreateControlPlaneRejectsAmbiguousHA(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

	_, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeControlPlaneComponent,
		ResourceSubtype: "ha",
		Name:            "ha-manager-ambiguous",
		DisplayName:     "HA Manager Ambiguous",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile: map[string]any{
			"componentSubtype": "ha",
			"endpoint":         "http://ha:10008",
			"role":             "active",
		},
	})
	if err == nil {
		t.Fatal("ambiguous ha subtype must be rejected")
	}

	created, err := svc.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeControlPlaneComponent,
		ResourceSubtype: "ha_monitor",
		Name:            "ha-monitor-prod",
		DisplayName:     "HA Monitor",
		EnvironmentID:   testEnvID,
		OwnerID:         testOwnerID,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Profile: map[string]any{
			"componentSubtype": "ha_monitor",
			"endpoint":         "http://ha-monitor:10008",
			"role":             "active",
		},
	})
	if err != nil {
		t.Fatalf("expected ha_monitor create to succeed, got %v", err)
	}
	if created == nil {
		t.Fatal("expected created resource")
	}
	if repo.profile["componentSubtype"] != "ha_monitor" {
		t.Fatalf("expected persisted ha_monitor profile, got %#v", repo.profile)
	}
}

func TestResourceServiceCreateRejectsInvalidProfileFields(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := newResourceServiceForTest(repo)

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
