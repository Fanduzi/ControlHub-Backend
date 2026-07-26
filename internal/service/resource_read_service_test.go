// Package service provides tests for resource read operations.
// input: internal/service ResourceService.List/Get/GetProfile, internal/model, testing
// output: TestResourceRead* functions
// pos: Validates resource read pagination and error handling
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// ---------------------------------------------------------------------------
// Read-specific fakes
// ---------------------------------------------------------------------------

// fakeResourceReadRepo wraps fakeResourceWriteRepo to override read methods
// with controllable error behavior for error-propagation tests.
type fakeResourceReadRepo struct {
	*fakeResourceWriteRepo

	listErr       error
	getErr        error
	getProfileErr error
}

func (f *fakeResourceReadRepo) ListResources(_ context.Context, _ model.ResourceListQuery) ([]model.Resource, int, error) {
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	return f.fakeResourceWriteRepo.ListResources(context.Background(), model.ResourceListQuery{})
}

func (f *fakeResourceReadRepo) GetResource(id uint64) (*model.Resource, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.fakeResourceWriteRepo.GetResource(id)
}

func (f *fakeResourceReadRepo) GetResourceProfile(id uint64) (*model.ResourceProfileResponse, error) {
	if f.getProfileErr != nil {
		return nil, f.getProfileErr
	}
	return f.fakeResourceWriteRepo.GetResourceProfile(id)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestResourceReadListReturnsItemsWithPageInfo(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{
		testResource1ID: {ID: testResource1ID, Name: "alpha"},
		testResource2ID: {ID: testResource2ID, Name: "beta"},
	}}
	svc := NewResourceService(repo)

	items, pageInfo, err := svc.List(context.Background(), model.ResourceListQuery{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if pageInfo == nil {
		t.Fatal("expected pageInfo, got nil")
	}
	if pageInfo.TotalItems != 2 {
		t.Fatalf("expected totalItems 2, got %d", pageInfo.TotalItems)
	}
	if pageInfo.TotalPages != 1 {
		t.Fatalf("expected totalPages 1, got %d", pageInfo.TotalPages)
	}
	if pageInfo.Page != 1 {
		t.Fatalf("expected page 1, got %d", pageInfo.Page)
	}
	if pageInfo.PageSize != 20 {
		t.Fatalf("expected pageSize 20, got %d", pageInfo.PageSize)
	}
	if pageInfo.HasNextPage {
		t.Fatalf("expected hasNextPage false on single page, got true")
	}
	if pageInfo.HasPreviousPage {
		t.Fatalf("expected hasPreviousPage false on first page, got true")
	}
}

func TestResourceReadListEmptyWhenNoMatch(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	items, pageInfo, err := svc.List(context.Background(), model.ResourceListQuery{
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(items))
	}
	if pageInfo.TotalItems != 0 {
		t.Fatalf("expected totalItems 0, got %d", pageInfo.TotalItems)
	}
	if pageInfo.TotalPages != 0 {
		t.Fatalf("expected totalPages 0, got %d", pageInfo.TotalPages)
	}
}

func TestResourceReadListPropagatesRepoError(t *testing.T) {
	repoErr := errors.New("connection refused")
	repo := &fakeResourceReadRepo{
		fakeResourceWriteRepo: &fakeResourceWriteRepo{},
		listErr:               repoErr,
	}
	svc := NewResourceService(repo)

	_, _, err := svc.List(context.Background(), model.ResourceListQuery{Page: 1, PageSize: 20})
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

func TestResourceReadGetReturnsResourceWhenFound(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{
		testResource1ID: {ID: testResource1ID, Name: "order-mysql-prod"},
	}}
	svc := NewResourceService(repo)

	item, err := svc.Get(testResource1ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if item.ID != testResource1ID {
		t.Fatalf("expected ID %d, got %d", testResource1ID, item.ID)
	}
	if item.Name != "order-mysql-prod" {
		t.Fatalf("expected name order-mysql-prod, got %s", item.Name)
	}
}

func TestResourceReadGetReturnsNotFoundForMissing(t *testing.T) {
	repo := &fakeResourceWriteRepo{resources: map[uint64]model.Resource{}}
	svc := NewResourceService(repo)

	_, err := svc.Get(testMissingID)
	if !errors.Is(err, ErrResourceNotFound) {
		t.Fatalf("expected ErrResourceNotFound, got %v", err)
	}
}

func TestResourceReadGetPropagatesRepoError(t *testing.T) {
	repoErr := errors.New("timeout")
	repo := &fakeResourceReadRepo{
		fakeResourceWriteRepo: &fakeResourceWriteRepo{},
		getErr:                repoErr,
	}
	svc := NewResourceService(repo)

	_, err := svc.Get(testResource1ID)
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetProfile
// ---------------------------------------------------------------------------

func TestResourceReadGetProfileReturnsProfileWhenFound(t *testing.T) {
	profile := &model.ResourceProfileResponse{
		ResourceID:   testResource1ID,
		ResourceType: model.ResourceTypeDatabaseInstance,
		Profile:      map[string]any{"engine": "mysql"},
	}
	repo := &fakeResourceWriteRepo{getProfileResult: profile}
	svc := NewResourceService(repo)

	result, err := svc.GetProfile(testResource1ID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.ResourceID != testResource1ID {
		t.Fatalf("expected resourceID %d, got %d", testResource1ID, result.ResourceID)
	}
	if result.Profile["engine"] != "mysql" {
		t.Fatalf("expected profile engine mysql, got %v", result.Profile["engine"])
	}
}

func TestResourceReadGetProfileReturnsNilWhenNotFound(t *testing.T) {
	repo := &fakeResourceWriteRepo{}
	svc := NewResourceService(repo)

	result, err := svc.GetProfile(testMissingID)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// fakeResourceWriteRepo returns empty-profile response when getProfileResult is nil
	if result == nil {
		t.Fatal("expected non-nil profile response")
	}
}

func TestResourceReadGetProfilePropagatesRepoError(t *testing.T) {
	repoErr := errors.New("disk error")
	repo := &fakeResourceReadRepo{
		fakeResourceWriteRepo: &fakeResourceWriteRepo{},
		getProfileErr:         repoErr,
	}
	svc := NewResourceService(repo)

	_, err := svc.GetProfile(testResource1ID)
	if !errors.Is(err, repoErr) {
		t.Fatalf("expected repo error, got %v", err)
	}
}
