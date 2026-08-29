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
	ListByResourceID(resourceID uint64) ([]model.ResourceRelation, error)
	ListRelationViewsByResourceID(resourceID uint64) ([]model.ResourceRelationView, error)
	ListClusterMembers(clusterID uint64) ([]model.ClusterMemberView, error)
	GetResource(id uint64) (*model.Resource, error)
	CreateRelation(ctx context.Context, input model.RelationCreateInput) (*model.ResourceRelation, error)
	DeleteRelation(ctx context.Context, relationID uint64) error
}

type RelationService struct {
	repo RelationRepository
}

type inventoryAuditRelationRepository interface {
	CreateRelationWithAudit(ctx context.Context, input model.RelationCreateInput, actorUserID uint64, eventType string) (*model.ResourceRelation, error)
	DeleteRelationWithAudit(ctx context.Context, relationID, actorUserID uint64, eventType string) error
}

func NewRelationService(repo RelationRepository) *RelationService {
	return &RelationService{repo: repo}
}

func (s *RelationService) ListByResourceID(resourceID uint64) ([]model.ResourceRelation, error) {
	return s.repo.ListByResourceID(resourceID)
}

func (s *RelationService) ListRelationViewsByResourceID(resourceID uint64) ([]model.ResourceRelationView, error) {
	return s.repo.ListRelationViewsByResourceID(resourceID)
}

func (s *RelationService) ListClusterMembers(clusterID uint64) ([]model.ClusterMemberView, error) {
	return s.repo.ListClusterMembers(clusterID)
}

func (s *RelationService) Create(ctx context.Context, fromResourceID uint64, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	return s.create(ctx, 0, fromResourceID, input)
}

func (s *RelationService) CreateInventory(ctx context.Context, actorUserID, fromResourceID uint64, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	return s.create(ctx, actorUserID, fromResourceID, input)
}

func (s *RelationService) create(ctx context.Context, actorUserID, fromResourceID uint64, input model.RelationCreateInput) (*model.ResourceRelation, error) {
	if fromResourceID == 0 {
		return nil, fmt.Errorf("%w: from resource id is required", ErrValidationFailed)
	}
	if input.ToResourceID == 0 {
		return nil, fmt.Errorf("%w: toResourceId is required", ErrValidationFailed)
	}
	if input.ToResourceID == fromResourceID {
		return nil, fmt.Errorf("%w: self-relations are not supported", ErrValidationFailed)
	}
	if err := input.RelationType.Validate(); err != nil {
		return nil, fmt.Errorf("%w: relationType is not supported", ErrValidationFailed)
	}
	fromResource, err := s.repo.GetResource(fromResourceID)
	if err != nil {
		return nil, err
	}
	if fromResource.IsArchived() {
		return nil, ErrResourceArchived
	}
	toResource, err := s.repo.GetResource(input.ToResourceID)
	if err != nil {
		return nil, err
	}
	if toResource.IsArchived() {
		return nil, ErrResourceArchived
	}
	input.FromResourceID = fromResourceID
	if actorUserID != 0 {
		repo, ok := s.repo.(inventoryAuditRelationRepository)
		if !ok {
			return nil, fmt.Errorf("inventory audit relation repository is required")
		}
		return repo.CreateRelationWithAudit(ctx, input, actorUserID, "inventory.relationship.created")
	}
	return s.repo.CreateRelation(ctx, input)
}

func (s *RelationService) Delete(ctx context.Context, relationID uint64) error {
	if relationID == 0 {
		return fmt.Errorf("%w: relation id is required", ErrValidationFailed)
	}
	return s.repo.DeleteRelation(ctx, relationID)
}

func (s *RelationService) DeleteInventory(ctx context.Context, actorUserID, relationID uint64) error {
	if relationID == 0 {
		return fmt.Errorf("%w: relation id is required", ErrValidationFailed)
	}
	if actorUserID == 0 {
		return fmt.Errorf("inventory audit actor is required")
	}
	repo, ok := s.repo.(inventoryAuditRelationRepository)
	if !ok {
		return fmt.Errorf("inventory audit relation repository is required")
	}
	return repo.DeleteRelationWithAudit(ctx, relationID, actorUserID, "inventory.relationship.deleted")
}
