// Package service provides business logic for the Phase 37 read-only query sandbox.
// input: context, errors, fmt, net, strconv, strings, time, go-sql-driver/mysql, internal/model
// output: QueryExecutionService, query execution repository/resolver/executor/clock interfaces, sentinel errors, NewQueryExecutionService, Execute, ListHistory, validateDSNBinding
// pos: Orchestrates guarded MySQL/TiDB SELECT execution — target/policy/guard gating, credential resolution + target binding, timed execution, and guaranteed per-attempt history + audit
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

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

// errPersistAttempt is returned when an attempt cannot be recorded to history or
// audit. Phase 37 guarantees every attempt is recorded, so a recording failure
// is surfaced as a controlled backend failure (502) rather than a silent
// success. The error carries no database internals.
var errPersistAttempt = fmt.Errorf("%w: could not record query attempt", ErrQueryBackendFailure)

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
// returns a response on success or a sentinel error otherwise. Every reachable
// outcome (rejected, failed, timeout, success) is persisted to history + audit;
// if a recording write fails the request is surfaced as a controlled
// ErrQueryBackendFailure rather than a silent success — Phase 37 never reports
// an unaudited attempt as complete. The actor is taken from the verified token
// by the caller, never from the request body.
func (s *QueryExecutionService) Execute(ctx context.Context, actorUserID uint64, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	start := s.clock.Now()

	target, err := s.findTarget(ctx, targetID)
	if err != nil {
		// Unknown target: no history row (no valid target to attribute it to).
		return model.QueryExecuteResponse{}, err
	}

	engine := target.ConnectionContext.Engine
	if !isExecutableEngine(engine) {
		return s.reject(ctx, target, actorUserID, nil, "query_not_allowed", "engine is not supported for read-only execution",
			fmt.Errorf("%w: engine is not supported for read-only execution", ErrQueryNotAllowed), start)
	}

	cred, err := s.executions.GetCredentialByResourceID(ctx, targetID)
	if err != nil || !credentialAllowsExecution(cred, engine, target.ConnectionContext.Environment) {
		// No credential, invalid ref, disabled, engine mismatch, or policy disallows -> locked.
		return s.reject(ctx, target, actorUserID, nil, "query_not_allowed", "target is not enabled for execution",
			fmt.Errorf("%w: target is not enabled for execution", ErrQueryNotAllowed), start)
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
		return s.reject(ctx, target, actorUserID, &guarded, "validation_failed", err.Error(),
			fmt.Errorf("%w: %v", ErrQueryValidationFailed, err), start)
	}

	dsn, err := s.credentials.Resolve(ctx, cred.CredentialRef)
	if err != nil {
		// Resolver rejects invalid refs (fail closed) and unset env keys. The DSN
		// is never included in the recorded or returned message.
		return s.reject(ctx, target, actorUserID, &guarded, "query_not_allowed", "credential could not be resolved",
			fmt.Errorf("%w: credential could not be resolved", ErrQueryNotAllowed), start)
	}
	// Defense in depth: the resolved DSN must point at the selected target's
	// host/port. A credential misconfigured to another database is fail-closed.
	if err := validateDSNBinding(dsn, target); err != nil {
		return s.reject(ctx, target, actorUserID, &guarded, "query_not_allowed", "credential is not bound to this target",
			fmt.Errorf("%w: credential is not bound to this target", ErrQueryNotAllowed), start)
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
		if _, perr := s.persistAttempt(ctx, target, actorUserID, &guarded, status, 0, code, safeMsg, start); perr != nil {
			return model.QueryExecuteResponse{}, errPersistAttempt
		}
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
	}

	// Success: record (history + audit) then return. A recording failure must
	// not yield a success response, so execID is guaranteed non-zero here.
	execID, perr := s.persistAttempt(ctx, target, actorUserID, &guarded, model.QueryExecutionSuccess, result.RowCount, "", "", start)
	if perr != nil {
		return model.QueryExecuteResponse{}, errPersistAttempt
	}

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

// reject records a rejected attempt and returns the provided error. If the
// attempt cannot be recorded, it returns a controlled errPersistAttempt instead
// — Phase 37 never silently drops an unaudited rejection.
func (s *QueryExecutionService) reject(ctx context.Context, target model.QueryTarget, actorUserID uint64, guarded *GuardedQuery, code, msg string, retErr error, start time.Time) (model.QueryExecuteResponse, error) {
	if _, perr := s.persistAttempt(ctx, target, actorUserID, guarded, model.QueryExecutionRejected, 0, code, msg, start); perr != nil {
		return model.QueryExecuteResponse{}, errPersistAttempt
	}
	return model.QueryExecuteResponse{}, retErr
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

// persistAttempt records one attempt's history row and audit event and returns
// the new execution id, or an error if either write fails. Unlike best-effort
// logging, callers treat a non-nil error as a controlled backend failure so the
// "every attempt is recorded" guarantee holds. The recorded message never
// contains the DSN.
func (s *QueryExecutionService) persistAttempt(ctx context.Context, target model.QueryTarget, actorUserID uint64, guarded *GuardedQuery, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) (uint64, error) {
	rec := s.buildRecord(target, actorUserID, guarded, status, rowCount, code, msg, start)
	id, err := s.executions.InsertExecution(ctx, rec)
	if err != nil {
		return 0, err
	}
	if err := s.executions.InsertAuditEvent(ctx, actorUserID, target.ResourceID, "query.executed", auditResultFor(status)); err != nil {
		return id, err
	}
	return id, nil
}

// validateDSNBinding verifies the resolved DSN points at the selected target's
// host/port. Phase 37 must never run a query against a database other than the
// one the user selected; a credential misconfigured to another host/port is a
// fail-closed condition. The returned error never includes the DSN value.
func validateDSNBinding(dsn string, target model.QueryTarget) error {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return errDSNUnparseable
	}
	if !strings.EqualFold(strings.TrimSpace(cfg.Net), "tcp") {
		return errDSNNotTCP
	}
	// The go-sql-driver normalizes a portless tcp address to :3306 during
	// ParseDSN (ensureHavePort), which would silently bind a `tcp(host)` DSN to
	// host:3306. Phase 37 requires the credential to name an explicit port, so
	// inspect the raw address segment and fail closed when it omits one. The
	// credential DSNs are server-controlled env values, so locating the address
	// via the net prefix is safe here.
	rawAddr, ok := rawAddressFor(dsn, cfg.Net)
	if !ok {
		return errDSNAddressMalformed
	}
	if _, portStr, splitErr := net.SplitHostPort(rawAddr); splitErr != nil || portStr == "" {
		return errDSNPortMissing
	}
	host, portStr, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return errDSNAddressMalformed
	}
	if !engineHostMatches(host, target.ConnectionContext.Host) {
		return errDSNHostMismatch
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return errDSNPortInvalid
	}
	if port != target.ConnectionContext.Port {
		return errDSNPortMismatch
	}
	return nil
}

// rawAddressFor extracts the address segment from a MySQL DSN's `net(addr)`
// authority. It returns ok=false when the DSN carries no explicit address.
func rawAddressFor(dsn, netName string) (string, bool) {
	prefix := netName + "("
	idx := strings.Index(dsn, prefix)
	if idx < 0 {
		return "", false
	}
	rest := dsn[idx+len(prefix):]
	end := strings.IndexByte(rest, ')')
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// DSN-binding errors are fixed strings; they never echo the parsed DSN, which
// contains the credential password.
var (
	errDSNUnparseable     = errors.New("credential dsn is not parseable")
	errDSNNotTCP          = errors.New("credential dsn is not a tcp connection")
	errDSNAddressMalformed = errors.New("credential dsn address is not host:port")
	errDSNHostMismatch    = errors.New("credential dsn host does not match the target")
	errDSNPortMissing     = errors.New("credential dsn port is missing")
	errDSNPortInvalid     = errors.New("credential dsn port is not numeric")
	errDSNPortMismatch    = errors.New("credential dsn port does not match the target")
)

// engineHostMatches compares two host names case-insensitively after trimming.
func engineHostMatches(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
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