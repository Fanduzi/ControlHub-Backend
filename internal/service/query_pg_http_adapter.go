// Package service adapts the three HTTP-facing governed methods onto the
// keyed PostgreSQL orchestration entries.
// input: context, internal/model — trusted identity, dispatched target,
// decoded request models
// output: executePGHTTP, executeSavedPGHTTP, navigatePGHTTP — narrow
// transport adapters that translate public requests into the internal keyed
// entries and project PGClaimedOutcome back into the shared response
// envelopes; no second claim/finalize/guard path exists here
// pos: T12 PG-HTTP dispatch seam (issue #120) — reached only when the
// construction-time WithPGHTTPEntries capability is wired and the trusted
// target engine is postgresql. Request adaptation is verbatim: database and
// schema keep their raw bytes, the client key is never normalized, and the
// saved/navigation requests add no SQL, declarations, Args, or target
// selectors. Response projection consumes only governed artifacts — the
// disclosed result, the unique PageResult window facts, and the
// runner-filled delivery scalars — never raw executor internals (OIDs,
// attnum, witness columns, sources, or binding metadata).
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"

	"github.com/fan/controlhub/internal/model"
)

// executePGHTTP adapts the public ordinary-execute request to the keyed PG
// entry and projects its outcome into the shared execute envelope. The
// dispatching caller has already read the target metadata only; every
// governed check — identity, scope, key requiredness, access, claim,
// execution, disclosure, finalize, and delivery — stays inside
// ExecutePGClaimed.
func (s *QueryExecutionService) executePGHTTP(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	pgReq := PGClaimedExecuteRequest{
		ClientExecutionID: req.ClientExecutionID,
		Database:          req.Database,
		Schema:            req.Schema,
		Statement:         req.Statement,
		MaxRows:           req.MaxRows,
		Capabilities:      append([]string(nil), req.Capabilities...),
	}
	if req.Pagination != nil {
		pgReq.Page, pgReq.PageSize = req.Pagination.Page, req.Pagination.PageSize
	}
	out, err := s.ExecutePGClaimed(ctx, identity, target.ResourceID, pgReq)
	if err != nil {
		return model.QueryExecuteResponse{}, err
	}
	if out.Result == nil {
		// A nil-result success is a contract violation the runner already
		// classifies; this branch only guards the projection.
		return model.QueryExecuteResponse{}, ErrQueryBackendFailure
	}
	return pgExecuteResponse(target, out), nil
}

// executeSavedPGHTTP adapts the saved-statement execute request to the
// keyed PG saved entry. The request carries no SQL and no scope fields of
// its own beyond the ?database= selector; values, capability, and paging
// pass through verbatim into the single-snapshot authorization and
// compilation chain.
func (s *QueryExecutionService) executeSavedPGHTTP(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, statementID uint64, req model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error) {
	out, err := s.ExecutePGSavedStatementClaimed(ctx, identity, target.ResourceID, statementID, req)
	if err != nil {
		return model.QueryExecuteResponse{}, err
	}
	if out.Result == nil {
		return model.QueryExecuteResponse{}, ErrQueryBackendFailure
	}
	return pgExecuteResponse(target, out), nil
}

// navigatePGHTTP adapts the public related-records request to the keyed PG
// navigation entry and projects its outcome into the shared navigation
// envelope. The public request carries no pagination — the internal
// defaults and PageResult produce the delivered page; no public paging
// field is invented here.
func (s *QueryExecutionService) navigatePGHTTP(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error) {
	out, err := s.NavigatePGRelatedRecordsClaimed(ctx, identity, target.ResourceID, PGClaimedNavigationRequest{
		ClientExecutionID: req.ClientExecutionID,
		Source:            req.Source,
		LocalValues:       append([]string(nil), req.LocalValues...),
		MaxRows:           req.MaxRows,
		Capabilities:      append([]string(nil), req.Capabilities...),
	})
	if err != nil {
		return model.RelatedRecordNavigationResponse{}, err
	}
	if out.Result == nil || out.Relation == nil {
		return model.RelatedRecordNavigationResponse{}, ErrQueryBackendFailure
	}
	return pgNavigationResponse(target, out), nil
}

// pgExecuteResponse projects a delivered keyed outcome into the public
// execute envelope. Every field sources a governed fact: the real finalize
// ID and terminal status, the decision's disclosed columns/rows/matrix,
// the unique PageResult window, and the runner-filled delivery scalars.
// The PG window is mandatory inside the governed chain, so the resolved
// page always reports — never the request's absent-pagination shape.
func pgExecuteResponse(target model.QueryTarget, out PGClaimedOutcome) model.QueryExecuteResponse {
	res := out.Result
	return model.QueryExecuteResponse{
		ExecutionID:      out.ExecutionID,
		Status:           out.Status,
		TargetResourceID: target.ResourceID,
		Engine:           target.ConnectionContext.Engine,
		Columns:          pgPublicColumns(res.Columns),
		Rows:             pgPublicRows(res.Rows),
		RowCount:         res.Page.RowCount,
		Truncated:        res.Page.BudgetReached,
		CellTruncated:    res.CellTruncated,
		DurationMs:       out.Delivery.DurationMs,
		LimitApplied:     out.Delivery.MaxRows,
		ExecutedAt:       out.Delivery.ExecutedAt,
		Pagination: &model.QueryExecutePaginationResponse{
			Page:            out.Delivery.Page,
			PageSize:        out.Delivery.PageSize,
			HasPreviousPage: out.Delivery.Page > 1,
			HasNextPage:     res.Page.HasMore,
		},
		Context: &model.QueryExecutionContext{
			Database: out.Delivery.Database,
			Schema:   out.Delivery.Schema,
		},
	}
}

// pgNavigationResponse projects a delivered navigation outcome into the
// public related-records envelope. sourceDatabase/referencedDatabase both
// report the one fixed connection database; the source and referenced
// schemas report through their own fields; context echoes the pinned
// execution scope — never the referenced relation's schema.
func pgNavigationResponse(target model.QueryTarget, out PGClaimedNavigationOutcome) model.RelatedRecordNavigationResponse {
	res := out.Result
	rel := out.Relation
	return model.RelatedRecordNavigationResponse{
		ExecutionID:        out.ExecutionID,
		Status:             out.Status,
		TargetResourceID:   target.ResourceID,
		Engine:             target.ConnectionContext.Engine,
		Columns:            pgPublicColumns(res.Columns),
		Rows:               pgPublicRows(res.Rows),
		RowCount:           res.Page.RowCount,
		Truncated:          res.Page.BudgetReached,
		CellTruncated:      res.CellTruncated,
		DurationMs:         out.Delivery.DurationMs,
		LimitApplied:       out.Delivery.MaxRows,
		ExecutedAt:         out.Delivery.ExecutedAt,
		SourceDatabase:     rel.Database,
		SourceObject:       rel.SourceObject,
		ForeignKey:         rel.ForeignKey,
		ReferencedDatabase: rel.Database,
		ReferencedObject:   rel.ReferencedObject,
		ReferencedColumns:  append([]string(nil), rel.ReferencedColumns...),
		SourceSchema:       rel.SourceSchema,
		ReferencedSchema:   rel.ReferencedSchema,
		Context: &model.QueryExecutionContext{
			Database: out.Delivery.Database,
			Schema:   out.Delivery.Schema,
		},
	}
}

// pgPublicColumns projects disclosed columns to the public column contract:
// name, database type, the disclosure decision, and copyability. Nullable
// is conservatively true — the PG path carries no result-level NOT NULL
// proof, so a reported true means "cannot exclude NULL", never a catalog
// claim. Internal identity metadata (DataTypeOID, TableOID, AttrNum) never
// crosses this boundary.
func pgPublicColumns(cols []PGDisclosedColumn) []model.QueryResultColumn {
	out := make([]model.QueryResultColumn, len(cols))
	for i, c := range cols {
		out[i] = model.QueryResultColumn{
			Name:         c.Name,
			DatabaseType: c.DatabaseType,
			Nullable:     true,
			DisplayMode:  c.DisplayMode,
			CopyAllowed:  c.CopyAllowed,
		}
	}
	return out
}

// pgPublicRows adapts native-text cells to the JSON row shape: a non-NULL
// cell delivers its verbatim string, a NULL cell delivers nil. A nil page
// normalizes to an empty array so the public contract never emits null.
func pgPublicRows(rows [][]*string) [][]any {
	out := pgDisclosedRowsAny(rows)
	if out == nil {
		return [][]any{}
	}
	return out
}
