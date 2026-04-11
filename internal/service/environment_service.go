package service

import "github.com/fan/controlhub/internal/model"

type EnvironmentRepository interface {
	ListEnvironments() ([]model.Environment, error)
}

type EnvironmentService struct {
	repo EnvironmentRepository
}

func NewEnvironmentService(repo EnvironmentRepository) *EnvironmentService {
	return &EnvironmentService{repo: repo}
}

func (s *EnvironmentService) List() ([]model.Environment, error) {
	return s.repo.ListEnvironments()
}
