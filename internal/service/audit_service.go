// Package service provides business logic for audit event queries.
package service

import (
	"context"

	"github.com/fan/controlhub/internal/model"
)

type AuditRepository interface {
	ListAuditEvents(ctx context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error)
	ListByResourceID(resourceID uint64) ([]model.AuditEvent, error)
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
	pageInfo := model.NewPageInfo(q.Page, q.PageSize, total)
	return items, &pageInfo, nil
}

func (s *AuditService) ListByResourceID(resourceID uint64) ([]model.AuditEvent, error) {
	return s.repo.ListByResourceID(resourceID)
}
