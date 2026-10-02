// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic SQL strings over a stub ColumnResolver
// output: intent tests for layout freezing, witness injection shape, cross-layer propagation, sublink suppression, DISTINCT Q+D, ordinals, reserved names, set-op merging, USING merge-source proofs (structured metadata, missing-capability and defect classification), reparseability
// pos: tests assert on the deparsed transport SQL structure because the contract is semantic — witnesses must be NULL-typed as the source reltype and never enter expansion/join/group/dedup keys
// note: if this file changes, update header and README.md
package pgsql

import (
	"context"
	"errors"
	"fmt"
	"maps"
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
	return nil, fmt.Errorf("%w: %s", ErrRelationNotFound, key)
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
	// A witness-free CTE (no entity inside) can be referenced from a
	// suppressed sublink — `*` inside freezes to its public columns only.
	r := rewriteOK(t,
		`WITH c AS (SELECT 1 AS x) `+
			`SELECT * FROM t WHERE EXISTS (SELECT * FROM c)`)
	reparseable(t, r)
	inner := r.SQL[strings.Index(r.SQL, "EXISTS"):]
	if strings.Contains(inner, "__chub_") {
		t.Fatalf("witness leaked into EXISTS sublink: %s", r.SQL)
	}
}

func TestInject_WitnessCoverageRequired(t *testing.T) {
	// An entity whose only reference lives under witness suppression can
	// never reach a top-level witness column — the binding would execute
	// unproven. Reject rather than silently attest nothing.
	cases := []string{
		// entity reachable only via a CTE referenced inside EXISTS
		`WITH c AS (SELECT id FROM orders) SELECT * FROM t WHERE EXISTS (SELECT * FROM c)`,
		// scalar sublink reading a witnessed CTE
		`WITH c AS (SELECT id FROM orders) SELECT (SELECT * FROM c) FROM t`,
		// explicit schema does not bypass the rule (qualified entity)
		`SELECT (SELECT id FROM app.items LIMIT 1) FROM app.orders`,
		// sublink hidden in IN's testexpr position
		`SELECT (SELECT id FROM items LIMIT 1) IN (SELECT 1) FROM orders`,
		// entity in a FROM-derived inside a suppressed sublink
		`SELECT * FROM t WHERE EXISTS (SELECT 1 FROM (SELECT id FROM orders) d)`,
	}
	for _, q := range cases {
		_, err := Rewrite(q, "app", testCols)
		re, ok := err.(*RejectError)
		if !ok || re.Code != "query_reference_not_witnessable" {
			t.Fatalf("Rewrite(%q) err = %v, want query_reference_not_witnessable", q, err)
		}
	}
	// A CTE referenced BOTH in a suppressed position and in the outer FROM
	// is still covered — the outer reference carries the witness.
	r := rewriteOK(t,
		`WITH c AS (SELECT id FROM orders) `+
			`SELECT * FROM c, t WHERE EXISTS (SELECT * FROM c)`)
	reparseable(t, r)
	if len(r.Witnesses) != 2 {
		t.Fatalf("expected witnesses for orders+t, got %+v", r.Witnesses)
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
	// `v.*` over a witness-free VALUES item stays verbatim — PostgreSQL
	// expands it to the positional colnames itself; the recorded public
	// layout must already carry them.
	r := rewriteOK(t, `SELECT v.* FROM (VALUES (1,2)) v(x,y)`)
	reparseable(t, r)
	if len(r.Public) != 2 || r.Public[0] != "x" || r.Public[1] != "y" {
		t.Fatalf("VALUES alias colnames not applied to public layout: %v (%s)", r.Public, r.SQL)
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

func TestInject_SetOpMergeNoCycle(t *testing.T) {
	// Two CTE references to the same entity enter a UNION in opposite
	// witness order — the merge must converge on one representative, not
	// create an A→B→A cycle the coverage check would chase forever.
	_, err := Rewrite(`WITH a AS (SELECT id FROM orders),
		b AS (SELECT id FROM orders),
		c AS (SELECT a.id FROM a CROSS JOIN b
		      UNION ALL SELECT b.id FROM b CROSS JOIN a)
		SELECT 1 WHERE EXISTS (SELECT 1 FROM c)`, "app", testCols)
	if err == nil {
		t.Fatal("expected coverage rejection — no witness reaches the output")
	}
	var re *RejectError
	if !errors.As(err, &re) || re.Code != "query_reference_not_witnessable" {
		t.Fatalf("expected query_reference_not_witnessable, got %v", err)
	}
}

func TestInject_CTEUseSiteColnames(t *testing.T) {
	// FROM c AS d(x,y) renames this reference's public columns — the layout
	// and any expansion must use x,y, not the body's original names.
	r := rewriteOK(t, `WITH c AS (SELECT id, amt FROM orders) SELECT d.* FROM c AS d(x,y)`)
	reparseable(t, r)
	if len(r.Public) != 2 || r.Public[0] != "x" || r.Public[1] != "y" {
		t.Fatalf("CTE use-site colnames not applied: %v (%s)", r.Public, r.SQL)
	}
}

func TestInject_EntityUseSiteColnames(t *testing.T) {
	// FROM orders AS o(x,y) — the relation's exposed columns are renamed for
	// this reference; DISTINCT wrapping must keep x,y, not id,amt.
	r := rewriteOK(t, `SELECT DISTINCT * FROM orders AS o(x,y)`)
	reparseable(t, r)
	if len(r.Public) != 2 || r.Public[0] != "x" || r.Public[1] != "y" {
		t.Fatalf("entity colnames not applied: %v (%s)", r.Public, r.SQL)
	}
}

func TestInject_DistinctStarQualifiedOrderBy(t *testing.T) {
	// o.* is retained verbatim; ORDER BY o.id must rebind to the position
	// its star expansion produced — not leak `o.` into the D layer.
	r := rewriteOK(t, `SELECT DISTINCT o.* FROM orders o ORDER BY o.id`)
	reparseable(t, r)
	if strings.Contains(r.SQL, "ORDER BY \"o\"") || strings.Contains(r.SQL, "ORDER BY o.") {
		t.Fatalf("inner alias leaked into D layer sort key: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, "ORDER BY 1") {
		t.Fatalf("qualified sort key not rebound to position: %s", r.SQL)
	}
	// Bare `*` must behave identically — the same output position.
	r = rewriteOK(t, `SELECT DISTINCT * FROM orders o ORDER BY o.id`)
	reparseable(t, r)
	if !strings.Contains(r.SQL, "ORDER BY 1") {
		t.Fatalf("bare-star qualified sort key not rebound: %s", r.SQL)
	}
	// Column-alias list: o.x addresses the first exposed column.
	r = rewriteOK(t, `SELECT DISTINCT * FROM orders o(x,y) ORDER BY o.x`)
	reparseable(t, r)
	if !strings.Contains(r.SQL, "ORDER BY 1") {
		t.Fatalf("aliased-column sort key not rebound: %s", r.SQL)
	}
}

type metaStub struct {
	stubResolver
	meta   map[string][]ColumnMetadata
	common map[[2]uint32]uint32
	err    error
}

const (
	fixtureInt4 = 1001
	fixtureInt8 = 1002
)

func (s metaStub) ColumnMetadata(schema, name string) ([]ColumnMetadata, error) {
	key := schema + "." + name
	if schema == "" {
		key = name
	}
	if m, ok := s.meta[key]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrRelationNotFound, key)
}

func (s metaStub) CommonType(left, right uint32) (uint32, error) {
	if s.err != nil {
		return 0, s.err
	}
	if left == right {
		return left, nil
	}
	if c, ok := s.common[[2]uint32{left, right}]; ok {
		return c, nil
	}
	return 0, fmt.Errorf("no common-type fixture for %d/%d", left, right)
}

func metaCol(name string, oid uint32, typmod int32, rel uint32, att int16) ColumnMetadata {
	return ColumnMetadata{
		Name:            name,
		Type:            ColumnType{OID: oid, Typmod: typmod},
		RelationOID:     rel,
		AttributeNumber: att,
	}
}

func joinFixtures() metaStub {
	return metaStub{
		stubResolver: stubResolver{
			"app.jl": {"id", "v"},
			"app.jr": {"id", "w"},
			"app.mi": {"id"},
			"app.mb": {"id"},
			"app.n1": {"id"},
			"app.n2": {"id"},
		},
		meta: map[string][]ColumnMetadata{
			"app.jl": {metaCol("id", fixtureInt4, -1, 101, 1), metaCol("v", fixtureInt4, -1, 101, 2)},
			"app.jr": {metaCol("id", fixtureInt4, -1, 102, 1), metaCol("w", fixtureInt4, -1, 102, 2)},
			"app.mi": {metaCol("id", fixtureInt4, -1, 103, 1)},
			"app.mb": {metaCol("id", fixtureInt8, -1, 104, 1)},
			"app.n1": {metaCol("id", fixtureInt4, 655366, 105, 1)},
			"app.n2": {metaCol("id", fixtureInt4, 655367, 106, 1)},
		},
		common: map[[2]uint32]uint32{
			{fixtureInt4, fixtureInt8}: fixtureInt8,
			{fixtureInt8, fixtureInt4}: fixtureInt8,
		},
	}
}

func TestInject_DistinctMergedLeafSortKey(t *testing.T) {
	res := joinFixtures()
	ok := []string{
		// Same-type merges: the picked side's Var IS the output item —
		// its qualifier stays legal (LEFT/INNER left, RIGHT right).
		`SELECT DISTINCT * FROM app.jl l LEFT JOIN app.jr r USING (id) ORDER BY l.id`,
		`SELECT DISTINCT * FROM app.jl l JOIN app.jr r USING (id) ORDER BY l.id`,
		`SELECT DISTINCT * FROM app.jl l RIGHT JOIN app.jr r USING (id) ORDER BY r.id`,
	}
	for _, sql := range ok {
		r, err := Rewrite(sql, "app", res)
		if err != nil {
			t.Fatalf("Rewrite(%q) = %v", sql, err)
		}
		reparseable(t, r)
		if strings.Contains(r.SQL, "ORDER BY l.") || strings.Contains(r.SQL, "ORDER BY r.") {
			t.Fatalf("leaf qualifier leaked into D layer: %s", r.SQL)
		}
	}
	bad := []string{
		// FULL never keeps a leaf Var as the merged output.
		`SELECT DISTINCT * FROM app.jl l FULL JOIN app.jr r USING (id) ORDER BY l.id`,
		// The unpicked side's qualifier was never the output item.
		`SELECT DISTINCT * FROM app.jl l LEFT JOIN app.jr r USING (id) ORDER BY r.id`,
		`SELECT DISTINCT * FROM app.jl l JOIN app.jr r USING (id) ORDER BY r.id`,
		// Different types: the picked side is coerced — the leaf Var no
		// longer matches the output item.
		`SELECT DISTINCT * FROM app.mi l LEFT JOIN app.mb r USING (id) ORDER BY l.id`,
		`SELECT DISTINCT * FROM app.mb l RIGHT JOIN app.mi r USING (id) ORDER BY r.id`,
		`SELECT DISTINCT * FROM app.n1 l LEFT JOIN app.n2 r USING (id) ORDER BY l.id`,
	}
	for _, sql := range bad {
		r, err := Rewrite(sql, "app", res)
		if err == nil {
			t.Fatalf("Rewrite(%q) should be rejected, got %s", sql, r.SQL)
		}
		var re *RejectError
		if !errors.As(err, &re) {
			t.Fatalf("Rewrite(%q): expected controlled rejection, got %v", sql, err)
		}
	}
}

func TestInject_MergedLeafProofMissingCapability(t *testing.T) {
	namesOnly := stubResolver{
		"app.jl": {"id", "v"},
		"app.jr": {"id", "w"},
	}
	_, err := Rewrite(
		`SELECT DISTINCT * FROM app.jl l LEFT JOIN app.jr r USING (id) ORDER BY l.id`,
		"app", namesOnly)
	if err == nil {
		t.Fatal("expected missing-evidence failure")
	}
	if !errors.Is(err, ErrTypeResolutionUnavailable) {
		t.Fatalf("expected ErrTypeResolutionUnavailable, got %v", err)
	}
	var re *RejectError
	if errors.As(err, &re) {
		t.Fatalf("missing type evidence must not become a rejection: %v", re.Code)
	}
	r, err := Rewrite(`SELECT v FROM app.jl`, "app", namesOnly)
	if err != nil {
		t.Fatalf("name-only query regressed: %v", err)
	}
	reparseable(t, r)
}

func TestInject_MergedLeafProofResolverErrors(t *testing.T) {
	bad := joinFixtures()
	bad.err = fmt.Errorf("%w: pg wire dropped", context.DeadlineExceeded)
	_, err := Rewrite(
		`SELECT DISTINCT * FROM app.mi l LEFT JOIN app.mb r USING (id) ORDER BY l.id`,
		"app", bad)
	if err == nil {
		t.Fatal("expected CommonType failure to propagate")
	}
	var re *RejectError
	if errors.As(err, &re) {
		t.Fatalf("backend failure misclassified as governed rejection: %v", re.Code)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CommonType cause lost: %v", err)
	}
}

type zeroCommonStub struct{ metaStub }

func (s zeroCommonStub) CommonType(_, _ uint32) (uint32, error) { return 0, nil }

func TestInject_CommonTypeZeroVerdict(t *testing.T) {
	_, err := Rewrite(
		`SELECT DISTINCT * FROM app.mi l LEFT JOIN app.mb r USING (id) ORDER BY l.id`,
		"app", zeroCommonStub{joinFixtures()})
	if err == nil {
		t.Fatal("expected provider zero-verdict to fail")
	}
	if !errors.Is(err, ErrTypeResolutionUnavailable) {
		t.Fatalf("expected ErrTypeResolutionUnavailable, got %v", err)
	}
	var re *RejectError
	if errors.As(err, &re) {
		t.Fatalf("resolver defect must not be a rejection: %v", re.Code)
	}
}

func TestInject_DuplicateAliasUndecidedMerge(t *testing.T) {
	namesOnly := stubResolver{
		"app.oj": {"id", "v"},
		"app.ij": {"id", "w"},
		"app.lj": {"id"},
	}
	_, err := Rewrite(
		`SELECT DISTINCT u.id AS k, v.id AS k FROM app.oj o LEFT JOIN app.ij i USING (id) AS u LEFT JOIN app.lj l USING (id) AS v ORDER BY k`,
		"app", namesOnly)
	if err == nil {
		t.Fatal("expected missing-evidence failure — no ordinal may be guessed from candidates")
	}
	if !errors.Is(err, ErrTypeResolutionUnavailable) {
		t.Fatalf("expected ErrTypeResolutionUnavailable, got %v", err)
	}
	var re *RejectError
	if errors.As(err, &re) {
		t.Fatalf("missing type evidence must not become a rejection: %v", re.Code)
	}
}

func TestInject_ColumnMetadataMismatch(t *testing.T) {
	base := joinFixtures()
	cases := map[string]map[string][]ColumnMetadata{
		"short": {"app.jl": {metaCol("id", fixtureInt4, -1, 101, 1)}},
		"renamed": {"app.jl": {
			metaCol("ID", fixtureInt4, -1, 101, 1), metaCol("v", fixtureInt4, -1, 101, 2)}},
		"zero_oid": {"app.jl": {
			metaCol("id", 0, -1, 101, 1), metaCol("v", fixtureInt4, -1, 101, 2)}},
		"zero_att": {"app.jl": {
			metaCol("id", fixtureInt4, -1, 101, 0), metaCol("v", fixtureInt4, -1, 101, 2)}},
	}
	for name, patch := range cases {
		res := base
		res.meta = maps.Clone(base.meta)
		for k, v := range patch {
			res.meta[k] = v
		}
		_, err := Rewrite(`SELECT * FROM app.jl`, "app", res)
		if err == nil {
			t.Fatalf("%s: expected metadata mismatch to fail", name)
		}
		var re *RejectError
		if errors.As(err, &re) {
			t.Fatalf("%s: resolver defect must not be a rejection: %v", name, re.Code)
		}
	}
}

func TestInject_NaturalJoinLateralExpansion(t *testing.T) {
	// Neither join side carries a witness, so the NATURAL join itself
	// needs no conversion — but the lateral CTE forces `*` expansion,
	// which references the generated join_using_alias. The alias is only
	// declarable if the NATURAL was frozen to an explicit USING.
	r := rewriteOK(t, `WITH c AS (SELECT a FROM app.t)
		SELECT * FROM app.orders o NATURAL JOIN app.items i CROSS JOIN c`)
	reparseable(t, r)
	if !strings.Contains(r.SQL, `USING`) {
		t.Fatalf("NATURAL not frozen to USING; generated alias undeclared: %s", r.SQL)
	}
	if !strings.Contains(r.SQL, `__chub_uj`) {
		t.Fatalf("merged column not routed through join_using_alias: %s", r.SQL)
	}
}

func TestInject_ResolverErrorNotPublic(t *testing.T) {
	// A resolver backend failure is an internal error, never a governed
	// RejectError — and its Unwrap chain must survive so the executor can
	// classify timeout/cancel/backend faults. Only ErrRelationNotFound maps
	// to query_object_not_found, with a fixed safe message.
	timeout := secretErrResolver{err: context.DeadlineExceeded}
	_, err := Rewrite(`SELECT * FROM ghost`, "app", timeout)
	if err == nil {
		t.Fatal("expected error")
	}
	var re *RejectError
	if errors.As(err, &re) {
		t.Fatalf("backend failure misclassified as governed rejection: %v", re.Code)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("resolver cause lost — timeout classification impossible: %v", err)
	}
	_, err = Rewrite(`SELECT * FROM ghost`, "app", stubResolver{})
	if !errors.As(err, &re) || re.Code != "query_object_not_found" {
		t.Fatalf("missing relation must map to query_object_not_found, got %v", err)
	}
	if strings.Contains(re.Message, "ghost") == false {
		t.Fatalf("not-found message should name the relation safely: %v", re.Message)
	}
	// A not-found wrapping sensitive diagnostics still produces the fixed
	// safe message — the marker must not appear in the public text.
	marked := secretErrResolver{err: fmt.Errorf("%w: dial 10.0.0.7 refused", ErrRelationNotFound)}
	_, err = Rewrite(`SELECT * FROM ghost`, "app", marked)
	if !errors.As(err, &re) || re.Code != "query_object_not_found" {
		t.Fatalf("wrapped not-found lost its mapping: %v", err)
	}
	if strings.Contains(re.Message, "10.0.0.7") || strings.Contains(re.Message, "dial") {
		t.Fatalf("resolver diagnostics leaked into public message: %v", re.Message)
	}
}

type secretErrResolver struct{ err error }

func (s secretErrResolver) Columns(schema, name string) ([]string, error) { return nil, s.err }
