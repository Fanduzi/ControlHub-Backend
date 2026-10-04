//go:build integration

// Package integration provides real-PostgreSQL paginated rewrite proofs.
// input: disposable PostgreSQL container, live pg_attribute resolver, pgsql.RewritePaginated, 220-row page fixtures
// output: G6 execution-window proofs — generated paginated SQL returns exactly the user's page slice with witness and FD integrity, incl. native LIMIT/OFFSET integer-spelling/int64 boundary acceptance and the unordered final-page AST window
// pos: T5 acceptance — computed fetch windows execute correctly against real PostgreSQL, incl. DISTINCT Q+D roots
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/fan/controlhub/internal/pgsql"
)

var (
	pgPageOnce sync.Once
	pgPageErr  error
)

func pageTablesPG(t *testing.T, c *pgx.Conn) {
	t.Helper()
	pgPageOnce.Do(func() {
		ddl := []string{
			`CREATE TABLE app.pg_page_rows (seq integer PRIMARY KEY)`,
			`INSERT INTO app.pg_page_rows SELECT generate_series(1,220)`,
			`CREATE TABLE app.pg_page_dups (seq integer)`,
			`INSERT INTO app.pg_page_dups SELECT g FROM generate_series(1,220) g UNION ALL SELECT g FROM generate_series(1,220) g`,
		}
		for _, s := range ddl {
			if _, err := c.Exec(context.Background(), s); err != nil {
				pgPageErr = fmt.Errorf("page fixture %q: %w", s, err)
				return
			}
		}
	})
	if pgPageErr != nil {
		t.Fatalf("page fixture: %v", pgPageErr)
	}
}

func rewritePage(t *testing.T, c *pgx.Conn, orig string, page, pageSize int) (*pgsql.RewriteResult, pgsql.PageWindow, pgResult) {
	t.Helper()
	return rewritePageOpts(t, c, orig,
		pgsql.PageOptions{Page: page, PageSize: pageSize, MaxRows: 100})
}

func rewritePageOpts(t *testing.T, c *pgx.Conn, orig string, opts pgsql.PageOptions) (*pgsql.RewriteResult, pgsql.PageWindow, pgResult) {
	t.Helper()
	rw, w, err := pgsql.RewritePaginated(orig, "app", liveResolver{c}, opts)
	if err != nil {
		t.Fatalf("RewritePaginated(%q %+v): %v", orig, opts, err)
	}
	return rw, w, runPG(t, c, rw.SQL)
}

func rewritePageErr(t *testing.T, c *pgx.Conn, orig string, page, pageSize int, code string) {
	t.Helper()
	rewritePageErrOpts(t, c, orig,
		pgsql.PageOptions{Page: page, PageSize: pageSize, MaxRows: 100}, code)
}

func rewritePageErrOpts(t *testing.T, c *pgx.Conn, orig string, opts pgsql.PageOptions, code string) {
	t.Helper()
	_, _, err := pgsql.RewritePaginated(orig, "app", liveResolver{c}, opts)
	if err == nil {
		t.Fatalf("RewritePaginated(%q %+v) succeeded, want %s", orig, opts, code)
	}
	r, ok := err.(*pgsql.RejectError)
	if !ok || r.Code != code {
		t.Fatalf("RewritePaginated(%q %+v) error = %v, want %s", orig, opts, err, code)
	}
}

func checkPagedProjection(t *testing.T, c *pgx.Conn, rw *pgsql.RewriteResult, orig, got pgResult) {
	t.Helper()
	nPub := len(rw.Public)
	if nPub != len(orig.names) {
		t.Fatalf("public width %d != original %d\nsql: %s", nPub, len(orig.names), rw.SQL)
	}
	if len(got.names) != nPub+len(rw.Witnesses) {
		t.Fatalf("width %d != public %d + witnesses %d\nsql: %s",
			len(got.names), nPub, len(rw.Witnesses), rw.SQL)
	}
	if !reflect.DeepEqual(rw.Public, orig.names) {
		t.Fatalf("public names %v != original %v\nsql: %s", rw.Public, orig.names, rw.SQL)
	}
	if !reflect.DeepEqual(got.names[:nPub], orig.names) ||
		!reflect.DeepEqual(got.oids[:nPub], orig.oids) ||
		!reflect.DeepEqual(got.typmods[:nPub], orig.typmods) {
		t.Fatalf("public FDs differ:\norig %v %v %v\ngot  %v %v %v\nsql: %s",
			orig.names, orig.oids, orig.typmods,
			got.names[:nPub], got.oids[:nPub], got.typmods[:nPub], rw.SQL)
	}
	for i, w := range rw.Witnesses {
		fd := nPub + i
		if got.names[fd] != w.Column {
			t.Fatalf("witness %d name %q != record %q", fd, got.names[fd], w.Column)
		}
		if want := reltypeOID(t, c, w.Entity.Schema, w.Entity.Name); got.oids[fd] != want {
			t.Fatalf("witness %s OID %d != reltype %d", w.Column, got.oids[fd], want)
		}
		for _, row := range got.rows {
			if row[fd] != nil {
				t.Fatalf("witness %s not NULL: %v", w.Column, row[fd])
			}
		}
	}
}

func publicRows(rw *pgsql.RewriteResult, got pgResult) [][]any {
	out := make([][]any, len(got.rows))
	for i, r := range got.rows {
		out[i] = r[:len(rw.Public)]
	}
	return out
}

func rootSelOf(t *testing.T, sql string) *pg.SelectStmt {
	t.Helper()
	tree, err := pgquery.Parse(sql)
	if err != nil {
		t.Fatalf("parse generated SQL: %v\n%s", err, sql)
	}
	sel := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	if sel == nil {
		t.Fatalf("generated root not a SelectStmt: %s", sql)
	}
	return sel
}

func aConstInt(n *pg.Node) (int64, bool) {
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

func assertRootWindow(t *testing.T, sel *pg.SelectStmt, limit, offset int64) {
	t.Helper()
	l, ok := aConstInt(sel.GetLimitCount())
	if !ok || l != limit {
		t.Fatalf("root LimitCount = %v, want %d", sel.GetLimitCount(), limit)
	}
	if offset == 0 {
		if sel.GetLimitOffset() != nil {
			if o, ok := aConstInt(sel.GetLimitOffset()); !ok || o != 0 {
				t.Fatalf("root LimitOffset = %v, want nil/0", sel.GetLimitOffset())
			}
		}
		return
	}
	o, ok := aConstInt(sel.GetLimitOffset())
	if !ok || o != offset {
		t.Fatalf("root LimitOffset = %v, want %d", sel.GetLimitOffset(), offset)
	}
}

func countLimitNodes(t *testing.T, m proto.Message) int {
	t.Helper()
	b, err := protojson.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return strings.Count(string(b), `"limitCount"`)
}

func seqSet(rows [][]any) map[int32]bool {
	s := map[int32]bool{}
	for _, r := range rows {
		if v, ok := r[0].(int32); ok {
			s[v] = true
		}
	}
	return s
}

func rowCounts(rows [][]any) map[string]int {
	m := map[string]int{}
	for _, r := range rows {
		m[cellKey(r)]++
	}
	return m
}

func allInRange(rows [][]any, lo, hi int32) bool {
	for _, r := range rows {
		v, ok := r[0].(int32)
		if !ok || v < lo || v > hi {
			return false
		}
	}
	return true
}

func TestPGPagination_UserWindow(t *testing.T) {
	c := sharedPG(t)
	pageTablesPG(t, c)
	const orig = `SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 30 OFFSET 10`

	before := runPG(t, c, orig)
	if len(before.rows) != 30 || before.rows[0][0] != int32(11) || before.rows[29][0] != int32(40) {
		t.Fatalf("original window not 30 rows 11..40: %v", before.rows)
	}

	rw1, w1, got1 := rewritePage(t, c, orig, 1, 25)
	if w1.Browsable != 30 || w1.PageStart != 0 || w1.FetchCount != 26 ||
		w1.FetchOffset != 10 || w1.RowLimit != 25 || w1.BudgetLimited {
		t.Fatalf("page1 window = %+v", w1)
	}
	checkPagedProjection(t, c, rw1, before, got1)
	sel1 := rootSelOf(t, rw1.SQL)
	assertRootWindow(t, sel1, 26, 10)
	if n := countLimitNodes(t, sel1); n != 1 {
		t.Fatalf("limit nodes = %d, want 1", n)
	}
	pr1, err := w1.Result(len(got1.rows))
	if err != nil || pr1.RowCount != 25 || !pr1.HasMore || pr1.BudgetReached {
		t.Fatalf("page1 Result = %+v, %v", pr1, err)
	}

	rw2, w2, got2 := rewritePage(t, c, orig, 2, 25)
	if w2.Browsable != 30 || w2.PageStart != 25 || w2.FetchCount != 5 ||
		w2.FetchOffset != 35 || w2.RowLimit != 5 || w2.BudgetLimited {
		t.Fatalf("page2 window = %+v", w2)
	}
	checkPagedProjection(t, c, rw2, before, got2)
	assertRootWindow(t, rootSelOf(t, rw2.SQL), 5, 35)
	pr2, err := w2.Result(len(got2.rows))
	if err != nil || pr2.RowCount != 5 || pr2.HasMore || pr2.BudgetReached {
		t.Fatalf("page2 Result = %+v, %v", pr2, err)
	}

	pub1, pub2 := publicRows(rw1, got1), publicRows(rw2, got2)
	if len(pub1) != 26 || len(pub2) != 5 {
		t.Fatalf("fetched page rows %d+%d", len(pub1), len(pub2))
	}
	if !reflect.DeepEqual(pub1[pr1.RowCount], before.rows[25]) {
		t.Fatalf("page-1 sentinel row %v != original row 26 %v", pub1[pr1.RowCount], before.rows[25])
	}
	if !reflect.DeepEqual(pub2[0], before.rows[25]) {
		t.Fatalf("page-2 first row %v != original row 26 %v", pub2[0], before.rows[25])
	}
	joined := append(append([][]any{}, pub1[:pr1.RowCount]...), pub2[:pr2.RowCount]...)
	if !reflect.DeepEqual(joined, before.rows) {
		t.Fatalf("delivered pages != original window\norig: %v\npages: %v", before.rows, joined)
	}

	rewritePageErr(t, c, orig, 3, 25, "page_out_of_range")
}

func TestPGPagination_UserWindowDistinct(t *testing.T) {
	c := sharedPG(t)
	pageTablesPG(t, c)
	const orig = `SELECT DISTINCT seq FROM app.pg_page_dups ORDER BY seq LIMIT 30 OFFSET 10`

	before := runPG(t, c, orig)
	if len(before.rows) != 30 || before.rows[0][0] != int32(11) || before.rows[29][0] != int32(40) {
		t.Fatalf("original DISTINCT window not 30 rows 11..40: %v", before.rows)
	}

	rw1, w1, got1 := rewritePage(t, c, orig, 1, 25)
	if w1.Browsable != 30 || w1.FetchCount != 26 || w1.FetchOffset != 10 || w1.RowLimit != 25 {
		t.Fatalf("distinct page1 window = %+v", w1)
	}
	checkPagedProjection(t, c, rw1, before, got1)
	d := rootSelOf(t, rw1.SQL)
	assertRootWindow(t, d, 26, 10)
	if n := countLimitNodes(t, d); n != 1 {
		t.Fatalf("limit nodes = %d, want D only", n)
	}
	q := d.GetFromClause()[0].GetRangeSubselect().GetSubquery().GetSelectStmt()
	if q == nil || q.GetLimitCount() != nil || q.GetLimitOffset() != nil {
		t.Fatalf("Q layer carries a page limit: %v", q)
	}
	pr1, err := w1.Result(len(got1.rows))
	if err != nil || pr1.RowCount != 25 || !pr1.HasMore {
		t.Fatalf("distinct page1 Result = %+v, %v", pr1, err)
	}

	rw2, w2, got2 := rewritePage(t, c, orig, 2, 25)
	checkPagedProjection(t, c, rw2, before, got2)
	assertRootWindow(t, rootSelOf(t, rw2.SQL), 5, 35)
	pr2, err := w2.Result(len(got2.rows))
	if err != nil || pr2.RowCount != 5 || pr2.HasMore {
		t.Fatalf("distinct page2 Result = %+v, %v", pr2, err)
	}
	pub1, pub2 := publicRows(rw1, got1), publicRows(rw2, got2)
	if !reflect.DeepEqual(pub1[pr1.RowCount], before.rows[25]) {
		t.Fatalf("distinct page-1 sentinel %v != original row 26 %v", pub1[pr1.RowCount], before.rows[25])
	}
	if !reflect.DeepEqual(pub2[0], before.rows[25]) {
		t.Fatalf("distinct page-2 first row %v != original row 26 %v", pub2[0], before.rows[25])
	}
	joined := append(append([][]any{}, pub1[:pr1.RowCount]...), pub2[:pr2.RowCount]...)
	if !reflect.DeepEqual(joined, before.rows) {
		t.Fatalf("DISTINCT delivered pages != original\norig: %v\npages: %v", before.rows, joined)
	}
	rewritePageErr(t, c, orig, 3, 25, "page_out_of_range")
}

func TestPGPagination_Forms(t *testing.T) {
	c := sharedPG(t)
	pageTablesPG(t, c)

	t.Run("small_limit_no_sentinel", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 3`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		if w.FetchCount != 3 || w.RowLimit != 3 || w.BudgetLimited {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 3 || pr.HasMore {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		if !reflect.DeepEqual(publicRows(rw, got), before.rows) {
			t.Fatalf("rows differ")
		}
		rewritePageErr(t, c, orig, 2, 25, "page_out_of_range")
	})

	t.Run("over_budget_limit", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 200`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 4, 25)
		if w.Browsable != 100 || w.PageStart != 75 || w.FetchCount != 25 || w.FetchOffset != 75 {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 25 || pr.HasMore || !pr.BudgetReached {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		if len(got.rows) != 25 {
			t.Fatalf("p4 rows = %d", len(got.rows))
		}
		want := before.rows[75:100]
		for i, r := range got.rows {
			if !reflect.DeepEqual(r[0], want[i][0]) {
				t.Fatalf("p4 row %d = %v, want %v", i, r[0], want[i][0])
			}
		}
		rewritePageErr(t, c, orig, 5, 25, "page_out_of_range")
	})

	t.Run("budget_partial_final_page", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 200`
		before := runPG(t, c, orig)
		rw, w, got := rewritePageOpts(t, c, orig,
			pgsql.PageOptions{Page: 3, PageSize: 25, MaxRows: 67})
		if w.Browsable != 67 || w.PageStart != 50 || w.FetchCount != 17 ||
			w.FetchOffset != 50 || w.RowLimit != 17 || !w.BudgetLimited {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		assertRootWindow(t, rootSelOf(t, rw.SQL), 17, 50)
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 17 || pr.HasMore || !pr.BudgetReached {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		if !reflect.DeepEqual(publicRows(rw, got), before.rows[50:67]) {
			t.Fatalf("p3 rows != original 51..67")
		}
		rewritePageErrOpts(t, c, orig,
			pgsql.PageOptions{Page: 4, PageSize: 25, MaxRows: 67}, "page_out_of_range")
	})

	t.Run("unbounded_ordered", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows ORDER BY seq`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 4, 25)
		if w.Browsable != 100 || w.PageStart != 75 || w.FetchCount != 25 || !w.BudgetLimited {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 25 || pr.HasMore || !pr.BudgetReached {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		for i, r := range got.rows {
			if r[0] != before.rows[75+i][0] {
				t.Fatalf("p4 row %d = %v, want %v", i, r[0], before.rows[75+i][0])
			}
		}
	})

	t.Run("offset_only", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows ORDER BY seq OFFSET 10`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 4, 25)
		if w.Browsable != 100 || w.FetchOffset != 85 {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		for i, r := range got.rows {
			if r[0] != before.rows[75+i][0] {
				t.Fatalf("p4 row %d = %v, want %v", i, r[0], before.rows[75+i][0])
			}
		}
	})

	t.Run("zero_limit", func(t *testing.T) {
		for _, orig := range []string{
			`SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 0`,
			`SELECT DISTINCT seq FROM app.pg_page_rows ORDER BY seq LIMIT 0`,
		} {
			before := runPG(t, c, orig)
			rw, w, got := rewritePage(t, c, orig, 1, 25)
			if w.Browsable != 0 || w.FetchCount != 0 || w.RowLimit != 0 || w.BudgetLimited {
				t.Fatalf("window = %+v", w)
			}
			checkPagedProjection(t, c, rw, before, got)
			pr, err := w.Result(len(got.rows))
			if err != nil || pr.RowCount != 0 || pr.HasMore || pr.BudgetReached {
				t.Fatalf("Result = %+v, %v", pr, err)
			}
			rewritePageErr(t, c, orig, 2, 25, "page_out_of_range")
		}
	})

	t.Run("filtered_under_page", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows WHERE seq <= 47 ORDER BY seq`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 2, 25)
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 22 || pr.HasMore || pr.BudgetReached {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		checkPagedProjection(t, c, rw, before, got)
		for i, r := range got.rows {
			if r[0] != before.rows[25+i][0] {
				t.Fatalf("p2 row %d = %v, want %v", i, r[0], before.rows[25+i][0])
			}
		}
	})

	t.Run("limit_forms_and_wide", func(t *testing.T) {
		for _, sql := range []string{
			`SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT ALL`,
			`SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT NULL`,
		} {
			before := runPG(t, c, sql)
			rw, w, got := rewritePage(t, c, sql, 1, 25)
			if w.Browsable != 100 || w.FetchCount != 26 || w.RowLimit != 25 {
				t.Fatalf("%q window = %+v", sql, w)
			}
			checkPagedProjection(t, c, rw, before, got)
			if len(got.rows) != 26 {
				t.Fatalf("%q fetched %d rows", sql, len(got.rows))
			}
		}
		const fetchOnly = `SELECT seq FROM app.pg_page_rows ORDER BY seq FETCH FIRST 3 ROWS ONLY`
		before := runPG(t, c, fetchOnly)
		rw, w, got := rewritePage(t, c, fetchOnly, 1, 25)
		if w.Browsable != 3 || w.FetchCount != 3 || w.RowLimit != 3 {
			t.Fatalf("fetch-only window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		if !reflect.DeepEqual(publicRows(rw, got), before.rows) {
			t.Fatalf("fetch-only rows differ")
		}
		const wide = `SELECT seq FROM app.pg_page_rows ORDER BY seq OFFSET 2147483648`
		before = runPG(t, c, wide)
		if len(before.rows) != 0 {
			t.Fatalf("wide original returned %d rows", len(before.rows))
		}
		rw, w, got = rewritePage(t, c, wide, 1, 25)
		if w.FetchOffset != 2147483648 || w.FetchCount != 26 {
			t.Fatalf("wide window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		assertRootWindow(t, rootSelOf(t, rw.SQL), 26, 2147483648)
		if len(got.rows) != 0 {
			t.Fatalf("wide page fetched %d rows", len(got.rows))
		}
	})
}

func TestPGPagination_NestedWindows(t *testing.T) {
	c := sharedPG(t)
	pageTablesPG(t, c)

	t.Run("derived_inner_limit", func(t *testing.T) {
		const orig = `SELECT d.seq FROM (SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 5 OFFSET 2) d ORDER BY d.seq LIMIT 30 OFFSET 1`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		checkPagedProjection(t, c, rw, before, got)
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != len(before.rows) || pr.HasMore {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		sel := rootSelOf(t, rw.SQL)
		assertRootWindow(t, sel, 26, 1)
		inner := sel.GetFromClause()[0].GetRangeSubselect().GetSubquery().GetSelectStmt()
		assertRootWindow(t, inner, 5, 2)
		if !reflect.DeepEqual(publicRows(rw, got), before.rows) {
			t.Fatalf("rows differ\norig: %v\npage: %v", before.rows, publicRows(rw, got))
		}
	})

	t.Run("cte_inner_limit", func(t *testing.T) {
		const orig = `WITH c AS (SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 7 OFFSET 3) SELECT seq FROM c ORDER BY seq LIMIT 30 OFFSET 2`
		before := runPG(t, c, orig)
		rw, _, got := rewritePage(t, c, orig, 1, 25)
		checkPagedProjection(t, c, rw, before, got)
		sel := rootSelOf(t, rw.SQL)
		assertRootWindow(t, sel, 26, 2)
		cte := sel.GetWithClause().GetCtes()[0].GetCommonTableExpr().GetCtequery().
			GetSelectStmt()
		assertRootWindow(t, cte, 7, 3)
		if n := countLimitNodes(t, sel); n != 2 {
			t.Fatalf("limit nodes = %d, want 2", n)
		}
		if !reflect.DeepEqual(publicRows(rw, got), before.rows) {
			t.Fatalf("rows differ")
		}
	})

	t.Run("window_function_preserved", func(t *testing.T) {
		const orig = `SELECT seq, row_number() OVER (ORDER BY seq DESC) AS rn FROM app.pg_page_rows ORDER BY seq LIMIT 30 OFFSET 10`
		before := runPG(t, c, orig)
		rw, _, got := rewritePage(t, c, orig, 1, 25)
		checkPagedProjection(t, c, rw, before, got)
		sel := rootSelOf(t, rw.SQL)
		assertRootWindow(t, sel, 26, 10)
		var over *pg.WindowDef
		for _, tgt := range sel.GetTargetList() {
			if fc := tgt.GetResTarget().GetVal().GetFuncCall(); fc != nil && fc.GetOver() != nil {
				over = fc.GetOver()
			}
		}
		if over == nil || len(over.GetOrderClause()) == 0 {
			t.Fatalf("OVER clause changed: %v", over)
		}
		pub := publicRows(rw, got)
		if !reflect.DeepEqual(pub[:25], before.rows[:25]) {
			t.Fatalf("delivered rows != original slice")
		}
	})

	t.Run("setop_root_window", func(t *testing.T) {
		const orig = `WITH c AS (SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 8) SELECT seq FROM c UNION ALL SELECT seq FROM c ORDER BY seq LIMIT 5`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		if w.Browsable != 5 || w.FetchCount != 5 || w.RowLimit != 5 {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		sel := rootSelOf(t, rw.SQL)
		assertRootWindow(t, sel, 5, 0)
		cte := sel.GetWithClause().GetCtes()[0].GetCommonTableExpr().GetCtequery().
			GetSelectStmt()
		assertRootWindow(t, cte, 8, 0)
		larg := sel.GetLarg()
		if larg == nil || larg.GetLimitCount() != nil {
			t.Fatalf("set-op branch carries a page limit")
		}
		if !reflect.DeepEqual(publicRows(rw, got), before.rows) {
			t.Fatalf("rows differ\norig: %v\npage: %v", before.rows, publicRows(rw, got))
		}
	})

	t.Run("parenthesized_setop_branches", func(t *testing.T) {
		const orig = `WITH c AS (SELECT seq FROM app.pg_page_rows ORDER BY seq LIMIT 8) (SELECT seq FROM c ORDER BY seq LIMIT 4 OFFSET 1) UNION ALL (SELECT seq FROM c ORDER BY seq LIMIT 4 OFFSET 2) ORDER BY seq LIMIT 5 OFFSET 1`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		if w.Browsable != 5 || w.FetchCount != 5 || w.FetchOffset != 1 || w.RowLimit != 5 {
			t.Fatalf("window = %+v", w)
		}
		checkPagedProjection(t, c, rw, before, got)
		sel := rootSelOf(t, rw.SQL)
		assertRootWindow(t, sel, 5, 1)
		assertRootWindow(t, sel.GetLarg(), 4, 1)
		assertRootWindow(t, sel.GetRarg(), 4, 2)
		cte := sel.GetWithClause().GetCtes()[0].GetCommonTableExpr().GetCtequery().
			GetSelectStmt()
		assertRootWindow(t, cte, 8, 0)
		if !reflect.DeepEqual(publicRows(rw, got), before.rows) {
			t.Fatalf("rows differ\norig: %v\npage: %v", before.rows, publicRows(rw, got))
		}
	})
}

func TestPGPagination_Unordered(t *testing.T) {
	c := sharedPG(t)
	pageTablesPG(t, c)

	t.Run("small_filtered_multiset", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows WHERE seq <= 3`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		checkPagedProjection(t, c, rw, before, got)
		if !reflect.DeepEqual(rowCounts(publicRows(rw, got)), rowCounts(before.rows)) {
			t.Fatalf("multisets differ")
		}
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 3 || pr.HasMore {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
	})

	t.Run("unordered_dups_multiset", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_dups WHERE seq <= 3`
		before := runPG(t, c, orig)
		if len(before.rows) != 6 {
			t.Fatalf("original dups rows = %d", len(before.rows))
		}
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		checkPagedProjection(t, c, rw, before, got)
		if !reflect.DeepEqual(rowCounts(publicRows(rw, got)), rowCounts(before.rows)) {
			t.Fatalf("dup multisets differ\norig: %v\npage: %v",
				before.rows, publicRows(rw, got))
		}
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 6 || pr.HasMore {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
	})

	t.Run("budget_capped_membership", func(t *testing.T) {
		const orig = `SELECT seq FROM app.pg_page_rows`
		before := runPG(t, c, orig)
		rw, w, got := rewritePage(t, c, orig, 1, 25)
		checkPagedProjection(t, c, rw, before, got)
		if sel := rootSelOf(t, rw.SQL); len(sel.GetSortClause()) != 0 {
			t.Fatalf("ORDER BY injected: %s", rw.SQL)
		}
		pr, err := w.Result(len(got.rows))
		if err != nil || pr.RowCount != 25 || !pr.HasMore || pr.BudgetReached {
			t.Fatalf("Result = %+v, %v", pr, err)
		}
		if len(got.rows) != 26 || !allInRange(got.rows, 1, 220) {
			t.Fatalf("fetched rows out of source domain: %v", got.rows)
		}
		src := seqSet(before.rows)
		for _, r := range got.rows {
			if !src[r[0].(int32)] {
				t.Fatalf("row %v not in source", r[0])
			}
		}
		rw4, w4, got4 := rewritePage(t, c, orig, 4, 25)
		checkPagedProjection(t, c, rw4, before, got4)
		assertRootWindow(t, rootSelOf(t, rw4.SQL), 25, 75)
		pr4, err := w4.Result(len(got4.rows))
		if err != nil || pr4.RowCount != 25 || pr4.HasMore || !pr4.BudgetReached {
			t.Fatalf("p4 Result = %+v, %v", pr4, err)
		}
		if !allInRange(got4.rows, 1, 220) {
			t.Fatalf("p4 rows out of source domain")
		}
		rewritePageErr(t, c, orig, 5, 25, "page_out_of_range")
	})
}

func TestPGPagination_IntegerLiteralForms(t *testing.T) {
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
	c := sharedPG(t)
	pageTablesPG(t, c)
	for _, tc := range cases {
		for _, clause := range []string{"LIMIT", "OFFSET"} {
			t.Run(clause+"_"+tc.literal, func(t *testing.T) {
				orig := "SELECT seq FROM app.pg_page_rows ORDER BY seq " + clause + " " + tc.literal
				before := runPG(t, c, orig)
				wantOffset, wantRows := int64(0), 26
				if clause == "OFFSET" {
					wantOffset, wantRows = tc.value, 0
				}
				rw, w, got := rewritePage(t, c, orig, 1, 25)
				if w.Page != 1 || w.PageSize != 25 || w.Browsable != 100 || w.PageStart != 0 || w.FetchCount != 26 || w.FetchOffset != wantOffset || w.RowLimit != 25 || !w.BudgetLimited {
					t.Fatalf("window=%+v want offset=%d", w, wantOffset)
				}
				assertRootWindow(t, rootSelOf(t, rw.SQL), 26, wantOffset)
				checkPagedProjection(t, c, rw, before, got)
				if len(got.rows) != wantRows {
					t.Fatalf("rows=%d want=%d", len(got.rows), wantRows)
				}
				result, err := w.Result(len(got.rows))
				if err != nil || result.BudgetReached {
					t.Fatalf("Result=%+v err=%v", result, err)
				}
				if clause == "LIMIT" {
					if len(before.rows) != 220 || !reflect.DeepEqual(publicRows(rw, got), before.rows[:26]) || result.RowCount != 25 || !result.HasMore {
						t.Fatalf("LIMIT page differs from native window: %+v", result)
					}
				} else if len(before.rows) != 0 || result.RowCount != 0 || result.HasMore {
					t.Fatalf("OFFSET page differs from native window: %+v", result)
				}
			})
		}
	}
}
