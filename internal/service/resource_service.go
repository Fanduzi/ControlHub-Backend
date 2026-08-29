// Package service provides business logic for resource reads, writes, and typed profile assembly.
// input: internal/model (Resource, ResourceProfileResponse, ResourceType, ResourceListQuery, PageInfo, ResourceCreateInput)
// output: NewResourceService, ResourceService.List/Get/GetProfile/Create, ErrResourceNotFound, ResourceRepository interface
// pos: Business logic for resource reads with pagination, create-with-profile atomicity, and strict profile field validation including Domain Name/Virtual IP identity
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrResourceNotFound    = errors.New("resource not found")
	ErrResourceConflict    = errors.New("resource conflict")
	ErrResourceArchived    = errors.New("resource archived")
	ErrProfileNotSupported = errors.New("resource type does not support profiles")
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
	// CreateResourceWithProfile inserts the resource and its initial profile in
	// one transaction (all-or-nothing); repositories that cannot run both writes
	// in a single transaction must still fail the create when the profile write
	// fails rather than silently dropping it.
	CreateResourceWithProfile(ctx context.Context, input model.ResourceCreateInput, profile map[string]any) (*model.Resource, error)
	UpdateResource(ctx context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error)
	ArchiveResource(ctx context.Context, id uint64, reason string) (*model.Resource, error)
	UnarchiveResource(ctx context.Context, id uint64) (*model.Resource, error)
}

type ResourceService struct {
	repo ResourceRepository
}

type inventoryAuditResourceRepository interface {
	UpdateResourceWithAudit(ctx context.Context, id uint64, input model.ResourceUpdateInput, actorUserID uint64, eventType string) (*model.Resource, error)
}

func NewResourceService(repo ResourceRepository) *ResourceService {
	return &ResourceService{repo: repo}
}

func (s *ResourceService) List(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, *model.PageInfo, error) {
	items, total, err := s.repo.ListResources(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	pageInfo := model.NewPageInfo(q.Page, q.PageSize, total)
	return items, &pageInfo, nil
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
	input = normalizeResourceCreateInput(input)

	var (
		created *model.Resource
		err     error
	)
	// A submitted profile object (even an empty one) is a profile write
	// request: route it through the atomic path so unsupported resource types
	// are rejected instead of silently dropping the profile.
	if input.Profile != nil {
		created, err = s.repo.CreateResourceWithProfile(ctx, input, input.Profile)
	} else {
		created, err = s.repo.CreateResource(ctx, input)
	}
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *ResourceService) Update(ctx context.Context, id uint64, patch model.ResourcePatchRequest) (*model.Resource, error) {
	input, err := s.validateUpdate(id, patch)
	if err != nil {
		return nil, err
	}
	return s.repo.UpdateResource(ctx, id, input)
}

// UpdateInventory validates an authenticated HTTP mutation and requires the
// repository's fail-closed, same-transaction inventory audit path.
func (s *ResourceService) UpdateInventory(ctx context.Context, actorUserID, id uint64, patch model.ResourcePatchRequest) (*model.Resource, error) {
	if actorUserID == 0 {
		return nil, errors.New("inventory audit actor is required")
	}
	input, err := s.validateUpdate(id, patch)
	if err != nil {
		return nil, err
	}
	repo, ok := s.repo.(inventoryAuditResourceRepository)
	if !ok {
		return nil, errors.New("inventory audit repository is required")
	}
	return repo.UpdateResourceWithAudit(ctx, id, input, actorUserID, "inventory.resource.updated")
}

func (s *ResourceService) validateUpdate(id uint64, patch model.ResourcePatchRequest) (model.ResourceUpdateInput, error) {
	if id == 0 {
		return model.ResourceUpdateInput{}, wrapValidation("resource id is required")
	}
	existing, err := s.Get(id)
	if err != nil {
		return model.ResourceUpdateInput{}, err
	}
	if existing.IsArchived() {
		return model.ResourceUpdateInput{}, ErrResourceArchived
	}
	if patch.HasImmutableFields() {
		return model.ResourceUpdateInput{}, wrapValidation("immutable resource fields cannot be updated")
	}
	if !patch.HasMutableFields() {
		return model.ResourceUpdateInput{}, wrapValidation("at least one mutable field is required")
	}
	if patch.Name != nil {
		if *patch.Name == "" {
			return model.ResourceUpdateInput{}, wrapValidation("name is required")
		}
		if !resourceNamePattern.MatchString(*patch.Name) {
			return model.ResourceUpdateInput{}, wrapValidation("name must be operations-friendly")
		}
	}
	if patch.ResourceSubtype != nil {
		if err := model.ValidateResourceSubtype(string(existing.ResourceType), *patch.ResourceSubtype); err != nil {
			return model.ResourceUpdateInput{}, wrapValidation(err.Error())
		}
	}
	if patch.DisplayName != nil && *patch.DisplayName == "" {
		return model.ResourceUpdateInput{}, wrapValidation("displayName is required")
	}
	if patch.EnvironmentID != nil && *patch.EnvironmentID == 0 {
		return model.ResourceUpdateInput{}, wrapValidation("environmentId is required")
	}
	if patch.OwnerID != nil && *patch.OwnerID == 0 {
		return model.ResourceUpdateInput{}, wrapValidation("ownerId is required")
	}
	if patch.Source != nil && *patch.Source != "manual" {
		return model.ResourceUpdateInput{}, wrapValidation("source must be manual")
	}
	if patch.LifecycleStatus != nil {
		if err := patch.LifecycleStatus.Validate(); err != nil {
			return model.ResourceUpdateInput{}, wrapValidation("lifecycleStatus is not supported")
		}
	}
	if patch.HealthStatus != nil {
		if err := patch.HealthStatus.Validate(); err != nil {
			return model.ResourceUpdateInput{}, wrapValidation("healthStatus is not supported")
		}
	}
	if patch.Labels != nil {
		if err := validateResourceLabels(*patch.Labels); err != nil {
			return model.ResourceUpdateInput{}, wrapValidation(err.Error())
		}
	}
	if patch.EnvironmentID != nil || patch.OwnerID != nil {
		existing, err := s.Get(id)
		if err != nil {
			return model.ResourceUpdateInput{}, err
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
			return model.ResourceUpdateInput{}, err
		}
	}
	return patch.ToUpdateInput(), nil
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
		if len(trimmed) > model.MaxArchiveReasonLength {
			return nil, wrapValidation(fmt.Sprintf("reason must be at most %d characters", model.MaxArchiveReasonLength))
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
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("resourceType", "Resource type is not supported")
	}
	if input.Name == "" {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("name", "Name is required")
	} else if !resourceNamePattern.MatchString(input.Name) {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("name", "Must match pattern: lowercase letters, numbers, dots, hyphens, underscores")
	}
	if input.DisplayName == "" {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("displayName", "Display name is required")
	}
	if input.EnvironmentID == 0 {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("environmentId", "Environment is required")
	}
	if input.OwnerID == 0 {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("ownerId", "Owner is required")
	}
	if err := input.LifecycleStatus.Validate(); err != nil {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("lifecycleStatus", "Lifecycle status is not supported")
	}
	if err := input.HealthStatus.Validate(); err != nil {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("healthStatus", "Health status is not supported")
	}
	if input.Source != "manual" {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("source", "Source must be manual")
	}
	if err := model.ValidateResourceSubtype(string(input.ResourceType), input.ResourceSubtype); err != nil {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("resourceSubtype", err.Error())
	}
	if err := validateResourceLabels(input.Labels); err != nil {
		if ve == nil {
			ve = newValidationError("validation failed")
		}
		ve.WithField("labels", err.Error())
	}

	if input.ResourceType.Validate() == nil {
		fields := input.Profile
		if fields == nil && profileRequiresIdentity(input.ResourceType) {
			fields = map[string]any{}
		}
		if fields != nil {
			if err := validateProfileFields(input.ResourceType, fields, true); err != nil {
				var pe *ValidationError
				if errors.As(err, &pe) {
					if ve == nil {
						ve = newValidationError("validation failed")
					}
					for field, message := range pe.Fields {
						ve.WithField(field, message)
					}
				} else {
					return err // ErrProfileNotSupported for types without a profile table
				}
			}
		}
	}

	if ve != nil {
		return ve
	}
	return nil
}

func profileRequiresIdentity(resourceType model.ResourceType) bool {
	specs, ok := profileFieldSchemas[resourceType]
	if !ok {
		return false
	}
	for _, spec := range specs {
		if spec.required {
			return true
		}
	}
	return false
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

func validateResourceLabels(labels map[string]string) error {
	for key, value := range labels {
		if containsControlChars(key) {
			return fmt.Errorf("label key %q contains control characters", key)
		}
		if containsControlChars(value) {
			return fmt.Errorf("label value for key %q contains control characters", key)
		}
		lowerKey := strings.ToLower(key)
		for _, reserved := range []string{"credential", "password", "token", "dsn", "secret"} {
			if strings.Contains(lowerKey, reserved) {
				return fmt.Errorf("label key %q is reserved for sensitive data", key)
			}
		}
	}
	return nil
}

func containsControlChars(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
