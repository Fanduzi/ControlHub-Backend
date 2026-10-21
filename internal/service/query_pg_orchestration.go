// Package service orchestrates the internal keyed PostgreSQL execution entries over the closed pgClaimedEntryKind enum (ordinary, saved, and navigation).
// input: context, errors, fmt, strings, time, internal/model, internal/pgsql
// output: ExecutePGClaimed, PGClaimedExecuteRequest, PGClaimedOutcome, ErrQueryExecutionAlreadyExists, ErrQueryExecutionConflict, WithPGClaimOrchestration — plus the private pgClaimedPrepared runner
// pos: T10-C keyed claim orchestration (issue #118, frozen G9 order) plus the T8-C delivery seam (issue #116) plus T12-R1 exits (issue #120) — each entry's front half validates shape, resolves access, resolves defaults (explicit schema verbatim), and computes its digest; ONE executePGClaimedPrepared runner then owns atomic TryClaimExecution → governed outcome → consistency gate on a coordinator-owned pre-finalize snapshot → the sole keyed FinalizeClaimWithAudit → DecideResultDelivery on that snapshot → result rebuilt from the decision's owned rows/matrix. The kind picks the audit event (query.executed for ordinary/saved, related_record_navigation for navigation) and statement persistence: only ordinary stores the masked preview and owner-only full statement; saved/navigation always persist the withheld preview and empty FullStatement. Not an HTTP route.
// note: if this file changes, update this header and module README.md.

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

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
	// Schema selects the schema the governed chain pins for the attempt;
	// empty resolves to the connection's configured default_schema. An
	// explicit name is kept verbatim — never trimmed or case-folded — and
	// schema existence/USAGE is decided by the real database binding gate
	// after claim, not by the connection identity.
	Schema    string
	Statement string
	Page      int
	PageSize  int
	MaxRows   int
	// Capabilities feeds only the post-finalize delivery gate (T8-PG): it
	// never reaches the request digest or claim identity, so a same-key
	// retry that changes it still belongs to the same claimed request.
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
	// Delivery is the narrow public-metadata set the shared runner fills
	// exactly once, after the keyed finalize persists: the resolved
	// connection scope, page window, and row budget, plus the finalized
	// record's timing. A transport adapter builds its response from these
	// scalars — it never re-derives scope or invents timestamps. The value
	// stays zero on every path that never finalized (shape failures, access
	// denials, claim conflicts, pre-claim evidence pairs).
	Delivery PGClaimedDeliveryFacts
}

// PGClaimedDeliveryFacts is the runner-owned scalar metadata a transport
// adapter needs to project the persisted attempt into a public response.
// Database/Schema are the resolved connection scope (pinned schema after
// default resolution); Page/PageSize/MaxRows are the resolved window; and
// DurationMs/ExecutedAt are copied from the finalized history record itself,
// never re-measured at response time. LimitApplied is the execution-window
// fact — the LIMIT literal the rewritten statement actually carried
// (PaginatePG's FetchCount, sentinel-inclusive) — copied from the
// coordinator's own delivery snapshot, never recomputed from the request
// and never the total row budget.
type PGClaimedDeliveryFacts struct {
	Database     string
	Schema       string
	Page         int
	PageSize     int
	MaxRows      int
	LimitApplied int
	DurationMs   int64
	ExecutedAt   time.Time
}

// pgClaimStore is the narrow repository seam the keyed orchestrator needs:
// atomic admission plus the keyed finalize primitive. The concrete MySQL
// repository satisfies it; no SQL is duplicated here.
type pgClaimStore interface {
	TryClaimExecution(ctx context.Context, in model.QueryExecutionClaimInput) error
	FinalizeClaimWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error)
}

// pgClaimedWithheldPreview is the fixed history preview for rows that must
// carry no statement content at all: pre-claim evidence pairs (access
// denial, claim-store failure) and any post-claim attempt whose masked
// preview renders empty. The original statement is never a fallback.
const pgClaimedWithheldPreview = "[statement withheld]"

// pgClaimedStatementPreview renders the keyed terminal row's history preview
// through the PG lexical masker: literal bodies and comments are replaced by
// fixed markers so no user value persists in the general preview column.
// The owner-only full-statement contract is unaffected — it saves the
// original text under its own permission boundary.
func pgClaimedStatementPreview(stmt string) string {
	if preview := pgsql.SafeStatementPreview(stmt, 512); strings.TrimSpace(preview) != "" {
		return preview
	}
	return pgClaimedWithheldPreview
}

// pgClaimedEntryKind is the closed enum of keyed PG entry sources. The
// server selects it — it is never a client input — and it decides only the
// audit-event family and the statement-persistence policy; the
// claim/finalize/delivery machinery is identical for every member.
// All three entries use this closed event selection rather than accepting
// an open-ended event type from callers.
type pgClaimedEntryKind int

const (
	pgClaimedEntryOrdinary pgClaimedEntryKind = iota
	pgClaimedEntrySaved
	pgClaimedEntryNavigation
)

// keyedAuditEvent selects the fixed server-owned audit event type the keyed
// finalize writes: ordinary and saved executions share query.executed;
// related-record navigation carries its own event.
func (k pgClaimedEntryKind) keyedAuditEvent() string {
	if k == pgClaimedEntryNavigation {
		return "related_record_navigation"
	}
	return "query.executed"
}

// evidenceKind selects the same fixed event family for the NULL-key
// evidence pairs written before any claim exists.
func (k pgClaimedEntryKind) evidenceKind() executionEvidenceKind {
	if k == pgClaimedEntryNavigation {
		return executionEvidenceNavigation
	}
	return executionEvidenceQuery
}

// persistsStatement reports whether the entry may store statement-derived
// history content. Only the ordinary entry keeps the masked preview and the
// owner-only full statement; saved and navigation entries must never let
// compiled/generated SQL, template identifiers, declarations, or parameter
// values reach history — every terminal row carries the fixed withheld
// preview and an empty FullStatement.
func (k pgClaimedEntryKind) persistsStatement() bool {
	return k == pgClaimedEntryOrdinary
}

// pgClaimedPrepared is the single prepared request the shared keyed runner
// consumes. Every field is already resolved by the entry's own front half —
// the runner never re-validates shape, never re-reads access, and never
// re-derives defaults. statement is exactly the text the guarded attempt
// receives (compiled $k text for parameterized entries); digest is the
// entry's resolved request digest. nullKey* fields carry the scope the
// pre-claim evidence helpers persist — for the ordinary entry those are the
// raw request values, for the saved entry a deterministic value-free scope.
type pgClaimedPrepared struct {
	kind         pgClaimedEntryKind
	access       BoundTargetAccess
	identity     model.QueryExecutionIdentity
	targetID     uint64
	clientKey    string
	database     string
	schema       string
	page         int
	pageSize     int
	maxRows      int
	timeout      time.Duration
	start        time.Time
	statement    string
	digest       string
	capabilities []string

	nullKeyDatabase string
	nullKeySchema   string
	nullKeyDigest   string

	// parameters is the optional trusted text-parameter plan (nil on the
	// ordinary unbound path); requirePinnedSchema strengthens the binding
	// gate so saved/navigation attempts prove pinned-schema existence/USAGE
	// even when every reference is already qualified. navigation carries the
	// prepared related-record binding only for the navigation entry — the
	// executor snapshots it and the binding gate proves it still holds.
	parameters          *pgParameterPlan
	requirePinnedSchema bool
	navigation          *pgNavigationBinding
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
// atomic claim → governed disclosure outcome → consistency + result-matrix
// structure gate → exactly one keyed finalize → post-finalize delivery
// decision. The idempotency key is consumed only after access resolution
// succeeds, so shape failures and access denials never occupy it. Every
// post-claim terminal outcome — guard/binding/disclosure rejection,
// cancellation, timeout, driver failure, or success — is persisted by the
// sole claimant's keyed finalize; the claim is never released or
// re-occupied. The shared delivery gate then judges only delivery: a
// truncated page without the declared capability returns the upgrade error
// while the recorded outcome stays success and no rows escape.
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
	if utf8.RuneCountInString(req.Statement) > model.MaxStatementLength {
		return PGClaimedOutcome{}, fmt.Errorf("%w: statement exceeds %d characters", ErrQueryValidationFailed, model.MaxStatementLength)
	}
	// Connection-scope shape: database is mandatory, schema optional. Both
	// stay byte-verbatim — a legal name that does not resolve is a later
	// database-gate outcome, not a shape failure. These checks run before
	// any target/credential access so a malformed request never touches it.
	if err := model.ValidateIdentifierShape("database", req.Database, true); err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if err := model.ValidateIdentifierShape("schema", req.Schema, false); err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	page, pageSize, err := resolvePGClaimPagination(req.Page, req.PageSize)
	if err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if req.MaxRows < 0 {
		return PGClaimedOutcome{}, fmt.Errorf("%w: maxRows must not be negative", ErrQueryValidationFailed)
	}
	if s.pgPolicies == nil || s.pgOutcomeFn == nil {
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	if _, ok := s.executions.(pgClaimStore); !ok {
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
		return s.rejectPGClaimedAccess(ctx, access.Target, identity, pgClaimedEntryOrdinary,
			req.Database, req.Schema, pgClaimedOrdinaryRawDigest(req), err, start)
	}
	cred := access.Credential

	// 3. Default resolution — an empty schema falls back to the connection's
	//    configured default_schema; an explicit name is kept verbatim. The
	//    connection identity stays (target, database): schema existence and
	//    USAGE are real database judgments the binding gate makes after
	//    claim, so a valid-but-missing schema is a keyed terminal outcome,
	//    never a pre-claim access denial.
	schema, maxRows, timeout := resolvePGClaimedDefaults(access, req.Schema, req.MaxRows)

	// 4. Request digest over resolved values — the statement's original bytes
	//    and never capabilities/clientExecutionId.
	digest := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: cred.DatabaseName, Schema: schema, Statement: req.Statement,
		Page: page, PageSize: pageSize, MaxRows: maxRows,
	})

	// Steps 5-7 (atomic admission → governed attempt → consistency gate →
	// sole keyed finalize → delivery decision) are the shared runner every
	// keyed PG entry converges on — the ordinary entry fills the prepared
	// request with raw request text and no parameter plan.
	return s.executePGClaimedPrepared(ctx, pgClaimedPrepared{
		kind:         pgClaimedEntryOrdinary,
		access:       access,
		identity:     identity,
		targetID:     targetID,
		clientKey:    req.ClientExecutionID,
		database:     cred.DatabaseName,
		schema:       schema,
		page:         page,
		pageSize:     pageSize,
		maxRows:      maxRows,
		timeout:      timeout,
		start:        start,
		statement:    req.Statement,
		digest:       digest,
		capabilities: req.Capabilities,
		// NULL-key evidence keeps the raw request scope/digest — the same
		// fields the pre-extraction helpers persisted.
		nullKeyDatabase: req.Database,
		nullKeySchema:   req.Schema,
		nullKeyDigest:   pgClaimedOrdinaryRawDigest(req),
	})
}

// pgClaimedOrdinaryRawDigest is the digest the ordinary entry's NULL-key
// evidence rows carry: raw request fields, exactly as the pre-extraction
// record builder computed them — never the resolved claim digest.
func pgClaimedOrdinaryRawDigest(req PGClaimedExecuteRequest) string {
	return DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: req.Database, Schema: req.Schema, Statement: req.Statement,
		Page: req.Page, PageSize: req.PageSize, MaxRows: req.MaxRows,
	})
}

// executePGClaimedPrepared is the single keyed-execution runner every PG
// entry converges on once its front half has produced a fully resolved
// pgClaimedPrepared. It owns the frozen order: atomic claim admission →
// governed disclosure outcome → consistency gate on a coordinator-owned
// pre-finalize delivery snapshot → exactly one keyed FinalizeClaimWithAudit
// → DecideResultDelivery on that same snapshot → result rebuilt from the
// decision's owned payload. No other code path may claim or finalize.
func (s *QueryExecutionService) executePGClaimedPrepared(ctx context.Context, p pgClaimedPrepared) (PGClaimedOutcome, error) {
	claimStore, ok := s.executions.(pgClaimStore)
	if !ok {
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	// The entry kind is a closed server-owned enum: an out-of-range value is
	// a wiring defect — fail before any claim rather than pick an arbitrary
	// audit family or statement policy.
	switch p.kind {
	case pgClaimedEntryOrdinary, pgClaimedEntrySaved, pgClaimedEntryNavigation:
	default:
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	target := p.access.Target

	// 5. Atomic admission — the single INSERT that decides occupancy.
	claim := model.QueryExecutionClaimInput{
		TargetResourceID:  p.targetID,
		ClientExecutionID: p.clientKey,
		Identity:          p.identity,
		DatabaseName:      p.database,
		SchemaName:        p.schema,
		RequestDigest:     p.digest,
		AttemptTimeoutMs:  p.timeout.Milliseconds(),
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
			return s.failPGClaimedAccess(ctx, target, p.identity, p.kind,
				p.nullKeyDatabase, p.nullKeySchema, p.nullKeyDigest, p.start)
		}
	}

	// 6. Governed attempt — the T9-B disclosure-decorated chain arms its own
	//    statement budget inside the remaining request context.
	res, ev, execErr := s.pgOutcomeFn(ctx, PGDisclosureInput{
		Execute: PGExecuteInput{
			ConnConfig:   p.access.pgConfig,
			Database:     p.database,
			PinnedSchema: p.schema,
			Statement:    p.statement,
			Page:         pgsql.PageOptions{Page: p.page, PageSize: p.pageSize, MaxRows: p.maxRows},

			Timeout:             p.timeout,
			parameters:          p.parameters,
			requirePinnedSchema: p.requirePinnedSchema,
			navigation:          p.navigation,
		},
		TargetResourceID: p.targetID,
		Policies:         s.pgPolicies,
	})

	// Consistency gate — before the one finalize, and only there. Success
	// requires a delivered result, evidence, no error, and an evidence record
	// classified success; a classified failure requires evidence. Any other
	// (result, evidence, error) combination is an internal contract fault:
	// the attempt finalizes as failed under a fixed internal code and never
	// delivers rows — a non-empty result can never override an evidence
	// failure, and a missing backend PID or remote_state is never invented.
	contractFault := (execErr == nil && (res == nil || ev == nil || ev.Status != model.QueryExecutionSuccess)) ||
		(execErr != nil && ev != nil && ev.Status == model.QueryExecutionSuccess)

	// Structure evidence — a non-nil disclosed result must carry a complete
	// cellTruncated matrix aligned with its delivered page (T8-C). A missing
	// or misaligned matrix is producer damage, folded into the same internal
	// contract fault: it is never repaired by fabricating all-false marks,
	// never classified as a disclosure refusal, and never discovered only
	// after the success pair already persisted.
	//
	// The coordinator takes its own delivery snapshot here — before the
	// keyed finalize: the native-text rows, the cloned matrix, and the
	// cloned columns/provenance are what the pre-finalize shape check, the
	// post-finalize capability gate, and the returned payload all consume.
	// The producer's object is never re-read for delivery data after this
	// point and is never returned.
	var delivery *PGDisclosedResult
	var gateCols []model.QueryResultColumn
	var gateRows [][]any
	var gateMatrix [][]bool
	if res != nil {
		delivery = &PGDisclosedResult{
			Columns:      append([]PGDisclosedColumn(nil), res.Columns...),
			Page:         res.Page,
			FetchCount:   res.FetchCount,
			Refs:         clonePGRefSlice(res.Refs),
			PublicProofs: clonePGProofSlice(res.PublicProofs),
			Sources:      clonePGSourceCatalog(res.Sources),
			BoundRefs:    clonePGBoundRefMap(res.BoundRefs),
		}
		gateCols = pgDisclosedModelColumns(delivery.Columns)
		gateRows = pgDisclosedRowsAny(res.Rows)
		gateMatrix = CloneCellTruncated(res.CellTruncated)
		if !contractFault && validateCellTruncatedMatrix(len(gateCols), gateRows, gateMatrix) != nil {
			contractFault = true
		}
	}
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
	code, msg := "", ""
	switch {
	case contractFault:
		status, code, msg = model.QueryExecutionFailed,
			"query_internal_error", "internal execution contract violation"
	case res != nil && execErr == nil:
		status, rowCount = model.QueryExecutionSuccess, len(res.Rows)
	default:
		code, msg = pgClaimedTerminalEvidence(execErr, status)
	}
	// Statement persistence is entry-selected: only the ordinary entry stores
	// the masked preview and (for a user-actor success) the full statement;
	// saved and navigation rows always carry the withheld preview and an
	// empty FullStatement — compiled text, template identifiers,
	// declarations, and parameter values never persist.
	preview := pgClaimedWithheldPreview
	if p.kind.persistsStatement() {
		preview = pgClaimedStatementPreview(p.statement)
	}
	finished := s.clock.Now()
	rec := model.QueryExecutionRecord{
		TargetResourceID:  p.targetID,
		Engine:            target.ConnectionContext.Engine,
		DatabaseName:      p.database,
		SchemaName:        p.schema,
		StatementDigest:   p.digest,
		StatementPreview:  preview,
		Status:            status,
		RowCount:          rowCount,
		DurationMs:        finished.Sub(p.start).Milliseconds(),
		ErrorCode:         code,
		ErrorMessage:      msg,
		RemoteState:       ev.RemoteState,
		ClientExecutionID: &p.clientKey,
		CreatedAt:         finished,
	}
	if p.identity.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = p.identity.ID
		if status == model.QueryExecutionSuccess && p.kind.persistsStatement() {
			rec.FullStatement = p.statement
		}
	} else {
		rec.ActorMachinePrincipalID = p.identity.ID
	}
	if ev.BackendPID != 0 {
		pid := int64(ev.BackendPID)
		rec.BackendPID = &pid
	}

	windowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), evidencePersistenceWindow)
	defer cancel()
	execID, ferr := claimStore.FinalizeClaimWithAudit(windowCtx, rec, p.kind.keyedAuditEvent(), auditResultFor(status))
	if ferr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	// The public-delivery scalars derive from the record the finalize just
	// persisted — resolved scope/window plus the record's own timing — and
	// are filled exactly once here. Adapters consume them verbatim.
	facts := PGClaimedDeliveryFacts{
		Database:   p.database,
		Schema:     p.schema,
		Page:       p.page,
		PageSize:   p.pageSize,
		MaxRows:    p.maxRows,
		DurationMs: rec.DurationMs,
		ExecutedAt: rec.CreatedAt,
	}
	if delivery != nil {
		// The executed window (sentinel-inclusive FetchCount) is the
		// limitApplied the transport publishes — a real page's LIMIT may be
		// below or above pageSize and is unrelated to the row budget.
		facts.LimitApplied = int(delivery.FetchCount)
	}

	// The outcome's evidence must tell the same terminal story as the
	// persisted row and outcome status. The executor's record is its own —
	// never rewritten in place — so when the verdict diverges from the
	// executor's classification (a contract fault), the outcome exposes an
	// independent copy carrying the final status while every measured fact
	// (stage, backend identity, remote_state, send flag) is kept verbatim:
	// a real remote_state=completed may legitimately coexist with a failed
	// verdict and is never rewritten for cosmetic agreement.
	outEv := ev
	if outEv.Status != status {
		aligned := *outEv
		aligned.Status = status
		outEv = &aligned
	}

	if contractFault {
		return PGClaimedOutcome{ExecutionID: execID, Status: status, Evidence: outEv, Delivery: facts}, ErrQueryBackendFailure
	}
	if execErr != nil {
		return PGClaimedOutcome{ExecutionID: execID, Status: status, Evidence: outEv, Delivery: facts}, execErr
	}

	// Post-finalize delivery gate (G2): the success pair is already
	// persisted, so a capability refusal is a delivery refusal only — the
	// keyed terminal record stays success, no rejected row is appended, and
	// no second finalize happens. The request's real capabilities decide;
	// a refusal returns the fixed upgrade error with the true execution id
	// and no rows. A persistence failure above already won over any
	// capability error — this gate only runs after finalize succeeded.
	decision, derr := DecideResultDelivery(ResultDeliveryInput{
		Columns:       gateCols,
		Rows:          gateRows,
		CellTruncated: gateMatrix,
		Capabilities:  p.capabilities,
	})
	if derr != nil {
		return PGClaimedOutcome{ExecutionID: execID, Status: status, Evidence: outEv, Delivery: facts}, derr
	}
	// The returned object is the coordinator's own snapshot — never the
	// producer's: its rows are rebuilt from the decision's owned payload
	// (nil → SQL NULL, string → a fresh *string over the immutable bytes)
	// and its matrix is the decision's clone. Producer-side mutation at or
	// after the finalize boundary cannot reach what was validated, gated,
	// and delivered.
	delivery.Rows = pgDecisionRowsDisclosed(decision.Rows)
	delivery.CellTruncated = decision.CellTruncated
	return PGClaimedOutcome{ExecutionID: execID, Status: status, Result: delivery, Evidence: outEv, Delivery: facts}, nil
}

// pgDisclosedRowsAny adapts native-text cells to the shared [][]any
// delivery shape: a non-NULL cell dereferences to string, NULL stays nil.
// No *string, []byte, or other undeclared shape crosses the boundary.
func pgDisclosedRowsAny(rows [][]*string) [][]any {
	if rows == nil {
		return nil
	}
	out := make([][]any, len(rows))
	for i, row := range rows {
		r := make([]any, len(row))
		for j, cell := range row {
			if cell != nil {
				r[j] = *cell
			}
		}
		out[i] = r
	}
	return out
}

// pgDecisionRowsDisclosed converts the delivery decision's payload back to
// the native-text cell shape: nil becomes SQL NULL, a string becomes a new
// string variable and pointer (the immutable bytes may be shared — no
// strings.Clone copy). The gate input came from pgDisclosedRowsAny, so
// string and nil are the only shapes that can appear; nothing is
// re-truncated, reformatted, or fmt.Sprint-ed.
func pgDecisionRowsDisclosed(rows [][]any) [][]*string {
	if rows == nil {
		return nil
	}
	out := make([][]*string, len(rows))
	for i, row := range rows {
		r := make([]*string, len(row))
		for j, cell := range row {
			if s, ok := cell.(string); ok {
				v := s
				r[j] = &v
			}
		}
		out[i] = r
	}
	return out
}

// pgDisclosedModelColumns adapts disclosed columns to the shared delivery
// contract's column shape. Nullable carries no evidence on this path and
// stays zero — the gate reads only count, display mode, and copyability.
func pgDisclosedModelColumns(cols []PGDisclosedColumn) []model.QueryResultColumn {
	out := make([]model.QueryResultColumn, len(cols))
	for i, c := range cols {
		out[i] = model.QueryResultColumn{
			Name:         c.Name,
			DatabaseType: c.DatabaseType,
			DisplayMode:  c.DisplayMode,
			CopyAllowed:  c.CopyAllowed,
		}
	}
	return out
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

// resolvePGClaimedDefaults resolves the three environment-dependent defaults
// every keyed entry shares: an empty schema falls back to the connection's
// configured default_schema (an explicit name is kept verbatim), maxRows
// takes the default then the shared caps, and the attempt timeout follows the
// environment class. Schema existence/USAGE stays the post-claim binding
// gate's judgment — never a pre-claim access verdict.
func resolvePGClaimedDefaults(access BoundTargetAccess, schema string, maxRows int) (string, int, time.Duration) {
	if schema == "" {
		schema = access.Credential.DefaultSchema
	}
	maxRows = resolvePGClaimMaxRows(access.Target.ConnectionContext.Environment, maxRows)
	timeout := defaultQueryTimeout
	if isProductionEnvironment(access.Target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}
	return schema, maxRows, timeout
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
// is untouched — a later permitted request may still occupy it. The caller
// supplies the entry kind (which fixes the audit event family) and the
// connection scope/digest the evidence row carries; ordinary callers pass
// the raw request scope so the row's fields are unchanged. The persisted
// row always carries the generic "query_not_allowed" denial contract; the
// returned error keeps only the normalized connection sentinels for the
// transport (pgAccessRefusalError) — raw driver or internal text never
// escapes into the unwrap chain either way.
func (s *QueryExecutionService) rejectPGClaimedAccess(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, kind pgClaimedEntryKind, database, schema, digest string, accessErr error, start time.Time) (PGClaimedOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, kind, database, schema, digest,
		model.QueryExecutionRejected, "query_not_allowed", accessErrMessage(accessErr), start)
	if _, perr := s.persistEvidencePair(ctx, rec, kind.evidenceKind()); perr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	return PGClaimedOutcome{}, pgAccessRefusalError(accessErr)
}

// pgAccessRefusalError picks the transport-visible identity of an access
// refusal by sentinel classification — never by message text. The three
// connection sentinels already carry fixed safe messages and publish their
// own HTTP codes, so they surface verbatim; every other refusal
// (TargetAccessError policy/secret/binding detail or an unexpected
// internal error) stays the generic not-allowed denial.
func pgAccessRefusalError(accessErr error) error {
	switch {
	case errors.Is(accessErr, ErrSchemaConnectionNotFound):
		return ErrSchemaConnectionNotFound
	case errors.Is(accessErr, ErrSchemaConnectionDisabled):
		return ErrSchemaConnectionDisabled
	case errors.Is(accessErr, ErrSchemaBindingMismatch):
		return ErrSchemaBindingMismatch
	}
	return fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErrMessage(accessErr))
}

// failPGClaimedAccess records the NULL-key failed pair for a claim-store
// admission failure or a backend read fault: no user SQL ran and the key
// never became the occupant. The write is the single bounded evidence
// attempt — no retry.
func (s *QueryExecutionService) failPGClaimedAccess(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, kind pgClaimedEntryKind, database, schema, digest string, start time.Time) (PGClaimedOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, kind, database, schema, digest,
		model.QueryExecutionFailed, "query_backend_error", "target database query failed", start)
	if _, perr := s.persistEvidencePair(ctx, rec, kind.evidenceKind()); perr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	return PGClaimedOutcome{}, ErrQueryBackendFailure
}

// buildPGClaimEvidenceRecord constructs the NULL-key evidence row for a
// pre-claim refusal/failure under the real request actor: the caller-owned
// connection scope and digest, fixed codes, NULL key — the row must never
// pretend the client key occupied anything.
func (s *QueryExecutionService) buildPGClaimEvidenceRecord(target model.QueryTarget, identity model.QueryExecutionIdentity, kind pgClaimedEntryKind, database, schema, digest string, status model.QueryExecutionStatus, code, msg string, start time.Time) model.QueryExecutionRecord {
	rec := model.QueryExecutionRecord{
		TargetResourceID: target.ResourceID,
		Engine:           target.ConnectionContext.Engine,
		DatabaseName:     database,
		SchemaName:       schema,
		StatementDigest:  digest,
		StatementPreview: pgClaimedWithheldPreview,
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
