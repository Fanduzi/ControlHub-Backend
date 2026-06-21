// Package service provides business logic for the Phase 37 read-only query sandbox.
// input: context, database/sql, errors, time, internal/model
// output: QueryExecutionService, query execution repository/resolver/executor/clock interfaces, sentinel errors, NewQueryExecutionService, Execute, ListHistory
// pos: Orchestrates guarded MySQL/TiDB SELECT execution — target/policy/guard gating, credential resolution, timed execution, and per-attempt history + audit
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors mapped by the handler to controlled HTTP responses. No guard,
// policy, credential, or target-database validation issue should map to 500.
var (
	ErrQueryValidationFailed = errors.New("query validation failed")
	ErrQueryNotAllowed       = errors.New("query not allowed")
	ErrQueryTargetNotFound   = errors.New("query target not found")
	ErrQueryTimeout          = errors.New("query timed out")
	ErrQueryBackendFailure   = errors.New("query backend error")
)

// ErrQueryResultTooLarge is returned by the executor when a result set exceeds
// the configured column/cell/payload caps. The service maps it to a controlled
// 400 (validation_failed), never a 500.
var ErrQueryResultTooLarge = errors.New("query result exceeds configured limits")

// Execution limits. Default is 5s/500 rows; production is tighter at 3s/100 rows.
const (
	defaultQueryTimeout    = 5 * time.Second
	productionQueryTimeout = 3 * time.Second
	productionHardMaxRows  = 100
)

// QueryExecutionRepository persists query execution history and audit events and
// reads credential metadata. The concrete MySQL implementation lives in
// internal/repository/mysql/query_execution_repository.go.
type QueryExecutionRepository interface {
	GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error)
	InsertExecution(ctx context.Context, rec model.QueryExecutionRecord) (uint64, error)
	ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error)
	InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error
}

// QueryCredentialResolver resolves a validated credential_ref to a DSN. It must
// validate the ref first and fail closed (no env lookup) on an invalid ref. The
// resolved DSN must never appear in an error, log, or response.
type QueryCredentialResolver interface {
	Resolve(ctx context.Context, credentialRef string) (string, error)
}

// QueryDatabaseExecutor runs a guarded SELECT against a target database under
// the provided context and returns the bounded result. It enforces column,
// cell, and payload caps and returns ErrQueryResultTooLarge when they are hit.
type QueryDatabaseExecutor interface {
	Query(ctx context.Context, dsn string, guarded GuardedQuery) (QueryDatabaseResult, error)
}

// QueryDatabaseResult is the bounded result set returned by the executor.
type QueryDatabaseResult struct {
	Columns   []model.QueryResultColumn
	Rows      [][]any
	RowCount  int
	Truncated bool
}

// Clock abstracts time so execution durations and timestamps are deterministic
// in tests.
type Clock interface {
	Now() time.Time
}

// QueryExecutionService orchestrates guarded read-only SELECT execution: it
// gates on target/policy/credential, guards the statement, resolves the
// credential, executes under a bounded timeout, and records history + audit for
// every attempt. The DSN/password never leaves the resolve→execute path.
type QueryExecutionService struct {
	targets     QueryTargetRepository
	executions  QueryExecutionRepository
	credentials QueryCredentialResolver
	executor    QueryDatabaseExecutor
	guard       *QueryGuard
	clock       Clock
}

// NewQueryExecutionService wires the service. targets is reused from the query
// target read model to look up the target under execution.
func NewQueryExecutionService(
	targets QueryTargetRepository,
	executions QueryExecutionRepository,
	credentials QueryCredentialResolver,
	executor QueryDatabaseExecutor,
	guard *QueryGuard,
	clock Clock,
) *QueryExecutionService {
	return &QueryExecutionService{
		targets:     targets,
		executions:  executions,
		credentials: credentials,
		executor:    executor,
		guard:       guard,
		clock:       clock,
	}
}

// Execute runs one guarded SELECT for a target and records the attempt. It
// returns a response on success or a sentinel error otherwise; every reachable
// outcome (rejected, failed, timeout, success) is persisted to history + audit.
// The actor is taken from the verified token by the caller, never from the
// request body.
func (s *QueryExecutionService) Execute(ctx context.Context, actorUserID uint64, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	start := s.clock.Now()

	target, err := s.findTarget(ctx, targetID)
	if err != nil {
		// Unknown target: no history row (no valid target to attribute it to).
		return model.QueryExecuteResponse{}, err
	}

	engine := target.ConnectionContext.Engine
	if !isExecutableEngine(engine) {
		const reason = "engine is not supported for read-only execution"
		s.recordAttempt(ctx, target, actorUserID, nil, model.QueryExecutionRejected, 0, "query_not_allowed", reason, start)
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", ErrQueryNotAllowed, reason)
	}

	cred, err := s.executions.GetCredentialByResourceID(ctx, targetID)
	if err != nil || !credentialAllowsExecution(cred, target.ConnectionContext.Environment) {
		// No credential, invalid ref, disabled, or policy disallows -> locked.
		const reason = "target is not enabled for execution"
		s.recordAttempt(ctx, target, actorUserID, nil, model.QueryExecutionRejected, 0, "query_not_allowed", reason, start)
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", ErrQueryNotAllowed, reason)
	}

	// Production requests are capped tighter before the guard applies its own
	// default/hard-cap logic.
	maxRows := req.MaxRows
	if isProductionEnvironment(target.ConnectionContext.Environment) && (maxRows == 0 || maxRows > productionHardMaxRows) {
		maxRows = productionHardMaxRows
	}
	guarded, err := s.guard.Guard(req.Statement, maxRows)
	if err != nil {
		// The guard error is structural (SQL shape) and carries no DSN, so it is
		// safe to surface as the validation message.
		s.recordAttempt(ctx, target, actorUserID, &guarded, model.QueryExecutionRejected, 0, "validation_failed", err.Error(), start)
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	dsn, err := s.credentials.Resolve(ctx, cred.CredentialRef)
	if err != nil {
		// Resolver rejects invalid refs (fail closed) and unset env keys. The DSN
		// is never included in the recorded or returned message.
		const reason = "credential could not be resolved"
		s.recordAttempt(ctx, target, actorUserID, &guarded, model.QueryExecutionRejected, 0, "query_not_allowed", reason, start)
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", ErrQueryNotAllowed, reason)
	}

	timeout := defaultQueryTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := s.executor.Query(execCtx, dsn, guarded)
	if err != nil {
		status, sentinel, code, safeMsg := classifyExecutorError(err)
		// safeMsg is a fixed string; the raw executor error (which may echo parts
		// of the DSN from the driver) is recorded only internally, never returned.
		s.recordAttempt(ctx, target, actorUserID, &guarded, status, 0, code, safeMsg, start)
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
	}

	// Success: record then return the response carrying the new execution id.
	execID, _ := s.executions.InsertExecution(ctx, s.buildRecord(target, actorUserID, &guarded, model.QueryExecutionSuccess, result.RowCount, "", "", start))
	s.recordAudit(ctx, actorUserID, target.ResourceID, model.QueryExecutionSuccess)

	return model.QueryExecuteResponse{
		ExecutionID:      execID,
		Status:           model.QueryExecutionSuccess,
		TargetResourceID: target.ResourceID,
		Engine:           engine,
		Columns:          result.Columns,
		Rows:             result.Rows,
		RowCount:         result.RowCount,
		Truncated:        result.Truncated,
		DurationMs:       s.clock.Now().Sub(start).Milliseconds(),
		LimitApplied:     guarded.LimitApplied,
		ExecutedAt:       s.clock.Now(),
	}, nil
}

// ListHistory returns execution history (metadata only) for a target with
// pagination metadata.
func (s *QueryExecutionService) ListHistory(ctx context.Context, targetID uint64, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, *model.PageInfo, error) {
	page, pageSize := model.NormalizePagination(q.Page, q.PageSize)
	items, total, err := s.executions.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: page, PageSize: pageSize,
	})
	if err != nil {
		return nil, nil, err
	}
	return items, &model.PageInfo{
		Page:       page,
		PageSize:   pageSize,
		TotalItems: total,
		TotalPages: model.ComputeTotalPages(total, pageSize),
	}, nil
}

// findTarget locates a single query target by id. Phase 37 reuses the list query
// (the read-model interface exposes no single-target getter); the target set is
// small so this O(n) scan is acceptable and avoids widening the repository
// interface. A later phase may add a dedicated lookup.
func (s *QueryExecutionService) findTarget(ctx context.Context, targetID uint64) (model.QueryTarget, error) {
	targets, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{})
	if err != nil {
		return model.QueryTarget{}, ErrQueryTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetID {
			return t, nil
		}
	}
	return model.QueryTarget{}, ErrQueryTargetNotFound
}

// classifyExecutorError maps an executor error to a history status, a sentinel
// for the handler, an audit/error code, and a client-safe message. A timeout is
// 408; an oversized result is 400 (validation); anything else from the target
// database is 502. The returned message is fixed and never echoes the raw
// executor error, which may contain DSN fragments from the driver.
func classifyExecutorError(err error) (model.QueryExecutionStatus, error, string, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return model.QueryExecutionTimeout, ErrQueryTimeout, "query_timeout", "query exceeded the time limit"
	case errors.Is(err, ErrQueryResultTooLarge):
		return model.QueryExecutionRejected, ErrQueryValidationFailed, "validation_failed", "result set exceeds configured limits"
	default:
		return model.QueryExecutionFailed, ErrQueryBackendFailure, "query_backend_error", "target database query failed"
	}
}

// buildRecord assembles a history record. It never includes the DSN or full
// result rows — only the digest, short preview, and outcome metadata.
func (s *QueryExecutionService) buildRecord(target model.QueryTarget, actorUserID uint64, guarded *GuardedQuery, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) model.QueryExecutionRecord {
	rec := model.QueryExecutionRecord{
		TargetResourceID: target.ResourceID,
		ActorUserID:      actorUserID,
		Engine:           target.ConnectionContext.Engine,
		Status:           status,
		RowCount:         rowCount,
		DurationMs:       s.clock.Now().Sub(start).Milliseconds(),
		ErrorCode:        truncateString(code, 64),
		ErrorMessage:     truncateString(msg, 512),
		CreatedAt:        s.clock.Now(),
	}
	if guarded != nil {
		rec.StatementDigest = guarded.StatementDigest
		rec.StatementPreview = guarded.StatementPreview
	}
	return rec
}

// recordAttempt persists a non-success attempt's history row and audit event.
// Best-effort: a persistence failure must not change the returned outcome or
// leak details.
func (s *QueryExecutionService) recordAttempt(ctx context.Context, target model.QueryTarget, actorUserID uint64, guarded *GuardedQuery, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) {
	rec := s.buildRecord(target, actorUserID, guarded, status, rowCount, code, msg, start)
	if _, err := s.executions.InsertExecution(ctx, rec); err != nil {
		// Best-effort; never surface to the client.
		_ = err
	}
	s.recordAudit(ctx, actorUserID, target.ResourceID, status)
}

func (s *QueryExecutionService) recordAudit(ctx context.Context, actorUserID, targetResourceID uint64, status model.QueryExecutionStatus) {
	if err := s.executions.InsertAuditEvent(ctx, actorUserID, targetResourceID, "query.executed", auditResultFor(status)); err != nil {
		_ = err
	}
}

// auditResultFor maps a history status to the audit_events.result vocabulary.
func auditResultFor(status model.QueryExecutionStatus) string {
	switch status {
	case model.QueryExecutionSuccess:
		return "success"
	case model.QueryExecutionTimeout:
		return "timeout"
	case model.QueryExecutionFailed:
		return "failed"
	default:
		return "validation_failed"
	}
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}