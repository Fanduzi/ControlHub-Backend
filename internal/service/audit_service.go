// Package service provides business logic for audit event queries.
// input: internal/model (AuditEvent, AuditListQuery, PageInfo)
// output: NewAuditService, AuditService.List/ListByResourceID, AuditRepository interface
// pos: Business logic for audit event queries with pagination support
// note: if this file changes, update header and README.md
package service

import (
	"context"

	"github.com/fan/controlhub/internal/model"
)

type AuditRepository interface {
	ListAuditEvents(ctx context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error)
	ListByResourceID(resourceID string) ([]model.AuditEvent, error)
}

type AuditService struct {
	repo AuditRepository
}

func NewAuditService(repo AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

func (s *AuditService) List(ctx context.Context, q model.AuditListQuery) ([]model.AuditEvent, *model.PageInfo, error) {
	items, total, err := s.repo.ListAuditEvents(ctx, q)
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

func (s *AuditService) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	return s.repo.ListByResourceID(resourceID)
}
