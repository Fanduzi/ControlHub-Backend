// Package service provides business logic for the Phase 37/38S read-only query sandbox.
// input: context, errors, fmt, net, strconv, strings, time, go-sql-driver/mysql, internal/model
// output: QueryExecutionService, validated user/machine Execute identity, repository/resolver/executor/clock interfaces, sentinel errors, ListHistory, validateDSNBinding
// pos: Orchestrates ordinary user/machine governed execution plus user-only template/navigation paths through one atomic identity-aware evidence implementation while preserving cancellation and disclosure behavior
// note: if this file changes, update this header and module README.md.
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

// evidencePersistenceWindow is the fixed two-second Evidence Persistence
// Window (Issue #35): the maximum time an Evidence-Bearing Query Attempt's
// Execution Evidence Pair write may take. The write runs in its own bounded
// context detached from request cancellation and deadline — a client disconnect
// can never drop terminal evidence — and is a single synchronous attempt with
// no retry, queue, worker, or disk buffer.
const evidencePersistenceWindow = 2 * time.Second

type executionEvidenceKind string

const (
	executionEvidenceQuery      executionEvidenceKind = "query"
	executionEvidenceNavigation executionEvidenceKind = "navigation"
)

// QueryExecutionRepository persists query execution history and audit events and
// reads credential metadata. The concrete MySQL implementation lives in
// internal/repository/mysql/query_execution_repository.go.
//
// InsertExecutionWithAudit is the repository-owned atomic Execution Evidence
// Pair (Issue #34): one transaction commits the history row and its fixed
// audit event together — query.executed for core execution (ordinary, paged,
// template) and related_record_navigation for related-record navigation
// (Issue #36). Every Evidence-Bearing Query Attempt must route through it; the
// standalone InsertExecution single-write seam is removed (Issue #36).
// InsertAuditEvent below is the audit-ONLY write for operations that
// intentionally create no execution-history row (explain, schema reads);
// governed execution and navigation never call it.
type QueryExecutionRepository interface {
	GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error)
	ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error)
	// InsertAuditEvent is the audit-ONLY write for operations that intentionally
	// create no execution-history row (explain, schema reads). Governed query
	// execution and related-record navigation never call it — every
	// Evidence-Bearing Query Attempt routes through InsertExecutionWithAudit
	// (Issue #36).
	InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error
	InsertExecutionWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error)
	QueryEvidencePersistenceFailures() int64
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
	// QueryTemplate executes only a compiler-produced guarded template statement.
	// It is not a generic parameterized-query API.
	QueryTemplate(ctx context.Context, dsn string, statement GuardedTemplateStatement) (QueryDatabaseResult, error)
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
	Apply(plan DisclosurePlan, columns []model.QueryResultColumn, rows [][]any) ([]model.QueryResultColumn, [][]any, error)
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
	// statements and compiler are wired via WithTemplateExecution for the
	// saved-statement (template) execution route; nil until then.
	statements QuerySavedStatementReader
	compiler   *TemplateStatementCompiler
}

// QueryEvidencePersistenceFailures exposes the dimensionless persistence-
// failure counter through the service layer (Issue #34), keeping the api
// layer free of repository imports.
func (s *QueryExecutionService) QueryEvidencePersistenceFailures() int64 {
	return s.executions.QueryEvidencePersistenceFailures()
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
func (s *QueryExecutionService) Execute(ctx context.Context, identity model.QueryExecutionIdentity, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	if err := identity.Validate(); err != nil {
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: invalid execution identity", ErrQueryValidationFailed)
	}
	start := s.clock.Now()

	// Resolve governed target access. The resolver performs: target lookup,
	// engine check, credential validation, policy enforcement, secret
	// resolution, and DSN binding validation — the same checks that
	// InspectCredentialRuntime performs so the two paths never drift.
	access, err := s.access.Resolve(ctx, identity.ID, targetID)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			// Unknown target: no history row (no valid target to attribute it to).
			return model.QueryExecuteResponse{}, err
		}
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			// access.Target is populated even on denial so we can record the
			// rejected attempt. The message is a fixed, leak-free string.
			return s.reject(ctx, access.Target, identity, nil, "query_not_allowed", accessErr.Error(),
				fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErr.Error()), start)
		}
		return model.QueryExecuteResponse{}, err
	}

	target := access.Target

	// maxRows is always the overall release cap; the guard owns the 0-default
	// and hard-cap clamping for both paged and non-paged execution.
	maxRows := clampProductionMaxRows(target.ConnectionContext.Environment, req.MaxRows)

	var guarded GuardedQuery
	if req.Pagination != nil {
		if err := model.ValidatePagination(req.Pagination.Page, req.Pagination.PageSize); err != nil {
			return s.reject(ctx, target, identity, &guarded, "validation_failed", err.Error(),
				fmt.Errorf("%w: %v", ErrQueryValidationFailed, err), start)
		}
		guarded, err = s.guard.GuardPaginatedSelect(req.Statement, req.Pagination.Page, req.Pagination.PageSize, maxRows)
		if errors.Is(err, ErrQueryPaginationNotApplicable) {
			guarded, err = s.guard.Guard(req.Statement, maxRows)
		}
	} else {
		guarded, err = s.guard.Guard(req.Statement, maxRows)
	}
	if err != nil {
		// The guard error is structural (SQL shape) and carries no DSN, so it is
		// safe to surface as the validation message.
		return s.reject(ctx, target, identity, &guarded, "validation_failed", err.Error(),
			fmt.Errorf("%w: %v", ErrQueryValidationFailed, err), start)
	}

	// The remaining governed chain — disclosure preflight, timed execution,
	// disclosure apply, history/audit persist, and response build — is shared
	// with template execution so the two paths cannot drift.
	var page, pageSize int
	if req.Pagination != nil {
		page, pageSize = req.Pagination.Page, req.Pagination.PageSize
	}
	return s.executeGuardedChain(ctx, target, identity, access.dsn, &guarded,
		func(execCtx context.Context, dsn string) (QueryDatabaseResult, error) {
			return s.executor.Query(execCtx, dsn, guarded)
		}, page, pageSize, start)
}

// clampProductionMaxRows applies the tighter production release cap before the
// guard applies its own default/hard-cap clamping. It is shared by Execute and
// ExecuteSavedStatement so the cap policy cannot drift.
func clampProductionMaxRows(environment string, requested int) int {
	if isProductionEnvironment(environment) && (requested == 0 || requested > productionHardMaxRows) {
		return productionHardMaxRows
	}
	return requested
}

// executeGuardedChain runs the post-guard governed chain: disclosure preflight,
// a timed executor run, disclosure apply, and the history/audit persist, then
// builds the response. It is shared by Execute and ExecuteSavedStatement so
// template execution reuses the exact ordinary execution chain per page.
func (s *QueryExecutionService) executeGuardedChain(
	ctx context.Context,
	target model.QueryTarget,
	identity model.QueryExecutionIdentity,
	dsn string,
	guarded *GuardedQuery,
	run func(execCtx context.Context, dsn string) (QueryDatabaseResult, error),
	page, pageSize int,
	start time.Time,
) (model.QueryExecuteResponse, error) {
	// Disclosure preflight: resolve column provenance and check policies.
	// Blocks if any projected column lacks an exact disclosure policy.
	plan, err := s.disclosure.Preflight(ctx, dsn, target.ResourceID, *guarded)
	if err != nil {
		// A genuine public disclosure-policy rejection stays rejected. A
		// canceled or deadline-expired disclosure read (blended into the same
		// blocked wrap by the disclosure service) is NOT a policy rejection —
		// it is a terminal failed/timeout client-cancellation or deadline
		// outcome and must reach the shared atomic evidence path (Issue #35).
		if errors.Is(err, ErrQueryDisclosureBlocked) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return s.reject(ctx, target, identity, guarded, "query_result_disclosure_blocked", "query blocked by result disclosure policy",
				fmt.Errorf("%w: %w", ErrQueryNotAllowed, err), start)
		}
		// All other post-target disclosure terminal failures record fixed safe
		// failed or timeout evidence and surface the existing controlled error.
		return s.recordTerminalOutcome(ctx, target, identity, guarded, err, start)
	}

	timeout := defaultQueryTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := run(execCtx, dsn)
	if err != nil {
		return s.recordTerminalOutcome(ctx, target, identity, guarded, err, start)
	}

	columns, rows, applyErr := s.disclosure.Apply(plan, result.Columns, result.Rows)
	if applyErr != nil {
		return s.recordTerminalOutcome(ctx, target, identity, guarded, applyErr, start)
	}
	result.Columns = columns
	result.Rows = rows

	// Success: record (history + audit) then return. A recording failure must
	// not yield a success response, so execID is guaranteed non-zero here.
	execID, perr := s.persistAttempt(ctx, target, identity, guarded, model.QueryExecutionSuccess, result.RowCount, "", "", start)
	if perr != nil {
		return model.QueryExecuteResponse{}, errPersistAttempt
	}

	var pagination *model.QueryExecutePaginationResponse
	if guarded.ResultLimit > 0 {
		offset := (page - 1) * pageSize
		// ResultLimit is the guard-owned effective window; when the release cap
		// truncates the requested pageSize, report the real window instead of
		// overstating what was released.
		pagination = &model.QueryExecutePaginationResponse{
			Page:            page,
			PageSize:        guarded.ResultLimit,
			HasPreviousPage: page > 1,
			HasNextPage:     result.Truncated && offset+guarded.ResultLimit < guarded.LimitApplied,
		}
	}

	return model.QueryExecuteResponse{
		ExecutionID:      execID,
		Status:           model.QueryExecutionSuccess,
		TargetResourceID: target.ResourceID,
		Engine:           target.ConnectionContext.Engine,
		Columns:          result.Columns,
		Rows:             result.Rows,
		RowCount:         result.RowCount,
		Truncated:        result.Truncated,
		DurationMs:       s.clock.Now().Sub(start).Milliseconds(),
		LimitApplied:     guarded.LimitApplied,
		ExecutedAt:       s.clock.Now(),
		Pagination:       pagination,
	}, nil
}

// reject records a rejected attempt and returns the provided error. If the
// attempt cannot be recorded, it returns a controlled errPersistAttempt instead
// — Phase 37 never silently drops an unaudited rejection.
func (s *QueryExecutionService) reject(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, guarded *GuardedQuery, code, msg string, retErr error, start time.Time) (model.QueryExecuteResponse, error) {
	if _, perr := s.persistAttempt(ctx, target, identity, guarded, model.QueryExecutionRejected, 0, code, msg, start); perr != nil {
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
	identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: actorUserID}

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
			return s.rejectNavigation(ctx, access.Target, identity, nil,
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
		// A canceled or deadline-expired inspector read is NOT a governance
		// rejection — it is a terminal failed/timeout client-cancellation or
		// deadline outcome and must reach the shared atomic evidence path
		// (Issue #35 / #36), exactly as disclosure preflight and query
		// execution do. Every other inspector failure stays a rejected
		// navigation_source_error attempt with the public
		// ErrNavigationSourceNotFound contract unchanged (Issue #40).
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return s.recordNavigationTerminalOutcome(ctx, target, identity, nil, err, start)
		}
		return s.rejectNavigation(ctx, target, identity, nil,
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
		return s.rejectNavigation(ctx, target, identity, nil,
			"navigation_fk_not_found", "foreign key not found on source table",
			ErrNavigationSourceNotFound, start)
	}

	// 4. Validate the live FK metadata is structurally sound and consistent.
	if err := validateRelatedRecordsFKMetadata(matchedFK); err != nil {
		return s.rejectNavigation(ctx, target, identity, nil,
			"navigation_metadata_invalid", "foreign key metadata is invalid",
			ErrNavigationSourceNotFound, start)
	}

	// 5. Validate localValues count matches FK column count.
	if len(req.LocalValues) != len(matchedFK.Columns) {
		return s.rejectNavigation(ctx, target, identity, nil,
			"navigation_value_mismatch", "localValues count does not match foreign key column count",
			ErrNavigationValueMismatch, start)
	}

	// 6. Disclosure preflight for related records.
	refSchema := matchedFK.Columns[0].ReferencedSchema
	refTable := matchedFK.Columns[0].ReferencedTable

	plan, err := s.disclosure.PreflightRelatedRecords(ctx, access.dsn, target.ResourceID, refSchema, refTable)
	if err != nil {
		// A genuine public disclosure-policy rejection stays rejected. A
		// canceled or deadline-expired disclosure read is NOT a policy
		// rejection — it is a terminal failed/timeout client-cancellation or
		// deadline outcome and must reach the shared atomic evidence path
		// (Issue #35), exactly as core query execution does.
		if errors.Is(err, ErrQueryDisclosureBlocked) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			return s.rejectNavigation(ctx, target, identity, matchedFK, "query_result_disclosure_blocked", "query blocked by result disclosure policy",
				fmt.Errorf("%w: %w", ErrQueryNotAllowed, err), start)
		}
		// All other post-target disclosure terminal failures record fixed
		// safe failed or timeout evidence and surface the existing controlled
		// error (Issue #36).
		return s.recordNavigationTerminalOutcome(ctx, target, identity, matchedFK, err, start)
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
		return s.recordNavigationTerminalOutcome(ctx, target, identity, matchedFK, err, start)
	}

	columns, rows, applyErr := s.disclosure.Apply(plan, result.Columns, result.Rows)
	if applyErr != nil {
		return s.recordNavigationTerminalOutcome(ctx, target, identity, matchedFK, applyErr, start)
	}
	result.Columns = columns
	result.Rows = rows

	// 10. Build relation metadata from trusted FK columns.
	refColumns := make([]string, len(matchedFK.Columns))
	for i, col := range matchedFK.Columns {
		refColumns[i] = col.ReferencedColumn
	}

	// 11. Record success using canonical inspected relation identity.
	execID, perr := s.persistNavigationAttempt(ctx, target, identity, matchedFK, model.QueryExecutionSuccess, result.RowCount, "", "", start)
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

// recordNavigationTerminalOutcome classifies one post-target navigation
// terminal failure (disclosure preflight, executor, or disclosure apply) and
// records its fixed-safe evidence through the shared atomic pair path,
// returning the controlled response error. The raw error — which may echo DSN
// fragments from the driver — is classified to a fixed status/code/message and
// never returned or persisted; every Evidence-Bearing Query Attempt's terminal
// outcome reaches the shared atomic evidence path (Issue #35 / #36).
func (s *QueryExecutionService) recordNavigationTerminalOutcome(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, fk *FKSummary, err error, start time.Time) (model.RelatedRecordNavigationResponse, error) {
	status, sentinel, code, safeMsg := classifyExecutorError(err)
	if _, perr := s.persistNavigationAttempt(ctx, target, identity, fk, status, 0, code, safeMsg, start); perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}
	return model.RelatedRecordNavigationResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
}

// rejectNavigation records a rejected navigation attempt and returns the error.
func (s *QueryExecutionService) rejectNavigation(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, fk *FKSummary, code, msg string, retErr error, start time.Time) (model.RelatedRecordNavigationResponse, error) {
	if _, perr := s.persistNavigationAttempt(ctx, target, identity, fk, model.QueryExecutionRejected, 0, code, msg, start); perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}
	return model.RelatedRecordNavigationResponse{}, retErr
}

// persistNavigationAttempt records a navigation attempt as one atomic
// Execution Evidence Pair (history row + the fixed related_record_navigation
// audit event) through the repository-owned primitive (Issue #36). It never
// stores localValues, result rows, SQL, credentials, or raw errors. When fk is
// nil (trusted resolution has not succeeded), only fixed generic metadata is
// recorded. When fk is non-nil, canonical inspected relation identity is used.
//
// Issue #35: the pair write runs in the fixed two-second Evidence Persistence
// Window detached from request cancellation/deadline, so a client disconnect
// can never drop navigation evidence; the write is one synchronous bounded
// attempt with no retry, queue, worker, or disk buffer.
func (s *QueryExecutionService) persistNavigationAttempt(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, fk *FKSummary, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) (uint64, error) {
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

	rec := s.buildRecord(target, identity, nil, status, rowCount, code, msg, start)
	rec.StatementDigest = truncateString(digest, 128)
	rec.StatementPreview = truncateString(preview, 256)
	return s.persistEvidencePair(ctx, rec, executionEvidenceNavigation)
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
// 408; an oversized result is 400 (validation); a disclosure policy block is
// 403 with HTTP sentinel ErrQueryDisclosureBlocked (Issue #48); a client
// cancellation is recorded failed/query_canceled with a fixed safe message
// (Issue #35); anything else from the target database is 502. The returned
// message is fixed and never echoes the raw executor error, which may contain
// DSN fragments from the driver.
func classifyExecutorError(err error) (model.QueryExecutionStatus, error, string, string) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return model.QueryExecutionTimeout, ErrQueryTimeout, "query_timeout", "query exceeded the time limit"
	case errors.Is(err, context.Canceled):
		// Client cancellation during query or disclosure work is a terminal
		// attempt outcome, recorded as failed/query_canceled with a fixed safe
		// message — the raw driver error is never persisted or returned.
		return model.QueryExecutionFailed, ErrQueryBackendFailure, "query_canceled", "query canceled"
	case errors.Is(err, ErrQueryDisclosureBackendFailure):
		// Disclosure machinery failure (inspector/read/parse infrastructure),
		// not a policy refusal: a terminal failed attempt with fixed safe
		// evidence (Issue #35 AC 4).
		return model.QueryExecutionFailed, ErrQueryBackendFailure, "query_disclosure_backend_error", "query disclosure governance failed"
	case errors.Is(err, ErrQueryResultTooLarge):
		return model.QueryExecutionRejected, ErrQueryValidationFailed, "validation_failed", "result set exceeds configured limits"
	case errors.Is(err, ErrQueryDisclosureBlocked):
		return model.QueryExecutionRejected, ErrQueryDisclosureBlocked, "query_result_disclosure_blocked", "query blocked by result disclosure policy"
	default:
		return model.QueryExecutionFailed, ErrQueryBackendFailure, "query_backend_error", "target database query failed"
	}
}

// buildRecord assembles a history record. It never includes the DSN or full
// result rows — only the digest, short preview, and outcome metadata.
func (s *QueryExecutionService) buildRecord(target model.QueryTarget, identity model.QueryExecutionIdentity, guarded *GuardedQuery, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) model.QueryExecutionRecord {
	rec := model.QueryExecutionRecord{
		TargetResourceID: target.ResourceID,
		Actor:            model.QueryExecutionActor{Kind: identity.Kind},
		Engine:           target.ConnectionContext.Engine,
		Status:           status,
		RowCount:         rowCount,
		DurationMs:       s.clock.Now().Sub(start).Milliseconds(),
		ErrorCode:        truncateString(code, 64),
		ErrorMessage:     truncateString(msg, 512),
		CreatedAt:        s.clock.Now(),
	}
	if identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = identity.ID
	} else {
		rec.ActorMachinePrincipalID = identity.ID
	}
	if guarded != nil {
		rec.StatementDigest = guarded.StatementDigest
		rec.StatementPreview = guarded.StatementPreview
	}
	return rec
}

// recordTerminalOutcome classifies one post-target terminal failure (executor,
// disclosure preflight, or disclosure apply) and records its fixed-safe
// evidence through the shared atomic path, returning the controlled response
// error. The raw error — which may echo DSN fragments from the driver — is
// classified to a fixed status/code/message and never returned or persisted;
// every Evidence-Bearing Query Attempt's terminal outcome reaches the shared
// atomic evidence path (Issue #35).
func (s *QueryExecutionService) recordTerminalOutcome(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, guarded *GuardedQuery, err error, start time.Time) (model.QueryExecuteResponse, error) {
	status, sentinel, code, safeMsg := classifyExecutorError(err)
	if _, perr := s.persistAttempt(ctx, target, identity, guarded, status, 0, code, safeMsg, start); perr != nil {
		return model.QueryExecuteResponse{}, errPersistAttempt
	}
	return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
}

// persistAttempt records one attempt's Execution Evidence Pair (history row +
// fixed query.executed audit event) through the repository-owned atomic
// primitive and returns the committed execution id, or an error if the pair
// write fails. Unlike best-effort logging, callers treat a non-nil error as a
// controlled backend failure so the "every attempt is recorded" guarantee
// holds. The recorded message never contains the DSN. The repository owns the
// transaction; the service never composes history and audit writes (Issue #34).
//
// Issue #35: the pair write runs in a context detached from request
// cancellation/deadline and bounded to the fixed two-second Evidence
// Persistence Window, so a client disconnect or deadline expiry can never drop
// the terminal evidence. context.WithoutCancel preserves the request's trace
// values while severing the Done channel; the write is one synchronous bounded
// attempt — no retry, queue, worker, or disk buffer. A window expiry or DB
// failure still surfaces the existing controlled backend failure.
func (s *QueryExecutionService) persistAttempt(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, guarded *GuardedQuery, status model.QueryExecutionStatus, rowCount int, code, msg string, start time.Time) (uint64, error) {
	rec := s.buildRecord(target, identity, guarded, status, rowCount, code, msg, start)
	return s.persistEvidencePair(ctx, rec, executionEvidenceQuery)
}

// persistEvidencePair is the single persistence implementation for every
// Evidence-Bearing Query Attempt. The private kind selects fixed server-owned
// audit metadata; callers cannot pass request-controlled event text.
func (s *QueryExecutionService) persistEvidencePair(ctx context.Context, rec model.QueryExecutionRecord, kind executionEvidenceKind) (uint64, error) {
	windowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), evidencePersistenceWindow)
	defer cancel()

	eventType := "query.executed"
	if kind == executionEvidenceNavigation {
		eventType = "related_record_navigation"
	}
	id, err := s.executions.InsertExecutionWithAudit(windowCtx, rec, eventType, auditResultFor(rec.Status))
	if err != nil {
		return 0, err
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
