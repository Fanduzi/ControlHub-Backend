// Package service provides static dictionary service for resource type taxonomy.
// input: internal/model (DictionaryItem)
// output: NewResourceTypeService, ResourceTypeService.List, ResourceTypeRepository interface
// pos: Static dictionary service for resource type taxonomy
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type ResourceTypeRepository interface {
	ListResourceTypes() ([]model.DictionaryItem, error)
}

type ResourceTypeService struct {
	repo ResourceTypeRepository
}

func NewResourceTypeService(repo ResourceTypeRepository) *ResourceTypeService {
	return &ResourceTypeService{repo: repo}
}

func (s *ResourceTypeService) List() ([]model.DictionaryItem, error) {
	return s.repo.ListResourceTypes()
}
