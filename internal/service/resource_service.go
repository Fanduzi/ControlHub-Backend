// Package service provides business logic for resource reads and typed profile assembly.
// input: internal/model (Resource, ResourceProfileResponse, ResourceType, ResourceListQuery, PageInfo)
// output: NewResourceService, ResourceService.List/Get/GetProfile, ErrResourceNotFound, ResourceRepository interface
// pos: Business logic for resource reads with pagination support
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"

	"github.com/fan/controlhub/internal/model"
)

var ErrResourceNotFound = errors.New("resource not found")

type ResourceRepository interface {
	ListResources(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, int, error)
	GetResource(id string) (*model.Resource, error)
	GetResourceProfile(id string) (*model.ResourceProfileResponse, error)
}

type ResourceService struct {
	repo ResourceRepository
}

func NewResourceService(repo ResourceRepository) *ResourceService {
	return &ResourceService{repo: repo}
}

func (s *ResourceService) List(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, *model.PageInfo, error) {
	items, total, err := s.repo.ListResources(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	pageInfo := &model.PageInfo{
		Page:       q.Page,
		PageSize:   q.PageSize,
		TotalItems: total,
		TotalPages: model.ComputeTotalPages(total, q.PageSize),
	}
	return items, pageInfo, nil
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
