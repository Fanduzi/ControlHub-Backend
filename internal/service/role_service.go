// Package service provides thin service for role dictionary listing.
// input: internal/model (Role)
// output: NewRoleService, RoleService.List, RoleRepository interface
// pos: Thin service for role dictionary listing
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type RoleRepository interface {
	ListRoles() ([]model.Role, error)
}

type RoleService struct {
	repo RoleRepository
}

func NewRoleService(repo RoleRepository) *RoleService {
	return &RoleService{repo: repo}
}

func (s *RoleService) List() ([]model.Role, error) {
	return s.repo.ListRoles()
}
