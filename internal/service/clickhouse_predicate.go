package service

import (
	"strconv"
	"strings"

	ast "github.com/AfterShip/clickhouse-sql-parser/parser"
	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/fan/controlhub/internal/model"
)

type chPredicate struct {
	types      map[string]string
	modes      map[string]model.ResultDisclosureMode
	params     ch.Parameters
	countAlias string
}

func (p chPredicate) compile(expr ast.Expr, depth int) (string, error) {
	if depth > 32 {
		return "", ErrQueryValidationFailed
	}
	switch n := expr.(type) {
	case *ast.ParamExprList: // One parenthesized predicate.
		if n.ColumnArgList == nil {
			return p.compile(chSingleItem(n.Items), depth+1)
		}
	case *ast.BinaryOperation:
		if n.HasGlobal || n.HasNot {
			return "", ErrQueryValidationFailed
		}
		switch op := strings.ToUpper(string(n.Operation)); op {
		case "AND", "OR":
			left, err := p.compile(n.LeftExpr, depth+1)
			if err != nil {
				return "", err
			}
			right, err := p.compile(n.RightExpr, depth+1)
			if err != nil {
				return "", err
			}
			return "(" + left + " " + op + " " + right + ")", nil
		case "=", "==", "!=", "<>", "<", "<=", ">", ">=":
			col := chColumnName(n.LeftExpr)
			value, err := p.bind(col, n.RightExpr)
			if err != nil {
				return "", err
			}
			return "src.`" + col + "` " + op + " " + value, nil
		}
	case *ast.BetweenClause:
		col := chColumnName(n.Expr)
		low, err := p.bind(col, n.Between)
		if err != nil {
			return "", err
		}
		high, err := p.bind(col, n.And)
		if err != nil {
			return "", err
		}
		op := " BETWEEN "
		if n.Not {
			op = " NOT BETWEEN "
		}
		return "(src.`" + col + "`" + op + low + " AND " + high + ")", nil
	}
	return "", ErrQueryValidationFailed
}

func (p chPredicate) bind(col string, literal ast.Expr) (string, error) {
	if col == "" || col == p.countAlias || len(p.params) >= 32 {
		return "", ErrQueryValidationFailed
	}
	typ := p.types[col]
	if typ == "" || p.modes[col] != model.ResultDisclosureRawCopyAllowed {
		return "", ErrQueryDisclosureBlocked
	}
	name := "worksheet_value"
	if len(p.params) > 0 {
		name += strconv.Itoa(len(p.params) + 1)
	}
	var text string
	var quoted bool
	switch v := literal.(type) {
	case *ast.NumberLiteral:
		if v.Base != 10 {
			return "", ErrQueryValidationFailed
		}
		text = v.Literal
	case *ast.StringLiteral:
		if strings.ContainsAny(v.Literal, "\\\r\n\x00") {
			return "", ErrQueryValidationFailed
		}
		text = strings.ReplaceAll(v.Literal, "''", "'")
		quoted = true
	default:
		return "", ErrQueryValidationFailed
	}
	expr, value, err := chFilterValue(name, typ, text, quoted)
	if err != nil {
		return "", err
	}
	p.params[name] = value
	return expr, nil
}
