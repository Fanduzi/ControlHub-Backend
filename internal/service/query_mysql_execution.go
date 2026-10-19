// Package service — keyed MySQL/TiDB execution claim policy and statement evidence metadata.
// input: context, errors, fmt, time, vitess sqlparser, internal/model
// output: mysqlEntryKind, mysqlClaimStore, mysqlExecutionAttempt (admit/persistRecord/reject/recordTerminalOutcome/finalizeSuccess/setNavigationRelation/navReject/navPreparationOutcome/navTerminalOutcome), mysqlOrdinaryDigest, mysqlSavedDigest, mysqlNavigationDigest, mysqlStatementMetadata
// pos: Per-attempt claim/evidence policy the ordinary, saved, and navigation MySQL/TiDB entries carry through the shared guarded chain — one claim admission, exactly one final evidence write (keyless pair or keyed finalize), privacy-safe statement metadata, canonical relation evidence, and the shared owned-snapshot success finalize whose malformed post-Apply evidence clears every user-success artifact; the pre-claim navigation preparation outcome returns the bare canonical sentinel while post-claim outcomes keep the wrapped safe classification; never the PG claim path
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vitess.io/vitess/go/vt/sqlparser"

	"github.com/fan/controlhub/internal/model"
)

// mysqlEntryKind is the closed server-owned enum selecting the attempt's
// audit family and statement-persistence policy. Values are never parsed
// from client input — each entry point fixes its own kind.
type mysqlEntryKind int

const (
	mysqlEntryOrdinary   mysqlEntryKind = iota // ordinary workbench execution
	mysqlEntrySaved                            // saved-statement execution
	mysqlEntryNavigation                       // related-record navigation
)

// evidenceKind maps the entry to its audit event family.
func (k mysqlEntryKind) evidenceKind() executionEvidenceKind {
	if k == mysqlEntryNavigation {
		return executionEvidenceNavigation
	}
	return executionEvidenceQuery
}

// persistsStatement reports whether the entry may store statement-derived
// history content. Only the ordinary entry keeps the masked digest/preview
// and the owner-only full statement; saved and navigation entries must never
// let compiled SQL, template identifiers, or parameter values reach history.
func (k mysqlEntryKind) persistsStatement() bool {
	return k == mysqlEntryOrdinary
}

// evidenceEventType maps the closed entry-kind enum to the audit_events
// event_type vocabulary. It is the single mapping the keyless pair and the
// keyed finalize share so the two evidence arms cannot drift.
func evidenceEventType(kind executionEvidenceKind) string {
	if kind == executionEvidenceNavigation {
		return "related_record_navigation"
	}
	return "query.executed"
}

// mysqlClaimStore is the claim-capable slice of the executions repository the
// keyed path needs: the single atomic INSERT that decides occupancy and the
// single transaction that links the claim to its terminal execution plus
// audit row. The production repository satisfies it; unit fakes count calls.
type mysqlClaimStore interface {
	TryClaimExecution(ctx context.Context, input model.QueryExecutionClaimInput) error
	FinalizeClaimWithAudit(ctx context.Context, record model.QueryExecutionRecord, eventType, result string) (uint64, error)
}

// mysqlExecutionAttempt is the per-execution claim/evidence policy one MySQL
// or TiDB entry carries through the guarded chain. ownedKey stays empty until
// a keyed admit wins the claim INSERT — the only state that may ever reach
// the keyed finalize — so a failed or skipped admission can never finalize
// under a key it does not own.
type mysqlExecutionAttempt struct {
	svc      *QueryExecutionService
	target   model.QueryTarget
	identity model.QueryExecutionIdentity
	start    time.Time
	kind     mysqlEntryKind
	ownedKey string
	// navRelation is the trusted canonical relation identity
	// "<referenced schema>.<referenced object>/<fk name>" set once FK
	// resolution succeeds; empty records the unresolved form.
	navRelation string
}

func newMySQLExecutionAttempt(s *QueryExecutionService, target model.QueryTarget, identity model.QueryExecutionIdentity, kind mysqlEntryKind, start time.Time) *mysqlExecutionAttempt {
	return &mysqlExecutionAttempt{svc: s, target: target, identity: identity, kind: kind, start: start}
}

// admit performs the single TryClaim that decides occupancy for a non-empty
// key. The MySQL/TiDB connection scope is the target plus empty database and
// empty schema — never the source/database disclosure identity. An empty key
// is the legacy keyless path and admits without touching the claim store.
// A duplicate key maps to already-exists, a same-key/different-digest row to
// conflict, and any other claim-store failure writes one NULL-key failed
// pair — the key must never masquerade as occupied — and reports a bare
// backend error (persistence failure wins). No retry, no guessed ownership.
func (a *mysqlExecutionAttempt) admit(ctx context.Context, key, digest string, timeout time.Duration) error {
	if key == "" {
		return nil
	}
	store, ok := a.svc.executions.(mysqlClaimStore)
	if !ok {
		return a.claimFailure(ctx)
	}
	err := store.TryClaimExecution(ctx, model.QueryExecutionClaimInput{
		TargetResourceID:  a.target.ResourceID,
		ClientExecutionID: key,
		Identity:          a.identity,
		DatabaseName:      "",
		SchemaName:        "",
		RequestDigest:     digest,
		AttemptTimeoutMs:  timeout.Milliseconds(),
		ClaimedAt:         a.svc.clock.Now(),
	})
	switch {
	case err == nil:
		a.ownedKey = key
		return nil
	case errors.Is(err, model.ErrQueryExecutionClaimExists):
		return ErrQueryExecutionAlreadyExists
	case errors.Is(err, model.ErrQueryExecutionClaimConflict):
		return ErrQueryExecutionConflict
	default:
		return a.claimFailure(ctx)
	}
}

// claimFailure persists one NULL-key failed pair for a non-duplicate claim
// admission failure and surfaces the bare backend sentinel. If the evidence
// write itself fails, the persistence error wins.
func (a *mysqlExecutionAttempt) claimFailure(ctx context.Context) error {
	rec := a.svc.buildRecord(a.target, a.identity, nil, model.QueryExecutionFailed, 0,
		"query_backend_error", "query claim admission failed", a.start)
	if _, err := a.svc.persistEvidencePair(ctx, rec, a.kind.evidenceKind()); err != nil {
		return errPersistAttempt
	}
	return ErrQueryBackendFailure
}

// persistRecord is the single terminal evidence dispatch every post-claim
// branch converges on: a keyless attempt writes the legacy execution+audit
// pair, a keyed attempt reaches the sole FinalizeClaimWithAudit call site —
// the linked execution row carries the owned key, plain-empty connection
// scope, NULL backend PID, and empty remote state. The persistence window is
// detached from request cancellation exactly like the keyless pair. A keyed
// finalize failure performs no compensation and no retry: the claim stays
// occupied and the persistence error surfaces.
func (a *mysqlExecutionAttempt) persistRecord(ctx context.Context, rec model.QueryExecutionRecord) (uint64, error) {
	kind := a.kind.evidenceKind()
	if a.ownedKey == "" {
		return a.svc.persistEvidencePair(ctx, rec, kind)
	}
	store, ok := a.svc.executions.(mysqlClaimStore)
	if !ok {
		// ownedKey can only be set by a successful TryClaim on a
		// claim-capable store — this is unreachable wiring-defect defense.
		return 0, errPersistAttempt
	}
	key := a.ownedKey
	rec.ClientExecutionID = &key
	rec.DatabaseName = ""
	rec.SchemaName = ""
	windowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), evidencePersistenceWindow)
	defer cancel()
	return store.FinalizeClaimWithAudit(windowCtx, rec, evidenceEventType(kind), auditResultFor(rec.Status))
}

// buildAttemptRecord applies the entry kind's statement-persistence policy on
// top of the shared record builder. Ordinary entries persist privacy-safe
// statement metadata from the sanitized AST — never the guard's raw-text
// preview — while keeping the owner-only full statement. Non-ordinary entries
// carry no digest, preview, or full statement so compiled/generated SQL never
// reaches history.
func (a *mysqlExecutionAttempt) buildAttemptRecord(guarded *GuardedQuery, status model.QueryExecutionStatus, rowCount int, code, msg string) model.QueryExecutionRecord {
	rec := a.svc.buildRecord(a.target, a.identity, guarded, status, rowCount, code, msg, a.start)
	switch a.kind {
	case mysqlEntryNavigation:
		rec.FullStatement = ""
		// Canonical trusted relation identity only — never SQL text,
		// locator values, or request-controlled source identity before
		// resolution. The unresolved form uses fixed generic markers.
		if a.navRelation == "" {
			rec.StatementDigest = "nav:unresolved"
			rec.StatementPreview = "related:unresolved"
		} else {
			rec.StatementDigest = truncateString("nav:"+a.navRelation, 128)
			rec.StatementPreview = truncateString("related:"+a.navRelation, 256)
		}
	case mysqlEntrySaved:
		// Compiled template SQL never reaches history.
		rec.FullStatement = ""
		rec.StatementDigest = ""
		rec.StatementPreview = ""
	default:
		if guarded != nil && guarded.OriginalStatement != "" {
			rec.StatementDigest, rec.StatementPreview = mysqlStatementMetadata(a.svc.guard, guarded.OriginalStatement)
		}
	}
	return rec
}

// setNavigationRelation records the canonical relation identity from trusted
// FK metadata for later evidence rows. Pre-resolution failures leave it empty.
func (a *mysqlExecutionAttempt) setNavigationRelation(fk *FKSummary) {
	a.navRelation = fmt.Sprintf("%s.%s/%s", fk.Columns[0].ReferencedSchema, fk.Columns[0].ReferencedTable, fk.Name)
}

// navReject persists one terminal rejected navigation record through the
// attempt's keyless/keyed dispatch and returns retErr.
func (a *mysqlExecutionAttempt) navReject(ctx context.Context, code, msg string, retErr error) (model.RelatedRecordNavigationResponse, error) {
	rec := a.buildAttemptRecord(nil, model.QueryExecutionRejected, 0, code, msg)
	if _, perr := a.persistRecord(ctx, rec); perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}
	return model.RelatedRecordNavigationResponse{}, retErr
}

// navPreparationOutcome classifies one pre-claim navigation preparation
// failure (the trusted inspector read before claim admission) through the
// shared MySQL taxonomy, records its fixed-safe evidence through the
// attempt's dispatch, and returns the BARE canonical sentinel — no wrapped
// safe message hangs on the returned chain. The raw error — which may echo
// DSN fragments — never reaches the response or history. Post-claim
// terminal failures keep the wrapped safe classification of
// navTerminalOutcome.
func (a *mysqlExecutionAttempt) navPreparationOutcome(ctx context.Context, err error) (model.RelatedRecordNavigationResponse, error) {
	status, sentinel, code, safeMsg := classifyExecutorError(err)
	rec := a.buildAttemptRecord(nil, status, 0, code, safeMsg)
	if _, perr := a.persistRecord(ctx, rec); perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}
	return model.RelatedRecordNavigationResponse{}, sentinel
}

// navTerminalOutcome classifies one post-claim navigation terminal failure
// (disclosure preflight, executor, or disclosure apply) through the shared
// MySQL taxonomy, records its fixed-safe evidence through the attempt's
// dispatch, and returns the controlled error. The raw error — which may echo
// DSN fragments — never reaches the response or history.
func (a *mysqlExecutionAttempt) navTerminalOutcome(ctx context.Context, err error) (model.RelatedRecordNavigationResponse, error) {
	status, sentinel, code, safeMsg := classifyExecutorError(err)
	rec := a.buildAttemptRecord(nil, status, 0, code, safeMsg)
	if _, perr := a.persistRecord(ctx, rec); perr != nil {
		return model.RelatedRecordNavigationResponse{}, errPersistAttempt
	}
	return model.RelatedRecordNavigationResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
}

// reject persists one terminal rejected pair through the attempt's
// keyless/keyed dispatch and returns retErr.
func (a *mysqlExecutionAttempt) reject(ctx context.Context, guarded *GuardedQuery, code, msg string, retErr error) (model.QueryExecuteResponse, error) {
	rec := a.buildAttemptRecord(guarded, model.QueryExecutionRejected, 0, code, msg)
	if _, perr := a.persistRecord(ctx, rec); perr != nil {
		return model.QueryExecuteResponse{}, errPersistAttempt
	}
	return model.QueryExecuteResponse{}, retErr
}

// recordTerminalOutcome is the attempt-aware terminal write for the guarded
// chain: every executor/disclosure failure classifies through the shared
// taxonomy and converges on exactly one persistRecord call.
func (a *mysqlExecutionAttempt) recordTerminalOutcome(ctx context.Context, guarded *GuardedQuery, err error) (model.QueryExecuteResponse, error) {
	status, sentinel, code, safeMsg := classifyExecutorError(err)
	rec := a.buildAttemptRecord(guarded, status, 0, code, safeMsg)
	if _, perr := a.persistRecord(ctx, rec); perr != nil {
		return model.QueryExecuteResponse{}, errPersistAttempt
	}
	return model.QueryExecuteResponse{}, fmt.Errorf("%w: %s", sentinel, safeMsg)
}

// finalizeSuccess is the single post-Apply success path all three entries
// share: it validates the producer's cell-truncation matrix, snapshots every
// result field into owned memory, writes the success record exactly once
// through the attempt's keyless/keyed dispatch, and runs the capability gate
// against the owned snapshot — never the producer's live result. A
// persistence failure wins over a capability refusal and yields execution ID
// 0; a capability refusal keeps the committed execution ID internally but
// returns an empty payload — the recorded outcome stays success and no
// second evidence write happens.
func (a *mysqlExecutionAttempt) finalizeSuccess(ctx context.Context, rec model.QueryExecutionRecord, result QueryDatabaseResult, caps []string) (QueryDatabaseResult, uint64, error) {
	if err := validateCellTruncatedMatrix(len(result.Columns), result.Rows, result.CellTruncated); err != nil {
		rec2 := rec
		rec2.Status = model.QueryExecutionFailed
		rec2.ErrorCode = "query_backend_error"
		rec2.ErrorMessage = "query backend returned malformed result evidence"
		// A failed terminal row never carries user-success artifacts: the
		// owner-private full statement is cleared and the row count stays
		// zero so no malformed page is ever described by fabricated counts.
		rec2.FullStatement = ""
		rec2.RowCount = 0
		if _, perr := a.persistRecord(ctx, rec2); perr != nil {
			return QueryDatabaseResult{}, 0, errPersistAttempt
		}
		return QueryDatabaseResult{}, 0, ErrQueryBackendFailure
	}
	owned := QueryDatabaseResult{
		Columns:       append([]model.QueryResultColumn(nil), result.Columns...),
		Rows:          cloneRows(result.Rows),
		CellTruncated: CloneCellTruncated(result.CellTruncated),
		RowCount:      result.RowCount,
		Truncated:     result.Truncated,
	}
	rec.RowCount = owned.RowCount
	rec.Status = model.QueryExecutionSuccess
	execID, perr := a.persistRecord(ctx, rec)
	if perr != nil {
		return QueryDatabaseResult{}, 0, errPersistAttempt
	}
	decision, derr := DecideResultDelivery(ResultDeliveryInput{
		Columns:       owned.Columns,
		Rows:          owned.Rows,
		CellTruncated: owned.CellTruncated,
		Capabilities:  caps,
	})
	if derr != nil {
		return QueryDatabaseResult{}, execID, derr
	}
	owned.Rows = decision.Rows
	owned.CellTruncated = decision.CellTruncated
	return owned, execID, nil
}

// mysqlOrdinaryDigest computes the ordinary entry's canonical request digest
// over resolved values: empty MySQL/TiDB connection scope, the raw statement
// bytes, resolved paging, and resolved maxRows. Capabilities and the client
// key are never digest inputs.
func (s *QueryExecutionService) mysqlOrdinaryDigest(req model.QueryExecuteRequest, maxRows int) string {
	page, pageSize := 0, 0
	if req.Pagination != nil {
		page, pageSize = req.Pagination.Page, req.Pagination.PageSize
	}
	return DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "", Schema: "", Statement: req.Statement,
		Page: page, PageSize: pageSize, MaxRows: maxRows,
	})
}

// mysqlSavedDigest computes the saved entry's canonical request digest over
// resolved values: empty connection scope, the statement identity, the typed
// declared values (kind constructors come from the declared parameter types,
// never from Go reflection), resolved paging, and resolved maxRows. The typed
// values influence only the digest — they are never persisted.
func (s *QueryExecutionService) mysqlSavedDigest(statementID uint64, definitions []TemplateParameterDefinition, values map[string]any, page, pageSize, maxRows int) string {
	named := make([]ClaimDigestNamedValue, 0, len(definitions))
	for _, def := range definitions {
		v, ok := values[def.Name]
		if !ok {
			continue
		}
		switch def.Type {
		case TemplateParameterInteger:
			named = append(named, ClaimDigestInteger(v.(int64)))
		case TemplateParameterDecimal:
			named = append(named, ClaimDigestDecimal(v.(string)))
		case TemplateParameterBoolean:
			named = append(named, ClaimDigestBoolean(v.(bool)))
		default:
			named = append(named, ClaimDigestString(v.(string)))
		}
		named[len(named)-1].Name = def.Name
	}
	return DigestSavedStatement(SavedStatementDigestInput{
		Database: "", Schema: "", StatementID: statementID,
		Values: named, Page: page, PageSize: pageSize, MaxRows: maxRows,
	})
}

// mysqlNavigationDigest computes the navigation entry's canonical request
// digest over trusted values: empty connection scope, the request's source
// database context and object, the FK's ordered source columns, the FK name,
// the owned locator values, and the resolved limit. Capabilities and the
// client key are never digest inputs.
func (s *QueryExecutionService) mysqlNavigationDigest(sourceDatabase, sourceObject string, fk *FKSummary, localValues []string, limit int) string {
	cols := make([]string, len(fk.Columns))
	for i, c := range fk.Columns {
		cols[i] = c.Column
	}
	return DigestRelatedRecords(RelatedRecordsDigestInput{
		Database: "", Schema: "",
		SourceSchema: sourceDatabase, SourceObject: sourceObject,
		SourceColumns: cols, ForeignKey: fk.Name,
		LocalValues: append([]string(nil), localValues...),
		Page:        0, PageSize: 0, MaxRows: limit,
	})
}

// mysqlStatementWithheld is the fixed evidence placeholder for a statement
// that cannot be rendered disclosure-safely — non-SELECT shapes or parse
// failures never fall back to raw SQL.
const mysqlStatementWithheld = "[statement withheld]"

// UTF-8-safe bounds for evidence statement metadata: the masked shape digest
// and the masked history preview.
const (
	mysqlStatementDigestMaxRunes  = 128
	mysqlStatementPreviewMaxRunes = 512
)

// mysqlStatementMetadata renders privacy-safe evidence metadata for one
// guarded statement: it re-parses with the guard's pinned Vitess parser,
// strips every comment and replaces every literal with a '?' placeholder on
// the AST, then serializes the sanitized tree for both the shape digest and
// the history preview. Only a successfully parsed SELECT renders; anything
// else — and any literal node the sanitizer does not cover, such as the
// parser's standalone BoolVal/NullVal expressions — returns the fixed
// withheld marker for both slots: never a raw-SQL fallback, never a literal
// value, never a comment. This is evidence metadata only: it runs after the
// guard has already accepted the statement and rewrites nothing the executor
// sees.
func mysqlStatementMetadata(g *QueryGuard, statement string) (digest, preview string) {
	stmt, err := g.parser.Parse(statement)
	if err != nil {
		return mysqlStatementWithheld, mysqlStatementWithheld
	}
	if _, ok := stmt.(*sqlparser.Select); !ok {
		return mysqlStatementWithheld, mysqlStatementWithheld
	}
	unsupported := false
	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (kont bool, werr error) {
		if c, ok := node.(sqlparser.Commented); ok {
			c.SetComments(nil)
		}
		if l, ok := node.(*sqlparser.Literal); ok {
			l.Type = sqlparser.IntVal
			l.Val = "?"
		}
		switch node.(type) {
		case sqlparser.BoolVal, *sqlparser.NullVal:
			unsupported = true
		}
		return true, nil
	}, stmt)
	if unsupported {
		return mysqlStatementWithheld, mysqlStatementWithheld
	}
	rendered := sqlparser.String(stmt)
	return utf8SafeTruncate(rendered, mysqlStatementDigestMaxRunes),
		utf8SafeTruncate(rendered, mysqlStatementPreviewMaxRunes)
}

// utf8SafeTruncate bounds s to at most maxRunes runes without splitting a
// multi-byte character.
func utf8SafeTruncate(s string, maxRunes int) string {
	n := 0
	for i := range s {
		if n == maxRunes {
			return s[:i]
		}
		n++
	}
	return s
}
