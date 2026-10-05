// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic SQL strings over metadata-capable stub resolvers
// output: intent tests for RefID/RefPath mapping, public-column proof records, source occurrence catalogs, and witness covered-source/carrier propagation
// pos: T7-S1 verification-record tests; asserts rewrite-time evidence coordinates without claiming live PostgreSQL execution semantics
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"testing"
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
