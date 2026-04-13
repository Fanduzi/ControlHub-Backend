// Package service provides business logic for resource relation queries.
// input: internal/model (ResourceRelation)
// output: NewRelationService, RelationService.ListByResourceID, RelationRepository interface
// pos: Business logic for resource relation queries
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type RelationRepository interface {
	ListByResourceID(resourceID string) ([]model.ResourceRelation, error)
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
