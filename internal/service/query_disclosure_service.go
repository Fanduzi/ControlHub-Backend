// Package service evaluates and applies result-disclosure policies for query
// results. It resolves column provenance from SQL AST or FK metadata, looks up
// per-column policies, and transforms result rows server-side before
// serialization. Absence of an exact matching policy is blocked (fail-closed).
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

// QueryDisclosureReader reads disclosure policies.
type QueryDisclosureReader interface {
	ListByTarget(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error)
	GetByScope(ctx context.Context, targetResourceID uint64, database, object, column string) (model.ResultDisclosurePolicy, error)
}

// QueryDisclosureWriter writes disclosure policies.
type QueryDisclosureWriter interface {
	Insert(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error)
	Update(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error
	Delete(ctx context.Context, targetResourceID uint64, database, object, column string) error
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
	if err := s.validateTargetExists(ctx, targetResourceID); err != nil {
		return nil, err
	}
	return s.policies.ListByTarget(ctx, targetResourceID)
}

// CreatePolicy inserts a new disclosure policy. Validates target existence and
// request fields first.
func (s *QueryDisclosureService) CreatePolicy(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	if err := req.Validate(); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if err := s.validateTargetExists(ctx, req.TargetResourceID); err != nil {
		return 0, err
	}
	return s.writer.Insert(ctx, req)
}

// UpdatePolicy modifies an existing disclosure policy. Validates target
// existence and request fields first.
func (s *QueryDisclosureService) UpdatePolicy(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if err := s.validateTargetExists(ctx, req.TargetResourceID); err != nil {
		return err
	}
	return s.writer.Update(ctx, req)
}

// DeletePolicy removes a disclosure policy by scope. It is idempotent.
func (s *QueryDisclosureService) DeletePolicy(ctx context.Context, targetResourceID uint64, database, object, column string) error {
	if err := s.validateTargetExists(ctx, targetResourceID); err != nil {
		return err
	}
	return s.writer.Delete(ctx, targetResourceID, database, object, column)
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
		return DisclosurePlan{}, fmt.Errorf("parse dsn for disclosure preflight: %w", err)
	}

	projection, err := resolveExecuteProjection(ctx, s.inspector, executeProjectionInput{
		dsn:      dsn,
		database: cfg.DBName,
		guarded:  guarded,
	})
	if err != nil {
		return DisclosurePlan{}, fmt.Errorf("%w: %v", ErrQueryDisclosureBlocked, err)
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
		return DisclosurePlan{}, fmt.Errorf("inspect related-record source for disclosure: %w", err)
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
		policy, err := s.policies.GetByScope(ctx, targetResourceID, col.SourceDatabase, col.SourceObject, col.SourceColumn)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return DisclosurePlan{}, fmt.Errorf("%w: no policy for %s.%s.%s", ErrQueryDisclosureBlocked, col.SourceDatabase, col.SourceObject, col.SourceColumn)
			}
			return DisclosurePlan{}, fmt.Errorf("lookup disclosure policy: %w", err)
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

// validateTargetExists checks that a target resource exists in the query target
// read model.
func (s *QueryDisclosureService) validateTargetExists(ctx context.Context, targetResourceID uint64) error {
	targets, _, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetResourceID})
	if err != nil {
		return ErrQueryTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetResourceID {
			return nil
		}
	}
	return ErrQueryTargetNotFound
}
