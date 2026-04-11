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
