// Package service resolves result-column provenance from guarded SQL projections.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

var errProjectionUnsupported = errors.New("query projection cannot be resolved")

// ColumnProvenance identifies the source table column for a projected column.
type ColumnProvenance struct {
	OutputName     string // name as it appears in the result set
	SourceDatabase string
	SourceObject   string // table name
	SourceColumn   string
}

// ProjectionPlan is the resolved list of column provenances from a SELECT.
type ProjectionPlan struct {
	Columns []ColumnProvenance
}

type projectionSource struct {
	database string
	object   string
	alias    string
}

type executeProjectionInput struct {
	dsn      string
	database string
	guarded  GuardedQuery
}

type relatedRecordProjectionInput struct {
	dsn               string
	database          string
	object            string
	referencedColumns []string
}

// resolveExecuteProjection resolves only direct column projections from the
// original statement that the query guard approved.
func resolveExecuteProjection(ctx context.Context, inspector QuerySchemaInspector, input executeProjectionInput) (ProjectionPlan, error) {
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		return ProjectionPlan{}, fmt.Errorf("construct SQL parser: %w", err)
	}

	statement, err := parser.Parse(input.guarded.OriginalStatement)
	if err != nil {
		return ProjectionPlan{}, fmt.Errorf("%w: parse statement: %v", errProjectionUnsupported, err)
	}
	selectStatement, ok := statement.(*sqlparser.Select)
	if !ok {
		// Non-SELECT statements (SHOW, DESCRIBE, EXPLAIN, etc.) do not project
		// user table column values; no disclosure governance is needed.
		return ProjectionPlan{}, nil
	}
	if isNoTableProjection(selectStatement) {
		// SELECT without a real table (e.g., SELECT 1, which the parser models
		// with the "dual" pseudo-table) projects no table column values, so
		// there is nothing to govern for disclosure.
		return ProjectionPlan{}, nil
	}

	source, err := resolveProjectionSource(selectStatement, input.database)
	if err != nil {
		return ProjectionPlan{}, err
	}
	detail, err := inspector.GetObjectDetails(ctx, input.dsn, source.database, source.object, "table")
	if err != nil {
		return ProjectionPlan{}, fmt.Errorf("inspect projection source: %w", err)
	}
	if detail == nil {
		return ProjectionPlan{}, fmt.Errorf("%w: source metadata is missing", errProjectionUnsupported)
	}

	columns := projectionColumnNames(detail.Columns)
	plan := ProjectionPlan{Columns: make([]ColumnProvenance, 0, len(selectStatement.SelectExprs.Exprs))}
	for _, expression := range selectStatement.SelectExprs.Exprs {
		switch expr := expression.(type) {
		case *sqlparser.StarExpr:
			if !matchesProjectionSource(expr.TableName, source) {
				return ProjectionPlan{}, fmt.Errorf("%w: %s", errProjectionUnsupported, sqlparser.String(expr))
			}
			for _, column := range detail.Columns {
				plan.Columns = append(plan.Columns, source.provenance(column.Name, column.Name))
			}
		case *sqlparser.AliasedExpr:
			column, ok := expr.Expr.(*sqlparser.ColName)
			if !ok || !matchesProjectionSource(column.Qualifier, source) {
				return ProjectionPlan{}, fmt.Errorf("%w: %s", errProjectionUnsupported, sqlparser.String(expr))
			}
			sourceColumn, exists := columns[strings.ToLower(column.Name.String())]
			if !exists {
				return ProjectionPlan{}, fmt.Errorf("%w: unknown column %s", errProjectionUnsupported, sqlparser.String(column))
			}
			outputName := column.Name.String()
			if !expr.As.IsEmpty() {
				outputName = expr.As.String()
			}
			plan.Columns = append(plan.Columns, source.provenance(outputName, sourceColumn))
		default:
			return ProjectionPlan{}, fmt.Errorf("%w: %s", errProjectionUnsupported, sqlparser.String(expression))
		}
	}
	return plan, nil
}

// resolveRelatedRecordProjection resolves the server-generated SELECT * for a
// referenced table after verifying every referenced FK column exists.
func resolveRelatedRecordProjection(ctx context.Context, inspector QuerySchemaInspector, input relatedRecordProjectionInput) (ProjectionPlan, error) {
	detail, err := inspector.GetObjectDetails(ctx, input.dsn, input.database, input.object, "table")
	if err != nil {
		return ProjectionPlan{}, fmt.Errorf("inspect related-record source: %w", err)
	}
	if detail == nil {
		return ProjectionPlan{}, fmt.Errorf("%w: related-record metadata is missing", errProjectionUnsupported)
	}

	columns := projectionColumnNames(detail.Columns)
	plan := ProjectionPlan{Columns: make([]ColumnProvenance, 0, len(input.referencedColumns))}
	for _, requestedColumn := range input.referencedColumns {
		column, exists := columns[strings.ToLower(requestedColumn)]
		if !exists {
			return ProjectionPlan{}, fmt.Errorf("%w: unknown related-record column %s", errProjectionUnsupported, requestedColumn)
		}
		plan.Columns = append(plan.Columns, ColumnProvenance{
			OutputName:     column,
			SourceDatabase: input.database,
			SourceObject:   input.object,
			SourceColumn:   column,
		})
	}
	return plan, nil
}

// isNoTableProjection reports whether the SELECT projects no real table
// column values — either it has no FROM clause or its only source is the
// "dual" pseudo-table the parser synthesizes for FROM-less SELECTs (e.g.
// SELECT 1). Such statements carry no table data to govern for disclosure.
func isNoTableProjection(statement *sqlparser.Select) bool {
	if len(statement.From) == 0 {
		return true
	}
	if len(statement.From) != 1 {
		return false
	}
	tableExpression, ok := statement.From[0].(*sqlparser.AliasedTableExpr)
	if !ok {
		return false
	}
	table, ok := tableExpression.Expr.(sqlparser.TableName)
	if !ok {
		return false
	}
	return table.Qualifier.IsEmpty() && strings.EqualFold(table.Name.String(), "dual")
}

func resolveProjectionSource(statement *sqlparser.Select, defaultDatabase string) (projectionSource, error) {
	if statement.With != nil || len(statement.From) != 1 {
		return projectionSource{}, fmt.Errorf("%w: %s", errProjectionUnsupported, sqlparser.String(statement))
	}
	tableExpression, ok := statement.From[0].(*sqlparser.AliasedTableExpr)
	if !ok {
		return projectionSource{}, fmt.Errorf("%w: %s", errProjectionUnsupported, sqlparser.String(statement.From[0]))
	}
	table, ok := tableExpression.Expr.(sqlparser.TableName)
	if !ok {
		return projectionSource{}, fmt.Errorf("%w: %s", errProjectionUnsupported, sqlparser.String(tableExpression))
	}

	database := defaultDatabase
	if !table.Qualifier.IsEmpty() {
		database = table.Qualifier.String()
	}
	return projectionSource{
		database: database,
		object:   table.Name.String(),
		alias:    tableExpression.As.String(),
	}, nil
}

func (s projectionSource) provenance(outputName, sourceColumn string) ColumnProvenance {
	return ColumnProvenance{
		OutputName:     outputName,
		SourceDatabase: s.database,
		SourceObject:   s.object,
		SourceColumn:   sourceColumn,
	}
}

func matchesProjectionSource(qualifier sqlparser.TableName, source projectionSource) bool {
	if qualifier.Name.IsEmpty() && qualifier.Qualifier.IsEmpty() {
		return true
	}
	if qualifier.Qualifier.IsEmpty() && source.alias != "" && strings.EqualFold(qualifier.Name.String(), source.alias) {
		return true
	}
	if !strings.EqualFold(qualifier.Name.String(), source.object) {
		return false
	}
	return qualifier.Qualifier.IsEmpty() || strings.EqualFold(qualifier.Qualifier.String(), source.database)
}

func projectionColumnNames(columns []ColumnSummary) map[string]string {
	byName := make(map[string]string, len(columns))
	for _, column := range columns {
		byName[strings.ToLower(column.Name)] = column.Name
	}
	return byName
}
