// Package service provides static dictionary service for lifecycle status taxonomy.
// input: internal/model (DictionaryItem)
// output: NewLifecycleStatusService, LifecycleStatusService.List, LifecycleStatusRepository interface
// pos: Static dictionary service for lifecycle status taxonomy
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type LifecycleStatusRepository interface {
	ListLifecycleStatuses() ([]model.DictionaryItem, error)
}

type LifecycleStatusService struct {
	repo LifecycleStatusRepository
}

func NewLifecycleStatusService(repo LifecycleStatusRepository) *LifecycleStatusService {
	return &LifecycleStatusService{repo: repo}
}

func (s *LifecycleStatusService) List() ([]model.DictionaryItem, error) {
	return s.repo.ListLifecycleStatuses()
}
