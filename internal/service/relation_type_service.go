// Package service provides static dictionary service for relation type taxonomy.
// input: internal/model (DictionaryItem)
// output: NewRelationTypeService, RelationTypeService.List, RelationTypeRepository interface
// pos: Static dictionary service for relation type taxonomy
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type RelationTypeRepository interface {
	ListRelationTypes() ([]model.DictionaryItem, error)
}

type RelationTypeService struct {
	repo RelationTypeRepository
}

func NewRelationTypeService(repo RelationTypeRepository) *RelationTypeService {
	return &RelationTypeService{repo: repo}
}

func (s *RelationTypeService) List() ([]model.DictionaryItem, error) {
	return s.repo.ListRelationTypes()
}
