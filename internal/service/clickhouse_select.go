package service

import (
	"slices"
	"strconv"
	"strings"

	ast "github.com/AfterShip/clickhouse-sql-parser/parser"
)

// The dialect parser owns syntax. This plan retains only the worksheet operations
// for which we can bind every source column. We never execute the input or serialize
// an arbitrary AST subtree back to SQL.
type chSelect struct {
	table                    string
	columns                  []string
	countAlias, group, order string
	where                    ast.Expr
	descending               bool
	limit                    uint64
	hasLimit                 bool
}

func parseCHSelect(statement string) (chSelect, error) {
	var q chSelect
	if len(statement) > 8192 {
		return q, ErrQueryValidationFailed
	}
	statements, err := ast.NewParser(statement).ParseStmts()
	if err != nil || len(statements) != 1 {
		return q, ErrQueryValidationFailed
	}
	s, ok := statements[0].(*ast.SelectQuery)
	if !ok || s.From == nil || len(s.SelectItems) == 0 || len(s.SelectItems) > 8 {
		return q, ErrQueryValidationFailed
	}
	// Fail closed on clauses that add sources, change cardinality, or change target settings.
	if s.InnerQuery != nil || s.With != nil || s.Top != nil || s.HasDistinct || s.DistinctOn != nil ||
		s.Window != nil || s.Prewhere != nil || s.WithTotal || s.Having != nil || s.LimitBy != nil ||
		s.Settings != nil || s.Format != nil || s.UnionAll != nil || s.UnionDistinct != nil || s.Except != nil || s.Intersect != nil {
		return q, ErrQueryValidationFailed
	}
	source, ok := s.From.Expr.(*ast.JoinTableExpr)
	if !ok || source.Table == nil || source.HasFinal || source.SampleRatio != nil || source.Table.HasFinal || source.Table.Alias != nil {
		return q, ErrQueryValidationFailed
	}
	table, ok := source.Table.Expr.(*ast.TableIdentifier)
	if !ok || table.Database != nil || table.Table == nil || !chIdentifier.MatchString(table.Table.Name) {
		return q, ErrQueryValidationFailed
	}
	q.table = table.Table.Name
	for _, item := range s.SelectItems {
		if len(item.Modifiers) != 0 {
			return q, ErrQueryValidationFailed
		}
		name := chColumnName(item.Expr)
		if chCountFunction(item.Expr) {
			if q.countAlias != "" {
				return q, ErrQueryValidationFailed
			}
			name = "count"
			if item.Alias != nil {
				name = chColumnName(item.Alias)
			}
			q.countAlias = name
		} else if item.Alias != nil {
			return q, ErrQueryValidationFailed
		}
		if name == "" || slices.Contains(q.columns, name) {
			return q, ErrQueryValidationFailed
		}
		q.columns = append(q.columns, name)
	}
	if s.Where != nil {
		q.where = s.Where.Expr
	}
	if g := s.GroupBy; g != nil {
		if g.AggregateType != "" || g.WithCube || g.WithRollup || g.WithTotals {
			return q, ErrQueryValidationFailed
		}
		list, ok := g.Expr.(*ast.ColumnExprList)
		if !ok {
			return q, ErrQueryValidationFailed
		}
		q.group = chColumnName(chSingleItem(list))
		if q.group == "" || q.countAlias == "" || q.group == q.countAlias || !slices.Contains(q.columns, q.group) {
			return q, ErrQueryValidationFailed
		}
	}
	if q.countAlias != "" {
		want := 1
		if q.group != "" {
			want++
		}
		if len(q.columns) != want {
			return q, ErrQueryValidationFailed
		}
	}
	if o := s.OrderBy; o != nil {
		if len(o.Items) != 1 || o.Interpolate != nil {
			return q, ErrQueryValidationFailed
		}
		item, ok := o.Items[0].(*ast.OrderExpr)
		if !ok || item.Fill != nil || item.Alias != nil {
			return q, ErrQueryValidationFailed
		}
		q.order = chColumnName(item.Expr)
		if q.order == "" || !slices.Contains(q.columns, q.order) {
			return q, ErrQueryValidationFailed
		}
		switch item.Direction {
		case ast.OrderDirectionAsc, ast.OrderDirectionNone:
		case ast.OrderDirectionDesc:
			q.descending = true
		default:
			return q, ErrQueryValidationFailed
		}
	}
	if l := s.Limit; l != nil {
		n, ok := l.Limit.(*ast.NumberLiteral)
		if !ok || l.Offset != nil || n.Base != 10 {
			return q, ErrQueryValidationFailed
		}
		q.limit, err = strconv.ParseUint(n.Literal, 10, 64)
		if err != nil {
			return q, ErrQueryValidationFailed
		}
		q.hasLimit = true
	}
	return q, nil
}

func chColumnName(expr ast.Expr) string {
	id, ok := expr.(*ast.Ident)
	if !ok || id == nil || !chIdentifier.MatchString(id.Name) {
		return ""
	}
	return id.Name
}

func chSingleItem(list *ast.ColumnExprList) ast.Expr {
	if list == nil || list.HasDistinct || len(list.Items) != 1 {
		return nil
	}
	col, ok := list.Items[0].(*ast.ColumnExpr)
	if !ok || col.Alias != nil {
		return nil
	}
	return col.Expr
}

func chCountFunction(expr ast.Expr) bool {
	f, ok := expr.(*ast.FunctionExpr)
	if !ok || f.Name == nil || !strings.EqualFold(f.Name.Name, "count") || f.Params == nil || f.Params.ColumnArgList != nil {
		return false
	}
	args := f.Params.Items
	if args == nil || args.HasDistinct {
		return false
	}
	if len(args.Items) == 0 {
		return true
	}
	star, ok := chSingleItem(args).(*ast.Ident)
	return ok && star.Name == "*" && star.QuoteType == 0
}
