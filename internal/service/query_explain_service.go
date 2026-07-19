// Package service provides the governed Explain service for Phase 38N.
// input: context, errors, fmt, time, internal/model
// output: QueryExplainService, NewQueryExplainService, ExplainAuditRecorder (interface), QueryExplainAuditRepository (interface)
// pos: Orchestrates governed target access, SELECT-only guard, typed Explain executor, normalization, and fixed audit
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// Explain timeouts mirror the execute path's bounded policy. Production gets
// a tighter cap. Explain is cheaper than execute (no row scan), but the
// deadline still protects against a hung target database.
const (
	defaultExplainTimeout    = 10 * time.Second
	productionExplainTimeout = 5 * time.Second
)

// QueryExplainAuditRecorder is the narrow audit boundary for Explain. The
// single method accepts the typed ExplainAuditOutcome enum — there is no raw
// string overload, so arbitrary callers cannot inject free-form text at the
// audit boundary (Oracle P2.6).
type QueryExplainAuditRecorder interface {
	RecordExplainAttempt(ctx context.Context, actorUserID, targetResourceID uint64, outcome model.ExplainAuditOutcome) error
}

// QueryExplainService orchestrates governed Explain: it resolves target
// access through the same path as execution, checks engine capability,
// guards the statement through the narrow SELECT-only entry, calls the typed
// Explain executor with a sealed ExplainStatement, normalizes the raw plan
// into the v1 bounded model, and records a fixed audit event. It never
// executes the bare SELECT and never creates a query_executions row.
type QueryExplainService struct {
	guard      *QueryGuard
	access     *TargetAccessResolver
	executor   QueryExplainExecutor
	normalizer *ExplainNormalizer
	clock      Clock
	audit      QueryExplainAuditRecorder
}

// NewQueryExplainService wires the service. guard is the same concrete
// *QueryGuard used by QueryExecutionService (shared parser + AST walker).
// access is the shared TargetAccessResolver. audit may be nil to disable
// audit recording (tests use this); production wires a real recorder.
func NewQueryExplainService(
	guard *QueryGuard,
	access *TargetAccessResolver,
	executor QueryExplainExecutor,
	normalizer *ExplainNormalizer,
	clock Clock,
	audit QueryExplainAuditRecorder,
) *QueryExplainService {
	return &QueryExplainService{
		guard:      guard,
		access:     access,
		executor:   executor,
		normalizer: normalizer,
		clock:      clock,
		audit:      audit,
	}
}

// Explain runs one governed EXPLAIN for a target and returns the normalized
// v1 response. The order (design line 35-46):
//
//  1. Resolve governed target access (same path as execute).
//  2. Verify the target engine supports the v1 normalizer (MySQL only).
//  3. Guard the statement through the narrow SELECT-only entry.
//  4. Call the typed Explain executor with a sealed ExplainStatement.
//  5. Normalize the raw plan into the bounded v1 model.
//  6. Record a fixed audit event (best-effort).
//
// The service holds no reference to QueryExecutionRepository and never calls
// InsertExecution — this is a structural guarantee that Explain cannot create
// a query_executions row.
func (s *QueryExplainService) Explain(ctx context.Context, actorUserID uint64, targetID uint64, req model.ExplainRequest) (model.ExplainResponse, error) {
	access, err := s.access.Resolve(ctx, actorUserID, targetID)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			return model.ExplainResponse{}, err
		}
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			s.recordAudit(ctx, actorUserID, access.Target.ResourceID, model.ExplainAuditRejected)
			return model.ExplainResponse{}, fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErr.Error())
		}
		return model.ExplainResponse{}, err
	}

	target := access.Target

	if !isExplainEngine(target.ConnectionContext.Engine) {
		s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditUnsupported)
		return model.ExplainResponse{}, ErrQueryExplainNotSupported
	}

	guarded, err := s.guard.GuardExplainSelect(req.Statement)
	if err != nil {
		s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditRejected)
		return model.ExplainResponse{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	timeout := defaultExplainTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionExplainTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	raw, err := s.executor.Explain(execCtx, access.dsn, NewExplainStatement(guarded.ExecutableSQL))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditError)
			return model.ExplainResponse{}, ErrQueryTimeout
		}
		if errors.Is(err, ErrQueryExplainNotSupported) {
			s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditUnsupported)
			return model.ExplainResponse{}, ErrQueryExplainNotSupported
		}
		s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditError)
		return model.ExplainResponse{}, ErrQueryBackendFailure
	}

	result, err := s.normalizer.Normalize(raw)
	if err != nil {
		s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditUnsupported)
		return model.ExplainResponse{}, ErrQueryExplainNotSupported
	}

	s.recordAudit(ctx, actorUserID, target.ResourceID, model.ExplainAuditSuccess)

	return model.ExplainResponse{
		TargetResourceID: target.ResourceID,
		Engine:           model.ExplainEngineMySQL,
		FormatVersion:    model.ExplainFormatVersion,
		Nodes:            result.Nodes,
		Risks:            result.Risks,
		Truncated:        result.Truncated,
	}, nil
}

// recordAudit is best-effort. Unlike execute's persist-or-fail policy, an
// unaudited Explain attempt does not create the silent-data-access risk that
// justifies execute's strictness — Explain returns no rows and only
// sanitized optimizer metadata. The deviation is documented in the spec.
func (s *QueryExplainService) recordAudit(ctx context.Context, actorUserID, targetResourceID uint64, outcome model.ExplainAuditOutcome) {
	if s.audit == nil {
		return
	}
	_ = s.audit.RecordExplainAttempt(ctx, actorUserID, targetResourceID, outcome)
}

// isExplainEngine reports whether an engine supports the v1 Explain
// normalizer. MySQL only in v1 (compatibility spike: TiDB v8.5 rejects
// EXPLAIN FORMAT=JSON). This is strictly narrower than isExecutableEngine,
// which includes TiDB.
func isExplainEngine(engine string) bool {
	switch normalizeEngineName(engine) {
	case "mysql":
		return true
	}
	return false
}

// normalizeEngineName lowercases and trims the engine string for comparison.
func normalizeEngineName(engine string) string {
	return strings.ToLower(strings.TrimSpace(engine))
}
