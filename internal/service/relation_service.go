// Package service provides business logic for resource relation queries.
// input: internal/model (ResourceRelation)
// output: NewRelationService, RelationService.ListByResourceID, RelationRepository interface
// pos: Business logic for resource relation queries
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrRelationNotFound = errors.New("relation not found")
	ErrRelationConflict = errors.New("relation conflict")
)

type RelationRepository interface {
	ListByResourceID(resourceID string) ([]model.ResourceRelation, error)
	GetResource(id string) (*model.Resource, error)
	CreateRelation(ctx context.Context, input model.RelationCreateInput) (*model.ResourceRelation, error)
	DeleteRelation(ctx context.Context, relationID string) error
}

type RelationService struct {
	repo RelationRepository
}

func NewRelationService(repo RelationRepository) *RelationService {
	return &RelationService{repo: repo}
}

func (s *RelationService) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	return s.repo.ListByResourceID(resourceID)
}

func (s *RelationService) Create(ctx context.Context, fromResourceID string, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	if fromResourceID == "" {
		return nil, fmt.Errorf("%w: from resource id is required", ErrValidationFailed)
	}
	if input.ToResourceID == "" {
		return nil, fmt.Errorf("%w: toResourceId is required", ErrValidationFailed)
	}
	if input.ToResourceID == fromResourceID {
		return nil, fmt.Errorf("%w: self-relations are not supported", ErrValidationFailed)
	}
	if err := input.RelationType.Validate(); err != nil {
		return nil, fmt.Errorf("%w: relationType is not supported", ErrValidationFailed)
	}
	if _, err := s.repo.GetResource(fromResourceID); err != nil {
		return nil, err
	}
	if _, err := s.repo.GetResource(input.ToResourceID); err != nil {
		return nil, err
	}
	input.FromResourceID = fromResourceID
	return s.repo.CreateRelation(ctx, input)
}

func (s *RelationService) Delete(ctx context.Context, relationID string) error {
	if relationID == "" {
		return fmt.Errorf("%w: relation id is required", ErrValidationFailed)
	}
	return s.repo.DeleteRelation(ctx, relationID)
}
