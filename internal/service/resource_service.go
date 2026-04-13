// Package service provides business logic for resource reads and typed profile assembly.
// input: internal/model (Resource, ResourceProfileResponse, ResourceType)
// output: NewResourceService, ResourceService.List/Get/GetProfile, ErrResourceNotFound, ResourceRepository interface
// pos: Business logic for resource reads and typed profile assembly
// note: if this file changes, update header and README.md
package service

import (
	"errors"

	"github.com/fan/controlhub/internal/model"
)

var ErrResourceNotFound = errors.New("resource not found")

type ResourceRepository interface {
	ListResources(resourceType string, environmentID string) ([]model.Resource, error)
	GetResource(id string) (*model.Resource, error)
	GetResourceProfile(id string) (*model.ResourceProfileResponse, error)
}

type ResourceService struct {
	repo ResourceRepository
}

func NewResourceService(repo ResourceRepository) *ResourceService {
	return &ResourceService{repo: repo}
}

func (s *ResourceService) List(resourceType string, environmentID string) ([]model.Resource, error) {
	return s.repo.ListResources(resourceType, environmentID)
}

func (s *ResourceService) Get(id string) (*model.Resource, error) {
	item, err := s.repo.GetResource(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, ErrResourceNotFound
	}
	return item, nil
}

func (s *ResourceService) GetProfile(id string) (*model.ResourceProfileResponse, error) {
	profile, err := s.repo.GetResourceProfile(id)
	if err != nil {
		return nil, err
	}
	return profile, nil
}
