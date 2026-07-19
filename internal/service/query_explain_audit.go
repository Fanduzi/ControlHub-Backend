// Package service provides the audit recorder for Phase 38N Explain.
// input: context, internal/model, internal/repository/mysql (via QueryExecutionRepository)
// output: ExplainAuditRecorder (implements QueryExplainAuditRecorder)
// pos: Records fixed query.explain audit events with no statement/plan/error text
// note: if this file changes, update header and README.md
package service

import (
	"context"

	"github.com/fan/controlhub/internal/model"
)

// ExplainAuditEventRepository is the narrow write boundary the Explain audit
// recorder needs. It mirrors the InsertAuditEvent method already on
// QueryExecutionRepository so the Explain recorder can reuse the same
// audit_events table without depending on the execute service.
type ExplainAuditEventRepository interface {
	InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error
}

// ExplainAuditRecorder is the production implementation of
// QueryExplainAuditRecorder. It writes a fixed query.explain event with a
// fixed outcome enum string. The recorder carries NO statement, digest,
// preview, plan, normalized node, risk, literal, credential, DSN, actor ID
// beyond the numeric FK, or driver error.
type ExplainAuditRecorder struct {
	repo ExplainAuditEventRepository
}

// NewExplainAuditRecorder wires the recorder with the shared audit repo.
func NewExplainAuditRecorder(repo ExplainAuditEventRepository) *ExplainAuditRecorder {
	return &ExplainAuditRecorder{repo: repo}
}

// RecordExplainAttempt writes one fixed audit event. The event type is the
// literal "query.explain"; the result is the typed enum string. The call is
// best-effort at the service layer; errors are returned so the caller can
// decide whether to log them.
func (r *ExplainAuditRecorder) RecordExplainAttempt(ctx context.Context, actorUserID, targetResourceID uint64, outcome model.ExplainAuditOutcome) error {
	if err := outcome.Validate(); err != nil {
		return err
	}
	return r.repo.InsertAuditEvent(ctx, actorUserID, targetResourceID, "query.explain", string(outcome))
}
