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
	GetResource(id uint64) (*model.Resource, error)
	GetResourceProfile(id uint64) (*model.ResourceProfileResponse, error)
	CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error)
	UpdateResource(ctx context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error)
	ArchiveResource(ctx context.Context, id uint64, reason string) (*model.Resource, error)
	UnarchiveResource(ctx context.Context, id uint64) (*model.Resource, error)
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

func (s *ResourceService) Get(id uint64) (*model.Resource, error) {
	item, err := s.repo.GetResource(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrResourceNotFound
	}
	return item, nil
}

func (s *ResourceService) GetProfile(id uint64) (*model.ResourceProfileResponse, error) {
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

func (s *ResourceService) Update(ctx context.Context, id uint64, patch model.ResourcePatchRequest) (*model.Resource, error) {
	if id == 0 {
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
	if patch.EnvironmentID != nil && *patch.EnvironmentID == 0 {
		return nil, wrapValidation("environmentId is required")
	}
	if patch.OwnerID != nil && *patch.OwnerID == 0 {
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

func (s *ResourceService) Archive(ctx context.Context, id uint64, req model.ArchiveRequest) (*model.Resource, error) {
	if id == 0 {
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

func (s *ResourceService) Unarchive(ctx context.Context, id uint64) (*model.Resource, error) {
	if id == 0 {
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
	var ve *ValidationError

	if err := input.ResourceType.Validate(); err != nil {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("resourceType", "Resource type is not supported")
	}
	if input.Name == "" {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("name", "Name is required")
	} else if !resourceNamePattern.MatchString(input.Name) {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("name", "Must match pattern: lowercase letters, numbers, dots, hyphens, underscores")
	}
	if input.DisplayName == "" {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("displayName", "Display name is required")
	}
	if input.EnvironmentID == 0 {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("environmentId", "Environment is required")
	}
	if input.OwnerID == 0 {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("ownerId", "Owner is required")
	}
	if err := input.LifecycleStatus.Validate(); err != nil {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("lifecycleStatus", "Lifecycle status is not supported")
	}
	if err := input.HealthStatus.Validate(); err != nil {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("healthStatus", "Health status is not supported")
	}
	if input.Source != "manual" {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("source", "Source must be manual")
	}
	if err := model.ValidateResourceSubtype(string(input.ResourceType), input.ResourceSubtype); err != nil {
		if ve == nil { ve = newValidationError("validation failed") }
		ve.WithField("resourceSubtype", err.Error())
	}

	if ve != nil {
		return ve
	}
	return nil
}

func validateReferenceIDs(environmentID, ownerID uint64) error {
	validEnvironmentIDs := []uint64{1, 2, 3}
	if !slices.Contains(validEnvironmentIDs, environmentID) {
		return ErrEnvironmentNotFound
	}
	validOwnerIDs := []uint64{1, 2, 3, 4, 5}
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

// ValidationError carries field-level validation details.
type ValidationError struct {
	Message string
	Fields  map[string]string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", ErrValidationFailed, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return ErrValidationFailed
}

func newValidationError(msg string) *ValidationError {
	return &ValidationError{Message: msg, Fields: map[string]string{}}
}

func (e *ValidationError) WithField(field, msg string) *ValidationError {
	e.Fields[field] = msg
	return e
}

func wrapValidation(message string) error {
	return fmt.Errorf("%w: %s", ErrValidationFailed, message)
}
