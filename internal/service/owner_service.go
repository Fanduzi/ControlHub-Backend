package service

import "github.com/fan/controlhub/internal/model"

type OwnerRepository interface {
	ListOwners() ([]model.Owner, error)
}

type OwnerService struct {
	repo OwnerRepository
}

func NewOwnerService(repo OwnerRepository) *OwnerService {
	return &OwnerService{repo: repo}
}

func (s *OwnerService) List() ([]model.Owner, error) {
	return s.repo.ListOwners()
}
