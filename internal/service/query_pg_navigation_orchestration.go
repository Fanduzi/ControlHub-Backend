// Package service holds the keyed related-record navigation front half.
// input: QueryExecutionIdentity, targetID, PGClaimedNavigationRequest
// (source identity + ordered locator strings + paging + key + capabilities)
// output: PGClaimedNavigationOutcome — runner outcome plus trusted relation
// metadata on terminal success; every terminal state flows through the same
// shared claimed-execution runner, never a separate claim/finalize path
// pos: PG-MULTI-ENTRY stage 3 — the server-selected related_record_navigation
// entry: request-shape validation before any access, governed access on the
// source database, one bounded pre-claim metadata preparation (R1-3: every
// preparation error canonicalizes to a bare sentinel via navPrepFail before
// outcome selection — factory sentinels return their safe identity with one
// NULL-key failed pair, wrapped detail never surfaces), digest over
// resolved identity + ordered locators, then the shared prepared runner.
// Binding SQL and the locator plan come only from the trusted private
// builder; raw locator text, generated SQL, and OID metadata never reach
// errors, history, or audit.
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
)

// PGClaimedNavigationRequest is the navigation entry's request contract: the
// source identity, ordered local locator values in constraint order, paging,
// the required client idempotency key, and capabilities. No SQL, no target
// (schema/object) coordinates, and no value OID hints ever enter — the
// server resolves the referenced relation from the FK mapping alone.
type PGClaimedNavigationRequest struct {
	ClientExecutionID string
	Source            model.RelatedRecordNavigationSource
	LocalValues       []string
	MaxRows           int
	Page              int
	PageSize          int
	Capabilities      []string
}

// PGNavigationRelation is the trusted relation metadata returned on terminal
// success — canonical schemas/objects/FK/column names only, never locator
// values, SQL, or OID internals.
type PGNavigationRelation struct {
	Database          string   `json:"database"`
	SourceSchema      string   `json:"sourceSchema"`
	SourceObject      string   `json:"sourceObject"`
	ForeignKey        string   `json:"foreignKey"`
	SourceColumns     []string `json:"sourceColumns"`
	ReferencedSchema  string   `json:"referencedSchema"`
	ReferencedObject  string   `json:"referencedObject"`
	ReferencedColumns []string `json:"referencedColumns"`
}

// PGClaimedNavigationOutcome is the navigation entry's return: the shared
// claimed-execution outcome plus trusted relation metadata populated only on
// terminal success (including a persisted-success capability refusal).
type PGClaimedNavigationOutcome struct {
	PGClaimedOutcome
	Relation *PGNavigationRelation
}

// NavigatePGRelatedRecordsClaimed is the keyed related-record navigation
// entry — the server-selected closed entry kind. It validates request shape
// before any access, resolves governed access on the source database only,
// prepares the FK mapping in one bounded read-only transaction, then hands a
// trusted navigation plan and binding to the shared claimed-execution
// runner. Caller-visible failures carry only fixed safe codes/messages.
func (s *QueryExecutionService) NavigatePGRelatedRecordsClaimed(ctx context.Context, identity model.QueryExecutionIdentity, targetID uint64, req PGClaimedNavigationRequest) (PGClaimedNavigationOutcome, error) {
	// 1. Request shape — everything cheap and request-local before any
	//    access resolution, database read, or key occupancy.
	if err := identity.Validate(); err != nil {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "identity is required")
	}
	if identity.Kind != model.QueryExecutionActorUser {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "identity must be a user")
	}
	if req.ClientExecutionID == "" {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "clientExecutionId is required")
	}
	if err := model.ValidateClientExecutionID(req.ClientExecutionID); err != nil {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "clientExecutionId is invalid")
	}
	if strings.IndexByte(req.ClientExecutionID, 0) >= 0 {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "clientExecutionId is invalid")
	}
	shape := model.RelatedRecordNavigationRequest{
		Source:            req.Source,
		LocalValues:       append([]string(nil), req.LocalValues...),
		MaxRows:           req.MaxRows,
		ClientExecutionID: req.ClientExecutionID,
		Capabilities:      append([]string(nil), req.Capabilities...),
	}
	if err := shape.Validate(); err != nil {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, err.Error())
	}
	page, pageSize, err := resolvePGClaimPagination(req.Page, req.PageSize)
	if err != nil {
		return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, err.Error())
	}
	// PostgreSQL keys the navigation database and schema explicitly — both
	// are required names here (the credential default applies only to
	// saved/ordinary statements, never to source coordinates).
	for _, field := range []struct{ name, value string }{
		{"source.database", req.Source.Database},
		{"source.schema", req.Source.Schema},
		{"source.object", req.Source.Object},
		{"source.foreignKey", req.Source.ForeignKey},
	} {
		if err := model.ValidateIdentifierShape(field.name, field.value, true); err != nil {
			return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, err.Error())
		}
		if !utf8.ValidString(field.value) {
			return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, field.name+" is invalid")
		}
	}
	localValues := append([]string(nil), req.LocalValues...)
	for _, v := range localValues {
		if !utf8.ValidString(v) || strings.IndexByte(v, 0) >= 0 {
			return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "localValues contains invalid text")
		}
	}
	capabilities := append([]string(nil), req.Capabilities...)

	if s.pgPolicies == nil || s.pgOutcomeFn == nil || s.pgNavPrepare == nil {
		return PGClaimedNavigationOutcome{}, ErrQueryBackendFailure
	}
	if _, ok := s.executions.(pgClaimStore); !ok {
		return PGClaimedNavigationOutcome{}, ErrQueryBackendFailure
	}

	start := s.clock.Now()
	source := req.Source
	nullKeyDigest := DigestRelatedRecords(RelatedRecordsDigestInput{
		Database:     source.Database,
		Schema:       source.Schema,
		SourceSchema: source.Schema,
		SourceObject: source.Object,
		ForeignKey:   source.ForeignKey,
		Page:         page,
		PageSize:     pageSize,
		MaxRows:      req.MaxRows,
	})

	// 2. Governed access — the source database is the connection identity;
	//    the referenced schema is resolved by the FK mapping on that
	//    connection, never from another database.
	access, err := s.access.ResolvePGExecution(ctx, targetID, source.Database)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			return PGClaimedNavigationOutcome{}, err
		}
		if errors.Is(err, ErrSchemaValidationFailed) {
			return PGClaimedNavigationOutcome{}, fmt.Errorf("%w: %s", ErrQueryValidationFailed, "source database is required")
		}
		out, rerr := s.rejectPGClaimedAccess(ctx, access.Target, identity, pgClaimedEntryNavigation,
			source.Database, source.Schema, nullKeyDigest, accessErrMessage(err), start)
		return PGClaimedNavigationOutcome{PGClaimedOutcome: out}, rerr
	}
	target := access.Target
	cred := access.Credential
	schema, maxRows, timeout := resolvePGClaimedDefaults(access, source.Schema, req.MaxRows)

	// 3. Preparation — the fixed metadata probe on one bounded READ ONLY
	//    transaction before any claim. A missing/invisible source or FK is
	//    the controlled not-found refusal; cancel/deadline keep their real
	//    category; anything else is the fixed internal failure.
	binding, err := s.pgNavPrepare(ctx, access.pgConfig, cred.DatabaseName, source, timeout)
	if err != nil {
		// Canonicalize before selecting the outcome: the preparation seam is
		// not trusted to return a bare sentinel — a wrapped or joined error
		// from any provider collapses to the fixed safe identity here, so
		// detail can never reach the caller or the persisted row.
		switch err = navPrepFail(err); {
		case errors.Is(err, context.Canceled):
			return s.terminalPGClaimedNavigation(ctx, target, identity, source.Database, schema,
				nullKeyDigest, model.QueryExecutionCancelled, "query_cancelled", "query canceled", context.Canceled, start)
		case errors.Is(err, ErrQueryTimeout):
			return s.terminalPGClaimedNavigation(ctx, target, identity, source.Database, schema,
				nullKeyDigest, model.QueryExecutionTimeout, "query_timeout", "database query timed out", ErrQueryTimeout, start)
		case errors.Is(err, errPGNavNoSource):
			return s.terminalPGClaimedNavigation(ctx, target, identity, source.Database, schema,
				nullKeyDigest, model.QueryExecutionRejected, "navigation_source_not_found", "navigation source object or foreign key not found", ErrNavigationSourceNotFound, start)
		case errors.Is(err, ErrPGVersionUnsupported), errors.Is(err, ErrPGConnectFailed), errors.Is(err, ErrPGVersionReadFailed):
			// The pool factory's fixed sentinels are already caller-safe: one
			// NULL-key failed pair with the same evidence the generic backend
			// failure carries, and the canonical sentinel itself returns so
			// PGPoolErrorCode still identifies the factory failure class.
			return s.terminalPGClaimedNavigation(ctx, target, identity, source.Database, schema,
				nullKeyDigest, model.QueryExecutionFailed, "query_backend_error", "target database query failed", err, start)
		default:
			out, ferr := s.failPGClaimedAccess(ctx, target, identity, pgClaimedEntryNavigation,
				source.Database, schema, nullKeyDigest, start)
			return PGClaimedNavigationOutcome{PGClaimedOutcome: out}, ferr
		}
	}
	// The preparation result becomes coordinator-owned here: a nil success
	// or a malformed mapping is a fixed internal failure with one NULL-key
	// failed pair — never a panic and never a key occupancy. The snapshot
	// is the only binding the digest, plan, attempt, and relation metadata
	// ever read; a preparation provider mutating its result afterwards
	// cannot reach any of them.
	binding, err = snapshotPGNavigationBinding(binding)
	if err != nil {
		out, ferr := s.failPGClaimedAccess(ctx, target, identity, pgClaimedEntryNavigation,
			source.Database, schema, nullKeyDigest, start)
		return PGClaimedNavigationOutcome{PGClaimedOutcome: out}, ferr
	}
	if binding == nil {
		out, ferr := s.failPGClaimedAccess(ctx, target, identity, pgClaimedEntryNavigation,
			source.Database, schema, nullKeyDigest, start)
		return PGClaimedNavigationOutcome{PGClaimedOutcome: out}, ferr
	}

	// 4. Locator cardinality — the ordered local column group must match the
	//    FK column count before claim.
	if len(localValues) != len(binding.pairs) {
		return s.terminalPGClaimedNavigation(ctx, target, identity, source.Database, schema,
			nullKeyDigest, model.QueryExecutionRejected, "navigation_value_mismatch", "navigation local values do not match the foreign key", ErrNavigationValueMismatch, start)
	}

	// 5. Trusted plan — generated only from the verified binding.
	plan, err := newPGNavigationPlan(binding, localValues)
	if err != nil {
		out, ferr := s.failPGClaimedAccess(ctx, target, identity, pgClaimedEntryNavigation,
			source.Database, schema, nullKeyDigest, start)
		return PGClaimedNavigationOutcome{PGClaimedOutcome: out}, ferr
	}

	localColumns := make([]string, 0, len(binding.pairs))
	for _, pair := range binding.pairs {
		localColumns = append(localColumns, pair.localName)
	}
	digest := DigestRelatedRecords(RelatedRecordsDigestInput{
		Database:      cred.DatabaseName,
		Schema:        schema,
		SourceSchema:  binding.source.schema,
		SourceObject:  binding.source.name,
		SourceColumns: localColumns,
		ForeignKey:    binding.fkName,
		LocalValues:   localValues,
		Page:          page,
		PageSize:      pageSize,
		MaxRows:       maxRows,
	})

	out, err := s.executePGClaimedPrepared(ctx, pgClaimedPrepared{
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
		kind:                pgClaimedEntryNavigation,
		parameters:          plan,
		requirePinnedSchema: true,
		navigation:          binding,
		nullKeyDatabase:     source.Database,
		nullKeySchema:       source.Schema,
		nullKeyDigest:       nullKeyDigest,
	})
	nav := PGClaimedNavigationOutcome{PGClaimedOutcome: out}
	// Trusted relation metadata only on actual terminal success — including
	// the persisted-success capability refusal.
	if out.Status == model.QueryExecutionSuccess {
		refColumns := make([]string, 0, len(binding.pairs))
		for _, pair := range binding.pairs {
			refColumns = append(refColumns, pair.refName)
		}
		nav.Relation = &PGNavigationRelation{
			Database:          cred.DatabaseName,
			SourceSchema:      binding.source.schema,
			SourceObject:      binding.source.name,
			ForeignKey:        binding.fkName,
			SourceColumns:     localColumns,
			ReferencedSchema:  binding.target.schema,
			ReferencedObject:  binding.target.name,
			ReferencedColumns: refColumns,
		}
	}
	return nav, err
}

// terminalPGClaimedNavigation writes the single NULL-key evidence pair for a
// navigation refusal after access resolution — rejected/not-found/value-
// mismatch/cancelled/timeout all land here keyed by the deterministic
// pre-claim digest, so the later keyed call stays a discoverable no-write
// path of its own.
func (s *QueryExecutionService) terminalPGClaimedNavigation(ctx context.Context, target model.QueryTarget, identity model.QueryExecutionIdentity, database, schema, digest string, status model.QueryExecutionStatus, code, msg string, retErr error, start time.Time) (PGClaimedNavigationOutcome, error) {
	rec := s.buildPGClaimEvidenceRecord(target, identity, pgClaimedEntryNavigation, database, schema, digest, status, code, msg, start)
	if _, perr := s.persistEvidencePair(ctx, rec, executionEvidenceNavigation); perr != nil {
		return PGClaimedNavigationOutcome{}, errPersistAttempt
	}
	return PGClaimedNavigationOutcome{}, retErr
}
