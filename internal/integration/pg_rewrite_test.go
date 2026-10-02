//go:build integration

// Package integration provides real-PostgreSQL rewrite-equivalence proofs.
// input: disposable PostgreSQL container, live pg_attribute resolver (ColumnMetadataResolver incl. SQL-backed CommonType), pgsql.Rewrite
// output: original-vs-rewritten result/ordering/pagination equivalence and witness reltype proofs
// pos: T4 acceptance — generated transport SQL executes correctly against real PostgreSQL
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		// 40 distinct values each duplicated — DISTINCT paging over dupes.
		`CREATE TABLE app.dup (seq int)`,
		`INSERT INTO app.dup SELECT g FROM generate_series(1,40) g UNION ALL SELECT g FROM generate_series(1,40) g`,
		// Same-valued USING keys at different numeric scales — the merged
		// column must surface the join-correct side's representation.
		`CREATE TABLE app.njl (id numeric)`,
		`CREATE TABLE app.njr (id numeric)`,
		`INSERT INTO app.njl VALUES (1.0), (2.0)`,
		`INSERT INTO app.njr VALUES (1.00), (3.00)`,
		// Mixed-type USING keys — merged column takes the common type.
		`CREATE TABLE app.l_int (id integer)`,
		`CREATE TABLE app.r_big (id bigint)`,
		`INSERT INTO app.l_int VALUES (1), (2)`,
		`INSERT INTO app.r_big VALUES (1), (3)`,
		// json payload is not comparable — DISTINCT on other cols must
		// still succeed while the entity stays witnessed.
		`CREATE TABLE app.jt (id int, payload json)`,
		`INSERT INTO app.jt VALUES (1,'{"a":1}'), (1,'{"b":2}'), (2,'{"c":3}')`,
		`SET search_path = app`,
	}
	for _, s := range ddl {
		if _, err := c.Exec(ctx, s); err != nil {
			return fmt.Errorf("seed %q: %w", s, err)
		}
	}
	return nil
}

// liveResolver backs pgsql.ColumnResolver with real pg_attribute reads — the
// same metadata shape the binding gate will feed after relation touch.
// atttypid preserves domain identity (udt_name would not), and atttypmod
// keeps the declared modifier so coerced merges stay provable.
type liveResolver struct{ c *pgx.Conn }

func (r liveResolver) Columns(schema, name string) ([]string, error) {
	metas, err := r.ColumnMetadata(schema, name)
	if err != nil {
		return nil, err
	}
	cols := make([]string, len(metas))
	for i, m := range metas {
		cols[i] = m.Name
	}
	return cols, nil
}

func (r liveResolver) ColumnMetadata(schema, name string) ([]pgsql.ColumnMetadata, error) {
	if schema == "" {
		schema = "app"
	}
	rows, err := r.c.Query(context.Background(),
		`SELECT a.attname, a.atttypid, a.atttypmod, a.attrelid, a.attnum
		 FROM pg_catalog.pg_attribute a
		 JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname=$1 AND c.relname=$2 AND a.attnum>0 AND NOT a.attisdropped
		 ORDER BY a.attnum`, schema, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var metas []pgsql.ColumnMetadata
	for rows.Next() {
		var m pgsql.ColumnMetadata
		if err := rows.Scan(&m.Name, &m.Type.OID, &m.Type.Typmod,
			&m.RelationOID, &m.AttributeNumber); err != nil {
			return nil, err
		}
		metas = append(metas, m)
	}
	if len(metas) == 0 {
		return nil, fmt.Errorf("%w: %s.%s", pgsql.ErrRelationNotFound, schema, name)
	}
	return metas, rows.Err()
}

func (r liveResolver) CommonType(leftOID, rightOID uint32) (uint32, error) {
	var lt, rt string
	if err := r.c.QueryRow(context.Background(),
		`SELECT pg_catalog.format_type($1::pg_catalog.oid, NULL), pg_catalog.format_type($2::pg_catalog.oid, NULL)`,
		leftOID, rightOID).Scan(&lt, &rt); err != nil {
		return 0, err
	}
	var oid uint32
	if err := r.c.QueryRow(context.Background(), fmt.Sprintf(
		`SELECT pg_catalog.pg_typeof((SELECT id FROM (SELECT NULL::%s AS id) AS l INNER JOIN (SELECT NULL::%s AS id) AS r USING (id) LIMIT 0))::pg_catalog.oid`,
		lt, rt)).Scan(&oid); err != nil {
		return 0, err
	}
	return oid, nil
}

type pgResult struct {
	names    []string
	oids     []uint32
	typmods  []int32
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
		res.typmods = append(res.typmods, fd.TypeModifier)
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
	if !reflect.DeepEqual(after.typmods[:nPub], before.typmods) {
		t.Fatalf("public column type modifiers differ: orig %v rewritten %v\nsql: %s",
			before.typmods, after.typmods[:nPub], rw.SQL)
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
	// NATURAL whose own sides carry no witness still gets expanded when a
	// LATERAL item does: the generated join_using_alias is only declarable
	// if the NATURAL froze to USING. Also nested deeper in the join tree.
	assertEquivalentFD(t, c,
		`WITH c AS (SELECT a FROM t) SELECT * FROM orders o NATURAL JOIN items i CROSS JOIN c`, false, true)
	assertEquivalentFD(t, c,
		`WITH c AS (SELECT a FROM t) SELECT * FROM (orders o NATURAL JOIN items i) JOIN t2 ON t2.a = oid CROSS JOIN c`, false, false)
	// Same value at different scales: RIGHT JOIN must project the RIGHT
	// side's representation (1.00), never COALESCE's left preference.
	assertEquivalentFD(t, c,
		`SELECT * FROM njl RIGHT JOIN njr USING (id) ORDER BY id`, true, true)
	// Forced expansion path (witness-carrying side): merged column must
	// still be PostgreSQL's own merged var — routed through the generated
	// join_using_alias — including its common-type coercion: integer+bigint
	// merge must keep bigint, not whichever side happened to be picked.
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM r_big) SELECT * FROM l_int l LEFT JOIN r USING (id) ORDER BY id`, true, false)
	assertEquivalentFD(t, c,
		`WITH l AS (SELECT id FROM l_int) SELECT * FROM l RIGHT JOIN r_big USING (id) ORDER BY id`, true, false)
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM r_big) SELECT * FROM l_int l JOIN r USING (id) ORDER BY id`, true, false)
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM njr) SELECT * FROM njl RIGHT JOIN r USING (id) ORDER BY id`, true, false)
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM njr) SELECT * FROM njl FULL JOIN r USING (id) ORDER BY id`, true, false)
	// A user's own join_using_alias is reused, not shadowed.
	assertEquivalentFD(t, c,
		`WITH r AS (SELECT id FROM r_big) SELECT * FROM l_int l LEFT JOIN r USING (id) AS u ORDER BY u.id`, true, false)
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
	// HAVING false side and empty inputs — aggregate shape must not
	// manufacture a global-aggregate row where the original yields none,
	// and a global aggregate over empty input still yields its single row.
	assertEquivalent(t, c,
		`SELECT DISTINCT amt, count(*) FROM orders GROUP BY amt HAVING count(*) > 100`, false)
	assertEquivalent(t, c, `SELECT DISTINCT amt FROM orders WHERE false`, false)
	assertEquivalent(t, c, `SELECT DISTINCT count(*) FROM orders WHERE false`, false)
	assertEquivalent(t, c,
		`SELECT count(*) FROM orders WHERE false`, false)
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
	// Retained stars + qualified sort keys: every spelling must rebind.
	assertEquivalent(t, c,
		`SELECT DISTINCT * FROM orders o ORDER BY o.id`, true)
	assertEquivalent(t, c,
		`SELECT DISTINCT o.* FROM orders o ORDER BY o.id`, true)
	assertEquivalent(t, c,
		`SELECT DISTINCT * FROM orders o(x,y) ORDER BY o.x`, true)
	// Merged-column sort provenance (bare `*` over USING joins): the
	// picked side's leaf qualifier remains a legal reference when both
	// sides carry equal types — the merged output IS that Var verbatim.
	assertEquivalent(t, c,
		`SELECT DISTINCT * FROM orders o LEFT JOIN items i USING (id) ORDER BY o.id`, true)
	assertEquivalent(t, c,
		`SELECT DISTINCT * FROM orders o RIGHT JOIN items i USING (id) ORDER BY i.id`, true)
	assertEquivalent(t, c,
		`SELECT DISTINCT * FROM orders o JOIN items i USING (id) ORDER BY o.id`, true)
	// And the exact reverse shapes must fail the way the original does:
	// FULL keeps no leaf Var, the unpicked side never was the output
	// item, and differing types wrap the picked Var in a coercion that
	// no longer matches a plain leaf reference.
	assertBothError(t, c,
		`SELECT DISTINCT * FROM orders o FULL JOIN items i USING (id) ORDER BY o.id`)
	assertBothError(t, c,
		`SELECT DISTINCT * FROM orders o LEFT JOIN items i USING (id) ORDER BY i.id`)
	assertBothError(t, c,
		`SELECT DISTINCT * FROM l_int l LEFT JOIN r_big r USING (id) ORDER BY l.id`)
	// Entity column-alias list under DISTINCT.
	assertEquivalent(t, c, `SELECT DISTINCT * FROM orders AS o(x,y)`, false)
	// Expression sort key through Q+D.
	assertEquivalent(t, c,
		`SELECT DISTINCT amt + 1 AS bumped FROM orders ORDER BY amt + 1`, true)
	// Entity with a non-comparable (json) payload column — DISTINCT on a
	// comparable column succeeds and the witness keeps jt's reltype.
	rw := assertEquivalent(t, c, `SELECT DISTINCT id FROM jt ORDER BY id`, true)
	if len(rw.Witnesses) != 1 || rw.Witnesses[0].Entity.Name != "jt" {
		t.Fatalf("jt witness missing: %+v", rw.Witnesses)
	}
	// Dedup over the json column itself fails identically on both sides.
	assertBothError(t, c, `SELECT DISTINCT payload FROM jt`)

	// Zero-row shapes.
	assertEquivalent(t, c, `SELECT id FROM orders LIMIT 0`, false)
	assertEquivalent(t, c, `SELECT DISTINCT id FROM orders LIMIT 0`, false)

	// 30-row window across three pages — ORDER/LIMIT/OFFSET transfer once.
	for _, off := range []int{0, 10, 20} {
		assertEquivalentFD(t, c,
			fmt.Sprintf(`SELECT * FROM big ORDER BY seq LIMIT 10 OFFSET %d`, off), true, true)
	}
	assertDistinctPageWindows(t, c)
}

// assertDistinctPageWindows drives the spec's two-page G6 window over a
// DISTINCT result: original `LIMIT 30 OFFSET 10` gives rows 11..40 of the
// deduped sequence; the rewritten pages are the precomputed G6 windows
// `LIMIT 26 OFFSET 10` (25 rows + sentinel) and `LIMIT 5 OFFSET 35`.
func assertDistinctPageWindows(t *testing.T, c *pgx.Conn) {
	t.Helper()
	orig := `SELECT DISTINCT seq FROM dup ORDER BY seq LIMIT 30 OFFSET 10`
	full := runPG(t, c, orig)
	if len(full.rows) != 30 {
		t.Fatalf("fixture drift: expected 30-window, got %d", len(full.rows))
	}
	page1 := runRewritePublic(t, c,
		`SELECT DISTINCT seq FROM dup ORDER BY seq LIMIT 26 OFFSET 10`)
	page2 := runRewritePublic(t, c,
		`SELECT DISTINCT seq FROM dup ORDER BY seq LIMIT 5 OFFSET 35`)
	// Page 1 = first 25 window rows; row 26 is the has-more sentinel and
	// must equal the original window's 26th row without entering the page.
	if len(page1) != 26 {
		t.Fatalf("page1 expected 25+sentinel rows, got %d", len(page1))
	}
	for i := 0; i < 25; i++ {
		if !reflect.DeepEqual(page1[i], full.rows[i]) {
			t.Fatalf("page1 row %d: %v != %v", i, page1[i], full.rows[i])
		}
	}
	if !reflect.DeepEqual(page1[25], full.rows[25]) {
		t.Fatalf("sentinel row mismatch: %v != %v", page1[25], full.rows[25])
	}
	// Page 2 resumes after the sentinel — exactly the original tail.
	if len(page2) != 5 {
		t.Fatalf("page2 expected 5 rows, got %d", len(page2))
	}
	for i := range page2 {
		if !reflect.DeepEqual(page2[i], full.rows[25+i]) {
			t.Fatalf("page2 row %d: %v != %v", i, page2[i], full.rows[25+i])
		}
	}
}

// runRewritePublic executes the rewritten SQL and returns public columns
// only — witness columns stripped by the recorded positions.
func runRewritePublic(t *testing.T, c *pgx.Conn, sql string) [][]any {
	t.Helper()
	rw, err := pgsql.Rewrite(sql, "app", liveResolver{c})
	if err != nil {
		t.Fatalf("Rewrite(%q): %v", sql, err)
	}
	res := runPG(t, c, rw.SQL)
	nPub := len(rw.Public)
	out := make([][]any, len(res.rows))
	for i, row := range res.rows {
		out[i] = row[:nPub]
	}
	return out
}

// assertBothError proves an invalid query fails identically: the ORIGINAL
// must fail first; then the rewrite must either be rejected up front or
// fail with the same SQLSTATE at execution.
func assertBothError(t *testing.T, c *pgx.Conn, orig string) {
	t.Helper()
	var oerr *pgconn.PgError
	if _, err := c.Exec(context.Background(), orig); err == nil {
		t.Fatalf("invalid-fixture query unexpectedly succeeded: %s", orig)
	} else if !errors.As(err, &oerr) {
		t.Fatalf("original error is not a PgError: %v", err)
	}
	rw, err := pgsql.Rewrite(orig, "app", liveResolver{c})
	if err != nil {
		return // rejected at rewrite time — a valid identical outcome
	}
	if _, err := c.Exec(context.Background(), rw.SQL); err == nil {
		t.Fatalf("rewrite succeeded where original fails: %s", rw.SQL)
	} else {
		var rerr *pgconn.PgError
		if !errors.As(err, &rerr) {
			t.Fatalf("rewrite error is not a PgError: %v", err)
		}
		if rerr.Code != oerr.Code {
			t.Fatalf("error class differs: orig %s rewritten %s\nsql: %s", oerr.Code, rerr.Code, rw.SQL)
		}
	}
}

func assertSortRejected(t *testing.T, c *pgx.Conn, orig string) {
	t.Helper()
	assertCodeMirrored(t, c, orig, "42P10")
}

func assertCodeMirrored(t *testing.T, c *pgx.Conn, orig, wantCode string) {
	t.Helper()
	var oerr *pgconn.PgError
	if _, err := c.Exec(context.Background(), orig); err == nil {
		t.Fatalf("fixture expected %s, query succeeded: %s", wantCode, orig)
	} else if !errors.As(err, &oerr) || oerr.Code != wantCode {
		t.Fatalf("original must fail with %s, got %v", wantCode, err)
	}
	rw, err := pgsql.Rewrite(orig, "app", liveResolver{c})
	if err != nil {
		var re *pgsql.RejectError
		if !errors.As(err, &re) || re.Code != "query_not_allowed" {
			t.Fatalf("rewrite must reject query_not_allowed or fail %s, got %v", wantCode, err)
		}
		return
	}
	if _, err := c.Exec(context.Background(), rw.SQL); err == nil {
		t.Fatalf("rewrite succeeded where original fails %s: %s", wantCode, rw.SQL)
	} else {
		var rerr *pgconn.PgError
		if !errors.As(err, &rerr) || rerr.Code != wantCode {
			t.Fatalf("rewrite error must be %s, got %v\nsql: %s", wantCode, err, rw.SQL)
		}
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

func TestPGRewrite_JoinReview5_NaturalNoCommon(t *testing.T) {
	c := sharedPG(t)
	const lLive, rLive = `c AS l(x)`, `c AS r(y)`
	const lDead, rDead = `(SELECT id FROM c WHERE false) AS l(x)`,
		`(SELECT id FROM c WHERE false) AS r(y)`
	cases := []struct {
		name              string
		left, right, join string
		want              int
	}{
		{"inner", lLive, rLive, `NATURAL JOIN`, 9},
		{"left", lLive, rLive, `NATURAL LEFT JOIN`, 9},
		{"right", lLive, rLive, `NATURAL RIGHT JOIN`, 9},
		{"full", lLive, rLive, `NATURAL FULL JOIN`, 9},
		{"inner_right_empty", lLive, rDead, `NATURAL JOIN`, 0},
		{"left_right_empty", lLive, rDead, `NATURAL LEFT JOIN`, 3},
		{"right_right_empty", lLive, rDead, `NATURAL RIGHT JOIN`, 0},
		{"full_right_empty", lLive, rDead, `NATURAL FULL JOIN`, 3},
		{"inner_left_empty", lDead, rLive, `NATURAL JOIN`, 0},
		{"left_left_empty", lDead, rLive, `NATURAL LEFT JOIN`, 0},
		{"right_left_empty", lDead, rLive, `NATURAL RIGHT JOIN`, 3},
		{"full_left_empty", lDead, rLive, `NATURAL FULL JOIN`, 3},
		{"inner_both_empty", lDead, rDead, `NATURAL JOIN`, 0},
		{"left_both_empty", lDead, rDead, `NATURAL LEFT JOIN`, 0},
		{"right_both_empty", lDead, rDead, `NATURAL RIGHT JOIN`, 0},
		{"full_both_empty", lDead, rDead, `NATURAL FULL JOIN`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql := `WITH c AS (SELECT id FROM app.orders) SELECT l.x, r.y FROM ` +
				tc.left + ` ` + tc.join + ` ` + tc.right
			orig := runPG(t, c, sql)
			if len(orig.rows) != tc.want {
				t.Fatalf("expected %d rows, got %d\nsql: %s", tc.want, len(orig.rows), sql)
			}
			assertEquivalent(t, c, sql, false)
		})
	}
}

func TestPGRewrite_JoinReview5_SortProvenance(t *testing.T) {
	c := sharedPG(t)
	t.Run("picked_leaf_already_common_type", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.r_big AS l LEFT JOIN app.l_int AS r USING (id) ORDER BY l.id`, true)
	})
	t.Run("cte_join_side", func(t *testing.T) {
		assertEquivalent(t, c,
			`WITH r AS (SELECT id FROM app.items) SELECT DISTINCT * FROM app.orders AS o LEFT JOIN r USING (id) ORDER BY o.id`, true)
	})
	t.Run("column_alias_list_using", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.orders AS o(x,y) LEFT JOIN app.items AS i(x,z) USING (x) ORDER BY o.x`, true)
	})
	t.Run("inner_coerced_keeps_right", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.l_int l JOIN app.r_big r USING (id) ORDER BY r.id`, true)
	})
	t.Run("right_join_bigint_preserved", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.l_int l RIGHT JOIN app.r_big r USING (id) ORDER BY r.id`, true)
	})
	t.Run("derived_join_side", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.orders o LEFT JOIN (SELECT id FROM app.items) r USING (id) ORDER BY o.id`, true)
	})
	t.Run("cte_definition_alias_list", func(t *testing.T) {
		assertEquivalent(t, c,
			`WITH r(x) AS (SELECT id FROM app.items) SELECT DISTINCT * FROM app.orders AS o(x,y) LEFT JOIN r USING (x) ORDER BY o.x`, true)
	})
	t.Run("partial_entity_alias_lists", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.orders AS o(x) LEFT JOIN app.items AS i(x) USING (x) ORDER BY o.x`, true)
	})
	t.Run("repeated_cte_references", func(t *testing.T) {
		assertEquivalent(t, c,
			`WITH r AS (SELECT id FROM app.items) SELECT DISTINCT * FROM r AS a LEFT JOIN r AS b USING (id) ORDER BY a.id`, true)
	})
	t.Run("forced_expansion_cte_side", func(t *testing.T) {
		assertEquivalent(t, c,
			`WITH r AS (SELECT id FROM app.items) SELECT DISTINCT * FROM app.orders o LEFT JOIN r USING (id) ORDER BY o.id`, true)
	})
	t.Run("explicit_merged_projection", func(t *testing.T) {
		assertEquivalent(t, c,
			`WITH r AS (SELECT id FROM app.items) SELECT DISTINCT id, o.amt FROM app.orders o LEFT JOIN r USING (id) ORDER BY o.id`, true)
	})
	t.Run("user_using_alias_key", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.orders o LEFT JOIN app.items i USING (id) AS u ORDER BY u.id`, true)
	})
	t.Run("negative_typmod", func(t *testing.T) {
		for _, s := range []string{
			`CREATE TABLE IF NOT EXISTS app.numeric_scale1 (id numeric(10,1))`,
			`CREATE TABLE IF NOT EXISTS app.numeric_scale2 (id numeric(10,2))`,
			`INSERT INTO app.numeric_scale1 VALUES (1.0)`,
			`INSERT INTO app.numeric_scale2 VALUES (1.00)`,
		} {
			if _, err := c.Exec(context.Background(), s); err != nil {
				t.Fatalf("typmod fixture %q: %v", s, err)
			}
		}
		assertSortRejected(t, c,
			`SELECT DISTINCT * FROM app.numeric_scale1 AS l LEFT JOIN app.numeric_scale2 AS r USING (id) ORDER BY l.id`)
	})
	t.Run("negative_merge_keys", func(t *testing.T) {
		for _, sql := range []string{
			`SELECT DISTINCT * FROM app.orders o FULL JOIN app.items i USING (id) ORDER BY o.id`,
			`SELECT DISTINCT * FROM app.orders o LEFT JOIN app.items i USING (id) ORDER BY i.id`,
			`SELECT DISTINCT * FROM app.l_int l LEFT JOIN app.r_big r USING (id) ORDER BY l.id`,
			`SELECT DISTINCT * FROM app.l_int l JOIN app.r_big r USING (id) ORDER BY l.id`,
		} {
			assertSortRejected(t, c, sql)
		}
	})
	t.Run("domain_evidence", func(t *testing.T) {
		for _, s := range []string{
			`CREATE DOMAIN app.merge_domain1 AS integer`,
			`CREATE DOMAIN app.merge_domain2 AS integer`,
			`CREATE TABLE app.domain1_key (id app.merge_domain1)`,
			`CREATE TABLE app.domain1_key_other (id app.merge_domain1)`,
			`CREATE TABLE app.domain2_key (id app.merge_domain2)`,
			`INSERT INTO app.domain1_key VALUES (1)`,
			`INSERT INTO app.domain1_key_other VALUES (1)`,
			`INSERT INTO app.domain2_key VALUES (1)`,
		} {
			if _, err := c.Exec(context.Background(), s); err != nil {
				t.Fatalf("domain fixture %q: %v", s, err)
			}
		}
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.domain1_key l LEFT JOIN app.domain1_key_other r USING (id) ORDER BY l.id`, true)
		assertSortRejected(t, c,
			`SELECT DISTINCT * FROM app.domain1_key l LEFT JOIN app.domain2_key r USING (id) ORDER BY l.id`)
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.l_int l LEFT JOIN app.domain1_key r USING (id) ORDER BY l.id`, true)
	})
}

func TestPGRewrite_JoinReview5_ScopeRebind(t *testing.T) {
	c := sharedPG(t)

	t.Run("output_name_shadows_input", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT id AS original_id, -amt AS id FROM app.orders ORDER BY id`, true)
	})
	t.Run("duplicate_output_name_identical", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT id AS k, id AS k FROM app.orders ORDER BY k`, true)
	})
	t.Run("duplicate_output_name_ambiguous", func(t *testing.T) {
		assertCodeMirrored(t, c,
			`SELECT DISTINCT id AS k, -amt AS k FROM app.orders ORDER BY k`, "42702")
	})
	t.Run("bare_key_top_level_only", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT id FROM (app.orders o FULL JOIN app.items i USING (id)) RIGHT JOIN app.l_int l USING (id) ORDER BY l.id`, true)
	})
	t.Run("bare_key_ambiguous_siblings", func(t *testing.T) {
		assertCodeMirrored(t, c,
			`SELECT DISTINCT * FROM app.orders o LEFT JOIN app.items i USING (id), app.l_int l ORDER BY id`, "42702")
	})
	t.Run("key_join_alias_vs_leaf_output", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT o.id AS out_id FROM app.orders o LEFT JOIN app.items i USING (id) AS u ORDER BY u.id`, true)
	})
	t.Run("key_bare_merged_vs_leaf_output", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT o.id AS out_id FROM app.orders o LEFT JOIN app.items i USING (id) ORDER BY id`, true)
	})
	t.Run("key_join_alias_full_merge", func(t *testing.T) {
		assertSortRejected(t, c,
			`SELECT DISTINCT o.id AS out_id FROM app.orders o FULL JOIN app.items i USING (id) AS u ORDER BY u.id`)
	})
	t.Run("duplicate_alias_same_underlying_var", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT u.id AS k, v.id AS k FROM app.orders o LEFT JOIN app.items i USING (id) AS u LEFT JOIN app.l_int l USING (id) AS v ORDER BY k`, true)
	})
	t.Run("qualified_key_ambiguous_in_item", func(t *testing.T) {
		assertCodeMirrored(t, c,
			`SELECT DISTINCT o.* FROM app.orders AS o(x,x) ORDER BY o.x`, "42702")
	})
	t.Run("dotted_alias_not_schema_path", func(t *testing.T) {
		assertEquivalent(t, c,
			`SELECT DISTINCT * FROM app.orders AS "app.items" CROSS JOIN app.items ORDER BY "app.items".id, app.items.id`, true)
	})
	t.Run("alias_hides_schema_path", func(t *testing.T) {
		assertCodeMirrored(t, c,
			`SELECT DISTINCT orders.* FROM app.orders AS orders ORDER BY app.orders.id`, "42P01")
	})
}
