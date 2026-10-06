// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic SQL strings over metadata-capable stub resolvers
// output: intent tests for RefID/RefPath mapping, public-column proof records, source occurrence catalogs, witness covered-source/carrier propagation, VALUES expression dependencies, named-window clause resolution, and correlated-subquery scope handling
// pos: T7-S1 verification-record tests; asserts rewrite-time evidence coordinates without claiming live PostgreSQL execution semantics
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"strings"
	"testing"

	pg "github.com/pganalyze/pg_query_go/v6"
)

func proofFixtures() metaStub {
	return metaStub{
		stubResolver: stubResolver{
			"app.orders": {"id", "amt"},
			"app.items":  {"id", "oid"},
		},
		meta: map[string][]ColumnMetadata{
			"app.orders": {
				metaCol("id", fixtureInt4, -1, 401, 1),
				metaCol("amt", fixtureInt4, -1, 401, 2),
			},
			"app.items": {
				metaCol("id", fixtureInt4, -1, 402, 1),
				metaCol("oid", fixtureInt4, -1, 402, 2),
			},
		},
		common: map[[2]uint32]uint32{},
	}
}

func requireProof(t *testing.T, got []PublicColumnProof, idx int) PublicColumnProof {
	t.Helper()
	if idx >= len(got) {
		t.Fatalf("missing public proof %d in %+v", idx, got)
	}
	return got[idx]
}

func requireDep(t *testing.T, deps []SourceRef, occurrence SourceOccurrenceID, ordinal int) {
	t.Helper()
	want := SourceRef{Occurrence: occurrence, Slot: columnSlot(ordinal)}
	if !sourceRefIn(deps, want) {
		t.Fatalf("dependencies %+v missing %+v", deps, want)
	}
}

func TestPublicProofs_DirectExpressionConstantAndUnresolved(t *testing.T) {
	res := proofFixtures()

	r, err := Rewrite(`SELECT id FROM orders`, "app", res)
	if err != nil {
		t.Fatalf("direct rewrite: %v", err)
	}
	proof := requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || proof.FDOrigin.Kind != FDOriginKnownColumn {
		t.Fatalf("direct proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
	if proof.FDOrigin.Source != proof.Dependencies[0] || proof.FDOrigin.RelationOID != 401 || proof.FDOrigin.AttributeNumber != 1 {
		t.Fatalf("direct FD origin = %+v, deps = %+v", proof.FDOrigin, proof.Dependencies)
	}

	r, err = Rewrite(`SELECT id + 1 AS x FROM orders`, "app", res)
	if err != nil {
		t.Fatalf("expression rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || proof.FDOrigin.Kind != FDOriginNoColumnOrigin {
		t.Fatalf("expression proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)

	r, err = Rewrite(`SELECT 1 AS x`, "app", res)
	if err != nil {
		t.Fatalf("constant rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || len(proof.Dependencies) != 0 || proof.FDOrigin.Kind != FDOriginNoColumnOrigin {
		t.Fatalf("constant proof = %+v", proof)
	}

	r, err = Rewrite(`SELECT id FROM orders`, "app", stubResolver{"app.orders": {"id", "amt"}})
	if err != nil {
		t.Fatalf("name-only rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofUnresolved || len(proof.Dependencies) != 1 || proof.FDOrigin.Kind != FDOriginUnresolved {
		t.Fatalf("name-only proof must retain its dependency while marking FD evidence unresolved: %+v", proof)
	}
}

func TestPublicProofs_CTEDirectAndExpression(t *testing.T) {
	res := proofFixtures()
	r, err := Rewrite(`WITH c AS (SELECT id FROM orders) SELECT id FROM c`, "app", res)
	if err != nil {
		t.Fatalf("CTE direct rewrite: %v", err)
	}
	proof := requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || proof.FDOrigin.Kind != FDOriginKnownColumn {
		t.Fatalf("CTE direct proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
	if len(r.Witnesses) != 1 || len(r.Witnesses[0].CoveredSources) != 1 || r.Witnesses[0].CoveredSources[0] != 0 {
		t.Fatalf("CTE witness coverage = %+v", r.Witnesses)
	}
	if r.Witnesses[0].CarrierOccurrence != 1 {
		t.Fatalf("CTE use carrier = %d, want original CTE RangeVar RefID 1", r.Witnesses[0].CarrierOccurrence)
	}
	if got := r.SourceCatalog[1]; got.Kind != SourceCTERangeVar || !got.HasRefID || got.RefID != 1 {
		t.Fatalf("CTE occurrence catalog entry = %+v", got)
	}

	r, err = Rewrite(`WITH c AS (SELECT id FROM orders) SELECT id + 1 AS x FROM c`, "app", res)
	if err != nil {
		t.Fatalf("CTE expression rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || proof.FDOrigin.Kind != FDOriginNoColumnOrigin {
		t.Fatalf("CTE expression proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
}

func TestProofs_OutputPositionsAndSameTableAliases(t *testing.T) {
	r, err := Rewrite(`SELECT a.id, b.id FROM orders a JOIN orders b ON a.id = b.id`, "app", proofFixtures())
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if len(r.PublicProofs) != 2 || len(r.Witnesses) != 2 || len(r.WitnessPositions) != 2 {
		t.Fatalf("publics=%v proofs=%+v witnesses=%+v positions=%v", r.Public, r.PublicProofs, r.Witnesses, r.WitnessPositions)
	}
	for i := range r.PublicProofs {
		if r.PublicProofs[i].TransportIndex != i {
			t.Fatalf("public proof %d transport index = %d", i, r.PublicProofs[i].TransportIndex)
		}
		requireDep(t, r.PublicProofs[i].Dependencies, SourceOccurrenceID(i), 0)
	}
	for i, w := range r.Witnesses {
		want := SourceOccurrenceID(i)
		if len(w.CoveredSources) != 1 || w.CoveredSources[0] != want || w.CarrierOccurrence != want {
			t.Fatalf("same-table alias witness %d = %+v", i, w)
		}
	}
	if len(r.Refs) != 2 || r.Refs[0].RefID == r.Refs[1].RefID {
		t.Fatalf("same-table aliases must keep immutable occurrence IDs: %+v", r.Refs)
	}
}

func TestProofs_RepeatedCTEUsesKeepSourceAndDistinctCarriers(t *testing.T) {
	r, err := Rewrite(`WITH c AS (SELECT id FROM orders) SELECT a.id FROM c a JOIN c b ON a.id = b.id`, "app", proofFixtures())
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if len(r.Refs) != 3 {
		t.Fatalf("refs = %+v", r.Refs)
	}
	if r.Refs[1].Kind != RefCTE || r.Refs[2].Kind != RefCTE {
		t.Fatalf("CTE use kinds = %+v", r.Refs)
	}
	if len(r.Witnesses) != 2 {
		t.Fatalf("two CTE carriers should each propagate one witness: %+v", r.Witnesses)
	}
	for i, w := range r.Witnesses {
		if len(w.CoveredSources) != 1 || w.CoveredSources[0] != 0 {
			t.Fatalf("CTE use %d must not duplicate underlying orders occurrence: %+v", i, w)
		}
		wantCarrier := SourceOccurrenceID(i + 1)
		if w.CarrierOccurrence != wantCarrier {
			t.Fatalf("CTE use %d carrier = %d, want %d", i, w.CarrierOccurrence, wantCarrier)
		}
	}
}

func TestProofs_DerivedDistinctCarriesTerminalSource(t *testing.T) {
	r, err := Rewrite(`SELECT DISTINCT id FROM (SELECT id FROM orders) d`, "app", proofFixtures())
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if len(r.Witnesses) != 1 {
		t.Fatalf("witnesses = %+v", r.Witnesses)
	}
	w := r.Witnesses[0]
	if len(w.CoveredSources) != 1 || w.CoveredSources[0] != 0 {
		t.Fatalf("Q+D covered sources = %+v", w.CoveredSources)
	}
	if w.CarrierOccurrence >= 0 {
		t.Fatalf("Q+D carrier must be generated, got %d", w.CarrierOccurrence)
	}
	if got := r.SourceCatalog[w.CarrierOccurrence]; got.Kind != SourceGeneratedCarrier || got.HasRefID {
		t.Fatalf("generated carrier catalog entry = %+v", got)
	}
	proof := requireProof(t, r.PublicProofs, 0)
	requireDep(t, proof.Dependencies, 0, 0)
}

func TestProofs_SetOperationMergesCoverageNotOriginalIDs(t *testing.T) {
	r, err := Rewrite(`WITH a AS (SELECT id FROM orders), b AS (SELECT id FROM orders) SELECT id FROM a UNION ALL SELECT id FROM b`, "app", proofFixtures())
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	if len(r.Witnesses) != 1 {
		t.Fatalf("witnesses = %+v", r.Witnesses)
	}
	w := r.Witnesses[0]
	if len(w.CoveredSources) != 2 || w.CoveredSources[0] != 0 || w.CoveredSources[1] != 1 {
		t.Fatalf("set-op covered sources = %+v", w.CoveredSources)
	}
	if w.CarrierOccurrence >= 0 {
		t.Fatalf("set-op carrier must be generated, got %d", w.CarrierOccurrence)
	}
	if len(r.Refs) != 4 || r.Refs[0].RefID != 0 || r.Refs[1].RefID != 1 || r.Refs[2].Kind != RefCTE || r.Refs[3].Kind != RefCTE {
		t.Fatalf("set-op original refs must remain distinct: %+v", r.Refs)
	}
	proof := requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || proof.FDOrigin.Kind != FDOriginNoColumnOrigin {
		t.Fatalf("set-op public proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
	requireDep(t, proof.Dependencies, 1, 0)
}

func TestProofs_ValuesExpressionDependencies(t *testing.T) {
	res := proofFixtures()

	// Pure constants are terminal — complete with an empty dependency set.
	r, err := Rewrite(`SELECT v.x FROM (VALUES (1)) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("constant VALUES rewrite: %v", err)
	}
	proof := requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || len(proof.Dependencies) != 0 || proof.FDOrigin.Kind != FDOriginNoColumnOrigin {
		t.Fatalf("constant VALUES proof = %+v", proof)
	}

	// A LATERAL VALUES cell references the preceding item's column — the
	// dependency must reach the entity slot, not the derived carrier.
	r, err = Rewrite(`SELECT v.x FROM app.orders o CROSS JOIN LATERAL (VALUES (o.id)) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("lateral VALUES rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete {
		t.Fatalf("lateral VALUES proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
	for _, dep := range proof.Dependencies {
		if dep.Occurrence < 0 {
			t.Fatalf("lateral VALUES dep parked on generated occurrence: %+v", dep)
		}
	}

	// Multiple rows feed the same output position — deps union per slot.
	r, err = Rewrite(`SELECT v.x FROM app.orders o CROSS JOIN LATERAL (VALUES (o.id),(o.amt)) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("multi-row lateral VALUES rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || len(proof.Dependencies) != 2 {
		t.Fatalf("multi-row VALUES proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
	requireDep(t, proof.Dependencies, 0, 1)

	// A correlated SubLink inside a lateral VALUES cell reaches the entity.
	r, err = Rewrite(`SELECT v.x FROM app.orders o CROSS JOIN LATERAL (VALUES ((SELECT o.id))) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("VALUES sublink rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete {
		t.Fatalf("VALUES sublink proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)

	// A subquery inside a lateral VALUES cell reading a witnessed CTE keeps
	// the CTE's underlying entity as the terminal source.
	r, err = Rewrite(`WITH c AS (SELECT id FROM orders) SELECT v.x FROM c CROSS JOIN LATERAL (VALUES ((SELECT id FROM c))) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("VALUES CTE-sublink rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete {
		t.Fatalf("VALUES CTE-sublink proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
}

func TestProofs_NamedWindowDependencies(t *testing.T) {
	res := proofFixtures()

	// Inline and named spellings of the same window produce identical deps.
	inline, err := Rewrite(`SELECT row_number() OVER (ORDER BY o.amt) AS rn FROM app.orders o`, "app", res)
	if err != nil {
		t.Fatalf("inline window rewrite: %v", err)
	}
	named, err := Rewrite(`SELECT row_number() OVER w AS rn FROM app.orders o WINDOW w AS (ORDER BY o.amt)`, "app", res)
	if err != nil {
		t.Fatalf("named window rewrite: %v", err)
	}
	ip := requireProof(t, inline.PublicProofs, 0)
	np := requireProof(t, named.PublicProofs, 0)
	if ip.Resolution != ProofComplete || np.Resolution != ProofComplete {
		t.Fatalf("window proofs unresolved: inline=%+v named=%+v", ip, np)
	}
	requireDep(t, np.Dependencies, 0, 1) // orders.amt slot
	if len(ip.Dependencies) != len(np.Dependencies) || ip.Dependencies[0] != np.Dependencies[0] {
		t.Fatalf("named window deps diverge from inline: %+v vs %+v", ip.Dependencies, np.Dependencies)
	}

	// Two named windows each contribute only their own clause's sources.
	r, err := Rewrite(`SELECT row_number() OVER w1 AS r1, row_number() OVER w2 AS r2 FROM app.orders o WINDOW w1 AS (ORDER BY o.amt), w2 AS (PARTITION BY o.id)`, "app", res)
	if err != nil {
		t.Fatalf("two-window rewrite: %v", err)
	}
	p1 := requireProof(t, r.PublicProofs, 0)
	p2 := requireProof(t, r.PublicProofs, 1)
	requireDep(t, p1.Dependencies, 0, 1)
	requireDep(t, p2.Dependencies, 0, 0)
	if len(p1.Dependencies) != 1 || len(p2.Dependencies) != 1 {
		t.Fatalf("windows leaked each other's deps: p1=%+v p2=%+v", p1.Dependencies, p2.Dependencies)
	}

	// Inheritance: w2 copies w's partition clause and adds its own order.
	r, err = Rewrite(`SELECT rank() OVER w2 AS r FROM app.orders o WINDOW w AS (PARTITION BY o.id), w2 AS (w ORDER BY o.amt)`, "app", res)
	if err != nil {
		t.Fatalf("inherited window rewrite: %v", err)
	}
	p := requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete || len(p.Dependencies) != 2 {
		t.Fatalf("inherited window proof = %+v", p)
	}
	requireDep(t, p.Dependencies, 0, 0)
	requireDep(t, p.Dependencies, 0, 1)

	// A window with no column references stays a complete empty set.
	r, err = Rewrite(`SELECT row_number() OVER w AS rn FROM app.orders o WINDOW w AS ()`, "app", res)
	if err != nil {
		t.Fatalf("empty window rewrite: %v", err)
	}
	p = requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete || len(p.Dependencies) != 0 {
		t.Fatalf("empty window proof = %+v", p)
	}
}

func TestProofs_CorrelatedSubqueryScope(t *testing.T) {
	res := proofFixtures()

	// A scalar sublink with no FROM of its own resolves the outer column.
	r, err := Rewrite(`SELECT (SELECT o.id) AS x FROM app.orders o`, "app", res)
	if err != nil {
		t.Fatalf("correlated sublink rewrite: %v", err)
	}
	proof := requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete {
		t.Fatalf("correlated sublink proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)

	// Inner-level columns shadow outer names.
	r, err = Rewrite(`WITH c AS (SELECT 99 AS id) SELECT (SELECT id FROM c) AS x FROM app.orders o`, "app", res)
	if err != nil {
		t.Fatalf("shadowed sublink rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete || len(proof.Dependencies) != 0 {
		t.Fatalf("inner name must shadow the outer column: %+v", proof)
	}

	// Two levels of nesting walk the scope chain outward.
	r, err = Rewrite(`SELECT (SELECT (SELECT o.amt)) AS x FROM app.orders o`, "app", res)
	if err != nil {
		t.Fatalf("two-level correlated rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete {
		t.Fatalf("two-level correlated proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 1)

	// ORDER BY inside a sublink binds the output alias (SQL92), not the
	// FROM scope — z names the public column, which tracks orders.id.
	r, err = Rewrite(`WITH c AS (SELECT id FROM orders) SELECT (SELECT id AS z FROM c ORDER BY z LIMIT 1) AS x FROM c`, "app", res)
	if err != nil {
		t.Fatalf("output-alias ORDER BY sublink rewrite: %v", err)
	}
	proof = requireProof(t, r.PublicProofs, 0)
	if proof.Resolution != ProofComplete {
		t.Fatalf("output-alias ORDER BY proof = %+v", proof)
	}
	requireDep(t, proof.Dependencies, 0, 0)
}

func TestProofs_WindowOrderKeysUseInputColumns(t *testing.T) {
	res := proofFixtures()

	// Window ORDER BY keys are input expressions (SQL99), never output
	// names: bare `amt` binds the input column o.amt — not the output
	// column `o.id AS amt`. Inline and named spellings must agree.
	for _, sql := range []string{
		`SELECT o.id AS amt, row_number() OVER (ORDER BY amt) AS rn FROM app.orders o`,
		`SELECT o.id AS amt, row_number() OVER w AS rn FROM app.orders o WINDOW w AS (ORDER BY amt)`,
	} {
		r, err := Rewrite(sql, "app", res)
		if err != nil {
			t.Fatalf("window input-key rewrite %q: %v", sql, err)
		}
		p := requireProof(t, r.PublicProofs, 1)
		if p.Resolution != ProofComplete || len(p.Dependencies) != 1 {
			t.Fatalf("window input-key proof %q = %+v", sql, p)
		}
		requireDep(t, p.Dependencies, 0, 1) // o.amt — o.id would be wrong
	}

	// `ORDER BY 1` inside a window is a constant, not an output ordinal.
	r, err := Rewrite(`SELECT row_number() OVER (ORDER BY 1) AS rn FROM app.orders`, "app", res)
	if err != nil {
		t.Fatalf("window ordinal-constant rewrite: %v", err)
	}
	p := requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete || len(p.Dependencies) != 0 {
		t.Fatalf("window constant-order proof = %+v", p)
	}
}

func TestProofs_CTEBodyCorrelatesOuterScope(t *testing.T) {
	res := proofFixtures()

	// A CTE body inside a subquery may correlate to grandparent-level
	// columns — the owning level's own FROM is the only scope cut off.
	r, err := Rewrite(`SELECT (WITH c AS (SELECT o.id AS x) SELECT x FROM c) AS picked FROM app.orders o`, "app", res)
	if err != nil {
		t.Fatalf("correlated CTE rewrite: %v", err)
	}
	p := requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete {
		t.Fatalf("correlated CTE proof = %+v", p)
	}
	requireDep(t, p.Dependencies, 0, 0)

	// Reverse: a top-level CTE body must not see the owning level's own
	// FROM items — `o` belongs to the same query, so it stays unresolved
	// (PostgreSQL rejects the same-level reference outright).
	r, err = Rewrite(`WITH c AS (SELECT o.id AS x) SELECT x FROM c, app.orders o`, "app", res)
	if err != nil {
		var rej *RejectError
		if !errors.As(err, &rej) {
			t.Fatalf("same-level CTE ref error = %v, want RejectError", err)
		}
		return
	}
	p = requireProof(t, r.PublicProofs, 0)
	if p.Resolution == ProofComplete {
		t.Fatalf("same-level CTE ref must not resolve: %+v", p)
	}
}

func TestProofs_ValuesInnerWithShadowsOuter(t *testing.T) {
	res := proofFixtures()

	// A lateral VALUES select's own WITH works and correlates legally.
	r, err := Rewrite(`SELECT v.x FROM app.orders o CROSS JOIN LATERAL (WITH c AS (SELECT o.amt AS x) VALUES ((SELECT x FROM c))) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("inner WITH VALUES rewrite: %v", err)
	}
	p := requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete || len(p.Dependencies) != 1 {
		t.Fatalf("inner WITH VALUES proof = %+v", p)
	}
	requireDep(t, p.Dependencies, 0, 1) // o.amt

	// An outer same-name CTE must be shadowed — resolving the inner
	// reference against `SELECT 0` would fabricate a constant source.
	r, err = Rewrite(`WITH c AS (SELECT 0 AS x) SELECT v.x FROM app.orders o CROSS JOIN LATERAL (WITH c AS (SELECT o.amt AS x) VALUES ((SELECT x FROM c))) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("shadowed WITH VALUES rewrite: %v", err)
	}
	p = requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete || len(p.Dependencies) != 1 {
		t.Fatalf("shadowed WITH VALUES proof = %+v", p)
	}
	requireDep(t, p.Dependencies, 0, 1)

	// Inner and outer bodies depend on different columns — the dep must
	// come from the inner definition, not the outer entity's column.
	r, err = Rewrite(`WITH c AS (SELECT id AS x FROM orders) SELECT v.x FROM c, app.orders o CROSS JOIN LATERAL (WITH c AS (SELECT o.amt AS x) VALUES ((SELECT x FROM c))) v(x)`, "app", res)
	if err != nil {
		t.Fatalf("divergent WITH VALUES rewrite: %v", err)
	}
	var occOfO SourceOccurrenceID = -1
	for _, ref := range r.Refs {
		if ref.Alias == "o" {
			occOfO = SourceOccurrenceID(ref.RefID)
		}
	}
	if occOfO < 0 {
		t.Fatalf("refs %+v lack alias o", r.Refs)
	}
	p = requireProof(t, r.PublicProofs, 0)
	if p.Resolution != ProofComplete || len(p.Dependencies) != 1 {
		t.Fatalf("divergent WITH VALUES proof = %+v", p)
	}
	requireDep(t, p.Dependencies, occOfO, 1)
}

func TestProofs_ValuesTailSublinkFrozen(t *testing.T) {
	res := proofFixtures()

	// A VALUES layer's local WITH now carries propagated witnesses — a `*`
	// left verbatim in a tail sublink would expand over the injected column
	// and change the scalar subquery's width. The transport SQL must show
	// the LIMIT/OFFSET/ORDER BY sublink projecting only the public column.
	for _, sql := range []string{
		`WITH a AS (SELECT id FROM app.orders) SELECT v.x FROM a CROSS JOIN LATERAL (WITH c AS (SELECT id FROM a) VALUES (1) LIMIT (SELECT * FROM c LIMIT 1)) AS v(x)`,
		`WITH a AS (SELECT id FROM app.orders) SELECT v.x FROM a CROSS JOIN LATERAL (WITH c AS (SELECT id FROM a) VALUES (1) OFFSET (SELECT * FROM c LIMIT 1)) AS v(x)`,
		`WITH a AS (SELECT id FROM app.orders) SELECT v.x FROM a CROSS JOIN LATERAL (WITH c AS (SELECT id FROM a) VALUES (1) ORDER BY (SELECT * FROM c LIMIT 1)) AS v(x)`,
	} {
		r, err := Rewrite(sql, "app", res)
		if err != nil {
			t.Fatalf("tail-sublink rewrite %q: %v", sql, err)
		}
		// Trigger condition: the local CTE really did pick up a witness.
		if !strings.Contains(r.SQL, "__chub_w") {
			t.Fatalf("expected propagated witness in transport SQL %q", r.SQL)
		}
		// The tail sublink is the only position that ever held a SELECT
		// star — the `.*` inside witness CASE expressions is legitimate
		// and stays.
		if strings.Contains(r.SQL, "SELECT *") {
			t.Fatalf("unfrozen star survived tail sublink: %q", r.SQL)
		}
		if !strings.Contains(r.SQL, "id") {
			t.Fatalf("tail sublink lost its public column: %q", r.SQL)
		}
	}
}

func TestCanonicalRefMapping_PathIndexedNotOrder(t *testing.T) {
	gr, err := GuardPG(`SELECT a.id, b.id FROM orders a JOIN orders b ON a.id = b.id`)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	if len(gr.Refs) != 2 {
		t.Fatalf("refs = %+v", gr.Refs)
	}
	// Canonical tree: qualified twins of the original refs. RefIDs carry
	// the same path-to-ID pairing because collection order matches.
	tree := parseTree(t, `SELECT a.id, b.id FROM "app".orders a JOIN "app".orders b ON a.id = b.id`)

	// Reversing the original slice must not change which node receives
	// which RefID — mapping keys on structural path, never slice order.
	rev := []RangeVarRef{gr.Refs[1], gr.Refs[0]}
	byNode, err := mapCanonicalRefs(rev, tree, "app")
	if err != nil {
		t.Fatalf("path-indexed mapping on reversed refs: %v", err)
	}
	_, canonicalByNode, err := collectRangeVarRefs(tree)
	if err != nil {
		t.Fatalf("canonical collect: %v", err)
	}
	for node, canonID := range canonicalByNode {
		if byNode[node] != canonID {
			t.Fatalf("node at path %v mapped to RefID %d, want %d", canonicalPathOf(tree, node), byNode[node], canonID)
		}
	}

	// A duplicate original structural path fails closed.
	dup := []RangeVarRef{{RefID: 0, Path: gr.Refs[0].Path, Kind: RefEntity, Name: "orders"},
		{RefID: 1, Path: gr.Refs[0].Path, Kind: RefEntity, Name: "orders"}}
	if _, err := mapCanonicalRefs(dup, tree, "app"); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("duplicate path mapping = %v, want ErrEvidenceUnavailable", err)
	}
}

func canonicalPathOf(tree *pg.ParseResult, want *pg.RangeVar) string {
	refs, byNode, err := collectRangeVarRefs(tree)
	if err != nil {
		return ""
	}
	if id, ok := byNode[want]; ok && int(id) < len(refs) {
		var b []byte
		for _, f := range refs[int(id)].Path {
			b = append(b, f.field...)
			b = append(b, '/')
		}
		return string(b)
	}
	return ""
}

func TestCanonicalRefMapping_FailsClosedOnShapeOrPathChange(t *testing.T) {
	gr, err := GuardPG(`SELECT id FROM orders`)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	for _, sql := range []string{
		`SELECT id FROM "app".items`,
		`SELECT id FROM (SELECT id FROM "app".orders) d`,
	} {
		tree := parseTree(t, sql)
		if _, err := mapCanonicalRefs(gr.Refs, tree, "app"); !errors.Is(err, ErrEvidenceUnavailable) {
			t.Fatalf("mapCanonicalRefs(%q) = %v, want ErrEvidenceUnavailable", sql, err)
		}
	}
}

func TestGuardRewrite_MixingOriginalAnalysesFailsClosed(t *testing.T) {
	gr, err := GuardPG(`SELECT id FROM orders`)
	if err != nil {
		t.Fatalf("guard: %v", err)
	}
	_, _, err = rewriteFromGuard(`SELECT amt FROM orders`, "app", gr, proofFixtures(), nil)
	if !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("mixed statement error = %v, want ErrEvidenceUnavailable", err)
	}
}
