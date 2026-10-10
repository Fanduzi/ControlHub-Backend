//go:build integration

// Package service proves the T7-S3/S4 governed executor end to end against a
// disposable PostgreSQL 16 server.
// input: context, errors, fmt, strings, sync, testing, time, jackc/pgx/v5,
// jackc/pgx/v5/pgconn, internal/pgsql
// output: acceptance tests for the frozen contract — production
// OpenPostgresPool → ExecutePGGoverned → PGBind → RewriteFromGuardPaginated →
// ExecParams → per-position verification → second-connection lock audit →
// cleanup: simple/constant/zero-row verticals, G7 native-text fidelity,
// S1/S2 identity layouts, T4/T5 equivalence, blocked-DDL drift, unexplained
// lock Mismatch, timeout/cancel classification, and resource release
// pos: T7-S3/S4 real-PostgreSQL acceptance — the restricted ro_bind account
// runs the whole production chain; admin only builds fixtures and injects
// controlled interleaves
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/pgsql"
)

var (
	pgExecOnce sync.Once
	pgExecErr  error
)

// pgExecFixture extends the shared bind fixture with governed-execution data:
// deterministic rows in already-granted tables plus one G7 type-fidelity
// table created and explicitly granted to ro_bind.
func pgExecFixture(t *testing.T) pgPoolLab {
	t.Helper()
	lab := pgBindFixture(t)
	pgExecOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		admin, err := pgx.Connect(ctx, fmt.Sprintf(
			"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=ch-t7s34-fixture",
			lab.host, lab.port, lab.db, lab.user, pgPoolFactoryPassword))
		if err != nil {
			pgExecErr = err
			return
		}
		defer admin.Close(ctx)
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS app.g7types (
			    i integer PRIMARY KEY, b bigint, n numeric, t timestamptz,
			    ts timestamp, d date, tm time, iv interval, by bytea,
			    arr integer[], j json, jb jsonb, flag boolean, u uuid,
			    f float8, s text)`,
			`GRANT SELECT ON app.g7types TO ro_bind`,
			`DELETE FROM app.g7types`,
			`INSERT INTO app.g7types VALUES
			   (1, 9007199254740993, 123456789.123456789123456789,
			    '2024-01-02 03:04:05.123456+00'::timestamptz,
			    '2024-01-02 03:04:05.123456'::timestamp,
			    '2024-01-02', '03:04:05.123456', '1 day 02:03:04.5',
			    '\xdeadbeef'::bytea, '{1,2,NULL,4}',
			    '{"a": 9007199254740993}'::json,
			    '{"a": 9007199254740993}'::jsonb,
			    true, 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11'::uuid,
			    0.1, 'plain')`,
			`INSERT INTO app.g7types (i, s) VALUES
			   (2, ''), (3, 'NULL')`,
			// i=4 is large enough to be stored out of line: reading it
			// detoasts under an ACCESS SHARE lock on the toast relation —
			// the audit must explain that lock through the ownership edge,
			// not through the approved set.
			`INSERT INTO app.g7types (i, s) VALUES (4, repeat('x', 10000))`,
			`DELETE FROM app.orders`,
			`INSERT INTO app.orders (id, item, qty, note, amount, ts) VALUES
			   (1, 'alpha', 10, 'n1', 100.50, '2024-01-01 00:00:00+00'),
			   (2, 'beta', 20, 'n2', 200.75, '2024-01-02 00:00:00+00'),
			   (3, 'gamma', 20, NULL, NULL, NULL)`,
			`DELETE FROM app.smallvals`,
			`INSERT INTO app.smallvals (id, tag)
			   SELECT g, 'tag-' || g::text FROM generate_series(1, 30) g`,
		}
		for _, stmt := range stmts {
			if _, err := admin.Exec(ctx, stmt); err != nil {
				pgExecErr = fmt.Errorf("exec fixture %q: %w", stmt, err)
				return
			}
		}
	})
	if pgExecErr != nil {
		t.Fatalf("pg exec fixture: %v", pgExecErr)
	}
	return lab
}

func pgExecLabConfig(t *testing.T, lab pgPoolLab) *pgx.ConnConfig {
	t.Helper()
	dsn := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=ro_bind password=%s sslmode=disable connect_timeout=5 application_name=ch-t7s34-exec",
		lab.host, lab.port, lab.db, pgBindRolePassword)
	cfg, err := validatePGDSNBinding(dsn, lab.host, lab.port, lab.db)
	if err != nil {
		t.Fatalf("validate dsn: %v", err)
	}
	return cfg
}

func pgExecInput(t *testing.T, lab pgPoolLab, stmt string, page pgsql.PageOptions) PGExecuteInput {
	t.Helper()
	return PGExecuteInput{
		ConnConfig:   pgExecLabConfig(t, lab),
		Database:     lab.db,
		PinnedSchema: "app",
		Statement:    stmt,
		Page:         page,
		Timeout:      15 * time.Second,
	}
}

func pageOpts(page, size int) pgsql.PageOptions {
	return pgsql.PageOptions{Page: page, PageSize: size, MaxRows: 100}
}

func textCell(t *testing.T, rows [][]*string, r, c int) string {
	t.Helper()
	if r >= len(rows) || c >= len(rows[r]) || rows[r][c] == nil {
		t.Fatalf("cell (%d,%d) missing or NULL", r, c)
	}
	return *rows[r][c]
}

// First vertical: a simple table query, a constant SELECT, and zero-row
// results each pass the full chain — pool → bind → rewrite → ExecParams →
// per-position verification → independent lock audit → cleanup — and the
// executor releases every backend afterwards.
func TestPGExecute_MinimalVertical(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()

	t.Run("simple table query", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT o.id, o.item, o.qty, o.note, o.amount FROM orders o ORDER BY o.id`,
			pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Columns) != 5 {
			t.Fatalf("columns = %d", len(res.Columns))
		}
		if res.Columns[0].Name != "id" || res.Columns[0].DatabaseType != "INT8" {
			t.Fatalf("column 0 = %+v", res.Columns[0])
		}
		if res.Columns[4].DatabaseType != "NUMERIC" {
			t.Fatalf("column 4 = %+v", res.Columns[4])
		}
		if len(res.Rows) != 3 {
			t.Fatalf("rows = %d", len(res.Rows))
		}
		if textCell(t, res.Rows, 0, 0) != "1" || textCell(t, res.Rows, 0, 1) != "alpha" {
			t.Fatalf("row 0 = %v", res.Rows[0])
		}
		if res.Rows[2][3] != nil || res.Rows[2][4] != nil {
			t.Fatal("NULL note/amount must stay nil")
		}
		if res.Page.HasMore {
			t.Fatal("single page must not report more rows")
		}
	})

	t.Run("constant select", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT 1, 'x'::text, NULL::text`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Columns) != 3 || len(res.Rows) != 1 {
			t.Fatalf("constant result cols=%d rows=%d", len(res.Columns), len(res.Rows))
		}
		if textCell(t, res.Rows, 0, 0) != "1" || textCell(t, res.Rows, 0, 1) != "x" {
			t.Fatalf("constant row = %v", res.Rows[0])
		}
		if res.Rows[0][2] != nil {
			t.Fatal("NULL literal must stay nil")
		}
	})

	t.Run("zero rows keeps FD proof", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT o.id, o.item FROM orders o WHERE 1 = 0`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Columns) != 2 || res.Columns[0].DatabaseType != "INT8" {
			t.Fatalf("zero-row columns = %+v", res.Columns)
		}
		if len(res.Rows) != 0 {
			t.Fatalf("zero-row query returned %d rows", len(res.Rows))
		}
	})

	t.Run("user limit zero", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT o.id FROM orders o LIMIT 0`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Columns) != 1 || len(res.Rows) != 0 {
			t.Fatalf("LIMIT 0 result cols=%d rows=%d", len(res.Columns), len(res.Rows))
		}
	})

	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// G7 — every delivered cell keeps the database-native text exactly: large
// integers, full-precision numeric, UTC timestamptz, zone-less timestamp,
// hex bytea, arrays with NULL elements, json/jsonb text, boolean, uuid,
// float, and the NULL/empty/"NULL" three-way distinction.
func TestPGExecute_G7NativeText(t *testing.T) {
	lab := pgExecFixture(t)
	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT g.b, g.n, g.t, g.ts, g.d, g.tm, g.iv, g.by, g.arr, g.j, g.jb,
		        g.flag, g.u, g.f, g.s
		   FROM app.g7types g WHERE g.i = 1`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	wantTypes := []string{
		"INT8", "NUMERIC", "TIMESTAMPTZ", "TIMESTAMP", "DATE", "TIME",
		"INTERVAL", "BYTEA", "_INT4", "JSON", "JSONB", "BOOL", "UUID",
		"FLOAT8", "TEXT",
	}
	if len(res.Columns) != len(wantTypes) {
		t.Fatalf("columns = %d", len(res.Columns))
	}
	for i, want := range wantTypes {
		if res.Columns[i].DatabaseType != want {
			t.Fatalf("column %d type = %q, want %q", i, res.Columns[i].DatabaseType, want)
		}
	}
	want := []string{
		"9007199254740993",
		"123456789.123456789123456789",
		"2024-01-02 03:04:05.123456+00",
		"2024-01-02 03:04:05.123456",
		"2024-01-02",
		"03:04:05.123456",
		"1 day 02:03:04.5",
		`\xdeadbeef`,
		"{1,2,NULL,4}",
		`{"a": 9007199254740993}`,
		`{"a": 9007199254740993}`,
		"t",
		"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11",
		"0.1",
		"plain",
	}
	if len(res.Rows) != 1 {
		t.Fatalf("rows = %d", len(res.Rows))
	}
	for i, w := range want {
		got := textCell(t, res.Rows, 0, i)
		if got != w {
			t.Fatalf("cell %d (%s) = %q, want %q", i, res.Columns[i].Name, got, w)
		}
	}

	// NULL, empty string, and the literal text "NULL" stay three distinct
	// values.
	res2, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT g.n, g.s FROM app.g7types g WHERE g.i IN (2, 3) ORDER BY g.i`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res2.Rows) != 2 {
		t.Fatalf("rows = %d", len(res2.Rows))
	}
	if res2.Rows[0][0] != nil {
		t.Fatal("SQL NULL must stay nil")
	}
	if res2.Rows[0][1] == nil || *res2.Rows[0][1] != "" {
		t.Fatalf("empty string = %v, want non-nil empty", res2.Rows[0][1])
	}
	if res2.Rows[1][1] == nil || *res2.Rows[1][1] != "NULL" {
		t.Fatalf("text 'NULL' = %v, want literal text", res2.Rows[1][1])
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Identity and layout through the entry: double aliases, CTE use sites,
// derived tables, JOIN USING merges, quoted aliases, repeated public names —
// verification is by final position, never by set count.
func TestPGExecute_IdentityLayouts(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()

	cases := []struct {
		name string
		stmt string
		cols int
		rows int
	}{
		{"same table two aliases",
			`SELECT x.id, y.item FROM orders x JOIN orders y ON x.id = y.id ORDER BY x.id`, 2, 3},
		{"cte used twice",
			`WITH d AS (SELECT id, item FROM app.orders)
			  SELECT a.id, b.item FROM d a JOIN d b ON a.id = b.id ORDER BY a.id`, 2, 3},
		{"derived table",
			`SELECT q.id, q.qty FROM (SELECT id, qty FROM app.orders) q ORDER BY q.id`, 2, 3},
		{"join using merge",
			`SELECT id, s.tag, w.total FROM app.smallvals s JOIN analytics.widevals w USING (id) ORDER BY id`, 3, 0},
		{"quoted relation and column",
			`SELECT d."Line No" FROM "Order Details" d`, 1, 0},
		{"duplicate public names",
			`SELECT o.id, u.id FROM orders o JOIN users u ON o.id = u.id`, 2, 0},
		{"set-op merged coverage via CTEs",
			`WITH a AS (SELECT id FROM app.orders), b AS (SELECT id FROM app.orders)
			 SELECT id FROM a UNION ALL SELECT id FROM b ORDER BY id`, 1, 6},
		{"natural join merge",
			`SELECT id, s.tag FROM app.smallvals s NATURAL JOIN analytics.widevals w`, -1, 0},
		{"alias star layout",
			`SELECT o.* FROM orders o ORDER BY o.id`, 6, 3},
		{"ordinal sort reference",
			`SELECT o.id, o.item FROM orders o ORDER BY 1`, 2, 3},
		{"cte chain",
			`WITH a AS (SELECT id, item FROM app.orders),
			      b AS (SELECT id, item FROM a)
			 SELECT id, item FROM b ORDER BY id`, 2, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab, tc.stmt, pageOpts(1, 25)))
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if tc.cols >= 0 && len(res.Columns) != tc.cols {
				t.Fatalf("cols=%d, want %d", len(res.Columns), tc.cols)
			}
			if len(res.Rows) != tc.rows {
				t.Fatalf("rows=%d, want %d", len(res.Rows), tc.rows)
			}
		})
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// T4/T5 forms through the entry: aggregates on empty input, GROUPING SETS,
// windows, DISTINCT and DISTINCT ON, the Q+D path, pagination sentinel and
// boundary pages.
func TestPGExecute_ShapesAndPagination(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()

	t.Run("empty-input aggregate", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT count(*), max(o.qty) FROM orders o WHERE 1 = 0`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Rows) != 1 || textCell(t, res.Rows, 0, 0) != "0" || res.Rows[0][1] != nil {
			t.Fatalf("empty aggregate row = %v", res.Rows[0])
		}
	})

	t.Run("grouping sets and having", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT o.qty, count(*) FROM orders o GROUP BY GROUPING SETS ((o.qty), ())
			  HAVING count(*) >= 1 ORDER BY o.qty NULLS LAST`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Rows) != 3 {
			t.Fatalf("grouping rows = %d, want 3 (qty 10, qty 20, grand total)", len(res.Rows))
		}
	})

	t.Run("window function", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT o.id, row_number() OVER (ORDER BY o.id) FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Rows) != 3 || textCell(t, res.Rows, 2, 1) != "3" {
			t.Fatalf("window rows = %v", res.Rows)
		}
	})

	t.Run("distinct", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT DISTINCT o.qty FROM orders o ORDER BY o.qty`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Rows) != 2 {
			t.Fatalf("distinct rows = %d, want 2", len(res.Rows))
		}
	})

	t.Run("distinct on", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT DISTINCT ON (o.qty) o.qty, o.item FROM orders o ORDER BY o.qty, o.id`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Rows) != 2 || textCell(t, res.Rows, 0, 1) != "alpha" {
			t.Fatalf("distinct-on rows = %v", res.Rows)
		}
	})

	t.Run("count star non-key group by", func(t *testing.T) {
		res, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT o.item, count(*) FROM orders o GROUP BY o.item
			  HAVING count(*) >= 1 ORDER BY o.item`, pageOpts(1, 25)))
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		if len(res.Rows) != 3 || textCell(t, res.Rows, 0, 1) != "1" {
			t.Fatalf("group rows = %v", res.Rows)
		}
	})

	t.Run("pagination sentinel and bounds", func(t *testing.T) {
		page1, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT s.id, s.tag FROM smallvals s ORDER BY s.id`, pageOpts(1, 10)))
		if err != nil {
			t.Fatalf("page1: %v", err)
		}
		if len(page1.Rows) != 10 || !page1.Page.HasMore {
			t.Fatalf("page1 rows=%d hasMore=%v", len(page1.Rows), page1.Page.HasMore)
		}
		if textCell(t, page1.Rows, 0, 0) != "1" || textCell(t, page1.Rows, 9, 0) != "10" {
			t.Fatalf("page1 ids %s..%s", *page1.Rows[0][0], *page1.Rows[9][0])
		}
		page3, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT s.id FROM smallvals s ORDER BY s.id`, pageOpts(3, 10)))
		if err != nil {
			t.Fatalf("page3: %v", err)
		}
		if len(page3.Rows) != 10 || page3.Page.HasMore {
			t.Fatalf("page3 rows=%d hasMore=%v", len(page3.Rows), page3.Page.HasMore)
		}
		if textCell(t, page3.Rows, 0, 0) != "21" || textCell(t, page3.Rows, 9, 0) != "30" {
			t.Fatalf("page3 ids %s..%s", *page3.Rows[0][0], *page3.Rows[9][0])
		}
		if _, err := ExecutePGGoverned(ctx, pgExecInput(t, lab,
			`SELECT s.id FROM smallvals s LIMIT 30`, pageOpts(4, 10))); err == nil {
			t.Fatal("page 4 beyond 30 rows must reject")
		} else {
			var re *pgsql.RejectError
			if !errors.As(err, &re) || re.Code != "page_out_of_range" {
				t.Fatalf("page4 err = %v, want page_out_of_range", err)
			}
		}
	})

	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Views, partitions, and the shared catalog all satisfy the lock audit by
// recorded identity — the observer never guesses.
func TestPGExecute_AuditSources(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		stmt string
	}{
		{"view dep closure", `SELECT v.id, v.item FROM app.order_view v ORDER BY v.id`},
		{"nested view", `SELECT n.id FROM app.nested_view n ORDER BY n.id`},
		{"partition children", `SELECT e.id FROM app.events e`},
		{"shared catalog", `SELECT d.datname FROM pg_catalog.pg_database d ORDER BY d.datname`},
		{"materialized view", `SELECT m.id FROM app.order_mv m`},
		{"toast detoast lock", `SELECT g.s FROM app.g7types g WHERE g.i = 4`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ExecutePGGoverned(ctx, pgExecInput(t, lab, tc.stmt, pageOpts(1, 25))); err != nil {
				t.Fatalf("execute: %v", err)
			}
		})
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// An extra relation lock inside the attempt — nothing the query was approved
// for — is unexplained access: the audit must Mismatch and reject, dropping
// the whole result.
func TestPGExecute_AuditMismatch(t *testing.T) {
	lab := pgExecFixture(t)
	pgExecAfterBindHook = func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `LOCK TABLE app.users IN ACCESS SHARE MODE`)
		return err
	}
	defer func() { pgExecAfterBindHook = nil }()

	_, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o`, pageOpts(1, 25)))
	if err == nil {
		t.Fatal("unexplained held lock passed the audit")
	}
	var re *pgsql.RejectError
	if !errors.As(err, &re) || re.Code != "query_binding_mismatch" {
		t.Fatalf("err = %v, want query_binding_mismatch", err)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// While the attempt holds its ACCESS SHARE locks, concurrent DDL on a bound
// relation is blocked — the barrier asserts the 57014 block before the
// attempt finishes, so "drift under held locks" is proven impossible, not
// assumed.
func TestPGExecute_DDLBlockedUnderLock(t *testing.T) {
	lab := pgExecFixture(t)
	admin := lab.adminConn(t)
	blocked := make(chan error, 1)
	var ddlErr error
	reported := make(chan struct{})
	// The hook waits inside the attempt for the DDL outcome: the execution
	// transaction stays open with its ACCESS SHARE locks held the whole
	// time, so a succeeding ALTER would be a real safety violation, not a
	// timing artifact. The DDL's own statement_timeout is the deadlock guard.
	pgExecAfterBindHook = func(ctx context.Context, tx pgx.Tx) error {
		go func() {
			cctx := context.Background()
			atx, err := admin.Begin(cctx)
			if err != nil {
				blocked <- err
				return
			}
			defer atx.Rollback(cctx)
			if _, err := atx.Exec(cctx, `SET LOCAL statement_timeout = '1500ms'`); err != nil {
				blocked <- err
				return
			}
			_, err = atx.Exec(cctx, `ALTER TABLE app.orders RENAME COLUMN item TO item_x`)
			blocked <- err
		}()
		select {
		case ddlErr = <-blocked:
		case <-time.After(8 * time.Second):
		}
		close(reported)
		return nil
	}
	defer func() { pgExecAfterBindHook = nil }()

	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id, o.item FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("rows = %d", len(res.Rows))
	}
	<-reported
	var pgErr *pgconn.PgError
	if !errors.As(ddlErr, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("blocked DDL error = %v, want SQLSTATE 57014", ddlErr)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Timeout and cancellation classify onto the frozen vocabulary, no result is
// delivered, and a later independent attempt still runs — the connection and
// lock state converged.
func TestPGExecute_TimeoutCancelAndRecovery(t *testing.T) {
	lab := pgExecFixture(t)

	t.Run("user sql timeout", func(t *testing.T) {
		in := pgExecInput(t, lab,
			`SELECT count(*) FROM generate_series(1, 2000000000)`, pageOpts(1, 25))
		in.Timeout = 1500 * time.Millisecond
		started := time.Now()
		_, err := ExecutePGGoverned(context.Background(), in)
		if !errors.Is(err, ErrQueryTimeout) {
			t.Fatalf("err = %v, want ErrQueryTimeout", err)
		}
		if elapsed := time.Since(started); elapsed > 10*time.Second {
			t.Fatalf("timeout attempt ran %s", elapsed)
		}
	})

	t.Run("lock wait timeout", func(t *testing.T) {
		admin := lab.adminConn(t)
		locked := make(chan error, 1)
		release := make(chan struct{})
		go func() {
			cctx := context.Background()
			tx, err := admin.Begin(cctx)
			if err != nil {
				locked <- err
				return
			}
			if _, err := tx.Exec(cctx, `LOCK TABLE app.lockme IN ACCESS EXCLUSIVE MODE`); err != nil {
				tx.Rollback(cctx)
				locked <- err
				return
			}
			locked <- nil
			<-release
			tx.Rollback(cctx)
		}()
		if err := <-locked; err != nil {
			t.Fatalf("admin lock: %v", err)
		}
		defer close(release)

		in := pgExecInput(t, lab, `SELECT l.id FROM lockme l`, pageOpts(1, 25))
		in.Timeout = 1500 * time.Millisecond
		_, err := ExecutePGGoverned(context.Background(), in)
		if !errors.Is(err, ErrQueryTimeout) {
			t.Fatalf("err = %v, want ErrQueryTimeout", err)
		}
	})

	t.Run("caller cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			in := pgExecInput(t, lab,
				`SELECT count(*) FROM generate_series(1, 2000000000)`, pageOpts(1, 25))
			_, err := ExecutePGGoverned(ctx, in)
			done <- err
		}()
		time.Sleep(300 * time.Millisecond)
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("canceled attempt did not return")
		}
	})

	// After timeout and cancellation, a fresh attempt on the same target
	// executes normally — no leaked connection or lock state.
	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("recovery attempt: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("recovery rows = %d", len(res.Rows))
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// A probe statement run on the same connection after materialization must
// not corrupt the saved FieldDescriptions or cell bytes — the attempt copies
// them out of pgx buffers before anything else can reuse the memory. If any
// slice still aliased the driver buffers, verification would fail here.
func TestPGExecute_FDLifetimeProbe(t *testing.T) {
	lab := pgExecFixture(t)
	probeRan := false
	pgExecAfterMaterializeHook = func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT 'overwrite-probe'::text, 1::int4, 2::int4, now()::text`)
		if err != nil {
			return err
		}
		probeRan = true
		for rows.Next() {
		}
		rows.Close()
		return rows.Err()
	}
	defer func() { pgExecAfterMaterializeHook = nil }()

	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id, o.item FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !probeRan {
		t.Fatal("same-connection probe never ran")
	}
	if len(res.Columns) != 2 || res.Columns[0].Name != "id" ||
		res.Columns[0].DatabaseType != "INT8" {
		t.Fatalf("saved FDs overwritten: %+v", res.Columns)
	}
	if len(res.Rows) != 3 || textCell(t, res.Rows, 0, 0) != "1" ||
		textCell(t, res.Rows, 0, 1) != "alpha" {
		t.Fatalf("saved rows overwritten: %v", res.Rows)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// A server error arriving mid-read (division by zero on the second row)
// is a classified backend failure — the partial buffer is dropped, the
// transaction still rolls back, and a later attempt runs normally.
func TestPGExecute_MidReadFailure(t *testing.T) {
	lab := pgExecFixture(t)
	_, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id, 1 / (o.id - 2) FROM orders o`, pageOpts(1, 25)))
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("err = %v, want ErrQueryBackendFailure", err)
	}
	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("recovery rows=%d", len(res.Rows))
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Killing the execution backend mid-attempt fails the attempt; ROLLBACK on
// the dead connection also fails inside cleanup — neither failure may mask
// the other, everything still releases, and a later attempt executes.
func TestPGExecute_BackendTerminated(t *testing.T) {
	lab := pgExecFixture(t)
	admin := lab.adminConn(t)
	pgExecAfterBindHook = func(ctx context.Context, tx pgx.Tx) error {
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_catalog.pg_backend_pid()`).Scan(&pid); err != nil {
			return err
		}
		var killed bool
		if err := admin.QueryRow(context.Background(),
			`SELECT pg_catalog.pg_terminate_backend($1)`, pid).Scan(&killed); err != nil {
			return err
		}
		// pg_terminate_backend posts the signal asynchronously; give the
		// backend a beat to die so the next statement deterministically fails
		// instead of racing the kill.
		time.Sleep(150 * time.Millisecond)
		return nil
	}
	defer func() { pgExecAfterBindHook = nil }()

	_, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a classified failure", err)
	}
	pgExecAfterBindHook = nil // recovery attempt must not kill its own backend
	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("recovery: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("recovery rows=%d", len(res.Rows))
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// The rejected path through the entry keeps the same contract the gate
// established: missing objects and unwitnessable references are controlled
// rejections, never internal failures — and nothing is delivered.
func TestPGExecute_ControlledRejections(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		stmt string
		want string
	}{
		{"missing object", `SELECT * FROM nosuch_table`, "query_object_not_found"},
		{"no catalog fallback", `SELECT * FROM pg_locks`, "query_object_not_found"},
		{"where sublink not witnessable",
			`SELECT o.id FROM orders o WHERE EXISTS (SELECT 1 FROM users u WHERE u.id = o.id)`,
			"query_reference_not_witnessable"},
		{"entity ref in set-op leaf not witnessable",
			`SELECT id FROM orders UNION ALL SELECT id FROM orders`,
			"query_reference_not_witnessable"},
		{"non select", `DELETE FROM orders`, "query_not_allowed"},
		{"multi statement", `SELECT 1; SELECT 2`, "query_not_allowed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ExecutePGGoverned(ctx, pgExecInput(t, lab, tc.stmt, pageOpts(1, 25)))
			if err == nil {
				t.Fatalf("%q executed", tc.stmt)
			}
			if code := rejectCodeOf(t, err); code != tc.want {
				t.Fatalf("%q code = %s, want %s (err %v)", tc.stmt, code, tc.want, err)
			}
		})
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}
