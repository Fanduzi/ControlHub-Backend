// Package service provides static dictionary service for lifecycle status taxonomy.
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
