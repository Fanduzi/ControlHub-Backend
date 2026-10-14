// Package service — unit tests for the T9-B disclosure preflight and
// transform stages.
// input: constructed RewriteResult/PGBindResult records and fake
// QueryDisclosureReader outcomes — no database
// output: proof→policy-key mapping, policy union semantics, error-class and
// input-ownership assertions for resolvePGDisclosurePlan/applyPGDisclosure
// pos: constructed-record layer of the T9-B acceptance matrix — real-PG
// execution evidence lives in pg_disclosure_integration_test.go and the
// real-MySQL close-out in internal/integration
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// fakePGPolicyReader records every GetByScope key and serves a fixed policy
// map — missing keys return sql.ErrNoRows like the real repository.
type fakePGPolicyReader struct {
	policies map[pgPolicyKey]model.ResultDisclosureMode
	rawModes map[pgPolicyKey]model.ResultDisclosureMode // bypasses nothing; stores any mode incl. invalid
	err      error
	calls    []pgPolicyKey
	target   uint64
}

func (f *fakePGPolicyReader) ListByTarget(_ context.Context, _ uint64) ([]model.ResultDisclosurePolicy, error) {
	return nil, nil // the preflight never lists — it resolves exact scopes only
}

func (f *fakePGPolicyReader) GetByScope(_ context.Context, target uint64, database, schema, object, column string) (model.ResultDisclosurePolicy, error) {
	key := pgPolicyKey{database: database, schema: schema, object: object, column: column}
	f.calls = append(f.calls, key)
	f.target = target
	if f.err != nil {
		return model.ResultDisclosurePolicy{}, f.err
	}
	mode, ok := f.policies[key]
	if !ok {
		if raw, rok := f.rawModes[key]; rok {
			return model.ResultDisclosurePolicy{Mode: raw}, nil
		}
		return model.ResultDisclosurePolicy{}, sql.ErrNoRows
	}
	return model.ResultDisclosurePolicy{Mode: mode}, nil
}

// pgTestRewrite builds a rewrite record: one public proof per dependency
// list, against the given source catalog.
func pgTestRewrite(catalog map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence, deps ...[]pgsql.SourceRef) *pgsql.RewriteResult {
	proofs := make([]pgsql.PublicColumnProof, len(deps))
	for i, d := range deps {
		proofs[i] = pgsql.PublicColumnProof{TransportIndex: i, Resolution: pgsql.ProofComplete, Dependencies: d}
	}
	return &pgsql.RewriteResult{PublicProofs: proofs, SourceCatalog: catalog}
}

func pgEntityOcc(id pgsql.SourceOccurrenceID, ref pgsql.RefID) pgsql.SourceOccurrence {
	return pgsql.SourceOccurrence{ID: id, Kind: pgsql.SourceEntityRangeVar, RefID: ref, HasRefID: true}
}

func pgTestBind(cols ...string) *PGBindResult {
	attmap := make([]pgsql.ColumnMetadata, len(cols))
	for i, c := range cols {
		attmap[i] = pgsql.ColumnMetadata{Name: c, AttributeNumber: int16(i + 1)}
	}
	return &PGBindResult{Refs: map[pgsql.RefID]*PGBoundRef{
		7: {
			RefID: 7, Schema: "app", Name: "ORDERS", CatalogName: "orders",
			TouchConfirmed: true, Columns: attmap,
		},
	}}
}

func colDep(occ pgsql.SourceOccurrenceID, ord int) pgsql.SourceRef {
	return pgsql.SourceRef{Occurrence: occ, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotColumn, Ordinal: ord}}
}

func wholeDep(occ pgsql.SourceOccurrenceID) pgsql.SourceRef {
	return pgsql.SourceRef{Occurrence: occ, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotWholeRow, Ordinal: -1}}
}

func TestPGDisclosurePlan_SourceResolution(t *testing.T) {
	catalog := map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{
		100: pgEntityOcc(100, 7),
		101: {ID: 101, Kind: pgsql.SourceDerived},
		102: {ID: 102, Kind: pgsql.SourceNonEntity},
	}
	reader := &fakePGPolicyReader{policies: map[pgPolicyKey]model.ResultDisclosureMode{
		{"db_a", "app", "orders", "id"}:   model.ResultDisclosureRawCopyAllowed,
		{"db_a", "app", "orders", "qty"}:  model.ResultDisclosureMaskedNoCopy,
		{"db_a", "app", "orders", "note"}: model.ResultDisclosureRawCopyAllowed,
	}}
	rw := pgTestRewrite(catalog,
		[]pgsql.SourceRef{colDep(100, 0)},                 // orders.id
		[]pgsql.SourceRef{colDep(100, 0), colDep(100, 1)}, // id+qty expression
		[]pgsql.SourceRef{wholeDep(100)},                  // whole-row
		[]pgsql.SourceRef{},                               // literal-only
		[]pgsql.SourceRef{colDep(101, 0)},                 // derived terminal
		[]pgsql.SourceRef{colDep(102, 0), colDep(100, 0)}, // mixed non-entity + entity
	)
	plan, err := resolvePGDisclosurePlan(context.Background(), reader, 42, "db_a", rw, pgTestBind("id", "qty", "note"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(plan.columns) != 6 {
		t.Fatalf("plan columns = %d, want 6", len(plan.columns))
	}
	// Single raw source → raw.
	if plan.columns[0].mode != model.ResultDisclosureRawCopyAllowed || !plan.columns[0].copyAllowed {
		t.Fatalf("col0 mode = %s copy=%v", plan.columns[0].mode, plan.columns[0].copyAllowed)
	}
	// Any masked source → masked. Whole-row expands to all columns → masked.
	for _, i := range []int{1, 2} {
		if plan.columns[i].mode != model.ResultDisclosureMaskedNoCopy || plan.columns[i].copyAllowed {
			t.Fatalf("col%d mode = %s copy=%v, want masked_no_copy/false", i, plan.columns[i].mode, plan.columns[i].copyAllowed)
		}
	}
	// Whole-row must have produced one key per attmap column.
	if len(plan.columns[2].keys) != 3 {
		t.Fatalf("whole-row keys = %v, want 3 columns", plan.columns[2].keys)
	}
	// Literal-only and derived-terminal columns have no governed source → raw.
	for _, i := range []int{3, 4} {
		if plan.columns[i].mode != model.ResultDisclosureRawCopyAllowed || !plan.columns[i].copyAllowed {
			t.Fatalf("col%d mode = %s, want raw for no-entity-source", i, plan.columns[i].mode)
		}
	}
	// Mixed non-entity + governed entity resolves by the entity's policy.
	if plan.columns[5].mode != model.ResultDisclosureRawCopyAllowed {
		t.Fatalf("col5 mode = %s, want raw (entity id is raw)", plan.columns[5].mode)
	}
	// The reader saw the canonical bound identity — not the user spelling
	// "ORDERS" — and the per-attempt cache dedupes repeat keys across
	// columns: id (col0), qty (col1), note (whole-row col2).
	wantCalls := []pgPolicyKey{
		{"db_a", "app", "orders", "id"},
		{"db_a", "app", "orders", "qty"},
		{"db_a", "app", "orders", "note"},
	}
	if len(reader.calls) != len(wantCalls) {
		t.Fatalf("reader calls = %v", reader.calls)
	}
	for i, c := range reader.calls {
		if c != wantCalls[i] {
			t.Fatalf("call %d = %v, want %v", i, c, wantCalls[i])
		}
	}
	if reader.target != 42 {
		t.Fatalf("reader target = %d, want 42", reader.target)
	}
}

func TestPGDisclosurePlan_BlockedAndMissing(t *testing.T) {
	catalog := map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{100: pgEntityOcc(100, 7)}
	rw := pgTestRewrite(catalog, []pgsql.SourceRef{colDep(100, 0)})
	bind := pgTestBind("id")

	// Missing policy → ErrQueryDisclosureBlocked naming the canonical scope.
	reader := &fakePGPolicyReader{policies: map[pgPolicyKey]model.ResultDisclosureMode{}}
	_, err := resolvePGDisclosurePlan(context.Background(), reader, 1, "db_a", rw, bind)
	if !errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("missing policy err = %v, want ErrQueryDisclosureBlocked", err)
	}
	if !strings.Contains(err.Error(), "db_a.app.orders.id") {
		t.Fatalf("blocked error must name the scope, got %q", err.Error())
	}

	// Store failure → ErrQueryDisclosureBackendFailure, never blocked.
	reader.err = fmt.Errorf("connection refused")
	_, err = resolvePGDisclosurePlan(context.Background(), reader, 1, "db_a", rw, bind)
	if !errors.Is(err, ErrQueryDisclosureBackendFailure) || errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("store failure err = %v, want backend failure", err)
	}

	// Invalid stored mode → blocked (fail closed), same as the MySQL path.
	reader.err = nil
	reader.rawModes = map[pgPolicyKey]model.ResultDisclosureMode{
		{"db_a", "app", "orders", "id"}: "not_a_mode",
	}
	_, err = resolvePGDisclosurePlan(context.Background(), reader, 1, "db_a", rw, bind)
	if !errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("invalid stored mode err = %v, want blocked", err)
	}
}

func TestPGDisclosurePlan_EvidenceFaults(t *testing.T) {
	bind := pgTestBind("id", "qty")
	reader := &fakePGPolicyReader{policies: map[pgPolicyKey]model.ResultDisclosureMode{
		{"db_a", "app", "orders", "id"}: model.ResultDisclosureRawCopyAllowed,
	}}
	cases := map[string]*pgsql.RewriteResult{
		"incomplete proof": {
			PublicProofs:  []pgsql.PublicColumnProof{{Resolution: pgsql.ProofUnresolved}},
			SourceCatalog: map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{},
		},
		"unknown occurrence": pgTestRewrite(
			map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{},
			[]pgsql.SourceRef{colDep(999, 0)}),
		"unbound ref": pgTestRewrite(
			map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{100: pgEntityOcc(100, 88)},
			[]pgsql.SourceRef{colDep(100, 0)}),
		"untouched ref": pgTestRewrite(
			map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{100: pgEntityOcc(100, 7)},
			[]pgsql.SourceRef{colDep(100, 0)}),
		"ordinal out of range": pgTestRewrite(
			map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{100: pgEntityOcc(100, 7)},
			[]pgsql.SourceRef{colDep(100, 9)}),
		"unknown slot kind": pgTestRewrite(
			map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{100: pgEntityOcc(100, 7)},
			[]pgsql.SourceRef{{Occurrence: 100, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotUnknown}}}),
		"non-terminal dep": pgTestRewrite(
			map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{
				200: {ID: 200, Kind: pgsql.SourceCTERangeVar, HasRefID: true, RefID: 55},
			},
			[]pgsql.SourceRef{colDep(200, 0)}),
	}
	for name, rw := range cases {
		b := bind
		if name == "untouched ref" {
			b = pgTestBind("id", "qty")
			b.Refs[7].TouchConfirmed = false
		}
		_, err := resolvePGDisclosurePlan(context.Background(), reader, 1, "db_a", rw, b)
		if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
			t.Fatalf("%s: err = %v, want ErrEvidenceUnavailable", name, err)
		}
		if errors.Is(err, ErrQueryDisclosureBlocked) {
			t.Fatalf("%s: evidence fault must not collapse into blocked", name)
		}
	}
	// A damaged ref record must never look up a same-named entity anyway.
	if len(reader.calls) != 0 {
		t.Fatalf("evidence faults must not issue policy reads; calls=%v", reader.calls)
	}
}

func TestPGDisclosureApply(t *testing.T) {
	str := func(s string) *string { return &s }
	verified := &PGVerifiedResult{
		Columns: []PGResultColumn{{Name: "id", DatabaseType: "INT8"}, {Name: "note", DatabaseType: "TEXT"}},
		Rows:    [][]*string{{str("1"), str("plain")}, {str("2"), nil}},
	}
	plan := &pgDisclosurePlan{columns: []pgColumnDecision{
		{mode: model.ResultDisclosureRawCopyAllowed, copyAllowed: true},
		{mode: model.ResultDisclosureMaskedNoCopy, copyAllowed: false},
	}}
	out, err := applyPGDisclosure(plan, verified)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Values: raw byte-exact, masked non-NULL replaced, NULL preserved.
	if *out.Rows[0][0] != "1" || *out.Rows[0][1] != "[MASKED]" {
		t.Fatalf("row0 = %v", out.Rows[0])
	}
	if *out.Rows[1][0] != "2" || out.Rows[1][1] != nil {
		t.Fatalf("row1 = %v — NULL must stay NULL", out.Rows[1])
	}
	// Input untouched.
	if *verified.Rows[0][1] != "plain" {
		t.Fatalf("input row mutated: %q", *verified.Rows[0][1])
	}
	// Column metadata + modes aligned by position.
	if out.Columns[1].Name != "note" || out.Columns[1].DisplayMode != model.ResultDisclosureMaskedNoCopy || out.Columns[1].CopyAllowed {
		t.Fatalf("col1 = %+v", out.Columns[1])
	}

	// Shape mismatches are machinery failures — never blocked.
	for name, tc := range map[string]struct {
		plan *pgDisclosurePlan
		v    *PGVerifiedResult
	}{
		"nil plan":      {nil, verified},
		"empty plan":    {&pgDisclosurePlan{}, verified},
		"column drift":  {&pgDisclosurePlan{columns: make([]pgColumnDecision, 3)}, verified},
		"row too short": {plan, &PGVerifiedResult{Columns: verified.Columns, Rows: [][]*string{{str("1")}}}},
	} {
		_, err := applyPGDisclosure(tc.plan, tc.v)
		if !errors.Is(err, ErrQueryDisclosureBackendFailure) || errors.Is(err, ErrQueryDisclosureBlocked) {
			t.Fatalf("%s: err = %v, want backend failure", name, err)
		}
	}
	// Internally inconsistent plan → machinery failure.
	bad := &pgDisclosurePlan{columns: []pgColumnDecision{
		{mode: model.ResultDisclosureRawCopyAllowed, copyAllowed: false},
		{mode: model.ResultDisclosureMaskedNoCopy, copyAllowed: false},
	}}
	if _, err := applyPGDisclosure(bad, verified); !errors.Is(err, ErrQueryDisclosureBackendFailure) {
		t.Fatalf("inconsistent plan: err = %v", err)
	}
}

func TestPGDisclosedInput_Validation(t *testing.T) {
	if _, err := ExecutePGGovernedDisclosed(context.Background(), PGDisclosureInput{}); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("nil policies: %v", err)
	}
	if _, err := ExecutePGGovernedDisclosed(context.Background(), PGDisclosureInput{
		Policies: &fakePGPolicyReader{},
	}); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("zero target: %v", err)
	}
}
