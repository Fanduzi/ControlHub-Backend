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
