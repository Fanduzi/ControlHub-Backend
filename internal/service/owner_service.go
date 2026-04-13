// Package service provides thin service for owner dictionary listing.
// input: internal/model (Owner)
// output: NewOwnerService, OwnerService.List, OwnerRepository interface
// pos: Thin service for owner dictionary listing
// note: if this file changes, update header and README.md
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
