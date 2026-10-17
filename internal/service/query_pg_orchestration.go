// Package service orchestrates the internal keyed PostgreSQL execution entry.
// input: context, errors, fmt, strings, time, internal/model,
// internal/pgsql
// output: ExecutePGClaimed, PGClaimedExecuteRequest, PGClaimedOutcome,
// ErrQueryExecutionAlreadyExists, ErrQueryExecutionConflict,
// WithPGClaimOrchestration
// pos: T10-C production claim orchestration (issue #118, frozen G9 order) —
// request validation → access resolution → default resolution → request
// digest → atomic TryClaimExecution → governed PG disclosure outcome →
// exactly one FinalizeClaimWithAudit → internal result. This entry is not an
// HTTP route; its result never serializes to a client response.
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// Claim-admission sentinels surfaced by the keyed orchestrator. They are
// distinct from persistence failures: a refused admission executes nothing
// and writes nothing.
var (
	// ErrQueryExecutionAlreadyExists marks a same-key, same-digest retry:
	// the earlier attempt already owns the outcome; the caller should read
	// the keyed state instead of re-executing.
	ErrQueryExecutionAlreadyExists = errors.New("execution_already_exists")
	// ErrQueryExecutionConflict marks a same-key, different-digest request:
	// a different request already owns the idempotency key.
	ErrQueryExecutionConflict = errors.New("client_execution_id_conflict")
)

// Resolved admission defaults for the keyed PG entry. The page always
// resolves (pagination is mandatory inside the governed chain); maxRows
// resolves to the workbench default and clamps at the shared hard bound —
// production tightens to productionHardMaxRows through the existing cap.
const (
	pgClaimDefaultPageSize = 50
	pgClaimDefaultMaxRows  = 100
	pgClaimHardMaxRows     = 500
)

// PGClaimedExecuteRequest is the typed internal input of the keyed ordinary
// PG entry. The actor is never part of it — the trusted identity arrives as a
// separate argument. Capabilities are accepted for the future delivery-gate
// seam only: they never reach the request digest, and this entry performs no
// client delivery.
type PGClaimedExecuteRequest struct {
	ClientExecutionID string
	Database          string
	// Schema selects the pinned schema; empty resolves to the connection's
	// configured default_schema. A non-empty schema outside the pinned
	// connection identity is refused during access resolution.
	Schema    string
	Statement string
	Page      int
	PageSize  int
	MaxRows   int
	// Capabilities is reserved for the post-finalize delivery gate (T8-PG);
	// it is not part of the claim identity and changes no behavior here.
	Capabilities []string
}

// PGClaimedOutcome is the internal result of a keyed PG execution. Result is
// the disclosed result only on a delivered success; Evidence always carries
// the T10-B attempt scalars (backend identity, remote_state, classified
// status) — never fabricated.
type PGClaimedOutcome struct {
	ExecutionID uint64
	Status      model.QueryExecutionStatus
	Result      *PGDisclosedResult
	Evidence    *PGAttemptEvidence
}

// pgClaimStore is the narrow repository seam the keyed orchestrator needs:
// atomic admission plus the keyed finalize primitive. The concrete MySQL
// repository satisfies it; no SQL is duplicated here.
type pgClaimStore interface {
	TryClaimExecution(ctx context.Context, in model.QueryExecutionClaimInput) error
	FinalizeClaimWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error)
}

// WithPGClaimOrchestration wires the disclosure policy reader the keyed PG
// entry needs for its provenance-bound preflight. The concrete reader is the
// same five-part canonical store T9-B consumes.
func (s *QueryExecutionService) WithPGClaimOrchestration(policies QueryDisclosureReader) *QueryExecutionService {
	s.pgPolicies = policies
	return s
}

// ExecutePGClaimed runs one ordinary keyed PostgreSQL execution through the
// frozen G9 order: validate → resolve access → resolve defaults → digest →
// atomic claim → governed disclosure outcome → exactly one keyed finalize.
// The idempotency key is consumed only after access resolution succeeds, so
// shape failures and access denials never occupy it. Every post-claim
// terminal outcome — guard/binding/disclosure rejection, cancellation,
// timeout, driver failure, or success — is persisted by the sole claimant's
// keyed finalize; the claim is never released or re-occupied.
//
// Access denials after a resolved target write one NULL-key rejected pair
// along the existing refusal contract; a claim-store failure writes one
// NULL-key failed pair and never lets the client key masquerade as the
// occupant. A finalize failure returns the safe persistence error: the claim
// stays discoverable, non-retryable, and no compensating pair is written.
func (s *QueryExecutionService) ExecutePGClaimed(ctx context.Context, identity model.QueryExecutionIdentity, targetID uint64, req PGClaimedExecuteRequest) (PGClaimedOutcome, error) {
	// 1. Request shape / trusted identity — before any access work. A
	//    malformed request consumes no key and writes no evidence pair.
	if err := identity.Validate(); err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: invalid execution identity", ErrQueryValidationFailed)
	}
	if req.ClientExecutionID == "" {
		return PGClaimedOutcome{}, fmt.Errorf("%w: clientExecutionId is required", ErrQueryValidationFailed)
	}
	if err := model.ValidateClientExecutionID(req.ClientExecutionID); err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if strings.IndexByte(req.ClientExecutionID, 0) >= 0 {
		return PGClaimedOutcome{}, fmt.Errorf("%w: clientExecutionId must not contain NUL bytes", ErrQueryValidationFailed)
	}
	if strings.TrimSpace(req.Statement) == "" {
		return PGClaimedOutcome{}, fmt.Errorf("%w: statement is required", ErrQueryValidationFailed)
	}
	page, pageSize, err := resolvePGClaimPagination(req.Page, req.PageSize)
	if err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if req.MaxRows < 0 {
		return PGClaimedOutcome{}, fmt.Errorf("%w: maxRows must not be negative", ErrQueryValidationFailed)
	}
	if s.pgPolicies == nil {
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	claimStore, ok := s.executions.(pgClaimStore)
	if !ok {
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	start := s.clock.Now()

	// 2. Access resolution — target, connection row, enabled, environment
	//    policy, credential_ref, DSN binding. The key is never touched while
	//    access is unresolved.
	access, err := s.access.ResolvePGExecution(ctx, targetID, req.Database)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			return PGClaimedOutcome{}, err
		}
		if errors.Is(err, ErrSchemaValidationFailed) {
			// An empty database names no connection identity — a request-shape
			// failure: nothing occupied, nothing executed, no evidence pair.
			return PGClaimedOutcome{}, fmt.Errorf("%w: database is required", ErrQueryValidationFailed)
		}
		// Target resolved but access refused (incl. non-PG engine, missing or
		// disabled connection, policy block, binding mismatch): record the
		// existing NULL-key rejected pair; the key stays unoccupied so a
		// later, permitted request can claim it.
		return s.rejectPGClaimedAccess(ctx, access.Target, identity, req, accessErrMessage(err), start)
	}
	target := access.Target
	cred := access.Credential

	// 3. Default resolution — schema falls back to the pinned default; the
	//    claim identity then binds the resolved scope. An explicit schema
	//    outside the pinned connection identity names a connection context
	//    that does not exist — access denial, no claim.
	schema := req.Schema
	if schema == "" {
		schema = cred.DefaultSchema
	}
	if schema != cred.DefaultSchema {
		return s.rejectPGClaimedAccess(ctx, target, identity, req, "target is not enabled for execution", start)
	}
	maxRows := resolvePGClaimMaxRows(target.ConnectionContext.Environment, req.MaxRows)
	timeout := defaultQueryTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}

	// 4. Request digest over resolved values — the statement's original bytes
	//    and never capabilities/clientExecutionId.
	digest := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: cred.DatabaseName, Schema: schema, Statement: req.Statement,
		Page: page, PageSize: pageSize, MaxRows: maxRows,
	})

	// 5. Atomic admission — the single INSERT that decides occupancy.
	claim := model.QueryExecutionClaimInput{
		TargetResourceID:  targetID,
		ClientExecutionID: req.ClientExecutionID,
		Identity:          identity,
		DatabaseName:      cred.DatabaseName,
		SchemaName:        schema,
		RequestDigest:     digest,
		AttemptTimeoutMs:  timeout.Milliseconds(),
		ClaimedAt:         s.clock.Now(),
	}
	if err := claimStore.TryClaimExecution(ctx, claim); err != nil {
		switch {
		case errors.Is(err, model.ErrQueryExecutionClaimExists):
			return PGClaimedOutcome{}, ErrQueryExecutionAlreadyExists
		case errors.Is(err, model.ErrQueryExecutionClaimConflict):
			return PGClaimedOutcome{}, ErrQueryExecutionConflict
		default:
			// Non-duplicate admission failure: no user SQL ran, and the key
			// must not masquerade as occupied — record the NULL-key failed
			// pair and surface the backend failure. Bounded single attempt.
			return s.failPGClaimedAccess(ctx, target, identity, req, start)
		}
	}

	// 6. Governed attempt — the T9-B disclosure-decorated chain arms its own
	//    statement budget inside the remaining request context.
	res, ev, execErr := ExecutePGGovernedDisclosedOutcome(ctx, PGDisclosureInput{
		Execute: PGExecuteInput{
			ConnConfig:   access.pgConfig,
			Database:     cred.DatabaseName,
			PinnedSchema: schema,
			Statement:    req.Statement,
			Page:         pgsql.PageOptions{Page: page, PageSize: pageSize, MaxRows: maxRows},
			Timeout:      timeout,
		},
		TargetResourceID: targetID,
		Policies:         s.pgPolicies,
	})
	if ev == nil {
		// The executor contract guarantees evidence on every post-validation
		// path; a nil record is a wiring fault — finalize as failed rather
		// than fabricate scalars.
		ev = &PGAttemptEvidence{Stage: PGStageInput, Status: model.QueryExecutionFailed}
	}

	// 7. Exactly one keyed finalize — evidence window detached from request
	//    cancellation. A finalize failure returns the safe persistence error;
	//    the claim remains discoverable and non-retryable.
	status := ev.Status
	rowCount := 0
	if res != nil && execErr == nil {
		status = model.QueryExecutionSuccess
		rowCount = len(res.Rows)
	}
	code, msg := pgClaimedTerminalEvidence(execErr, status)
	rec := model.QueryExecutionRecord{
		TargetResourceID:  targetID,
		Engine:            target.ConnectionContext.Engine,
		DatabaseName:      cred.DatabaseName,
		SchemaName:        schema,
		StatementDigest:   digest,
		StatementPreview:  truncateString(req.Statement, 512),
		Status:            status,
		RowCount:          rowCount,
		DurationMs:        s.clock.Now().Sub(start).Milliseconds(),
		ErrorCode:         code,
		ErrorMessage:      msg,
		RemoteState:       ev.RemoteState,
		ClientExecutionID: &req.ClientExecutionID,
		CreatedAt:         s.clock.Now(),
	}
	if identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = identity.ID
		if status == model.QueryExecutionSuccess {
			rec.FullStatement = req.Statement
		}
	} else {
		rec.ActorMachinePrincipalID = identity.ID
	}
	if ev.BackendPID != 0 {
		pid := int64(ev.BackendPID)
		rec.BackendPID = &pid
	}

	windowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), evidencePersistenceWindow)
	defer cancel()
	execID, ferr := claimStore.FinalizeClaimWithAudit(windowCtx, rec, "query.executed", auditResultFor(status))
	if ferr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}

	if execErr != nil {
		return PGClaimedOutcome{ExecutionID: execID, Status: status, Evidence: ev}, execErr
	}
	return PGClaimedOutcome{ExecutionID: execID, Status: status, Result: res, Evidence: ev}, nil
}

// resolvePGClaimPagination resolves the keyed entry's mandatory page window.
// Both fields absent resolves to (1, default); both present must validate; a
// half-specified window is a shape failure.
func resolvePGClaimPagination(page, pageSize int) (int, int, error) {
	if page == 0 && pageSize == 0 {
		return 1, pgClaimDefaultPageSize, nil
	}
	if page <= 0 || pageSize <= 0 {
		return 0, 0, fmt.Errorf("page and pageSize must be provided together")
	}
	if err := model.ValidatePagination(page, pageSize); err != nil {
		return 0, 0, err
	}
	return page, pageSize, nil
}

// resolvePGClaimMaxRows applies the default then the shared caps: production
// clamps to productionHardMaxRows first, then the workbench hard bound.
func resolvePGClaimMaxRows(environment string, requested int) int {
	maxRows := requested
	if maxRows <= 0 {
		maxRows = pgClaimDefaultMaxRows
	}
	maxRows = clampProductionMaxRows(environment, maxRows)
	if maxRows > pgClaimHardMaxRows {
		maxRows = pgClaimHardMaxRows
	}
	return maxRows
}

// pgClaimedTerminalEvidence maps the attempt's classified outcome to the
// fixed evidence code/message persisted with the keyed terminal row. The
// raw executor error — which may carry driver text — never reaches history:
// only the pgsql.RejectError code (already a controlled taxonomy) and the
// fixed disclosure-blocked code survive.
func pgClaimedTerminalEvidence(execErr error, status model.QueryExecutionStatus) (code, msg string) {
	if execErr == nil {
		return "", ""
	}
	var re *pgsql.RejectError
	switch {
	case status == model.QueryExecutionRejected && errors.Is(execErr, ErrQueryDisclosureBlocked):
		return "query_result_disclosure_blocked", "query blocked by result disclosure policy"
	case status == model.QueryExecutionRejected && errors.As(execErr, &re) && re.Code != "":
		return re.Code, "query rejected by governance"
	case status == model.QueryExecutionRejected:
		return "query_rejected", "query rejected by governance"
	case status == model.QueryExecutionTimeout:
		return "query_timeout", "query exceeded the time limit"
	case status == model.QueryExecutionCancelled:
		return "query_cancelled", "query canceled"
	default:
		return "query_backend_error", "target database query failed"
	}
}

// accessErrMessage reduces an access-resolution error to its fixed denial
// text. Bare sentinels (missing/disabled connection, binding mismatch) share
// the same controlled message as TargetAccessError so no internal detail —
// and no credential state — escapes.
func accessErrMessage(err error) string {
	var accessErr *TargetAccessError
	if errors.As(err, &accessErr) {
		return accessErr.Error()
	}
	return "target is not enabled for execution"
}

// rejectPGClaimedAccess records the existing NULL-key rejected pair for an
// access refusal after a resolved target, then returns the refusal. The key
// is untouched — a later permitted request may still occupy it.
func (s *QueryExecutionService) rejectPGClaimedAccess(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, req PGClaimedExecuteRequest, msg string, start time.Time) (PGClaimedOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, req, model.QueryExecutionRejected,
		"query_not_allowed", msg, start)
	if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceQuery); perr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	return PGClaimedOutcome{}, fmt.Errorf("%w: %s", ErrQueryNotAllowed, msg)
}

// failPGClaimedAccess records the NULL-key failed pair for a claim-store
// admission failure: no user SQL ran and the key never became the occupant.
// The write is the single bounded evidence attempt — no retry.
func (s *QueryExecutionService) failPGClaimedAccess(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, req PGClaimedExecuteRequest, start time.Time) (PGClaimedOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, req, model.QueryExecutionFailed,
		"query_backend_error", "target database query failed", start)
	if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceQuery); perr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	return PGClaimedOutcome{}, ErrQueryBackendFailure
}

// buildPGClaimEvidenceRecord constructs the NULL-key evidence row for a
// pre-claim refusal/failure under the real request actor: request connection
// scope, fixed codes, NULL key — the row must never pretend the client key
// occupied anything.
func (s *QueryExecutionService) buildPGClaimEvidenceRecord(target model.QueryTarget, identity model.QueryExecutionIdentity, req PGClaimedExecuteRequest, status model.QueryExecutionStatus, code, msg string, start time.Time) model.QueryExecutionRecord {
	rec := model.QueryExecutionRecord{
		TargetResourceID: target.ResourceID,
		Engine:           target.ConnectionContext.Engine,
		DatabaseName:     req.Database,
		SchemaName:       req.Schema,
		StatementDigest: DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
			Database: req.Database, Schema: req.Schema, Statement: req.Statement,
			Page: req.Page, PageSize: req.PageSize, MaxRows: req.MaxRows,
		}),
		StatementPreview: truncateString(req.Statement, 512),
		Status:           status,
		DurationMs:       s.clock.Now().Sub(start).Milliseconds(),
		ErrorCode:        code,
		ErrorMessage:     msg,
		CreatedAt:        s.clock.Now(),
	}
	if identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = identity.ID
	} else {
		rec.ActorMachinePrincipalID = identity.ID
	}
	return rec
}
