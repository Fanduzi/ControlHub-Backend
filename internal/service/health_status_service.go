// Package service provides static dictionary service for health status taxonomy.
// input: internal/model (DictionaryItem)
// output: NewHealthStatusService, HealthStatusService.List, HealthStatusRepository interface
// pos: Static dictionary service for health status taxonomy
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type HealthStatusRepository interface {
	ListHealthStatuses() ([]model.DictionaryItem, error)
}

type HealthStatusService struct {
	repo HealthStatusRepository
}

func NewHealthStatusService(repo HealthStatusRepository) *HealthStatusService {
	return &HealthStatusService{repo: repo}
}

func (s *HealthStatusService) List() ([]model.DictionaryItem, error) {
	return s.repo.ListHealthStatuses()
}
