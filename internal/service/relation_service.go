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
