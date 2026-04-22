// Package service provides business logic for resource reads and typed profile assembly.
// input: internal/model (Resource, ResourceProfileResponse, ResourceType, ResourceListQuery, PageInfo)
// output: NewResourceService, ResourceService.List/Get/GetProfile, ErrResourceNotFound, ResourceRepository interface
// pos: Business logic for resource reads with pagination support
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrResourceNotFound    = errors.New("resource not found")
	ErrResourceConflict    = errors.New("resource conflict")
	ErrResourceArchived    = errors.New("resource archived")
	ErrEnvironmentNotFound = errors.New("environment not found")
	ErrOwnerNotFound       = errors.New("owner not found")
	ErrValidationFailed    = errors.New("validation failed")
)

var resourceNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

type ResourceRepository interface {
	ListResources(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, int, error)
	GetResource(id string) (*model.Resource, error)
	GetResourceProfile(id string) (*model.ResourceProfileResponse, error)
	CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error)
	UpdateResource(ctx context.Context, id string, input model.ResourceUpdateInput) (*model.Resource, error)
	ArchiveResource(ctx context.Context, id string, reason string) (*model.Resource, error)
	UnarchiveResource(ctx context.Context, id string) (*model.Resource, error)
}

type ResourceService struct {
	repo       ResourceRepository
	profileSvc *ProfileService
}

func NewResourceService(repo ResourceRepository, profileSvc ...*ProfileService) *ResourceService {
	var ps *ProfileService
	if len(profileSvc) > 0 {
		ps = profileSvc[0]
	}
	return &ResourceService{repo: repo, profileSvc: ps}
}

func (s *ResourceService) List(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, *model.PageInfo, error) {
	items, total, err := s.repo.ListResources(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	pageInfo := &model.PageInfo{
		Page:       q.Page,
		PageSize:   q.PageSize,
		TotalItems: total,
		TotalPages: model.ComputeTotalPages(total, q.PageSize),
	}
	return items, pageInfo, nil
}

func (s *ResourceService) Get(id string) (*model.Resource, error) {
	item, err := s.repo.GetResource(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrResourceNotFound
	}
	return item, nil
}

func (s *ResourceService) GetProfile(id string) (*model.ResourceProfileResponse, error) {
	profile, err := s.repo.GetResourceProfile(id)
	if err != nil {
		return nil, err
	}
	return profile, nil
}

func (s *ResourceService) Create(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	if err := validateResourceCreateInput(input); err != nil {
		return nil, err
	}
	if err := validateReferenceIDs(input.EnvironmentID, input.OwnerID); err != nil {
		return nil, err
	}
	created, err := s.repo.CreateResource(ctx, normalizeResourceCreateInput(input))
	if err != nil {
		return nil, err
	}
	if len(input.Profile) > 0 && s.profileSvc != nil {
		_ = s.profileSvc.PutProfile(ctx, created.ID, input.Profile)
	}
	return created, nil
}

func (s *ResourceService) Update(ctx context.Context, id string, patch model.ResourcePatchRequest) (*model.Resource, error) {
	if id == "" {
		return nil, wrapValidation("resource id is required")
	}
	existing, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if existing.IsArchived() {
		return nil, ErrResourceArchived
	}
	if patch.HasImmutableFields() {
		return nil, wrapValidation("immutable resource fields cannot be updated")
	}
	if !patch.HasMutableFields() {
		return nil, wrapValidation("at least one mutable field is required")
	}
	if patch.Name != nil {
		if *patch.Name == "" {
			return nil, wrapValidation("name is required")
		}
		if !resourceNamePattern.MatchString(*patch.Name) {
			return nil, wrapValidation("name must be operations-friendly")
		}
	}
	if patch.ResourceSubtype != nil {
		if err := model.ValidateResourceSubtype(string(existing.ResourceType), *patch.ResourceSubtype); err != nil {
			return nil, wrapValidation(err.Error())
		}
	}
	if patch.DisplayName != nil && *patch.DisplayName == "" {
		return nil, wrapValidation("displayName is required")
	}
	if patch.EnvironmentID != nil && *patch.EnvironmentID == "" {
		return nil, wrapValidation("environmentId is required")
	}
	if patch.OwnerID != nil && *patch.OwnerID == "" {
		return nil, wrapValidation("ownerId is required")
	}
	if patch.Source != nil && *patch.Source != "manual" {
		return nil, wrapValidation("source must be manual")
	}
	if patch.LifecycleStatus != nil {
		if err := patch.LifecycleStatus.Validate(); err != nil {
			return nil, wrapValidation("lifecycleStatus is not supported")
		}
	}
	if patch.HealthStatus != nil {
		if err := patch.HealthStatus.Validate(); err != nil {
			return nil, wrapValidation("healthStatus is not supported")
		}
	}
	if patch.EnvironmentID != nil || patch.OwnerID != nil {
		existing, err := s.Get(id)
		if err != nil {
			return nil, err
		}
		environmentID := existing.EnvironmentID
		ownerID := existing.OwnerID
		if patch.EnvironmentID != nil {
			environmentID = *patch.EnvironmentID
		}
		if patch.OwnerID != nil {
			ownerID = *patch.OwnerID
		}
		if err := validateReferenceIDs(environmentID, ownerID); err != nil {
			return nil, err
		}
	}
	updated, err := s.repo.UpdateResource(ctx, id, patch.ToUpdateInput())
	if err != nil {
		return nil, err
	}
	return updated, nil
}

func (s *ResourceService) Archive(ctx context.Context, id string, req model.ArchiveRequest) (*model.Resource, error) {
	if id == "" {
		return nil, wrapValidation("resource id is required")
	}
	if req.Reason != nil {
		trimmed := strings.TrimSpace(*req.Reason)
		if trimmed == "" {
			return nil, wrapValidation("reason must be non-empty")
		}
	}
	existing, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if existing.IsArchived() {
		return existing, nil
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	return s.repo.ArchiveResource(ctx, id, reason)
}

func (s *ResourceService) Unarchive(ctx context.Context, id string) (*model.Resource, error) {
	if id == "" {
		return nil, wrapValidation("resource id is required")
	}
	existing, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if !existing.IsArchived() {
		return existing, nil
	}
	return s.repo.UnarchiveResource(ctx, id)
}

func validateResourceCreateInput(input model.ResourceCreateInput) error {
	if err := input.ResourceType.Validate(); err != nil {
		return wrapValidation("resourceType is not supported")
	}
	if input.Name == "" {
		return wrapValidation("name is required")
	}
	if !resourceNamePattern.MatchString(input.Name) {
		return wrapValidation("name must be operations-friendly")
	}
	if input.DisplayName == "" {
		return wrapValidation("displayName is required")
	}
	if input.EnvironmentID == "" {
		return wrapValidation("environmentId is required")
	}
	if input.OwnerID == "" {
		return wrapValidation("ownerId is required")
	}
	if err := input.LifecycleStatus.Validate(); err != nil {
		return wrapValidation("lifecycleStatus is not supported")
	}
	if err := input.HealthStatus.Validate(); err != nil {
		return wrapValidation("healthStatus is not supported")
	}
	if input.Source != "manual" {
		return wrapValidation("source must be manual")
	}
	if err := model.ValidateResourceSubtype(string(input.ResourceType), input.ResourceSubtype); err != nil {
		return wrapValidation(err.Error())
	}
	return nil
}

func validateReferenceIDs(environmentID, ownerID string) error {
	validEnvironmentIDs := []string{
		"env-prod",
		"env-staging",
		"10000000-0000-0000-0000-000000000001",
		"10000000-0000-0000-0000-000000000002",
		"10000000-0000-0000-0000-000000000003",
	}
	if !slices.Contains(validEnvironmentIDs, environmentID) {
		return ErrEnvironmentNotFound
	}
	validOwnerIDs := []string{
		"owner-dba",
		"owner-platform",
		"20000000-0000-0000-0000-000000000001",
		"20000000-0000-0000-0000-000000000002",
		"20000000-0000-0000-0000-000000000003",
		"20000000-0000-0000-0000-000000000004",
		"20000000-0000-0000-0000-000000000005",
	}
	if !slices.Contains(validOwnerIDs, ownerID) {
		return ErrOwnerNotFound
	}
	return nil
}

func normalizeResourceCreateInput(input model.ResourceCreateInput) model.ResourceCreateInput {
	if input.Labels == nil {
		input.Labels = map[string]string{}
	}
	return input
}

func wrapValidation(message string) error {
	return fmt.Errorf("%w: %s", ErrValidationFailed, message)
}
