// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic SQL strings / parse trees plus stub resolvers
// output: intent tests for PaginatePG window arithmetic, limit-form acceptance incl. PG radix/separator integer spellings and int64 boundary, option/tree validation, Result bookkeeping, and RewritePaginated pipeline shape
// pos: G6 window contract — root LIMIT/OFFSET rewritten once into an execution window; tests assert node forms and deparsed transport, never evaluated rows
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"google.golang.org/protobuf/proto"
)

func parseTree(t *testing.T, sql string) *pg.ParseResult {
	t.Helper()
	tree, err := pgquery.Parse(sql)
	if err != nil {
		t.Fatalf("parse %q: %v", sql, err)
	}
	return tree
}

func rootSelect(t *testing.T, tree *pg.ParseResult) *pg.SelectStmt {
	t.Helper()
	return tree.GetStmts()[0].GetStmt().GetSelectStmt()
}

func rejectCode(err error) string {
	if r, ok := err.(*RejectError); ok {
		return r.Code
	}
	return ""
}

type windowWant struct {
	browsable, pageStart, fetchCount, fetchOffset, rowLimit int64
	budgetLimited                                           bool
}

func assertWindow(t *testing.T, w PageWindow, want windowWant) {
	t.Helper()
	got := windowWant{w.Browsable, w.PageStart, w.FetchCount, w.FetchOffset, int64(w.RowLimit), w.BudgetLimited}
	if got != want {
		t.Fatalf("window = %+v, want %+v", got, want)
	}
}

func paginate(t *testing.T, sql string, page, pageSize, maxRows int) (PageWindow, *pg.ParseResult) {
	t.Helper()
	tree := parseTree(t, sql)
	w, err := PaginatePG(tree, PageOptions{Page: page, PageSize: pageSize, MaxRows: maxRows})
	if err != nil {
		t.Fatalf("PaginatePG(%q p%d s%d B%d) = %v", sql, page, pageSize, maxRows, err)
	}
	return w, tree
}

func paginateErr(t *testing.T, sql string, page, pageSize, maxRows int, code string) {
	t.Helper()
	tree := parseTree(t, sql)
	_, err := PaginatePG(tree, PageOptions{Page: page, PageSize: pageSize, MaxRows: maxRows})
	if err == nil {
		t.Fatalf("PaginatePG(%q p%d s%d B%d) succeeded, want %s", sql, page, pageSize, maxRows, code)
	}
	if rejectCode(err) != code {
		t.Fatalf("PaginatePG(%q) error = %v, want code %s", sql, err, code)
	}
}

func rootLimitText(t *testing.T, tree *pg.ParseResult) string {
	t.Helper()
	out, err := pgquery.Deparse(tree)
	if err != nil {
		t.Fatalf("deparse: %v", err)
	}
	return out
}

func litInt(n *pg.Node) (int64, bool) {
	c := n.GetAConst()
	if c == nil || c.GetIsnull() {
		return 0, false
	}
	if iv := c.GetIval(); iv != nil {
		return int64(iv.GetIval()), true
	}
	if fv := c.GetFval(); fv != nil {
		v, err := strconv.ParseInt(fv.GetFval(), 10, 64)
		if err == nil {
			return v, true
		}
	}
	return 0, false
}

func assertLiteralNode(t *testing.T, n *pg.Node, want int64, what string) {
	t.Helper()
	got, ok := litInt(n)
	if !ok || got != want {
		t.Fatalf("%s = %v, want literal %d", what, n, want)
	}
	c := n.GetAConst()
	if want > math.MaxInt32 && c.GetFval() == nil {
		t.Fatalf("%s wide value not fval: %v", what, n)
	}
	if want <= math.MaxInt32 && c.GetIval() == nil {
		t.Fatalf("%s int32-range value not ival: %v", what, n)
	}
}

func assertRootLimit(t *testing.T, tree *pg.ParseResult, wantLimit, wantOffset int64) {
	t.Helper()
	sel := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	if sel.GetLimitOption() != pg.LimitOption_LIMIT_OPTION_COUNT {
		t.Fatalf("LimitOption = %v, want COUNT", sel.GetLimitOption())
	}
	assertLiteralNode(t, sel.GetLimitCount(), wantLimit, "LimitCount")
	if wantOffset == 0 {
		if sel.GetLimitOffset() != nil {
			t.Fatalf("LimitOffset = %v, want nil", sel.GetLimitOffset())
		}
		return
	}
	assertLiteralNode(t, sel.GetLimitOffset(), wantOffset, "LimitOffset")
}

func TestPaginatePG_WindowMatrix(t *testing.T) {
	type kase struct {
		sql                   string
		budget                int
		page                  int
		want                  windowWant
		wantLimit, wantOffset int64
		wantErr               string
		wantTail              string
	}
	cases := []kase{
		{sql: "SELECT 1", budget: 100, page: 1, want: windowWant{100, 0, 26, 0, 25, true},
			wantLimit: 26, wantTail: "LIMIT 26"},
		{sql: "SELECT 1", budget: 100, page: 4, want: windowWant{100, 75, 25, 75, 25, true},
			wantLimit: 25, wantOffset: 75, wantTail: "LIMIT 25 OFFSET 75"},
		{sql: "SELECT 1", budget: 100, page: 5, wantErr: "page_out_of_range"},
		{sql: "SELECT 1 LIMIT 3", budget: 100, page: 1, want: windowWant{3, 0, 3, 0, 3, false},
			wantLimit: 3},
		{sql: "SELECT 1 LIMIT 3", budget: 100, page: 2, wantErr: "page_out_of_range"},
		{sql: "SELECT 1 LIMIT 30 OFFSET 10", budget: 100, page: 1, want: windowWant{30, 0, 26, 10, 25, false},
			wantLimit: 26, wantOffset: 10},
		{sql: "SELECT 1 LIMIT 30 OFFSET 10", budget: 100, page: 2, want: windowWant{30, 25, 5, 35, 5, false},
			wantLimit: 5, wantOffset: 35},
		{sql: "SELECT 1 LIMIT 30 OFFSET 10", budget: 100, page: 3, wantErr: "page_out_of_range"},
		{sql: "SELECT 1 LIMIT 200", budget: 100, page: 4, want: windowWant{100, 75, 25, 75, 25, true},
			wantLimit: 25, wantOffset: 75},
		{sql: "SELECT 1 LIMIT 200", budget: 100, page: 5, wantErr: "page_out_of_range"},
		{sql: "SELECT 1 OFFSET 10", budget: 100, page: 4, want: windowWant{100, 75, 25, 85, 25, true},
			wantLimit: 25, wantOffset: 85},
		{sql: "SELECT 1 LIMIT 0 OFFSET 10", budget: 100, page: 1, want: windowWant{0, 0, 0, 10, 0, false},
			wantLimit: 0, wantOffset: 10},
		{sql: "SELECT 1 LIMIT 0 OFFSET 10", budget: 100, page: 2, wantErr: "page_out_of_range"},
		{sql: "SELECT 1", budget: 67, page: 3, want: windowWant{67, 50, 17, 50, 17, true},
			wantLimit: 17, wantOffset: 50},
		{sql: "SELECT 1", budget: 67, page: 4, wantErr: "page_out_of_range"},
		{sql: "SELECT 1 LIMIT ALL", budget: 100, page: 1, want: windowWant{100, 0, 26, 0, 25, true},
			wantLimit: 26},
		{sql: "SELECT 1 LIMIT NULL", budget: 100, page: 1, want: windowWant{100, 0, 26, 0, 25, true},
			wantLimit: 26},
		{sql: "SELECT 1 FETCH FIRST 3 ROWS ONLY", budget: 100, page: 1, want: windowWant{3, 0, 3, 0, 3, false},
			wantLimit: 3},
		{sql: "SELECT 1 FETCH FIRST ROW ONLY", budget: 100, page: 1, want: windowWant{1, 0, 1, 0, 1, false},
			wantLimit: 1},
		{sql: "SELECT 1 LIMIT (3)", budget: 100, page: 1, want: windowWant{3, 0, 3, 0, 3, false},
			wantLimit: 3},
		{sql: "SELECT 1 LIMIT 2147483648 OFFSET 2147483648", budget: 100, page: 2,
			want:       windowWant{100, 25, 26, 2147483673, 25, true},
			wantLimit:  26,
			wantOffset: 2147483673,
			wantTail:   "LIMIT 26 OFFSET 2147483673"},
		{sql: "SELECT 1 LIMIT 9223372036854775807 OFFSET 9223372036854775807", budget: 100, page: 1,
			want:       windowWant{100, 0, 26, math.MaxInt64, 25, true},
			wantLimit:  26,
			wantOffset: math.MaxInt64,
			wantTail:   "LIMIT 26 OFFSET 9223372036854775807"},
		{sql: "SELECT 1 LIMIT 9223372036854775807 OFFSET 9223372036854775807", budget: 100, page: 2,
			wantErr: "page_out_of_range"},
		{sql: "SELECT 1 LIMIT 9223372036854775808", budget: 100, page: 1, wantErr: "unsupported_limit_offset_form"},
		{sql: "SELECT 1", budget: 100, page: math.MaxInt, wantErr: "page_out_of_range"},
	}
	for _, c := range cases {
		t.Run(c.sql+"@p"+strconv.Itoa(c.page)+"B"+strconv.Itoa(c.budget), func(t *testing.T) {
			if c.wantErr != "" {
				paginateErr(t, c.sql, c.page, 25, c.budget, c.wantErr)
				return
			}
			w, tree := paginate(t, c.sql, c.page, 25, c.budget)
			assertWindow(t, w, c.want)
			assertRootLimit(t, tree, c.wantLimit, c.wantOffset)
			out := rootLimitText(t, tree)
			if c.wantTail != "" && !strings.HasSuffix(out, c.wantTail) {
				t.Fatalf("deparsed %q lacks tail %q", out, c.wantTail)
			}
			assertRootLimit(t, parseTree(t, out), c.wantLimit, c.wantOffset)
		})
	}
}

func TestPaginatePG_PageSizeMatrix(t *testing.T) {
	w, tree := paginate(t, "SELECT 1", 1, 100, 100)
	assertWindow(t, w, windowWant{100, 0, 100, 0, 100, true})
	assertRootLimit(t, tree, 100, 0)
	w, tree = paginate(t, "SELECT 1", 2, 10, 55)
	assertWindow(t, w, windowWant{55, 10, 11, 10, 10, true})
	assertRootLimit(t, tree, 11, 10)
}

func TestPaginatePG_UnsupportedForms(t *testing.T) {
	cases := []string{
		"SELECT 1 LIMIT -1",
		"SELECT 1 OFFSET -1",
		"SELECT 1 LIMIT $1",
		"SELECT 1 OFFSET $1",
		"SELECT 1 LIMIT '3'",
		"SELECT 1 OFFSET '3'",
		"SELECT 1 LIMIT 3.0",
		"SELECT 1 OFFSET 3.0",
		"SELECT 1 LIMIT 5.5",
		"SELECT 1 LIMIT 3e0",
		"SELECT 1 OFFSET 3e0",
		"SELECT 1 LIMIT (2+3)",
		"SELECT 1 OFFSET (2+3)",
		"SELECT 1 LIMIT 3::bigint",
		"SELECT 1 OFFSET 3::bigint",
		"SELECT 1 LIMIT +3",
		"SELECT 1 OFFSET +3",
		"SELECT 1 OFFSET NULL",
		"SELECT 1 LIMIT 9223372036854775808",
		"SELECT 1 OFFSET 9223372036854775808",
		"SELECT 1 ORDER BY 1 FETCH FIRST 3 ROWS WITH TIES",
	}
	for _, sql := range cases {
		t.Run(sql, func(t *testing.T) {
			paginateErr(t, sql, 1, 25, 100, "unsupported_limit_offset_form")
			_, _, err := RewritePaginated(sql, "app", testCols,
				PageOptions{Page: 1, PageSize: 25, MaxRows: 100})
			if rejectCode(err) != "unsupported_limit_offset_form" {
				t.Fatalf("RewritePaginated(%q) = %v, want unsupported_limit_offset_form", sql, err)
			}
		})
	}
}

func TestPaginatePG_OptionValidation(t *testing.T) {
	tree := parseTree(t, "SELECT 1")
	for _, page := range []int{0, -1} {
		if _, err := PaginatePG(tree, PageOptions{Page: page, PageSize: 25, MaxRows: 100}); rejectCode(err) != "page_out_of_range" {
			t.Fatalf("page %d: err = %v", page, err)
		}
	}
	for _, o := range []PageOptions{
		{Page: 1, PageSize: 0, MaxRows: 100},
		{Page: 1, PageSize: 7, MaxRows: 100},
		{Page: 1, PageSize: 25, MaxRows: 0},
		{Page: 1, PageSize: 25, MaxRows: -1},
	} {
		if _, err := PaginatePG(tree, o); rejectCode(err) != "validation_failed" {
			t.Fatalf("opts %+v: err = %v", o, err)
		}
	}
	if _, err := PaginatePG(tree, PageOptions{Page: math.MaxInt, PageSize: 100, MaxRows: 100}); rejectCode(err) != "page_out_of_range" {
		t.Fatalf("max page: err = %v", err)
	}
}

func TestPaginatePG_MalformedTrees(t *testing.T) {
	if _, err := PaginatePG(nil, PageOptions{Page: 1, PageSize: 25, MaxRows: 100}); rejectCode(err) != "query_not_allowed" {
		t.Fatalf("nil tree: err = %v", err)
	}
	multi, err := pgquery.Parse("SELECT 1; SELECT 2")
	if err != nil {
		t.Fatalf("parse multi: %v", err)
	}
	if _, err := PaginatePG(multi, PageOptions{Page: 1, PageSize: 25, MaxRows: 100}); rejectCode(err) != "query_not_allowed" {
		t.Fatalf("multi tree: err = %v", err)
	}
	nonSel, err := pgquery.Parse("CREATE TABLE z (a int)")
	if err != nil {
		t.Fatalf("parse nonselect: %v", err)
	}
	if _, err := PaginatePG(nonSel, PageOptions{Page: 1, PageSize: 25, MaxRows: 100}); rejectCode(err) != "query_not_allowed" {
		t.Fatalf("nonselect tree: err = %v", err)
	}
}

func TestPaginatePG_ErrorLeavesTreeUnchanged(t *testing.T) {
	cases := []struct {
		sql  string
		opts PageOptions
	}{
		{"SELECT 1 LIMIT 30 OFFSET 10", PageOptions{Page: 3, PageSize: 25, MaxRows: 100}},
		{"SELECT 1 LIMIT -1", PageOptions{Page: 1, PageSize: 25, MaxRows: 100}},
		{"SELECT 1 OFFSET 9223372036854775808", PageOptions{Page: 1, PageSize: 25, MaxRows: 100}},
		{"SELECT 1 OFFSET 9223372036854775807", PageOptions{Page: 2, PageSize: 25, MaxRows: 100}},
	}
	for _, c := range cases {
		tree := parseTree(t, c.sql)
		before := proto.Clone(tree).(*pg.ParseResult)
		if _, err := PaginatePG(tree, c.opts); err == nil {
			t.Fatalf("%q: expected error", c.sql)
		}
		if !proto.Equal(before, tree) {
			t.Fatalf("%q: tree mutated on error", c.sql)
		}
	}
}

func TestPaginatePG_NonWindowFieldsPreserved(t *testing.T) {
	for _, sql := range []string{
		"SELECT a.x, count(*) OVER (ORDER BY a.x DESC) FROM (SELECT 1 AS x LIMIT 5 OFFSET 2) a ORDER BY a.x LIMIT 30 OFFSET 10",
		"WITH c AS (SELECT 1 AS x LIMIT 7 OFFSET 3) SELECT x FROM c ORDER BY x LIMIT 30 OFFSET 2",
		"SELECT 1 UNION ALL SELECT 2 ORDER BY 1 LIMIT 5",
		"(SELECT 1 AS x LIMIT 4 OFFSET 1) UNION ALL (SELECT 2 AS x LIMIT 4 OFFSET 2) ORDER BY x LIMIT 5 OFFSET 1",
	} {
		tree := parseTree(t, sql)
		clone := proto.Clone(tree).(*pg.ParseResult)
		sel := rootSelect(t, tree)
		sel2 := rootSelect(t, clone)
		if _, err := PaginatePG(tree, PageOptions{Page: 1, PageSize: 25, MaxRows: 100}); err != nil {
			t.Fatalf("%q: %v", sql, err)
		}
		sel.LimitOption, sel.LimitCount, sel.LimitOffset = sel2.GetLimitOption(), sel2.GetLimitCount(), sel2.GetLimitOffset()
		if !proto.Equal(clone, tree) {
			t.Fatalf("%q: non-window field changed", sql)
		}
	}
}

func TestPageWindow_Result(t *testing.T) {
	w := PageWindow{Browsable: 30, PageStart: 0, FetchCount: 26, FetchOffset: 10, RowLimit: 25}
	r, err := w.Result(26)
	if err != nil || r.RowCount != 25 || !r.HasMore || r.BudgetReached {
		t.Fatalf("Result(26) = %+v, %v", r, err)
	}
	w2 := PageWindow{Browsable: 30, PageStart: 25, FetchCount: 5, FetchOffset: 35, RowLimit: 5}
	r, err = w2.Result(5)
	if err != nil || r.RowCount != 5 || r.HasMore || r.BudgetReached {
		t.Fatalf("Result(5) = %+v, %v", r, err)
	}
	wb := PageWindow{Browsable: 100, PageStart: 75, FetchCount: 25, FetchOffset: 75, RowLimit: 25, BudgetLimited: true}
	r, err = wb.Result(25)
	if err != nil || r.RowCount != 25 || r.HasMore || !r.BudgetReached {
		t.Fatalf("budget Result(25) = %+v, %v", r, err)
	}
	r, err = wb.Result(24)
	if err != nil || r.RowCount != 24 || r.HasMore || r.BudgetReached {
		t.Fatalf("budget Result(24) = %+v, %v", r, err)
	}
	we := PageWindow{Browsable: 0, FetchOffset: 10}
	r, err = we.Result(0)
	if err != nil || r.RowCount != 0 || r.HasMore || r.BudgetReached {
		t.Fatalf("empty Result(0) = %+v, %v", r, err)
	}
	for _, n := range []int{-1, int(w.FetchCount) + 1} {
		_, err := w.Result(n)
		if err == nil {
			t.Fatalf("Result(%d) accepted", n)
		}
		var rej *RejectError
		if errors.As(err, &rej) {
			t.Fatalf("Result(%d) surfaced RejectError %v", n, rej)
		}
	}
}

func TestRewritePaginated_Pipeline(t *testing.T) {
	res, w, err := RewritePaginated("SELECT id FROM orders ORDER BY id LIMIT 30 OFFSET 10",
		"app", testCols, PageOptions{Page: 1, PageSize: 25, MaxRows: 100})
	if err != nil {
		t.Fatalf("RewritePaginated: %v", err)
	}
	assertWindow(t, w, windowWant{30, 0, 26, 10, 25, false})
	reparseable(t, res)
	if !strings.Contains(res.SQL, "LIMIT 26") {
		t.Fatalf("page-1 window missing: %s", res.SQL)
	}
	res2, w2, err := RewritePaginated("SELECT id FROM orders ORDER BY id LIMIT 30 OFFSET 10",
		"app", testCols, PageOptions{Page: 2, PageSize: 25, MaxRows: 100})
	if err != nil {
		t.Fatalf("RewritePaginated p2: %v", err)
	}
	assertWindow(t, w2, windowWant{30, 25, 5, 35, 5, false})
	if !strings.Contains(res2.SQL, "LIMIT 5") {
		t.Fatalf("page-2 window missing: %s", res2.SQL)
	}
	if _, _, err := RewritePaginated("SELEC 1", "app", testCols,
		PageOptions{Page: 1, PageSize: 25, MaxRows: 100}); err == nil {
		t.Fatal("syntax-invalid query accepted")
	}
}

func TestRewrite_UnpaginatedPreservesLimit(t *testing.T) {
	r := rewriteOK(t, "SELECT id FROM orders ORDER BY id LIMIT 7 OFFSET 2")
	if !strings.Contains(r.SQL, "LIMIT 7") || !strings.Contains(r.SQL, "OFFSET 2") {
		t.Fatalf("unpaginated rewrite altered limit: %s", r.SQL)
	}
}

func TestPaginatePG_IntegerLiteralForms(t *testing.T) {
	cases := []struct {
		literal string
		value   int64
	}{
		{"2_147_483_648", 2147483648},
		{"2_147_483_647", 2147483647},
		{"0x7fff_ffff", 2147483647},
		{"0X_8000_0000", 2147483648},
		{"0o17_777_777_777", 2147483647},
		{"0O_20_000_000_000", 2147483648},
		{"0b1111111111111111111111111111111", 2147483647},
		{"0B_1000_0000_0000_0000_0000_0000_0000_0000", 2147483648},
		{"000000002147483648", 2147483648},
		{"0_000_002_147_483_648", 2147483648},
		{"010000000000", 10000000000},
		{"9_223_372_036_854_775_807", math.MaxInt64},
		{"0x_7fff_ffff_ffff_ffff", math.MaxInt64},
		{"0o_777_777_777_777_777_777_777", math.MaxInt64},
		{"0b" + strings.Repeat("1", 63), math.MaxInt64},
	}
	for _, tc := range cases {
		for _, clause := range []string{"LIMIT", "OFFSET"} {
			t.Run(clause+"_"+tc.literal, func(t *testing.T) {
				sql := "SELECT 1 " + clause + " " + tc.literal
				want := windowWant{100, 0, 26, 0, 25, true}
				if clause == "OFFSET" {
					want.fetchOffset = tc.value
				}
				w, tree := paginate(t, sql, 1, 25, 100)
				assertWindow(t, w, want)
				assertRootLimit(t, tree, 26, want.fetchOffset)
				assertRootLimit(t, parseTree(t, rootLimitText(t, tree)), 26, want.fetchOffset)
				rw, w, err := RewritePaginated(sql, "app", testCols, PageOptions{Page: 1, PageSize: 25, MaxRows: 100})
				if err != nil {
					t.Fatalf("RewritePaginated: %v", err)
				}
				assertWindow(t, w, want)
				assertRootLimit(t, parseTree(t, rw.SQL), 26, want.fetchOffset)
				if clause == "OFFSET" && tc.value == math.MaxInt64 {
					_, _, err := RewritePaginated(sql, "app", testCols, PageOptions{Page: 2, PageSize: 25, MaxRows: 100})
					if rejectCode(err) != "page_out_of_range" {
						t.Fatalf("offset addition overflow: %v", err)
					}
				}
			})
		}
	}
}

func TestPaginatePG_IntegerLiteralRejections(t *testing.T) {
	for _, literal := range []string{
		"9_223_372_036_854_775_808",
		"0x_8000_0000_0000_0000",
		"0o_1_000_000_000_000_000_000_000",
		"0b1" + strings.Repeat("0", 63),
		"00009223372036854775808",
		"1_844_674_407_370_955_161_600",
		"2_147_483_648.0", "2_147_483_648e0",
		"-0x80000000", "+0x80000000", "'0x80000000'",
		"(0x80000000 + 0)", "0x80000000::bigint",
	} {
		for _, clause := range []string{"LIMIT", "OFFSET"} {
			t.Run(clause+"_"+literal, func(t *testing.T) {
				sql := "SELECT 1 " + clause + " " + literal
				tree := parseTree(t, sql)
				before := proto.Clone(tree)
				_, err := PaginatePG(tree, PageOptions{Page: 1, PageSize: 25, MaxRows: 100})
				if rejectCode(err) != "unsupported_limit_offset_form" || !proto.Equal(before, tree) {
					t.Fatalf("error=%v or tree mutated", err)
				}
				rw, _, err := RewritePaginated(sql, "app", testCols, PageOptions{Page: 1, PageSize: 25, MaxRows: 100})
				if rejectCode(err) != "unsupported_limit_offset_form" || rw != nil {
					t.Fatalf("rewrite=%v err=%v", rw, err)
				}
			})
		}
	}
}

func TestRewritePaginated_InvalidIntegerSyntax(t *testing.T) {
	for _, literal := range []string{"2__147_483_648", "2_147_483_648_", "0x__80000000", "0x80000000_", "0x", "0b102", "0o8"} {
		for _, clause := range []string{"LIMIT", "OFFSET"} {
			sql := "SELECT 1 " + clause + " " + literal
			_, _, err := RewritePaginated(sql, "app", testCols, PageOptions{Page: 1, PageSize: 25, MaxRows: 100})
			if rejectCode(err) != "query_not_allowed" {
				t.Fatalf("%q: %v", sql, err)
			}
		}
	}
}
