// Package service provides business logic for audit event queries.
// input: internal/model (AuditEvent)
// output: NewAuditService, AuditService.ListAll/ListByResourceID, AuditRepository interface
// pos: Business logic for audit event queries
// note: if this file changes, update header and README.md
package service

import "github.com/fan/controlhub/internal/model"

type AuditRepository interface {
	ListAll() ([]model.AuditEvent, error)
	ListByResourceID(resourceID string) ([]model.AuditEvent, error)
}

type AuditService struct {
	repo AuditRepository
}

func NewAuditService(repo AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

func (s *AuditService) ListAll() ([]model.AuditEvent, error) {
	return s.repo.ListAll()
}

func (s *AuditService) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	return s.repo.ListByResourceID(resourceID)
}
