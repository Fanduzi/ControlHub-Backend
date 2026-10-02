// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic SQL strings over a stub ColumnResolver
// output: intent tests for layout freezing, witness injection shape, cross-layer propagation, sublink suppression, DISTINCT Q+D, ordinals, reserved names, set-op merging, reparseability
// pos: tests assert on the deparsed transport SQL structure because the contract is semantic — witnesses must be NULL-typed as the source reltype and never enter expansion/join/group/dedup keys
// note: if this file changes, update header and README.md
package pgsql

import (
	"fmt"
	"strings"
	"testing"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// stubResolver answers Columns() from a fixed schema.name → cols map.
type stubResolver map[string][]string

func (s stubResolver) Columns(schema, name string) ([]string, error) {
	key := schema + "." + name
	if schema == "" {
		key = name
	}
	if c, ok := s[key]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("unknown relation %s", key)
}

var testCols = stubResolver{
	"app.orders": {"id", "amt"},
	"app.items":  {"id", "oid"},
	"app.t":      {"a", "b"},
	"app.t2":     {"a", "c"},
}

func rewriteOK(t *testing.T, sql string) *RewriteResult {
	t.Helper()
	r, err := Rewrite(sql, "app", testCols)
	if err != nil {
		t.Fatalf("Rewrite(%q) = %v", sql, err)
	}
	return r
}

// reparseable asserts the transport SQL compiles standalone.
func reparseable(t *testing.T, r *RewriteResult) {
	t.Helper()
	if _, err := pgquery.Parse(r.SQL); err != nil {
		t.Fatalf("rewritten SQL does not reparse: %v\n%s", err, r.SQL)
	}
}

func TestInject_OrdinaryLayerTemplate(t *testing.T) {
	r := rewriteOK(t, "SELECT id FROM orders")
	reparseable(t, r)
	if !strings.Contains(r.SQL, `CASE WHEN false THEN "orders".* END`) &&
		!strings.Contains(r.SQL, `CASE WHEN false THEN orders.* END`) {
		t.Fatalf("missing ordinary witness template: %s", r.SQL)
	}
	if len(r.Witnesses) != 1 || r.Witnesses[0].Entity.Name != "orders" {
		t.Fatalf("witnesses = %+v", r.Witnesses)
	}
}

func TestInject_AggregateLayerTemplate(t *testing.T) {
	r := rewriteOK(t, "SELECT count(*) FROM orders GROUP BY id")
	reparseable(t, r)
	if !strings.Contains(r.SQL, `array_agg`) || !strings.Contains(r.SQL, `FILTER (WHERE false)`) || !strings.Contains(r.SQL, `[1]`) {
		t.Fatalf("missing aggregate witness template: %s", r.SQL)
	}
	if strings.Contains(r.SQL, `CASE WHEN false`) {
		t.Fatalf("aggregate layer used ordinary template: %s", r.SQL)
	}
}

func TestInject_WindowOnlyLayerStaysOrdinary(t *testing.T) {
	// A window function must NOT make the layer aggregate — otherwise the
	// array_agg witness would collapse N rows into one.
	r := rewriteOK(t, "SELECT row_number() OVER (ORDER BY id) rn FROM orders")
	reparseable(t, r)
	if strings.Contains(r.SQL, `array_agg`) {
		t.Fatalf("window-only layer got aggregate witness: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, `CASE WHEN false`) {
		t.Fatalf("window-only layer lost ordinary witness: %s", r.SQL)
	}
	// count(*) OVER () is a window, not an aggregate — same rule.
	r = rewriteOK(t, "SELECT count(*) OVER () c FROM orders")
	if strings.Contains(r.SQL, `array_agg`) {
		t.Fatalf("window-agg misclassified as aggregate layer: %s", r.SQL)
	}
}

func TestInject_HavingIsAggregate(t *testing.T) {
	r := rewriteOK(t, "SELECT id FROM orders GROUP BY id HAVING count(*) > 0")
	reparseable(t, r)
	if !strings.Contains(r.SQL, `array_agg`) {
		t.Fatalf("HAVING layer missing aggregate template: %s", r.SQL)
	}
}

func TestInject_StarFreezing(t *testing.T) {
	// * over an entity keeps "alias".* — it cannot pull witnesses.
	r := rewriteOK(t, "SELECT * FROM orders")
	reparseable(t, r)
	if !strings.Contains(r.SQL, `orders.*`) {
		t.Fatalf("entity star not preserved as alias.*: %s", r.SQL)
	}
	// * over a witness-bearing derived table expands only public columns.
	r = rewriteOK(t, "SELECT * FROM (SELECT * FROM orders) d")
	reparseable(t, r)
	if strings.Contains(r.SQL, `d.*`) {
		t.Fatalf("derived star not frozen: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, `d.id`) || !strings.Contains(r.SQL, `d.amt`) {
		t.Fatalf("derived star did not expand public columns: %s", r.SQL)
	}
	// The witness must still propagate as a column, not via expansion.
	if !strings.Contains(r.SQL, `d.__chub_w1`) {
		t.Fatalf("witness not propagated from derived table: %s", r.SQL)
	}
}

func TestInject_AliasStarFreezing(t *testing.T) {
	r := rewriteOK(t, "SELECT d.* FROM (SELECT id, amt FROM orders) d")
	reparseable(t, r)
	if strings.Contains(r.SQL, `d.*`) {
		t.Fatalf("alias star over witnessed item not frozen: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, `d.id`) {
		t.Fatalf("public columns missing after freeze: %s", r.SQL)
	}
	// Entity alias.* stays verbatim — no injected columns inside a table.
	r = rewriteOK(t, "SELECT o.* FROM orders o")
	if !strings.Contains(r.SQL, `o.*`) {
		t.Fatalf("entity alias star unexpectedly frozen: %s", r.SQL)
	}
}

func TestInject_NaturalJoinFreezesOverWitnesses(t *testing.T) {
	// A witnessed CTE joined NATURAL must rewrite to USING over original
	// common columns — the witness name must never become a join key.
	r := rewriteOK(t, "WITH c AS (SELECT id, amt FROM orders) SELECT * FROM c NATURAL JOIN items")
	reparseable(t, r)
	// Reparse and inspect the join node itself: must be USING over the
	// original common column set only — never a witness name.
	tree, err := pgquery.Parse(r.SQL)
	if err != nil {
		t.Fatalf("reparse: %v", err)
	}
	var join *pg.JoinExpr
	walkTree(tree.ProtoReflect(), func(ctx *walkCtx, m protoreflect.Message) bool {
		if j, ok := m.Interface().(*pg.JoinExpr); ok {
			join = j
			return false
		}
		return true
	})
	if join == nil || join.GetIsNatural() || len(join.GetUsingClause()) == 0 {
		t.Fatalf("NATURAL not frozen to USING: %s", r.SQL)
	}
	for _, u := range join.GetUsingClause() {
		if strings.HasPrefix(u.GetString_().GetSval(), "__chub_") {
			t.Fatalf("witness leaked into USING: %s", r.SQL)
		}
	}
}

func TestInject_Ordinals(t *testing.T) {
	// In-range ordinals stay valid.
	r := rewriteOK(t, "SELECT id, amt FROM orders ORDER BY 1")
	reparseable(t, r)
	// Out-of-range ordinal must still error, not silently bind a witness.
	_, err := Rewrite("SELECT id FROM orders ORDER BY 9", "app", testCols)
	if err == nil {
		t.Fatalf("out-of-range ORDER BY ordinal accepted")
	}
	_, err = Rewrite("SELECT id FROM orders GROUP BY 7", "app", testCols)
	if err == nil {
		t.Fatalf("out-of-range GROUP BY ordinal accepted")
	}
}

func TestInject_CompositeConsumptionRejected(t *testing.T) {
	// Bare composite of a witness-bearing derived table → not witnessable.
	_, err := Rewrite("SELECT d FROM (SELECT * FROM orders) d", "app", testCols)
	re, ok := err.(*RejectError)
	if !ok || re.Code != "query_reference_not_witnessable" {
		t.Fatalf("err = %v, want query_reference_not_witnessable", err)
	}
	// Bare composite of a real entity is fine — reltype attested by witness.
	if _, err := Rewrite("SELECT orders FROM orders", "app", testCols); err != nil {
		t.Fatalf("entity composite wrongly rejected: %v", err)
	}
}

func TestInject_InternalNameCollision(t *testing.T) {
	for _, sql := range []string{
		`SELECT id AS "__chub_w1" FROM orders`,
		`WITH "__chub_evil" AS (SELECT 1) SELECT * FROM "__chub_evil"`,
		`SELECT * FROM orders AS "__chub_x"`,
	} {
		if _, err := Rewrite(sql, "app", testCols); err == nil {
			t.Fatalf("internal-name collision accepted: %s", sql)
		}
	}
}

func TestInject_CTEChainPropagation(t *testing.T) {
	r := rewriteOK(t, `WITH c1 AS (SELECT id FROM orders), c2 AS (SELECT id FROM c1) SELECT id FROM c2`)
	reparseable(t, r)
	if len(r.Witnesses) != 1 || r.Witnesses[0].Entity.Name != "orders" {
		t.Fatalf("witnesses = %+v", r.Witnesses)
	}
}

func TestInject_JoinTwoEntities(t *testing.T) {
	r := rewriteOK(t, "SELECT * FROM orders JOIN items ON orders.id = items.oid")
	reparseable(t, r)
	if len(r.Witnesses) != 2 {
		t.Fatalf("expected 2 witnesses, got %+v", r.Witnesses)
	}
	// Both join legs attested.
	names := map[string]bool{}
	for _, w := range r.Witnesses {
		names[w.Entity.Name] = true
	}
	if !names["orders"] || !names["items"] {
		t.Fatalf("missing entity witnesses: %+v", r.Witnesses)
	}
	// No witness name may appear inside the ON clause.
	if i := strings.Index(r.SQL, " ON "); i >= 0 && strings.Contains(r.SQL[i:], "__chub_") {
		t.Fatalf("witness leaked into join qual: %s", r.SQL[i:])
	}
}

func TestInject_DistinctQD(t *testing.T) {
	r := rewriteOK(t, "SELECT DISTINCT id FROM orders")
	reparseable(t, r)
	// D groups by the user column only; the witness rides via array_agg.
	if !strings.Contains(r.SQL, `GROUP BY`) || !strings.Contains(r.SQL, `__chub_u1`) {
		t.Fatalf("missing D-layer structure: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, `array_agg`) {
		t.Fatalf("D-layer witness not aggregate-passed: %s", r.SQL)
	}
	// DISTINCT must be gone from Q (dedup lives in D only).
	if strings.Contains(r.SQL, `DISTINCT`) {
		t.Fatalf("DISTINCT flag survived into transport: %s", r.SQL)
	}
	if r.Public[0] != "id" {
		t.Fatalf("public names = %v", r.Public)
	}
}

func TestInject_DistinctWithAggregate(t *testing.T) {
	r := rewriteOK(t, "SELECT DISTINCT id, count(*) FROM orders GROUP BY id HAVING count(*) > 0")
	reparseable(t, r)
	// Q keeps its own aggregate shape (array_agg witness inside Q).
	inner := r.SQL[strings.Index(r.SQL, "("):]
	if !strings.Contains(inner, `array_agg`) || !strings.Contains(inner, `GROUP BY`) {
		t.Fatalf("Q lost its aggregate shape: %s", r.SQL)
	}
}

func TestInject_DistinctOrderingRebind(t *testing.T) {
	// ORDER BY position and name both rebind to D's positions.
	for _, key := range []string{"1", "id", "amt"} {
		r := rewriteOK(t, "SELECT DISTINCT id, amt FROM orders ORDER BY "+key)
		reparseable(t, r)
	}
	r := rewriteOK(t, "SELECT DISTINCT id, amt FROM orders ORDER BY 2 DESC LIMIT 5 OFFSET 3")
	if !strings.Contains(r.SQL, `ORDER BY 2`) {
		t.Fatalf("ordinal key not rebound: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, `LIMIT 5`) || !strings.Contains(r.SQL, `OFFSET 3`) {
		t.Fatalf("pagination not transferred to D: %s", r.SQL)
	}
}

func TestInject_DistinctOnPreserved(t *testing.T) {
	r := rewriteOK(t, "SELECT DISTINCT ON (id) id, amt FROM orders ORDER BY id, amt DESC")
	reparseable(t, r)
	if !strings.Contains(r.SQL, `DISTINCT ON`) {
		t.Fatalf("DISTINCT ON converted to dedup layer: %s", r.SQL)
	}
}

func TestInject_DuplicateOutputNames(t *testing.T) {
	// Duplicated output names must survive the D-layer name restoration.
	r := rewriteOK(t, "SELECT DISTINCT id, id FROM orders")
	reparseable(t, r)
	if len(r.Public) != 2 || r.Public[0] != "id" || r.Public[1] != "id" {
		t.Fatalf("public names = %v", r.Public)
	}
}

func TestInject_OpaqueStarRejected(t *testing.T) {
	// `*` spanning a set-returning function is unfreezable — reject, don't guess.
	_, err := Rewrite("SELECT * FROM generate_series(1,3)", "app", testCols)
	if err == nil {
		t.Fatalf("* over opaque FROM item accepted")
	}
}

func TestInject_SetOperationSameEntity(t *testing.T) {
	// Two CTEs attesting the same entity may union — merged witness stays.
	r := rewriteOK(t,
		`WITH c1 AS (SELECT id FROM orders), c2 AS (SELECT id FROM orders) `+
			`SELECT id FROM (SELECT id FROM c1 UNION ALL SELECT id FROM c2) d`)
	reparseable(t, r)
	if len(r.Witnesses) != 1 {
		t.Fatalf("same-entity union should keep one witness, got %+v", r.Witnesses)
	}
	// Different entities merged into one union witness position → reject.
	_, err := Rewrite(
		`WITH c1 AS (SELECT id FROM orders), c2 AS (SELECT id FROM items) `+
			`SELECT id FROM (SELECT id FROM c1 UNION ALL SELECT id FROM c2) d`,
		"app", testCols)
	re, ok := err.(*RejectError)
	if !ok || re.Code != "query_reference_not_witnessable" {
		t.Fatalf("mixed-entity union should reject, err = %v", err)
	}
}

// ---------------------------------------------------------------------------
// Sublink suppression — a subquery inside an expression can never carry a
// witness outward, but `*` inside it must still freeze against the inner
// layout so injected columns never leak into its row width.

func TestInject_SublinkSuppressesWitnesses(t *testing.T) {
	r := rewriteOK(t,
		`WITH c AS (SELECT id FROM orders) `+
			`SELECT * FROM t WHERE EXISTS (SELECT * FROM c)`)
	reparseable(t, r)
	// The EXISTS body must freeze `*` to c's public column only — emitting a
	// witness there would change the sublink's row width (EXISTS ignores
	// values, but a scalar sublink's width is semantic).
	if strings.Contains(r.SQL, "EXISTS (SELECT") && strings.Contains(r.SQL[strings.Index(r.SQL, "EXISTS"):], "__chub_") {
		// witness inside the EXISTS body would appear after "EXISTS"
		inner := r.SQL[strings.Index(r.SQL, "EXISTS"):]
		if strings.Contains(inner, "__chub_") {
			t.Fatalf("witness leaked into EXISTS sublink: %s", r.SQL)
		}
	}
	// Scalar sublink over a single-column CTE keeps width 1 — `*` freezes to
	// the public column, not public+witness.
	r2 := rewriteOK(t,
		`WITH c AS (SELECT id FROM orders) SELECT (SELECT * FROM c) FROM t`)
	reparseable(t, r2)
	if !strings.Contains(r2.SQL, "(SELECT c.id FROM c)") {
		t.Fatalf("scalar sublink `*` not frozen to publics: %s", r2.SQL)
	}
}

func TestInject_CTEAliasColnamesRename(t *testing.T) {
	// WITH c(x) AS (...) renames the body's public output positionally —
	// references must use the alias name, not the body's internal name.
	r := rewriteOK(t, `WITH c(x) AS (SELECT id FROM orders) SELECT * FROM c`)
	reparseable(t, r)
	if !strings.Contains(r.SQL, "c.x") {
		t.Fatalf("CTE alias colnames not applied: %s", r.SQL)
	}
	if strings.Contains(r.SQL, "c.id") {
		t.Fatalf("body internal name leaked past alias colnames: %s", r.SQL)
	}
}

func TestInject_ValuesStarExpansion(t *testing.T) {
	// `v.*` over a VALUES alias-column item expands positionally.
	r := rewriteOK(t, `SELECT v.* FROM (VALUES (1,2)) v(x,y)`)
	reparseable(t, r)
	if !strings.Contains(r.SQL, "v.x") || !strings.Contains(r.SQL, "v.y") {
		t.Fatalf("VALUES alias `*` expansion wrong: %s", r.SQL)
	}
}

func TestInject_ReservedPrefixRejected(t *testing.T) {
	// User identifiers must not squat on the internal namespace — otherwise
	// generated names could collide with user columns.
	_, err := Rewrite(`SELECT a AS __chub_x FROM t`, "app", testCols)
	if err == nil {
		t.Fatalf("user alias with reserved prefix accepted")
	}
	_, err = Rewrite(`SELECT * FROM t AS __chub_evil`, "app", testCols)
	if err == nil {
		t.Fatalf("user table alias with reserved prefix accepted")
	}
}

func TestInject_OrdinalBounds(t *testing.T) {
	// ORDER BY 3 on a 2-public-column query: post-injection a third target
	// (the witness) exists — the original ordinal is out of range and must be
	// rejected, not silently allowed to address the witness.
	_, err := Rewrite(`SELECT id FROM orders ORDER BY 2`, "app", testCols)
	if err == nil {
		t.Fatalf("out-of-range ORDER BY ordinal accepted")
	}
	r := rewriteOK(t, `SELECT id, amt FROM orders ORDER BY 2`)
	reparseable(t, r)
	if !strings.Contains(r.SQL, "ORDER BY 2") {
		t.Fatalf("in-range ordinal not preserved: %s", r.SQL)
	}
}

func TestInject_RecursiveCTE(t *testing.T) {
	// A recursive CTE with no entity references rewrites cleanly — the
	// recursive self-reference is a CTE ref, not an entity.
	r := rewriteOK(t,
		`WITH RECURSIVE c AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM c WHERE n<3) `+
			`SELECT * FROM c`)
	reparseable(t, r)
	if len(r.Witnesses) != 0 {
		t.Fatalf("entity-free recursive CTE should emit no witnesses: %+v", r.Witnesses)
	}
	// An entity inside a recursive set-op leaf is unwitnessable — the anchor
	// leg sits inside the recursion and cannot attest outward.
	_, err := Rewrite(
		`WITH RECURSIVE c AS (SELECT id FROM orders UNION ALL SELECT id FROM c) SELECT * FROM c`,
		"app", testCols)
	if re, ok := err.(*RejectError); !ok || re.Code != "query_reference_not_witnessable" {
		t.Fatalf("entity in recursive CTE leaf should reject, err = %v", err)
	}
}

func TestInject_LimitFormsPreserved(t *testing.T) {
	// The rewriter never interprets limit forms — G6 validates them; T4 must
	// carry them through untouched (FETCH FIRST deparses to LIMIT per
	// pg_query normalization).
	for _, q := range []string{
		`SELECT * FROM t LIMIT ALL OFFSET 3`,
		`SELECT * FROM t FETCH FIRST 5 ROWS ONLY`,
		`SELECT * FROM t LIMIT 2+3`,
		`SELECT * FROM t LIMIT $1`,
		`SELECT * FROM t LIMIT 0`,
	} {
		r := rewriteOK(t, q)
		reparseable(t, r)
	}
}

func TestInject_Tablesample(t *testing.T) {
	// TABLESAMPLE wraps the relation but the row type is unchanged — the
	// witness still binds the leaf alias' reltype.
	r := rewriteOK(t, `SELECT * FROM t TABLESAMPLE SYSTEM (10)`)
	reparseable(t, r)
	if len(r.Witnesses) != 1 || r.Witnesses[0].Entity.Name != "t" {
		t.Fatalf("TABLESAMPLE witness wrong: %+v", r.Witnesses)
	}
}
