// Package service tests audit event query delegation.
// input: context, testing, internal/model
// output: AuditService environment-filter forwarding regression test
// pos: Service contract coverage for global audit list queries
// note: if this file changes, update this header and README.md.
package service

import (
	"context"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type captureAuditRepository struct {
	query model.AuditListQuery
}

func (r *captureAuditRepository) ListAuditEvents(_ context.Context, q model.AuditListQuery) ([]model.AuditEvent, int, error) {
	r.query = q
	return nil, 0, nil
}

func (*captureAuditRepository) ListByResourceID(uint64) ([]model.AuditEvent, error) {
	return nil, nil
}

func TestAuditServiceListPassesEnvironmentIDUnchanged(t *testing.T) {
	repo := &captureAuditRepository{}
	environmentID := uint64(7)
	query := model.AuditListQuery{EnvironmentID: &environmentID, Page: 1, PageSize: 20}

	if _, _, err := NewAuditService(repo).List(context.Background(), query); err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if repo.query.EnvironmentID != query.EnvironmentID {
		t.Fatalf("environment ID = %v, want unchanged pointer %v", repo.query.EnvironmentID, query.EnvironmentID)
	}
}
