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

// Navigation-specific sentinel errors. They reuse the handler error mapping
// (ErrQueryValidationFailed → 400, ErrQueryTargetNotFound → 404, etc.) but
// carry distinct messages for navigation failures.
var (
	ErrNavigationSourceNotFound = errors.New("navigation source object or foreign key not found")
	ErrNavigationValueMismatch  = errors.New("localValues count does not match foreign key column count")
)

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
	// QueryRelatedRecords runs a parameterized SELECT built exclusively for
	// related-record navigation. The SQL string is constructed by the service
	// from trusted identifiers only; values are bound via database/sql
	// placeholders (?). This method is not a generic parameterized-query API and
	// cannot be used for normal execution.
	QueryRelatedRecords(ctx context.Context, dsn string, input RelatedRecordsQueryInput) (QueryDatabaseResult, error)
}

// RelatedRecordsQueryInput is the narrow, navigation-only executor input. It
// carries only service-generated SQL and bound local values; it never carries
// referenced identifiers from the request, DSN, credentials, or raw SQL from the
// browser.
type RelatedRecordsQueryInput struct {
	Statement string
	Values    []any
	Limit     int
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

// QueryDisclosurePlanner resolves and applies result-disclosure policies for a
// single query execution. The concrete *QueryDisclosureService satisfies this
// interface; tests substitute a lightweight fake.
type QueryDisclosurePlanner interface {
	Preflight(ctx context.Context, dsn string, targetResourceID uint64, guarded GuardedQuery) (DisclosurePlan, error)
	PreflightRelatedRecords(ctx context.Context, dsn string, targetResourceID uint64, referencedDatabase string, referencedTable string) (DisclosurePlan, error)
	Apply(plan DisclosurePlan, columns []model.QueryResultColumn, rows [][]any) ([]model.QueryResultColumn, [][]any)
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
	access      *TargetAccessResolver
	inspector   QuerySchemaInspector
	disclosure  QueryDisclosurePlanner
}

// NewQueryExecutionService wires the service. targets is reused from the query
// target read model to look up the target under execution. inspector is used
// for FK metadata resolution in related-record navigation.
func NewQueryExecutionService(
	targets QueryTargetRepository,
	executions QueryExecutionRepository,
	credentials QueryCredentialResolver,
	executor QueryDatabaseExecutor,
	guard *QueryGuard,
	clock Clock,
	inspector QuerySchemaInspector,
	disclosure QueryDisclosurePlanner,
) *QueryExecutionService {
	return &QueryExecutionService{
		targets:     targets,
		executions:  executions,
		credentials: credentials,
		executor:    executor,
		guard:       guard,
		clock:       clock,
		access:      NewTargetAccessResolver(targets, executions, credentials),
		inspector:   inspector,
		disclosure:  disclosure,
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

	// Resolve governed target access. The resolver performs: target lookup,
	// engine check, credential validation, policy enforcement, secret
	// resolution, and DSN binding validation — the same checks that
	// InspectCredentialRuntime performs so the two paths never drift.
	access, err := s.access.Resolve(ctx, actorUserID, targetID)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			// Unknown target: no history row (no valid target to attribute it to).
			return model.QueryExecuteResponse{}, err
		}
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			// access.Target is populated even on denial so we can record the
			// rejected attempt. The message is a fixed, leak-free string.
			return s.reject(ctx, access.Target, actorUserID, nil, "query_not_allowed", accessErr.Error(),
				fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErr.Error()), start)
		}
		return model.QueryExecuteResponse{}, err
	}

	target := access.Target
	engine := target.ConnectionContext.Engine

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

	// Disclosure preflight: resolve column provenance and check policies.
	// Blocks if any projected column lacks an exact disclosure policy.
	plan, err := s.disclosure.Preflight(ctx, access.dsn, target.ResourceID, guarded)
	if err != nil {
		if errors.Is(err, ErrQueryDisclosureBlocked) {
			return s.reject(ctx, target, actorUserID, &guarded, "query_result_disclosure_blocked", "query blocked by result disclosure policy",
				fmt.Errorf("%w: %v", ErrQueryNotAllowed, err), start)
		}
		return model.QueryExecuteResponse{}, err
	}

	timeout := defaultQueryTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := s.executor.Query(execCtx, access.dsn, guarded)
	if err != nil {
		status, sentinel, code, safeMsg := classifyExecutorError(err)
		// safeMsg is a fixed string; the raw executor error (which may echo parts
		// of the DSN from the driver) is recorded only internally, never returned.
		if _, perr := s.persistAttempt(ctx, target, actorUserID, &guarded, status, 0, code, safeMsg, start); perr != nil {
			return model.QueryExecuteResponse{}, errPersistAttempt
		}
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
	}

	columns, rows := s.disclosure.Apply(plan, result.Columns, result.Rows)
	result.Columns = columns
	result.Rows = rows

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
// cursor-based pagination. actorRole "admin" sees all target rows; other roles
// see only their own. History is readable without credential readiness — it is
// an audit record. Unknown targets return ErrQueryTargetNotFound (404).
//
// Mode selection is explicit and unambiguous:
//   - No ?page= supplied (Page == 0): cursor mode. The first page has no
//     boundary predicate (Cursor == nil) and starts from the newest row.
//     Continuation pages (Cursor != nil) add a strictly-older-than predicate on
//     (created_at, id). Cursor mode never runs COUNT, never uses OFFSET, and
//     requests PageSize+1 rows to detect whether a next page exists.
//   - ?page= supplied (Page > 0): legacy offset mode with COUNT and OFFSET.
//     NextCursor is nil; PageInfo is populated.
//
// The handler rejects page+cursor together with a 400 validation_failed, so the
// service never sees both.
func (s *QueryExecutionService) ListHistory(ctx context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error) {
	if _, err := s.findTarget(ctx, targetID); err != nil {
		return nil, err
	}

	scope := "all"
	if actorRole != "admin" {
		scope = fmt.Sprintf("user:%d", actorUserID)
	}
	queryHash := model.ComputeQueryHash(targetID, q.Status, q.From, q.To, scope)

	if q.Page > 0 {
		return s.listHistoryOffset(ctx, actorUserID, actorRole, targetID, q)
	}
	return s.listHistoryCursor(ctx, actorUserID, actorRole, targetID, q, queryHash)
}

func (s *QueryExecutionService) listHistoryCursor(ctx context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery, queryHash string) (*model.QueryExecutionCursorPage, error) {
	if q.Cursor != nil {
		if err := model.ValidateCursor(*q.Cursor, queryHash); err != nil {
			return nil, fmt.Errorf("%w: invalid cursor: %v", ErrQueryValidationFailed, err)
		}
	}

	_, pageSize := model.NormalizePagination(0, q.PageSize)
	originalPageSize := pageSize
	pageSize++ // sentinel: request one extra to detect next page

	listQ := model.QueryExecutionListQuery{
		TargetResourceID: targetID,
		PageSize:         pageSize,
		Status:           q.Status,
		From:             q.From,
		To:               q.To,
		Mode:             model.PaginationModeCursor,
		Cursor:           q.Cursor,
	}
	if actorRole != "admin" {
		id := actorUserID
		listQ.ActorUserID = &id
	}

	items, _, err := s.executions.ListExecutions(ctx, listQ)
	if err != nil {
		return nil, err
	}

	for i := range items {
		if items[i].Actor.DisplayName == "" {
			items[i].Actor.DisplayName = model.UnknownHistoryActorDisplayName
		}
	}

	var nextCursor *string
	if len(items) > originalPageSize {
		items = items[:originalPageSize]
		last := items[len(items)-1]
		cs, encErr := model.EncodeCursor(last.CreatedAt, last.ID, queryHash)
		if encErr != nil {
			return nil, fmt.Errorf("%w: encode cursor: %v", ErrQueryBackendFailure, encErr)
		}
		nextCursor = &cs
	}

	return &model.QueryExecutionCursorPage{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

func (s *QueryExecutionService) listHistoryOffset(ctx context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error) {
	page, pageSize := model.NormalizePagination(q.Page, q.PageSize)
	listQ := model.QueryExecutionListQuery{
		TargetResourceID: targetID,
		Page:             page,
		PageSize:         pageSize,
		Status:           q.Status,
		From:             q.From,
		To:               q.To,
		Mode:             model.PaginationModeOffset,
	}
	if actorRole != "admin" {
		id := actorUserID
		listQ.ActorUserID = &id
	}

	items, total, err := s.executions.ListExecutions(ctx, listQ)
	if err != nil {
		return nil, err
	}

	for i := range items {
		if items[i].Actor.DisplayName == "" {
			items[i].Actor.DisplayName = model.UnknownHistoryActorDisplayName
		}
	}

	pageInfo := model.NewPageInfo(page, pageSize, total)
	return &model.QueryExecutionCursorPage{
		Items:    items,
		PageInfo: &pageInfo,
	}, nil
}

func (s *QueryExecutionService) findTarget(ctx context.Context, targetID uint64) (model.QueryTarget, error) {
	targets, _, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetID})
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

// NavigateRelatedRecords executes a governed FK navigation: it resolves the
// target, retrieves FK metadata from the schema inspector, matches the
// constraint, validates referenced identifiers, constructs a parameterized
// SELECT with a server-clamped numeric LIMIT, binds values, and records history
// + audit. The browser never supplies referenced identifiers, SQL, credentials,
// or actor identity. Every attempt is recorded; localValues, result rows, SQL
// text, DSN, and credentials are never persisted.
func (s *QueryExecutionService) NavigateRelatedRecords(ctx context.Context, actorUserID uint64, targetID uint64, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error) {
	start := s.clock.Now()

	// 1. Resolve governed target access (same path as Execute).
	access, err := s.access.Resolve(ctx, actorUserID, targetID)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			return model.RelatedRecordNavigationResponse{}, err
		}
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			// access.Target is populated even on denial so we can record the
			// rejected attempt. Before FK resolution, use fixed generic metadata.
			return s.rejectNavigation(ctx, access.Target, actorUserID, nil,
				"navigation_not_allowed", accessErr.Error(),
				fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErr.Error()), start)
		}
		return model.RelatedRecordNavigationResponse{}, err
	}

	target := access.Target

	// 2. Retrieve source table FK metadata via the schema inspector. The raw
	// inspector error must never be exposed, persisted, or audited.
	detail, err := s.inspector.GetObjectDetails(ctx, access.dsn, req.Source.Database, req.Source.Object, req.Source.Kind)
	if err != nil {
		return s.rejectNavigation(ctx, target, actorUserID, nil,
			"navigation_source_error", "could not retrieve source table metadata",
			ErrNavigationSourceNotFound, start)
	}

	// 3. Match the FK constraint by name.
	var matchedFK *FKSummary
	for i := range detail.ForeignKeys {
		if detail.ForeignKeys[i].Name == req.Source.ForeignKey {
			matchedFK = &detail.ForeignKeys[i]
			break
		}
	}
	if matchedFK == nil {
		return s.rejectNavigation(ctx, target, actorUserID, nil,
			"navigation_fk_not_found", "foreign key not found on source table",
			ErrNavigationSourceNotFound, start)
	}

	// 4. Validate the live FK metadata is structurally sound and consistent.
	if err := validateRelatedRecordsFKMetadata(matchedFK); err != nil {
		return s.rejectNavigation(ctx, target, actorUserID, nil,
			"navigation_metadata_invalid", "foreign key metadata is invalid",
			ErrNavigationSourceNotFound, start)
	}

	// 5. Validate localValues count matches FK column count.
	if len(req.LocalValues) != len(matchedFK.Columns) {
		return s.rejectNavigation(ctx, target, actorUserID, nil,
			"navigation_value_mismatch", "localValues count does not match foreign key column count",
			ErrNavigationValueMismatch, start)
	}

	// 6. Disclosure preflight for related records.
	refSchema := matchedFK.Columns[0].ReferencedSchema
	refTable := matchedFK.Columns[0].ReferencedTable

	plan, err := s.disclosure.PreflightRelatedRecords(ctx, access.dsn, target.ResourceID, refSchema, refTable)
	if err != nil {
		if errors.Is(err, ErrQueryDisclosureBlocked) {
			return s.rejectNavigation(ctx, target, actorUserID, matchedFK, "query_result_disclosure_blocked", "query blocked by result disclosure policy",
				fmt.Errorf("%w: %v", ErrQueryNotAllowed, err), start)
		}
		return model.RelatedRecordNavigationResponse{}, err
	}

	// 7. Build the parameterized SELECT from trusted FK metadata.
	//    SELECT * FROM `<ref_schema>`.`<ref_table>` WHERE `<ref_col1>` = ? AND ... LIMIT <decimal>
	//    Identifiers are backtick-quoted with embedded backticks doubled; values
	//    are bound via database/sql placeholders; the limit is a server-clamped
	//    decimal literal so the placeholder count matches the argument count.

	var whereClauses []string
	for _, col := range matchedFK.Columns {
		whereClauses = append(whereClauses, fmt.Sprintf("%s = ?", quoteMySQLIdentifier(col.ReferencedColumn)))
	}

	limit := req.ClampMaxRows()
	if isProductionEnvironment(target.ConnectionContext.Environment) && limit > productionHardMaxRows {
		limit = productionHardMaxRows
	}

	query := fmt.Sprintf("SELECT * FROM %s.%s WHERE %s LIMIT %d",
		quoteMySQLIdentifier(refSchema), quoteMySQLIdentifier(refTable), strings.Join(whereClauses, " AND "), limit)

	values := make([]any, len(req.LocalValues))
	for i, v := range req.LocalValues {
		values[i] = v
	}

	// 8. Execute with timeout (same policy as Execute).
	timeout := defaultQueryTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 9. Execute the bound query.
	result, err := s.executor.QueryRelatedRecords(execCtx, access.dsn, RelatedRecordsQueryInput{
		Statement: query,
		Values:    values,
		Limit:     limit,
	})
	if err != nil {
		status, sentinel, code, safeMsg := classifyExecutorError(err)
		if _, perr := s.persistNavigationAttempt(ctx, target, actorUserID, matchedFK, status, 0, code, safeMsg, start); perr != nil {
			return model.RelatedRecordNavigationResponse{}, errPersistAttempt
		}
		return model.RelatedRecordNavigationResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
	}

	columns, rows := s.disclosure.Apply(plan, result.Columns, result.Rows)
	result.Columns = columns
	result.Rows = rows

	// 10. Build relation metadata from trusted FK columns.
	refColumns := make([]string, len(matchedFK.Columns))
	for i, col := range matchedFK.Columns {
		refColumns[i] = col.ReferencedColumn
	}

	// 11. Record success using canonical inspected relation identity.
	execID, perr := s.persistNavigationAttempt(ctx, target, actorUserID, matchedFK, model.QueryExecutionSuccess, result.RowCount, "", "", start)
	if perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}

	return model.RelatedRecordNavigationResponse{
		ExecutionID:        execID,
		Status:             model.QueryExecutionSuccess,
		TargetResourceID:   target.ResourceID,
		Engine:             target.ConnectionContext.Engine,
		Columns:            result.Columns,
		Rows:               result.Rows,
		RowCount:           result.RowCount,
		Truncated:          result.Truncated,
		DurationMs:         s.clock.Now().Sub(start).Milliseconds(),
		LimitApplied:       limit,
		ExecutedAt:         s.clock.Now(),
		SourceDatabase:     req.Source.Database,
		SourceObject:       req.Source.Object,
		ForeignKey:         matchedFK.Name,
		ReferencedDatabase: refSchema,
		ReferencedObject:   refTable,
		ReferencedColumns:  refColumns,
	}, nil
}

// rejectNavigation records a rejected navigation attempt and returns the error.
func (s *QueryExecutionService) rejectNavigation(ctx context.Context, target model.QueryTarget, actorUserID uint64, fk *FKSummary, code, msg string, retErr error, start time.Time) (model.RelatedRecordNavigationResponse, error) {
	if _, perr := s.persistNavigationAttempt(ctx, target, actorUserID, fk, model.QueryExecutionRejected, 0, code, msg, start); perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}
	return model.RelatedRecordNavigationResponse{}, retErr
}

// persistNavigationAttempt records a navigation attempt. The recorded action is
// fixed "related_record_navigation" with relation identity metadata only.
// It never stores localValues, result rows, SQL, credentials, or raw errors.
// When fk is nil (trusted resolution has not succeeded), only fixed generic
// metadata is recorded. When fk is non-nil, canonical inspected relation
// identity is used.
func (s *QueryExecutionService) persistNavigationAttempt(ctx context.Context, target model.QueryTarget, actorUserID uint64, fk *FKSummary, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) (uint64, error) {
	var preview, digest string
	if fk == nil {
		// Pre-resolution: fixed generic metadata only. Request-controlled source
		// identity is never persisted until trusted resolution succeeds.
		preview = "related:unresolved"
		digest = "nav:unresolved"
	} else {
		// Post-resolution: canonical inspected relation identity and FK name.
		refSchema := fk.Columns[0].ReferencedSchema
		refTable := fk.Columns[0].ReferencedTable
		preview = fmt.Sprintf("related:%s.%s/%s", refSchema, refTable, fk.Name)
		digest = fmt.Sprintf("nav:%s.%s/%s", refSchema, refTable, fk.Name)
	}

	rec := model.QueryExecutionRecord{
		TargetResourceID: target.ResourceID,
		ActorUserID:      actorUserID,
		Engine:           target.ConnectionContext.Engine,
		StatementDigest:  truncateString(digest, 128),
		StatementPreview: truncateString(preview, 256),
		Status:           status,
		RowCount:         rowCount,
		DurationMs:       s.clock.Now().Sub(start).Milliseconds(),
		ErrorCode:        truncateString(code, 64),
		ErrorMessage:     truncateString(msg, 512),
		CreatedAt:        s.clock.Now(),
	}
	id, err := s.executions.InsertExecution(ctx, rec)
	if err != nil {
		return 0, err
	}
	// Audit event uses fixed action "related_record_navigation" with relation identity.
	auditResult := auditResultFor(status)
	if err := s.executions.InsertAuditEvent(ctx, actorUserID, target.ResourceID, "related_record_navigation", auditResult); err != nil {
		return id, err
	}
	return id, nil
}

// quoteMySQLIdentifier quotes a MySQL identifier with backticks and doubles
// any embedded backticks to prevent identifier injection.
func quoteMySQLIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// validateRelatedRecordsFKMetadata checks that live FK metadata is safe to use
// for query construction. It requires at least one mapping, non-empty local and
// referenced columns, non-empty referenced schema/table, and that every mapping
// points to the same referenced schema/table.
func validateRelatedRecordsFKMetadata(fk *FKSummary) error {
	if fk == nil || len(fk.Columns) == 0 {
		return fmt.Errorf("foreign key has no column mappings")
	}
	refSchema := strings.TrimSpace(fk.Columns[0].ReferencedSchema)
	refTable := strings.TrimSpace(fk.Columns[0].ReferencedTable)
	if refSchema == "" || refTable == "" {
		return fmt.Errorf("foreign key referenced schema or table is empty")
	}
	for i, col := range fk.Columns {
		if strings.TrimSpace(col.Column) == "" {
			return fmt.Errorf("foreign key local column %d is empty", i)
		}
		if strings.TrimSpace(col.ReferencedSchema) == "" || strings.TrimSpace(col.ReferencedTable) == "" {
			return fmt.Errorf("foreign key referenced schema or table is empty at column %d", i)
		}
		if strings.TrimSpace(col.ReferencedColumn) == "" {
			return fmt.Errorf("foreign key referenced column %d is empty", i)
		}
		if !strings.EqualFold(strings.TrimSpace(col.ReferencedSchema), refSchema) || !strings.EqualFold(strings.TrimSpace(col.ReferencedTable), refTable) {
			return fmt.Errorf("foreign key maps to multiple referenced tables")
		}
	}
	return nil
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
	errDSNUnparseable      = errors.New("credential dsn is not parseable")
	errDSNNotTCP           = errors.New("credential dsn is not a tcp connection")
	errDSNAddressMalformed = errors.New("credential dsn address is not host:port")
	errDSNHostMismatch     = errors.New("credential dsn host does not match the target")
	errDSNPortMissing      = errors.New("credential dsn port is missing")
	errDSNPortInvalid      = errors.New("credential dsn port is not numeric")
	errDSNPortMismatch     = errors.New("credential dsn port does not match the target")
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
