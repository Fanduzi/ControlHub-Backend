// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: guarded+qualified *pg.ParseResult plus caller-resolved PageOptions
// output: PageOptions, PageWindow, PageResult, PaginatePG, RewritePaginated — G6 execution-window rewrite of root LIMIT/OFFSET
// pos: G6 execution-window stage (T5) — turns the user's root LIMIT/OFFSET window plus the platform row budget into an execution window (pageSize+1 sentinel fetch at O+(page-1)*pageSize) rewritten once into the root SelectStmt before witness injection; accepted root literals are nonnegative int64 integers incl. PG radix prefixes (0x/0o/0b), legal digit separators and decimal leading zeros; window arithmetic validates fully before the tree is touched; Result reports the delivered page/sentinel/budget boundary without implying additional DB rows exist
// note: if this file changes, update header and README.md
package pgsql

import (
	"fmt"
	"math"
	"strconv"

	pg "github.com/pganalyze/pg_query_go/v6"

	"github.com/fan/controlhub/internal/model"
)

type PageOptions struct {
	Page, PageSize, MaxRows int
}

type PageWindow struct {
	Page, PageSize, RowLimit                      int
	Browsable, PageStart, FetchCount, FetchOffset int64
	BudgetLimited                                 bool
}

type PageResult struct {
	RowCount      int
	HasMore       bool
	BudgetReached bool
}

func PaginatePG(tree *pg.ParseResult, opts PageOptions) (PageWindow, error) {
	w := PageWindow{Page: opts.Page, PageSize: opts.PageSize}
	if opts.Page < 1 {
		return w, &RejectError{Code: "page_out_of_range", Message: "pgsql: page out of range"}
	}
	if !allowedPageSize(opts.PageSize) || opts.MaxRows <= 0 {
		return w, &RejectError{Code: "validation_failed", Message: "pgsql: invalid pagination options"}
	}
	if tree == nil || len(tree.GetStmts()) != 1 {
		return w, &RejectError{Code: "query_not_allowed", Message: "pgsql: expected a single SELECT statement"}
	}
	sel := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	if sel == nil {
		return w, &RejectError{Code: "query_not_allowed", Message: "pgsql: expected a single SELECT statement"}
	}
	switch sel.GetLimitOption() {
	case pg.LimitOption_LIMIT_OPTION_UNDEFINED, pg.LimitOption_LIMIT_OPTION_DEFAULT, pg.LimitOption_LIMIT_OPTION_COUNT:
	default:
		return w, &RejectError{Code: "unsupported_limit_offset_form", Message: "pgsql: unsupported LIMIT/OFFSET form"}
	}
	limit, limNull, err := limitConstInt(sel.GetLimitCount(), true)
	if err != nil {
		return w, err
	}
	unbounded := sel.GetLimitCount() == nil || limNull
	offset, _, err := limitConstInt(sel.GetLimitOffset(), false)
	if err != nil {
		return w, err
	}
	browsable := int64(opts.MaxRows)
	if !unbounded && limit < browsable {
		browsable = limit
	}
	pageStart, ok := checkedMul(int64(opts.Page)-1, int64(opts.PageSize))
	if !ok {
		return w, pageOutOfRange()
	}
	w.Browsable = browsable
	w.BudgetLimited = unbounded || limit > int64(opts.MaxRows)
	if browsable == 0 {
		if opts.Page != 1 {
			return w, pageOutOfRange()
		}
		w.FetchOffset = offset
	} else {
		if pageStart >= browsable {
			return w, pageOutOfRange()
		}
		fetchOffset, ok := checkedAdd(offset, pageStart)
		if !ok {
			return w, pageOutOfRange()
		}
		remaining := browsable - pageStart
		w.PageStart = pageStart
		w.RowLimit = int(min(int64(opts.PageSize), remaining))
		w.FetchCount = min(int64(opts.PageSize)+1, remaining)
		w.FetchOffset = fetchOffset
	}
	sel.LimitOption = pg.LimitOption_LIMIT_OPTION_COUNT
	sel.LimitCount = pageConstInt64(w.FetchCount)
	if w.FetchOffset == 0 {
		sel.LimitOffset = nil
	} else {
		sel.LimitOffset = pageConstInt64(w.FetchOffset)
	}
	return w, nil
}

func (w PageWindow) Result(fetchedRows int) (PageResult, error) {
	if fetchedRows < 0 || int64(fetchedRows) > w.FetchCount {
		return PageResult{}, fmt.Errorf("pgsql: fetched row count %d outside execution window", fetchedRows)
	}
	r := PageResult{
		RowCount: min(fetchedRows, w.RowLimit),
		HasMore:  int64(fetchedRows) > int64(w.RowLimit),
	}
	r.BudgetReached = w.BudgetLimited && w.RowLimit > 0 &&
		w.PageStart+int64(w.RowLimit) == w.Browsable && int64(fetchedRows) >= int64(w.RowLimit)
	return r, nil
}

func pageOutOfRange() error {
	return &RejectError{Code: "page_out_of_range", Message: "pgsql: page out of range"}
}

func allowedPageSize(n int) bool {
	for _, s := range model.AllowedPageSizes {
		if n == s {
			return true
		}
	}
	return false
}

func limitUnsupported() error {
	return &RejectError{Code: "unsupported_limit_offset_form", Message: "pgsql: unsupported LIMIT/OFFSET form"}
}

func limitConstInt(n *pg.Node, allowNull bool) (v int64, null bool, err error) {
	if n == nil {
		return 0, false, nil
	}
	c := n.GetAConst()
	if c == nil {
		return 0, false, limitUnsupported()
	}
	if c.GetIsnull() {
		if allowNull {
			return 0, true, nil
		}
		return 0, false, limitUnsupported()
	}
	switch val := c.GetVal().(type) {
	case *pg.A_Const_Ival:
		v = int64(val.Ival.GetIval())
	case *pg.A_Const_Fval:
		var ok bool
		v, ok = pageLiteralInt64(val.Fval.GetFval())
		if !ok {
			return 0, false, limitUnsupported()
		}
	default:
		return 0, false, limitUnsupported()
	}
	if v < 0 {
		return 0, false, limitUnsupported()
	}
	return v, false, nil
}

func pageLiteralInt64(s string) (int64, bool) {
	base, start := int64(10), 0
	if len(s) >= 2 && s[0] == '0' {
		switch s[1] {
		case 'x', 'X':
			base, start = 16, 2
		case 'o', 'O':
			base, start = 8, 2
		case 'b', 'B':
			base, start = 2, 2
		}
	}
	if start == 2 && start < len(s) && s[start] == '_' {
		start++
	}
	var value int64
	digit := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if c == '_' {
			if !digit {
				return 0, false
			}
			digit = false
			continue
		}
		var d int64
		switch {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case c >= 'a' && c <= 'f':
			d = int64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int64(c-'A') + 10
		default:
			return 0, false
		}
		if d >= base || value > (math.MaxInt64-d)/base {
			return 0, false
		}
		value = value*base + d
		digit = true
	}
	return value, digit
}

func pageConstInt64(n int64) *pg.Node {
	c := &pg.A_Const{Location: -1}
	if n >= math.MinInt32 && n <= math.MaxInt32 {
		c.Val = &pg.A_Const_Ival{Ival: &pg.Integer{Ival: int32(n)}}
	} else {
		c.Val = &pg.A_Const_Fval{Fval: &pg.Float{Fval: strconv.FormatInt(n, 10)}}
	}
	return &pg.Node{Node: &pg.Node_AConst{AConst: c}}
}

func checkedMul(a, b int64) (int64, bool) {
	if a != 0 && b != 0 && (a > math.MaxInt64/b || a < math.MinInt64/b) {
		return 0, false
	}
	return a * b, true
}

func checkedAdd(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
}
