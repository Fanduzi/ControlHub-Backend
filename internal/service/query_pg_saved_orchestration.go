// Package service orchestrates the keyed saved-statement PostgreSQL entry.
// input: context, database/sql, encoding/json, errors, fmt, internal/model
// output: ExecutePGSavedStatementClaimed — the saved entry of the shared
// keyed PG runner
// pos: PG-MULTI-ENTRY stage 2 (issue #118 + saved-statement execution):
// request shape → target lookup → ONE joined statement snapshot read (R1-2:
// its failure branch returns only bare canonical sentinels — canceled,
// timeout, not-found, backend — each with one NULL-key fixed-evidence pair;
// wrapped or joined read errors never surface) →
// ownership/scope authorization → access resolution on the statement's own
// connection scope → typed value validation → template compilation → saved
// digest → the shared claimed runner. Saved executions never persist
// statement text, template identifiers, declarations, or parameter values —
// every evidence row carries the fixed withheld preview and an empty full
// statement.
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// ExecutePGSavedStatementClaimed runs one authorized saved statement through
// the same keyed claim/finalize/delivery machinery as the ordinary PG entry.
// The caller supplies no SQL and no connection selectors: the database and
// schema come only from the single authorized statement snapshot (explicit
// schema verbatim, empty resolving to the connection's default_schema), the
// statement text compiles through the trusted parameter substrate, and the
// attempt proves pinned-schema existence/USAGE inside the real transaction
// even for constant or fully qualified statements.
//
// Every refusal before admission — unknown target, missing/unauthorized
// statement, access denial, invalid values, compile failure — leaves the
// client key unoccupied; refusals after the target resolves write exactly
// one NULL-key evidence pair under the query.executed family. A claim-store
// failure writes one NULL-key failed pair; a finalize failure returns the
// safe persistence error with no compensating write.
func (s *QueryExecutionService) ExecutePGSavedStatementClaimed(
	ctx context.Context,
	identity model.QueryExecutionIdentity,
	targetID, statementID uint64,
	req model.QuerySavedStatementExecuteRequest,
) (PGClaimedOutcome, error) {
	// 1. Request shape / trusted identity — before any target or statement
	//    read. The saved entry is user-only: machine actors receive no new
	//    privileges. A malformed request consumes no key, reads nothing, and
	//    writes no evidence pair.
	if err := identity.Validate(); err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: invalid execution identity", ErrQueryValidationFailed)
	}
	if identity.Kind != model.QueryExecutionActorUser {
		return PGClaimedOutcome{}, fmt.Errorf("%w: saved statement execution requires a user identity", ErrQueryValidationFailed)
	}
	if statementID == 0 {
		return PGClaimedOutcome{}, fmt.Errorf("%w: statementId is required", ErrQueryValidationFailed)
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
	if err := req.Validate(); err != nil {
		return PGClaimedOutcome{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	page, pageSize := 1, pgClaimDefaultPageSize
	if req.Pagination != nil {
		page, pageSize = req.Pagination.Page, req.Pagination.PageSize
	}

	// Snapshot the caller-owned inputs — no synchronized concurrent mutation
	// is promised, so raw value bytes and capabilities are copied before any
	// further work.
	var rawValues map[string]json.RawMessage
	if req.Values != nil {
		rawValues = make(map[string]json.RawMessage, len(req.Values))
		for name, raw := range req.Values {
			rawValues[name] = append(json.RawMessage(nil), raw...)
		}
	}
	capabilities := append([]string(nil), req.Capabilities...)

	if s.statements == nil || s.compiler == nil || s.pgPolicies == nil || s.pgOutcomeFn == nil {
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	if _, ok := s.executions.(pgClaimStore); !ok {
		return PGClaimedOutcome{}, ErrQueryBackendFailure
	}
	start := s.clock.Now()

	// The digest every pre-claim saved-entry evidence row carries: the
	// statement identity and resolved paging only — deterministic and free
	// of parameter values, which never approach any persisted row before
	// typed validation succeeds.
	nullKeyDigest := DigestSavedStatement(SavedStatementDigestInput{
		StatementID: statementID,
		Page:        page, PageSize: pageSize, MaxRows: req.MaxRows,
	})

	// 2. Target lookup — the anchor for attributable evidence. An unknown
	//    target yields nothing to record against.
	target, err := s.access.findTarget(ctx, targetID)
	if err != nil {
		return PGClaimedOutcome{}, ErrQueryTargetNotFound
	}

	// 3. Exactly one joined snapshot read — statement header and
	//    declarations must come from the same committed version.
	statement, err := s.statements.GetByID(ctx, targetID, statementID)
	if err != nil {
		// The read error is discarded in every branch — a wrapped or joined
		// store/driver error can carry secrets, so only bare sentinels and
		// fixed evidence codes leave this switch.
		switch {
		case errors.Is(err, context.Canceled):
			return s.terminalPGClaimedSavedRead(ctx, target, identity, nullKeyDigest,
				model.QueryExecutionCancelled, "query_cancelled", "query canceled", context.Canceled, start)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrQueryTimeout):
			return s.terminalPGClaimedSavedRead(ctx, target, identity, nullKeyDigest,
				model.QueryExecutionTimeout, "query_timeout", "query exceeded the time limit", ErrQueryTimeout, start)
		case errors.Is(err, sql.ErrNoRows):
			return s.rejectPGClaimedSavedNotFound(ctx, target, identity, nullKeyDigest, start)
		default:
			// A backend read fault never unwraps store/driver detail: one
			// NULL-key failed pair plus the fixed backend sentinel.
			return s.failPGClaimedAccess(ctx, target, identity, pgClaimedEntrySaved,
				"", "", nullKeyDigest, start)
		}
	}
	// The returned snapshot must be exactly the requested statement, carry a
	// valid scope, and — for personal statements — belong to this caller
	// (admins included: identity carries no bypass). Every violation is the
	// same fixed not-found denial so existence and ownership never leak.
	if statement.ID != statementID || statement.TargetResourceID != targetID ||
		statement.Scope.Validate() != nil ||
		(statement.Scope == model.QuerySavedStatementPersonal && statement.OwnerUserID != identity.ID) {
		return s.rejectPGClaimedSavedNotFound(ctx, target, identity, nullKeyDigest, start)
	}
	definitions := make([]TemplateParameterDefinition, len(statement.Parameters))
	for index, parameter := range statement.Parameters {
		definitions[index] = TemplateParameterDefinition{
			Name: parameter.Name, Type: TemplateParameterType(parameter.Type),
		}
	}

	// 4. Access resolution on the authorized statement's own connection
	//    scope — the statement's database is the connection identity; a
	//    disabled, deleted, or rebound connection fails closed with no
	//    fallback to another scope.
	access, err := s.access.ResolvePGExecution(ctx, targetID, statement.DatabaseName)
	if err != nil {
		switch {
		case errors.Is(err, ErrQueryTargetNotFound):
			return PGClaimedOutcome{}, err
		case errors.Is(err, ErrSchemaValidationFailed):
			// A stored statement naming no database cannot name a PG
			// connection identity — a stored-data fault, not a request
			// refusal: one NULL-key failed pair, key untouched.
			return s.failPGClaimedAccess(ctx, target, identity, pgClaimedEntrySaved,
				statement.DatabaseName, statement.SchemaName, nullKeyDigest, start)
		default:
			return s.rejectPGClaimedAccess(ctx, access.Target, identity, pgClaimedEntrySaved,
				statement.DatabaseName, statement.SchemaName, nullKeyDigest, accessErrMessage(err), start)
		}
	}
	cred := access.Credential

	// 5. Resolved scope — the statement's explicit schema stays verbatim; an
	//    empty one falls back to the connection's configured default_schema.
	schema, maxRows, timeout := resolvePGClaimedDefaults(access, statement.SchemaName, req.MaxRows)

	// 6. Typed values validate only against the authorized declarations —
	//    wrong, missing, extra, nil, invalid-typed, or oversized values all
	//    land before claim under one NULL-key rejected pair.
	values, fields := validateTemplateValues(definitions, rawValues)
	if len(fields) > 0 {
		valueErr := &TemplateValueValidationError{Fields: fields}
		rec := s.buildPGClaimEvidenceRecord(target, identity, pgClaimedEntrySaved,
			statement.DatabaseName, statement.SchemaName, nullKeyDigest,
			model.QueryExecutionRejected, "validation_failed", valueErr.Error(), start)
		if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceQuery); perr != nil {
			return PGClaimedOutcome{}, errPersistAttempt
		}
		return PGClaimedOutcome{}, valueErr
	}

	// 7. Compile the authorized statement text — lexical placeholder
	//    resolution plus text encoding of the validated values. Native $n in
	//    the stored source is rejected here; the real guard runs inside the
	//    claimed attempt.
	plan, err := s.compiler.compilePGPlan(TemplateStatementInput{
		Statement: statement.Statement, Definitions: definitions, Values: values,
	})
	if err != nil {
		rec := s.buildPGClaimEvidenceRecord(target, identity, pgClaimedEntrySaved,
			statement.DatabaseName, statement.SchemaName, nullKeyDigest,
			model.QueryExecutionRejected, "validation_failed", "saved statement validation failed", start)
		if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceQuery); perr != nil {
			return PGClaimedOutcome{}, errPersistAttempt
		}
		return PGClaimedOutcome{}, fmt.Errorf("%w: saved statement validation failed", ErrQueryValidationFailed)
	}

	// 8. Request digest over resolved values — statement identity and the
	//    typed canonical values (sorted by name inside the digest), never
	//    capabilities or the client key.
	digest := DigestSavedStatement(SavedStatementDigestInput{
		Database: cred.DatabaseName, Schema: schema, StatementID: statementID,
		Values: pgClaimedSavedDigestValues(definitions, values),
		Page:   page, PageSize: pageSize, MaxRows: maxRows,
	})

	// 9. Hand the resolved request to the one keyed runner — the guarded
	// observable statement is the compiled text, bound by the private plan.
	return s.executePGClaimedPrepared(ctx, pgClaimedPrepared{
		kind:                pgClaimedEntrySaved,
		access:              access,
		identity:            identity,
		targetID:            targetID,
		clientKey:           req.ClientExecutionID,
		database:            cred.DatabaseName,
		schema:              schema,
		page:                page,
		pageSize:            pageSize,
		maxRows:             maxRows,
		timeout:             timeout,
		start:               start,
		statement:           plan.statement,
		digest:              digest,
		capabilities:        capabilities,
		nullKeyDatabase:     statement.DatabaseName,
		nullKeySchema:       statement.SchemaName,
		nullKeyDigest:       nullKeyDigest,
		parameters:          plan,
		requirePinnedSchema: true,
	})
}

// rejectPGClaimedSavedNotFound writes the single NULL-key rejected pair for
// a saved-statement not-found/not-authorized refusal and returns the fixed
// denial — the key stays unoccupied and statement scope stays empty so no
// statement data escapes through evidence.
func (s *QueryExecutionService) rejectPGClaimedSavedNotFound(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, digest string, start time.Time) (PGClaimedOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, pgClaimedEntrySaved,
		"", "", digest,
		model.QueryExecutionRejected, "saved_statement_not_found", "saved statement not found", start)
	if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceQuery); perr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	return PGClaimedOutcome{}, ErrQuerySavedStatementNotFound
}

// terminalPGClaimedSavedRead writes the single NULL-key evidence pair for a
// cancelled or timed-out saved-statement snapshot read and returns the bare
// canonical sentinel. The read runs before claim, so the record has no key,
// no backend identity, and no statement scope — the fixed status/code/message
// are all that persist.
func (s *QueryExecutionService) terminalPGClaimedSavedRead(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, digest string, status model.QueryExecutionStatus, code, msg string, retErr error, start time.Time) (PGClaimedOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, pgClaimedEntrySaved,
		"", "", digest, status, code, msg, start)
	if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceQuery); perr != nil {
		return PGClaimedOutcome{}, errPersistAttempt
	}
	return PGClaimedOutcome{}, retErr
}

// pgClaimedSavedDigestValues converts the validated typed values into
// canonical digest pairs, iterating the authorized declarations so each
// value's kind comes from the stored declaration — never from caller data.
func pgClaimedSavedDigestValues(definitions []TemplateParameterDefinition, values map[string]any) []ClaimDigestNamedValue {
	out := make([]ClaimDigestNamedValue, 0, len(definitions))
	for _, definition := range definitions {
		v, ok := values[definition.Name]
		if !ok {
			continue
		}
		var nv ClaimDigestNamedValue
		switch definition.Type {
		case TemplateParameterString:
			nv = ClaimDigestString(v.(string))
		case TemplateParameterInteger:
			nv = ClaimDigestInteger(v.(int64))
		case TemplateParameterDecimal:
			nv = ClaimDigestDecimal(v.(string))
		case TemplateParameterBoolean:
			nv = ClaimDigestBoolean(v.(bool))
		default:
			continue
		}
		nv.Name = definition.Name
		out = append(out, nv)
	}
	return out
}
