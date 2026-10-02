//go:build integration

// Package integration provides real-PostgreSQL rewrite-equivalence proofs.
// input: disposable PostgreSQL container, live information_schema resolver, pgsql.Rewrite
// output: original-vs-rewritten result/ordering/pagination equivalence and witness reltype proofs
// pos: T4 acceptance — generated transport SQL executes correctly against real PostgreSQL
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	pgc "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/fan/controlhub/internal/pgsql"
)

var (
	pgOnce sync.Once
	pgConn *pgx.Conn
	pgErr  error
)

// sharedPG lazily starts one disposable PostgreSQL for the whole package run.
// The container is reclaimed by testcontainers' Ryuk reaper on exit.
func sharedPG(t *testing.T) *pgx.Conn {
	t.Helper()
	pgOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		c, err := pgc.Run(ctx, "postgres:16-alpine",
			pgc.WithDatabase("rewrite_lab"),
			pgc.WithUsername("lab"),
			pgc.WithPassword("lab"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).WithStartupTimeout(90*time.Second)),
		)
		if err != nil {
			pgErr = fmt.Errorf("start postgres container: %w", err)
			return
		}
		dsn, err := c.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			pgErr = fmt.Errorf("postgres dsn: %w", err)
			return
		}
		pgConn, pgErr = pgx.Connect(ctx, dsn)
		if pgErr != nil {
			return
		}
		if err := seedPGLab(ctx, pgConn); err != nil {
			pgErr = err
		}
	})
	if pgErr != nil {
		t.Fatalf("postgres fixture: %v", pgErr)
	}
	return pgConn
}

func seedPGLab(ctx context.Context, c *pgx.Conn) error {
	ddl := []string{
		`CREATE SCHEMA app`,
		`CREATE TABLE app.orders (id int, amt numeric)`,
		`CREATE TABLE app.items (id int, oid int)`,
		`CREATE TABLE app.t (a int, b text)`,
		`CREATE TABLE app.t2 (a int, c text)`,
		// Disjoint id sets exercise RIGHT/FULL merged-column null-extension.
		`INSERT INTO app.orders VALUES (1,10),(2,20),(3,30)`,
		`INSERT INTO app.items VALUES (2,200),(3,300),(4,400)`,
		`INSERT INTO app.t VALUES (1,'x'),(2,'y'),(5,'z')`,
		`INSERT INTO app.t2 VALUES (2,'p'),(5,'q'),(7,'r')`,
		`CREATE TABLE app.big (seq int)`,
		`INSERT INTO app.big SELECT generate_series(1,30)`,
		// Same-valued USING keys at different numeric scales — the merged
		// column must surface the join-correct side's representation.
		`CREATE TABLE app.njl (id numeric)`,
		`CREATE TABLE app.njr (id numeric)`,
		`INSERT INTO app.njl VALUES (1.0), (2.0)`,
		`INSERT INTO app.njr VALUES (1.00), (3.00)`,
		`SET search_path = app`,
	}
	for _, s := range ddl {
		if _, err := c.Exec(ctx, s); err != nil {
			return fmt.Errorf("seed %q: %w", s, err)
		}
	}
	return nil
}

// liveResolver backs pgsql.ColumnResolver with real information_schema — the
// same metadata shape the binding gate will feed after relation touch.
type liveResolver struct{ c *pgx.Conn }

func (r liveResolver) Columns(schema, name string) ([]string, error) {
	if schema == "" {
		schema = "app"
	}
	rows, err := r.c.Query(context.Background(),
		`SELECT column_name FROM information_schema.columns
		 WHERE table_schema=$1 AND table_name=$2 ORDER BY ordinal_position`, schema, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		cols = append(cols, s)
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: %s.%s", pgsql.ErrRelationNotFound, schema, name)
	}
	return cols, rows.Err()
}

type pgResult struct {
	names    []string
	oids     []uint32
	tableOID []uint32
	attNum   []uint16
	rows     [][]any
}

func runPG(t *testing.T, c *pgx.Conn, sql string) pgResult {
	t.Helper()
	rows, err := c.Query(context.Background(), sql)
	if err != nil {
		t.Fatalf("query failed:\n%s\nerror: %v", sql, err)
	}
	defer rows.Close()
	res := pgResult{}
	for _, fd := range rows.FieldDescriptions() {
		res.names = append(res.names, fd.Name)
		res.oids = append(res.oids, fd.DataTypeOID)
		res.tableOID = append(res.tableOID, fd.TableOID)
		res.attNum = append(res.attNum, fd.TableAttributeNumber)
	}
	for rows.Next() {
		v, err := rows.Values()
		if err != nil {
			t.Fatalf("scan %q: %v", sql, err)
		}
		res.rows = append(res.rows, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows %q: %v", sql, err)
	}
	return res
}

// reltypeOID returns the composite type OID of schema.name — the type a
// whole-row witness column must carry.
func reltypeOID(t *testing.T, c *pgx.Conn, schema, name string) uint32 {
	t.Helper()
	var oid uint32
	if err := c.QueryRow(context.Background(),
		`SELECT c.reltype FROM pg_class c
		 JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname=$1 AND c.relname=$2`, schema, name).Scan(&oid); err != nil {
		t.Fatalf("reltype %s.%s: %v", schema, name, err)
	}
	return oid
}

func cellKey(row []any) string {
	s := ""
	for _, v := range row {
		s += fmt.Sprintf("%#v|", v)
	}
	return s
}

// assertEquivalent rewrites orig, executes both against live PostgreSQL, and
// proves the public projection identical: column names, column count, row
// values, and (when ordered) row order. Witness columns must occupy exactly
// the trailing positions recorded in the rewrite result, carry the entity's
// reltype OID, and decode NULL.
func assertEquivalent(t *testing.T, c *pgx.Conn, orig string, ordered bool) *pgsql.RewriteResult {
	t.Helper()
	return assertEquivalentFD(t, c, orig, ordered, false)
}

// assertEquivalentFD additionally proves public-column source metadata
// (DataTypeOID, TableOID, TableAttributeNumber) survives the rewrite —
// set checkFD only where the projection stays direct (retained `*`/plain
// refs); derived/Q+D layers legitimately report TableOID 0.
func assertEquivalentFD(t *testing.T, c *pgx.Conn, orig string, ordered, checkFD bool) *pgsql.RewriteResult {
	t.Helper()
	rw, err := pgsql.Rewrite(orig, "app", liveResolver{c})
	if err != nil {
		t.Fatalf("Rewrite(%q): %v", orig, err)
	}
	before := runPG(t, c, orig)
	after := runPG(t, c, rw.SQL)

	nPub := len(rw.Public)
	if len(after.names) != nPub+len(rw.Witnesses) {
		t.Fatalf("rewritten width %d != public %d + witnesses %d\nsql: %s",
			len(after.names), nPub, len(rw.Witnesses), rw.SQL)
	}
	if !reflect.DeepEqual(after.names[:nPub], before.names) {
		t.Fatalf("public names differ: orig %v rewritten %v\nsql: %s",
			before.names, after.names[:nPub], rw.SQL)
	}
	if !reflect.DeepEqual(after.oids[:nPub], before.oids) {
		t.Fatalf("public column type OIDs differ: orig %v rewritten %v\nsql: %s",
			before.oids, after.oids[:nPub], rw.SQL)
	}
	if checkFD {
		if !reflect.DeepEqual(after.tableOID[:nPub], before.tableOID) ||
			!reflect.DeepEqual(after.attNum[:nPub], before.attNum) {
			t.Fatalf("public column source metadata changed: orig %v/%v rewritten %v/%v\nsql: %s",
				before.tableOID, before.attNum, after.tableOID[:nPub], after.attNum[:nPub], rw.SQL)
		}
	}
	for i, w := range rw.Witnesses {
		fd := nPub + i
		if after.names[fd] != w.Column {
			t.Fatalf("witness column %d name %q != record %q", fd, after.names[fd], w.Column)
		}
		want := reltypeOID(t, c, w.Entity.Schema, w.Entity.Name)
		if after.oids[fd] != want {
			t.Fatalf("witness %s OID %d != reltype(%s.%s) %d",
				w.Column, after.oids[fd], w.Entity.Schema, w.Entity.Name, want)
		}
		for _, row := range after.rows {
			if row[fd] != nil {
				t.Fatalf("witness %s not NULL: %v", w.Column, row[fd])
			}
		}
	}
	pub := make([][]any, len(after.rows))
	for i, row := range after.rows {
		pub[i] = row[:nPub]
	}
	if ordered {
		if !reflect.DeepEqual(before.rows, pub) {
			t.Fatalf("ordered rows differ\norig: %#v\nrewr: %#v\nsql: %s", before.rows, pub, rw.SQL)
		}
	} else {
		a := make([]string, len(before.rows))
		for i, r := range before.rows {
			a[i] = cellKey(r)
		}
		b := make([]string, len(pub))
		for i, r := range pub {
			b[i] = cellKey(r)
		}
		sort.Strings(a)
		sort.Strings(b)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("row multisets differ\norig: %v\nrewr: %v\nsql: %s", a, b, rw.SQL)
		}
	}
	return rw
}

func TestPGRewrite_BasicProjection(t *testing.T) {
	c := sharedPG(t)
	// Retained `*` — public FDs must keep TableOID/attnum, not just values.
	rw := assertEquivalentFD(t, c, `SELECT * FROM orders`, false, true)
	if len(rw.Witnesses) != 1 || rw.Witnesses[0].Entity.Name != "orders" {
		t.Fatalf("expected one orders witness, got %+v", rw.Witnesses)
	}
	rw = assertEquivalentFD(t, c, `SELECT o.id, o.amt FROM orders o ORDER BY o.id DESC`, true, true)
	if len(rw.Witnesses) != 1 {
		t.Fatalf("alias target must still witness orders: %+v", rw.Witnesses)
	}
	assertEquivalentFD(t, c, `SELECT * FROM orders WHERE false`, false, true)
}

func TestPGRewrite_JoinSemantics(t *testing.T) {
	c := sharedPG(t)
	// RIGHT JOIN: items id=4 unmatched — merged id must surface 4, not NULL.
	rw := assertEquivalentFD(t, c,
		`SELECT * FROM orders o RIGHT JOIN items i USING (id) ORDER BY id`, true, true)
	if len(rw.Witnesses) != 2 {
		t.Fatalf("RIGHT JOIN needs both leaf witnesses: %+v", rw.Witnesses)
	}
	assertEquivalentFD(t, c, `SELECT * FROM orders o FULL JOIN items i USING (id)`, false, true)
	assertEquivalentFD(t, c, `SELECT * FROM orders NATURAL JOIN items`, false, true)
	assertEquivalentFD(t, c, `SELECT * FROM orders o JOIN items i USING (id)`, false, true)
	assertEquivalentFD(t, c,
		`SELECT * FROM t NATURAL JOIN t2 ORDER BY a`, true, true)
	// Same value at different scales: RIGHT JOIN must project the RIGHT
	// side's representation (1.00), never COALESCE's left preference.
	assertEquivalentFD(t, c,
		`SELECT * FROM njl RIGHT JOIN njr USING (id) ORDER BY id`, true, true)
	// Forced expansion path (witness-carrying side): merged column still
	// follows PostgreSQL's per-type rule — the right Var verbatim.
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM njr) SELECT * FROM njl RIGHT JOIN r USING (id) ORDER BY id`, true, false)
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM njr) SELECT * FROM njl FULL JOIN r USING (id) ORDER BY id`, true, false)
	// GROUP BY over the merged column of a RIGHT JOIN — the generated
	// expression must stay groupable, i.e. be the right-side Var.
	assertEquivalent(t, c,
		`SELECT * FROM (SELECT id FROM orders) l RIGHT JOIN (SELECT id FROM items) r USING (id) GROUP BY id`, false)
	// Nested join: merged column of inner join feeds the outer ON.
	assertEquivalent(t, c,
		`SELECT * FROM (orders o JOIN items i USING (id)) JOIN t ON t.a = id`, false)
	// Join alias hides leaf aliases — wrapper must carry witnesses through.
	assertEquivalent(t, c,
		`SELECT * FROM (orders o JOIN items i USING (id)) j`, false)
	assertEquivalent(t, c,
		`SELECT j.id, j.oid FROM (orders o RIGHT JOIN items i USING (id)) j ORDER BY j.id`, true)
}

func TestPGRewrite_DistinctAndPagination(t *testing.T) {
	c := sharedPG(t)
	assertEquivalent(t, c,
		`SELECT DISTINCT amt FROM orders ORDER BY amt`, true)
	assertEquivalent(t, c,
		`SELECT DISTINCT o.id FROM orders o ORDER BY o.id`, true)
	assertEquivalent(t, c,
		`SELECT DISTINCT ON (id) id, amt FROM orders ORDER BY id, amt`, true)
	assertEquivalent(t, c,
		`SELECT id, amt FROM orders ORDER BY amt + 1, id LIMIT 2 OFFSET 1`, true)
	assertEquivalent(t, c,
		`SELECT id FROM orders ORDER BY id FETCH FIRST 2 ROWS ONLY`, true)

	// — #112 DISTINCT matrix —
	// DISTINCT over aggregates: the four shape states.
	assertEquivalent(t, c, `SELECT DISTINCT count(*) FROM orders`, false)
	assertEquivalent(t, c, `SELECT DISTINCT count(*) FROM orders GROUP BY amt`, false)
	assertEquivalent(t, c,
		`SELECT DISTINCT amt, count(*) FROM orders GROUP BY amt`, false)
	assertEquivalent(t, c,
		`SELECT DISTINCT amt, count(*) FROM orders GROUP BY amt HAVING count(*) > 0 ORDER BY amt`, true)
	// DISTINCT + window function in the same layer.
	assertEquivalent(t, c,
		`SELECT DISTINCT id, count(*) OVER () AS n FROM orders ORDER BY id`, true)
	// DISTINCT + GROUPING SETS / ROLLUP.
	assertEquivalent(t, c,
		`SELECT DISTINCT amt, count(*) FROM orders GROUP BY GROUPING SETS ((amt), ())`, false)
	assertEquivalent(t, c,
		`SELECT DISTINCT amt FROM orders GROUP BY ROLLUP (amt)`, false)
	// DISTINCT ON over an aggregate layer — stays unwrapped, still attested.
	assertEquivalent(t, c,
		`SELECT DISTINCT ON (amt) amt, count(*) FROM orders GROUP BY amt ORDER BY amt`, true)
	// Retained entity star + qualified sort key rebinding.
	assertEquivalent(t, c,
		`SELECT DISTINCT o.* FROM orders o ORDER BY o.id`, true)
	// Entity column-alias list under DISTINCT.
	assertEquivalent(t, c, `SELECT DISTINCT * FROM orders AS o(x,y)`, false)
	// Expression sort key through Q+D.
	assertEquivalent(t, c,
		`SELECT DISTINCT amt + 1 AS bumped FROM orders ORDER BY amt + 1`, true)
	// Non-comparable type: original and rewrite must fail identically.
	assertBothError(t, c,
		`SELECT DISTINCT v.j FROM (SELECT '{"a":1}'::json AS j) v`)

	// Zero-row shapes.
	assertEquivalent(t, c, `SELECT id FROM orders LIMIT 0`, false)
	assertEquivalent(t, c, `SELECT DISTINCT id FROM orders LIMIT 0`, false)

	// 30-row window across three pages — ORDER/LIMIT/OFFSET transfer once.
	for _, off := range []int{0, 10, 20} {
		assertEquivalentFD(t, c,
			fmt.Sprintf(`SELECT * FROM big ORDER BY seq LIMIT 10 OFFSET %d`, off), true, true)
	}
}

// assertBothError proves an invalid query fails identically on both sides —
// the rewrite must neither silently succeed nor fail in a different way.
func assertBothError(t *testing.T, c *pgx.Conn, orig string) {
	t.Helper()
	rw, err := pgsql.Rewrite(orig, "app", liveResolver{c})
	if err != nil {
		return // failing at rewrite time is a valid identical outcome
	}
	if _, err := c.Exec(context.Background(), orig); err == nil {
		t.Fatalf("original unexpectedly succeeded: %s", orig)
	}
	if _, err := c.Exec(context.Background(), rw.SQL); err == nil {
		t.Fatalf("rewrite succeeded where original fails: %s", rw.SQL)
	}
}

func TestPGRewrite_AggregateLayers(t *testing.T) {
	c := sharedPG(t)
	assertEquivalent(t, c, `SELECT count(*), sum(amt) FROM orders`, false)
	// ORDER-BY-only aggregate: layer must get the aggregate witness template —
	// an ordinary whole-row CASE witness would fail grouping at runtime.
	assertEquivalent(t, c, `SELECT 1 FROM orders ORDER BY count(*)`, false)
	// Window function is not a plain aggregate — ordinary witness template.
	assertEquivalent(t, c,
		`SELECT id, count(*) OVER (PARTITION BY amt % 2) FROM orders ORDER BY id`, true)
	// Aggregate inside a named window definition still marks this layer.
	assertEquivalent(t, c,
		`SELECT rank() OVER w FROM orders WINDOW w AS (ORDER BY count(*))`, false)
}

func TestPGRewrite_AliasAndNameInference(t *testing.T) {
	c := sharedPG(t)
	assertEquivalent(t, c,
		`SELECT d.x, d.y FROM (SELECT id, amt FROM orders) d(x, y)`, false)
	assertEquivalent(t, c,
		`SELECT v.n FROM (VALUES (1,'a'),(2,'b')) v(n, s) ORDER BY v.n`, true)
	// Cast output keeps the inner column name (id), not ?column?.
	assertEquivalent(t, c,
		`WITH c AS (SELECT id::text FROM orders) SELECT * FROM c`, false)
	assertEquivalent(t, c,
		`WITH c(a, b) AS (SELECT id, amt FROM orders) SELECT * FROM c ORDER BY a`, true)
	// Per-reference column alias lists — definition site, CTE use site,
	// entity use site, derived table, and a partial list.
	assertEquivalent(t, c,
		`WITH c AS (SELECT id, amt FROM orders) SELECT d.* FROM c AS d(x, y)`, false)
	assertEquivalent(t, c,
		`SELECT d.* FROM (SELECT id, amt FROM orders) AS d(x, y)`, false)
	assertEquivalentFD(t, c, `SELECT * FROM orders AS o(x, y)`, false, true)
	assertEquivalentFD(t, c, `SELECT * FROM orders AS o(x)`, false, true)
	// Non-entity CTE inside a suppressed sublink carries no witness to lose.
	assertEquivalent(t, c,
		`WITH c AS (SELECT 1 AS x) SELECT id FROM orders WHERE EXISTS (SELECT 1 FROM c)`, false)
	// A bare entity alias consumed as a whole-row value is ordinary data —
	// its own leaf witness still attests the relation.
	assertEquivalent(t, c, `SELECT row_to_json(o) FROM orders o`, false)
}

func TestPGRewrite_WhitespaceAndQuoting(t *testing.T) {
	c := sharedPG(t)
	// Leading whitespace must not corrupt byte-offset qualification.
	assertEquivalent(t, c, "  SELECT id FROM orders", false)
	assertEquivalent(t, c, "\n\tSELECT\n  o.id -- 注释: ok\nFROM orders o", false)
	assertEquivalent(t, c, `SELECT "id" FROM "orders"`, false)
}

func TestPGRewrite_Rejections(t *testing.T) {
	c := sharedPG(t)
	res := liveResolver{c}
	cases := []struct{ name, sql string }{
		// Explicitly qualified entity in a non-witnessable scalar sublink.
		{"qualified_sublink", `SELECT (SELECT id FROM app.items LIMIT 1) FROM app.orders`},
		// Same shape unqualified.
		{"unqualified_sublink", `SELECT (SELECT id FROM items LIMIT 1) FROM orders`},
		// Entity inside SubLink.testexpr.
		{"testexpr", `SELECT (SELECT id FROM items LIMIT 1) IN (SELECT 1) FROM orders`},
		// CTE entity consumed only through a suppressed EXISTS — no top witness.
		{"cte_suppressed", `WITH c AS (SELECT id FROM items) SELECT id FROM orders WHERE EXISTS (SELECT 1 FROM c)`},
		// Entity in EXISTS sublink.
		{"exists", `SELECT id FROM orders WHERE EXISTS (SELECT 1 FROM items)`},
		// Same entity entered twice through a UNION in opposite witness
		// order, consumed only via a suppressed EXISTS — merge must converge
		// without a cycle, then coverage must reject (bounded time).
		{"merge_cycle", `WITH a AS (SELECT id FROM orders), b AS (SELECT id FROM orders),
			c AS (SELECT a.id FROM a CROSS JOIN b UNION ALL SELECT b.id FROM b CROSS JOIN a)
			SELECT 1 WHERE EXISTS (SELECT 1 FROM c)`},
		// Whole-row composite of a witness-bearing derived source.
		{"whole_row", `SELECT j FROM (SELECT id FROM orders) j`},
		// Write / locking guards unchanged.
		{"for_update", `SELECT id FROM orders FOR UPDATE`},
		{"into", `SELECT id INTO tmp FROM orders`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := pgsql.Rewrite(tc.sql, "app", res); err == nil {
				t.Fatalf("expected rejection, rewrite succeeded: %s", tc.sql)
			}
		})
	}
}
