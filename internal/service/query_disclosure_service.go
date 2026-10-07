// Package service evaluates and applies result-disclosure policies for query results (fail-closed).
// input: context, database/sql, errors, fmt, mysql DSN, internal/model, QuerySchemaInspector, QueryTargetRepository
// output: QueryDisclosureService, DisclosurePlan, ColumnDisclosure, QueryDisclosureReader/Writer, ErrQueryDisclosure* sentinels
// pos: fail-closed disclosure governance (Phase 38Q); policy refusals stay blocked while machinery failures use a distinct backend sentinel (Issue #35); management paths enforce the canonical five-part scope with the engine-conditional schema rule (T9-A) and share one name-shape validation path across create/update/delete (T9-A-R1)
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
)

// ErrQueryDisclosureBlocked is returned when any column in a query's
// projection lacks an exact disclosure policy. It is the fail-closed default.
var ErrQueryDisclosureBlocked = errors.New("query blocked by result disclosure policy")

// ErrQueryDisclosureBackendFailure is returned when disclosure governance
// machinery itself fails (inspector/read/parse infrastructure), as opposed to a
// policy refusal. The execution service classifies it as a terminal failed
// attempt (Issue #35 AC 4: disclosure policy rejections stay rejected; all
// other post-target disclosure terminal failures record fixed safe failed or
// timeout evidence). The inner cause is %w-wrapped so a canceled or
// deadline-expired disclosure read stays classifiable.
var ErrQueryDisclosureBackendFailure = errors.New("query disclosure governance failed")

// ErrQueryDisclosurePolicyConflict is returned when a policy already exists
// for the requested scope (UNIQUE scope invariant).
var ErrQueryDisclosurePolicyConflict = errors.New("disclosure policy already exists for scope")

// ErrQueryDisclosurePolicyNotFound is returned when an update targets a scope
// with no existing policy.
var ErrQueryDisclosurePolicyNotFound = errors.New("disclosure policy not found")

// QueryDisclosureReader reads disclosure policies. GetByScope is the single
// exact five-part canonical lookup — (target_resource_id, database_name,
// schema_name, object_name, column_name) — that both the legacy MySQL/TiDB
// disclosure path (empty-schema identity) and the later PostgreSQL disclosure
// service consume. There is no fallback: a scope with no row is fail-closed
// "no policy", and lookup never borrows a same-named policy from another
// schema or database.
type QueryDisclosureReader interface {
	ListByTarget(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error)
	GetByScope(ctx context.Context, targetResourceID uint64, database, schema, object, column string) (model.ResultDisclosurePolicy, error)
}

// QueryDisclosureWriter writes disclosure policies.
type QueryDisclosureWriter interface {
	Insert(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error)
	Update(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error
	Delete(ctx context.Context, targetResourceID uint64, database, schema, object, column string) error
}

// DisclosurePlan is the resolved disclosure decision for a query's columns.
type DisclosurePlan struct {
	Columns []ColumnDisclosure
}

// ColumnDisclosure is the per-column disclosure decision.
type ColumnDisclosure struct {
	Provenance  ColumnProvenance
	Mode        model.ResultDisclosureMode
	CopyAllowed bool
}

// QueryDisclosureService evaluates and applies result-disclosure policies.
type QueryDisclosureService struct {
	policies  QueryDisclosureReader
	writer    QueryDisclosureWriter
	inspector QuerySchemaInspector
	targets   QueryTargetRepository
}

// NewQueryDisclosureService constructs a QueryDisclosureService.
func NewQueryDisclosureService(
	policies QueryDisclosureReader,
	writer QueryDisclosureWriter,
	inspector QuerySchemaInspector,
	targets QueryTargetRepository,
) *QueryDisclosureService {
	return &QueryDisclosureService{
		policies:  policies,
		writer:    writer,
		inspector: inspector,
		targets:   targets,
	}
}

// ListPolicies returns all disclosure policies for a target. Validates target
// existence first.
func (s *QueryDisclosureService) ListPolicies(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error) {
	if _, err := s.lookupTarget(ctx, targetResourceID); err != nil {
		return nil, err
	}
	return s.policies.ListByTarget(ctx, targetResourceID)
}

// CreatePolicy inserts a new disclosure policy. Validates target existence,
// request fields, and the engine-conditional scope contract first.
func (s *QueryDisclosureService) CreatePolicy(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	if err := req.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	target, err := s.lookupTarget(ctx, req.TargetResourceID)
	if err != nil {
		return 0, err
	}
	if err := validatePolicyScope(target, req.SchemaName, req.DatabaseName, req.ObjectName, req.ColumnName); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	return s.writer.Insert(ctx, req)
}

// UpdatePolicy modifies an existing disclosure policy. Validates target
// existence, request fields, and the engine-conditional scope contract first.
func (s *QueryDisclosureService) UpdatePolicy(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	target, err := s.lookupTarget(ctx, req.TargetResourceID)
	if err != nil {
		return err
	}
	if err := validatePolicyScope(target, req.SchemaName, req.DatabaseName, req.ObjectName, req.ColumnName); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if err := s.writer.Update(ctx, req); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQueryDisclosurePolicyNotFound
		}
		return err
	}
	return nil
}

// DeletePolicy removes a disclosure policy by its exact five-part scope. It is
// idempotent. The engine-conditional schema contract is the same as writes:
// PostgreSQL scopes require an explicit schema, other engines keep the
// empty-schema identity.
func (s *QueryDisclosureService) DeletePolicy(ctx context.Context, targetResourceID uint64, database, schema, object, column string) error {
	target, err := s.lookupTarget(ctx, targetResourceID)
	if err != nil {
		return err
	}
	if err := validatePolicyScope(target, schema, database, object, column); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	return s.writer.Delete(ctx, targetResourceID, database, schema, object, column)
}

// Preflight resolves column provenance from a guarded SQL statement and checks
// disclosure policies. Returns a DisclosurePlan with per-column modes, or
// ErrQueryDisclosureBlocked if any column lacks an exact policy. Called after
// QueryGuard.Guard, before executor.
func (s *QueryDisclosureService) Preflight(
	ctx context.Context,
	dsn string,
	targetResourceID uint64,
	guarded GuardedQuery,
) (DisclosurePlan, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return DisclosurePlan{}, fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, err)
	}

	projection, err := resolveExecuteProjection(ctx, s.inspector, executeProjectionInput{
		dsn:      dsn,
		database: cfg.DBName,
		guarded:  guarded,
	})
	if err != nil {
		// Unsupported/unresolvable projection shape is a governance refusal
		// (blocked). Anything else in the projection read is disclosure
		// machinery failure (infra/read), not a policy rejection: the cause is
		// %w-wrapped so the execution service can classify cancellation and
		// deadline via errors.Is (Issue #35).
		if errors.Is(err, errProjectionUnsupported) {
			return DisclosurePlan{}, fmt.Errorf("%w: %v", ErrQueryDisclosureBlocked, err)
		}
		return DisclosurePlan{}, fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, err)
	}

	if len(projection.Columns) == 0 {
		return DisclosurePlan{}, fmt.Errorf("%w: statement produces no resolvable columns for disclosure governance", ErrQueryDisclosureBlocked)
	}

	return s.buildDisclosurePlan(ctx, targetResourceID, projection)
}

// PreflightRelatedRecords resolves column provenance from FK metadata (no SQL
// parsing) and checks disclosure policies. Called with the referenced table's
// database and name before executor.
func (s *QueryDisclosureService) PreflightRelatedRecords(
	ctx context.Context,
	dsn string,
	targetResourceID uint64,
	referencedDatabase string,
	referencedTable string,
) (DisclosurePlan, error) {
	detail, err := s.inspector.GetObjectDetails(ctx, dsn, referencedDatabase, referencedTable, "table")
	if err != nil {
		// Inspector/read infrastructure failure is disclosure machinery
		// failure, not a policy refusal: wrap it in the backend sentinel so the
		// navigation service classifies it as fixed failed evidence exactly
		// like core execution (Issue #35 AC 4 / Issue #36).
		return DisclosurePlan{}, fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, err)
	}
	if detail == nil {
		return DisclosurePlan{}, fmt.Errorf("%w: related-record metadata is missing", ErrQueryDisclosureBlocked)
	}

	projection := ProjectionPlan{Columns: make([]ColumnProvenance, 0, len(detail.Columns))}
	for _, col := range detail.Columns {
		projection.Columns = append(projection.Columns, ColumnProvenance{
			OutputName:     col.Name,
			SourceDatabase: referencedDatabase,
			SourceObject:   referencedTable,
			SourceColumn:   col.Name,
		})
	}

	return s.buildDisclosurePlan(ctx, targetResourceID, projection)
}

// Apply transforms result rows according to the disclosure plan. For
// masked_no_copy columns, non-null values are replaced with "[MASKED]". For
// raw_copy_allowed columns, values pass through unchanged. Returns transformed
// rows and updated column metadata with DisplayMode/CopyAllowed.
//
// Returns an error if the plan column count doesn't match the result column
// count — this catches schema drift between preflight and execution that could
// otherwise leak unplanned raw values.
//
// Defensive validation: each ColumnDisclosure is validated before copying rows.
// Invalid mode/copy pairs, blocked mode, empty mode, or unknown values cause
// rejection before any row values can be returned.
func (s *QueryDisclosureService) Apply(
	plan DisclosurePlan,
	columns []model.QueryResultColumn,
	rows [][]any,
) ([]model.QueryResultColumn, [][]any, error) {
	if len(plan.Columns) == 0 {
		return nil, nil, fmt.Errorf("%w: disclosure plan has no columns; cannot validate result safety", ErrQueryDisclosureBlocked)
	}

	if len(plan.Columns) != len(columns) {
		return nil, nil, fmt.Errorf("%w: plan has %d columns but result has %d", ErrQueryDisclosureBlocked, len(plan.Columns), len(columns))
	}

	// Defensive validation: verify each column disclosure before copying rows.
	for i, cd := range plan.Columns {
		if err := cd.Mode.Validate(); err != nil {
			return nil, nil, fmt.Errorf("%w: column %d has invalid mode: %v", ErrQueryDisclosureBlocked, i, err)
		}
		if cd.Mode == model.ResultDisclosureRawCopyAllowed && !cd.CopyAllowed {
			return nil, nil, fmt.Errorf("%w: column %d has raw mode but copyAllowed=false", ErrQueryDisclosureBlocked, i)
		}
		if cd.Mode == model.ResultDisclosureMaskedNoCopy && cd.CopyAllowed {
			return nil, nil, fmt.Errorf("%w: column %d has masked mode but copyAllowed=true", ErrQueryDisclosureBlocked, i)
		}
	}

	// Update column metadata with disclosure decisions.
	outColumns := make([]model.QueryResultColumn, len(columns))
	copy(outColumns, columns)
	for i, cd := range plan.Columns {
		outColumns[i].DisplayMode = cd.Mode
		outColumns[i].CopyAllowed = cd.CopyAllowed
	}

	// Transform row values for masked columns.
	outRows := make([][]any, len(rows))
	for r, row := range rows {
		if len(row) != len(columns) {
			return nil, nil, fmt.Errorf("%w: row %d has %d cells but expected %d", ErrQueryDisclosureBlocked, r, len(row), len(columns))
		}
		outRow := make([]any, len(row))
		copy(outRow, row)
		for c, cd := range plan.Columns {
			outRow[c] = applyDisclosureMask(outRow[c], cd.Mode)
		}
		outRows[r] = outRow
	}

	return outColumns, outRows, nil
}

// buildDisclosurePlan looks up policies for each projected column and assembles
// the DisclosurePlan. Returns ErrQueryDisclosureBlocked if any column lacks an
// exact policy. Literal-only columns (empty source fields) are automatically
// marked raw_copy_allowed without a policy lookup.
func (s *QueryDisclosureService) buildDisclosurePlan(ctx context.Context, targetResourceID uint64, projection ProjectionPlan) (DisclosurePlan, error) {
	plan := DisclosurePlan{Columns: make([]ColumnDisclosure, 0, len(projection.Columns))}
	for _, col := range projection.Columns {
		if col.SourceDatabase == "" && col.SourceObject == "" && col.SourceColumn == "" {
			// Literal-only column: no table data to govern.
			plan.Columns = append(plan.Columns, ColumnDisclosure{
				Provenance:  col,
				Mode:        model.ResultDisclosureRawCopyAllowed,
				CopyAllowed: true,
			})
			continue
		}
		policy, err := s.policies.GetByScope(ctx, targetResourceID, col.SourceDatabase, col.SourceSchema, col.SourceObject, col.SourceColumn)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return DisclosurePlan{}, fmt.Errorf("%w: no policy for %s.%s.%s", ErrQueryDisclosureBlocked, col.SourceDatabase, col.SourceObject, col.SourceColumn)
			}
			// Policy-read infrastructure failure: not a policy refusal, so the
			// execution service records it as a terminal failed attempt instead of
			// a rejected one (Issue #35 AC 4).
			return DisclosurePlan{}, fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, err)
		}
		if err := policy.Mode.Validate(); err != nil {
			return DisclosurePlan{}, fmt.Errorf("%w: invalid stored mode for %s.%s.%s: %v", ErrQueryDisclosureBlocked, col.SourceDatabase, col.SourceObject, col.SourceColumn, err)
		}
		plan.Columns = append(plan.Columns, ColumnDisclosure{
			Provenance:  col,
			Mode:        policy.Mode,
			CopyAllowed: policy.Mode == model.ResultDisclosureRawCopyAllowed,
		})
	}
	return plan, nil
}

// validatePolicyScope is the canonical scope-validation path shared by create,
// update, and delete (T9-A-R1). It first applies the engine-agnostic name
// shape every management operation must enforce — requiredness, bounded
// length, no NUL bytes — via the same model helper upsert requests use, so a
// scope reaching the writer can never skip checks just because it carries no
// mode. It then applies the engine-conditional contract for the canonical
// five-part key (G1/G10): a PostgreSQL policy identity must carry an explicit
// schema — missing schema is a controlled validation error, never silently
// resolved to public or the connection default_schema — while every other
// engine keeps the legacy empty schema and the strict ASCII identifier rule
// MySQL/TiDB policies have always used.
func validatePolicyScope(target model.QueryTarget, schema, database, object, column string) error {
	for _, f := range []struct {
		name, value string
		required    bool
	}{
		{"database_name", database, true},
		{"schema_name", schema, false},
		{"object_name", object, true},
		{"column_name", column, true},
	} {
		if err := model.ValidateIdentifierShape(f.name, f.value, f.required); err != nil {
			return err
		}
	}
	if target.ConnectionContext.Engine == "postgresql" {
		if schema == "" {
			return fmt.Errorf("schema_name is required for postgresql policy scopes")
		}
		return nil
	}
	if schema != "" {
		return fmt.Errorf("schema_name must be empty for %q policy scopes", target.ConnectionContext.Engine)
	}
	for _, f := range []struct{ name, value string }{
		{"database_name", database},
		{"object_name", object},
		{"column_name", column},
	} {
		if err := model.ValidateLegacyScopeIdentifier(f.name, f.value); err != nil {
			return err
		}
	}
	return nil
}

// lookupTarget returns the target resource from the query target read model.
func (s *QueryDisclosureService) lookupTarget(ctx context.Context, targetResourceID uint64) (model.QueryTarget, error) {
	targets, _, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetResourceID})
	if err != nil {
		return model.QueryTarget{}, ErrQueryTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetResourceID {
			return t, nil
		}
	}
	return model.QueryTarget{}, ErrQueryTargetNotFound
}
