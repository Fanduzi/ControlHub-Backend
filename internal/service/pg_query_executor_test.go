// Package service proves the governed executor's offline verification logic.
// input: constructed FieldDescriptions, rewrite proof records, bound refs,
// observed pg_locks row sets
// output: unit tests for evaluatePGLockAudit (three-state verdict),
// verifyPGExpectedLayout (pre-wire expected-record validation),
// verifyPGPositions (per-position FD identity), pgColumnTypeName (G7 naming
// and the stable unrecognized marker), and projectPGPublicResult (witness
// strip)
// pos: T7-S3/S4 offline evidence tests — verdict and position checks are pure
// functions so every verdict arm and every contradiction form is covered
// without a database
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/fan/controlhub/internal/pgsql"
)

// --- three-state lock audit -------------------------------------------------

func lockRowsFor(vxid string, rels ...PGRelationIdentity) []pgLockRow {
	rows := []pgLockRow{{
		locktype: "virtualxid", mode: "ExclusiveLock", granted: true, vxid: vxid,
	}}
	for _, id := range rels {
		rows = append(rows, pgLockRow{
			locktype: "relation", mode: "AccessShareLock", granted: true,
			database: id.DatabaseOID, relation: id.RelationOID, vxid: vxid,
		})
	}
	return rows
}

func TestPGLockAudit_Verdicts(t *testing.T) {
	vxid := "7/42"
	approved := []PGRelationIdentity{{DatabaseOID: 5, RelationOID: 100}}
	explain := func(ids ...PGRelationIdentity) map[PGRelationIdentity]bool {
		m := map[PGRelationIdentity]bool{}
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	explained := explain(PGRelationIdentity{5, 100}, PGRelationIdentity{0, 1262})

	t.Run("verified", func(t *testing.T) {
		rows := lockRowsFor(vxid, PGRelationIdentity{5, 100}, PGRelationIdentity{0, 1262})
		v, un := evaluatePGLockAudit(rows, vxid, explained, approved)
		if v != pgAuditVerified || len(un) != 0 {
			t.Fatalf("verdict = %d unexplained %v, want Verified", v, un)
		}
	})

	t.Run("verified constant query shape", func(t *testing.T) {
		// No entity refs: required set empty but held probe locks must still
		// explain — an all-empty observation must never pass.
		rows := lockRowsFor(vxid, PGRelationIdentity{0, 1262})
		v, _ := evaluatePGLockAudit(rows, vxid, explained, nil)
		if v != pgAuditVerified {
			t.Fatalf("verdict = %d, want Verified", v)
		}
	})

	t.Run("unexplained held lock is mismatch", func(t *testing.T) {
		rows := lockRowsFor(vxid, PGRelationIdentity{5, 100}, PGRelationIdentity{5, 777})
		v, un := evaluatePGLockAudit(rows, vxid, explained, approved)
		if v != pgAuditMismatch || len(un) != 1 || un[0] != (PGRelationIdentity{5, 777}) {
			t.Fatalf("verdict = %d unexplained %v, want Mismatch on (5,777)", v, un)
		}
	})

	t.Run("missing approved lock is unavailable", func(t *testing.T) {
		rows := lockRowsFor(vxid, PGRelationIdentity{0, 1262})
		v, _ := evaluatePGLockAudit(rows, vxid, explained, approved)
		if v != pgAuditUnavailable {
			t.Fatalf("verdict = %d, want Unavailable", v)
		}
	})

	t.Run("empty observation is unavailable", func(t *testing.T) {
		if v, _ := evaluatePGLockAudit(nil, vxid, explained, approved); v != pgAuditUnavailable {
			t.Fatalf("verdict = %d, want Unavailable", v)
		}
	})

	t.Run("missing identity is unavailable", func(t *testing.T) {
		rows := []pgLockRow{{
			locktype: "relation", mode: "AccessShareLock", granted: true,
			database: 5, relation: 100, vxid: vxid,
		}}
		if v, _ := evaluatePGLockAudit(rows, vxid, explained, approved); v != pgAuditUnavailable {
			t.Fatalf("verdict = %d, want Unavailable", v)
		}
	})

	t.Run("foreign vxid is unavailable", func(t *testing.T) {
		rows := lockRowsFor("7/43", PGRelationIdentity{5, 100})
		if v, _ := evaluatePGLockAudit(rows, vxid, explained, approved); v != pgAuditUnavailable {
			t.Fatalf("verdict = %d, want Unavailable", v)
		}
	})

	t.Run("ungranted row is unavailable", func(t *testing.T) {
		rows := lockRowsFor(vxid, PGRelationIdentity{5, 100})
		rows = append(rows, pgLockRow{
			locktype: "relation", mode: "AccessExclusiveLock", granted: false,
			database: 5, relation: 555, vxid: vxid,
		})
		if v, _ := evaluatePGLockAudit(rows, vxid, explained, approved); v != pgAuditUnavailable {
			t.Fatalf("verdict = %d, want Unavailable", v)
		}
	})
}

// --- per-position verification ----------------------------------------------

func pgRewriteFixture() (*pgsql.RewriteResult, *PGBindResult) {
	// One public column from orders (oid 100, attnum 1) plus one witness at
	// transport position 1 covering entity occurrence RefID 0.
	refs := []pgsql.RangeVarRef{{RefID: 0, Kind: pgsql.RefEntity, Name: "orders"}}
	rw := &pgsql.RewriteResult{
		Public:           []string{"id"},
		Witnesses:        []pgsql.WitnessRecord{{Column: "__chub_w0", CoveredSources: []pgsql.SourceOccurrenceID{0}, CarrierOccurrence: 0}},
		WitnessPositions: []int{1},
		PublicProofs: []pgsql.PublicColumnProof{{
			TransportIndex: 0,
			Resolution:     pgsql.ProofComplete,
			Dependencies:   []pgsql.SourceRef{{Occurrence: 0, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotColumn, Ordinal: 0}}},
			FDOrigin: pgsql.FDOrigin{
				Kind:            pgsql.FDOriginKnownColumn,
				Source:          pgsql.SourceRef{Occurrence: 0, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotColumn, Ordinal: 0}},
				RelationOID:     100,
				AttributeNumber: 1,
			},
		}},
		SourceCatalog: map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{
			0: {ID: 0, Kind: pgsql.SourceEntityRangeVar, RefID: 0, HasRefID: true},
		},
		Refs: refs,
	}
	bind := &PGBindResult{
		Refs: map[pgsql.RefID]*PGBoundRef{
			0: {RefID: 0, Identity: PGRelationIdentity{DatabaseOID: 5, RelationOID: 100}, RelTypeOID: 900, TouchConfirmed: true,
				Columns: []pgsql.ColumnMetadata{
					{Name: "id", RelationOID: 100, AttributeNumber: 1},
					{Name: "item", RelationOID: 100, AttributeNumber: 2},
				}},
		},
	}
	return rw, bind
}

func pgFDs(col ...pgconn.FieldDescription) []pgconn.FieldDescription { return col }

func TestPGVerifyPositions(t *testing.T) {
	goodFDs := pgFDs(
		pgconn.FieldDescription{Name: "id", TableOID: 100, TableAttributeNumber: 1, DataTypeOID: 20},
		pgconn.FieldDescription{Name: "__chub_w0", DataTypeOID: 900},
	)

	t.Run("verified", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		if err := verifyPGPositions(goodFDs, rw, bind); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})

	t.Run("swapped approved object is a binding mismatch", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		fds := pgFDs(
			pgconn.FieldDescription{Name: "id", TableOID: 777, TableAttributeNumber: 1, DataTypeOID: 20},
			pgconn.FieldDescription{Name: "__chub_w0", DataTypeOID: 900},
		)
		err := verifyPGPositions(fds, rw, bind)
		var re *pgsql.RejectError
		if !errors.As(err, &re) || re.Code != "query_binding_mismatch" {
			t.Fatalf("err = %v, want query_binding_mismatch", err)
		}
	})

	t.Run("swapped attribute number is a binding mismatch", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		fds := pgFDs(
			pgconn.FieldDescription{Name: "id", TableOID: 100, TableAttributeNumber: 2, DataTypeOID: 20},
			pgconn.FieldDescription{Name: "__chub_w0", DataTypeOID: 900},
		)
		err := verifyPGPositions(fds, rw, bind)
		var re *pgsql.RejectError
		if !errors.As(err, &re) || re.Code != "query_binding_mismatch" {
			t.Fatalf("err = %v, want query_binding_mismatch", err)
		}
	})

	t.Run("wrong witness reltype is a binding mismatch", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		fds := pgFDs(
			pgconn.FieldDescription{Name: "id", TableOID: 100, TableAttributeNumber: 1, DataTypeOID: 20},
			pgconn.FieldDescription{Name: "__chub_w0", DataTypeOID: 901},
		)
		err := verifyPGPositions(fds, rw, bind)
		var re *pgsql.RejectError
		if !errors.As(err, &re) || re.Code != "query_binding_mismatch" {
			t.Fatalf("err = %v, want query_binding_mismatch", err)
		}
	})

	t.Run("transport column count drift is evidence failure", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		err := verifyPGPositions(goodFDs[:1], rw, bind)
		if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
			t.Fatalf("err = %v, want evidence unavailable", err)
		}
	})

	t.Run("no-column-origin keeps deps without TableOID assertion", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		rw.PublicProofs[0].FDOrigin = pgsql.FDOrigin{Kind: pgsql.FDOriginNoColumnOrigin}
		if err := verifyPGPositions(goodFDs, rw, bind); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
}

// Expected-record validation runs before any user byte reaches the wire —
// every arm here is an injected internal fault, and catching it means
// ExecParams was never reached.
func TestPGVerifyExpectedLayout(t *testing.T) {
	verified := func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		if err := verifyPGExpectedLayout(rw, bind); err != nil {
			t.Fatalf("expected-layout verify: %v", err)
		}
	}
	t.Run("valid fixture passes", verified)

	expectFail := func(name string, mutate func(rw *pgsql.RewriteResult, bind *PGBindResult)) {
		t.Run(name, func(t *testing.T) {
			rw, bind := pgRewriteFixture()
			mutate(rw, bind)
			err := verifyPGExpectedLayout(rw, bind)
			if !errors.Is(err, pgsql.ErrEvidenceUnavailable) &&
				!errors.Is(err, ErrQueryResultTooLarge) {
				t.Fatalf("err = %v, want evidence failure", err)
			}
		})
	}

	expectFail("two proofs at coordinate zero leave an FD unchecked", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.Public = append(rw.Public, "extra")
		rw.PublicProofs = append(rw.PublicProofs, rw.PublicProofs[0])
		// Two proofs both claim transport 0; witness still claims 1 —
		// coordinate 2 would arrive in FDs with no record owning it.
	})

	expectFail("witness position collides with a public claim", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.WitnessPositions[0] = 0 // both records now fight over coordinate 0
	})

	expectFail("witness position outside transport", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.WitnessPositions[0] = 9
	})

	expectFail("dependency ordinal out of bounds", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.PublicProofs[0].Dependencies[0].Slot.Ordinal = 7
	})

	expectFail("proof claims an attnum its slot does not own", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		// Slot 0 owns attnum 1; proof and a colluding FD could both claim
		// attnum 2 — the attmap check refuses the consistent lie.
		rw.PublicProofs[0].FDOrigin.AttributeNumber = 2
	})

	expectFail("bound entity missing from rewrite coverage", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		// The bind gate confirmed a second entity ref the rewrite never
		// carried — coverage must be checked against the binding, not only
		// the catalog.
		bind.Refs[7] = &PGBoundRef{RefID: 7, TouchConfirmed: true,
			Identity: PGRelationIdentity{DatabaseOID: 5, RelationOID: 700}}
	})

	expectFail("catalog entity escapes the binding", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.Refs = append(rw.Refs, pgsql.RangeVarRef{RefID: 9, Kind: pgsql.RefEntity, Name: "ghost"})
		rw.SourceCatalog[9] = pgsql.SourceOccurrence{ID: 9, Kind: pgsql.SourceEntityRangeVar, RefID: 9, HasRefID: true}
	})

	expectFail("unresolved origin stops before the wire", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.PublicProofs[0].FDOrigin.Kind = pgsql.FDOriginUnresolved
	})

	expectFail("incomplete proof stops before the wire", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.PublicProofs[0].Resolution = pgsql.ProofUnresolved
	})

	expectFail("uncovered entity occurrence is evidence failure", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.Refs = append(rw.Refs, pgsql.RangeVarRef{RefID: 9, Kind: pgsql.RefEntity, Name: "ghost"})
		rw.SourceCatalog[9] = pgsql.SourceOccurrence{ID: 9, Kind: pgsql.SourceEntityRangeVar, RefID: 9, HasRefID: true}
		bind.Refs[9] = &PGBoundRef{RefID: 9, TouchConfirmed: true,
			Identity: PGRelationIdentity{DatabaseOID: 5, RelationOID: 900}}
	})

	expectFail("unsorted covered sources are evidence failure", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.Witnesses[0].CoveredSources = []pgsql.SourceOccurrenceID{3, 0}
	})

	expectFail("FD origin source outside its dependencies", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.PublicProofs[0].FDOrigin.Source = pgsql.SourceRef{
			Occurrence: 0, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotColumn, Ordinal: 1}}
	})

	expectFail("unknown slot kind on an entity dep", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.PublicProofs[0].Dependencies[0].Slot.Kind = pgsql.SourceSlotUnknown
	})

	expectFail("whole-row slot carrying an ordinal", func(rw *pgsql.RewriteResult, bind *PGBindResult) {
		rw.PublicProofs[0].Dependencies[0].Slot = pgsql.SourceSlot{Kind: pgsql.SourceSlotWholeRow, Ordinal: 3}
	})

	t.Run("whole-row slot at -1 is legal", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		rw.PublicProofs[0].Dependencies[0].Slot = pgsql.SourceSlot{Kind: pgsql.SourceSlotWholeRow, Ordinal: -1}
		rw.PublicProofs[0].FDOrigin.Source.Slot = pgsql.SourceSlot{Kind: pgsql.SourceSlotWholeRow, Ordinal: -1}
		if err := verifyPGExpectedLayout(rw, bind); err != nil {
			t.Fatalf("whole-row dep: %v", err)
		}
	})

	t.Run("occurrence zero is a legal entity id", func(t *testing.T) {
		rw, bind := pgRewriteFixture()
		// The fixture itself keys everything on RefID 0 — a nil-map or
		// "nonzero means present" bug would break it.
		if err := verifyPGExpectedLayout(rw, bind); err != nil {
			t.Fatalf("occurrence 0: %v", err)
		}
	})
}

// --- G7 type naming + public projection -------------------------------------

func TestPGColumnTypeName(t *testing.T) {
	tm := pgtype.NewMap()
	cases := []struct {
		oid  uint32
		want string
	}{
		{20, "INT8"}, {1700, "NUMERIC"}, {1184, "TIMESTAMPTZ"}, {3802, "JSONB"},
		{1009, "_TEXT"}, {17, "BYTEA"}, {16, "BOOL"}, {25, "TEXT"},
	}
	for _, tc := range cases {
		if got := pgColumnTypeName(tm, tc.oid); got != tc.want {
			t.Fatalf("oid %d = %q, want %q", tc.oid, got, tc.want)
		}
	}
	// An unrecognized OID keeps a stable marker — never a TEXT guess and
	// never a dropped column.
	got := pgColumnTypeName(tm, 4999999)
	if got != "oid:4999999" {
		t.Fatalf("unknown oid name = %q, want oid:4999999", got)
	}
	if got := pgColumnTypeName(nil, 20); got != "oid:20" {
		t.Fatalf("nil map name = %q, want oid:20", got)
	}
}

func TestPGProjectPublicResult(t *testing.T) {
	fds := []pgconn.FieldDescription{
		{Name: "id", TableOID: 100, TableAttributeNumber: 1, DataTypeOID: 20},
		{Name: "__chub_w0", DataTypeOID: 900},
	}
	rw, _ := pgRewriteFixture()
	id, item := "42", "widget"
	rows := [][]*string{{&id, nil}, {&item, nil}}
	out := projectPGPublicResult(fds, rows, rw, pgtype.NewMap())
	if len(out.Columns) != 1 || out.Columns[0].Name != "id" || out.Columns[0].DatabaseType != "INT8" {
		t.Fatalf("columns = %+v", out.Columns)
	}
	if len(out.Rows) != 2 || len(out.Rows[0]) != 1 || out.Rows[0][0] == nil || *out.Rows[0][0] != "42" {
		t.Fatalf("rows = %+v", out.Rows)
	}
}

func TestPGExecuteInputValidation(t *testing.T) {
	cfg := &pgx.ConnConfig{}
	for _, in := range []PGExecuteInput{
		{ConnConfig: nil, Database: "d", PinnedSchema: "s", Timeout: 1},
		{ConnConfig: cfg, Database: "", PinnedSchema: "s", Timeout: 1},
		{ConnConfig: cfg, Database: "d", PinnedSchema: "", Timeout: 1},
		{ConnConfig: cfg, Database: "d", PinnedSchema: "s", Timeout: 0},
		{ConnConfig: cfg, Database: "d", PinnedSchema: "s", Timeout: -1},
	} {
		if _, err := ExecutePGGoverned(context.Background(), in); !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("input %+v: err = %v, want ErrQueryValidationFailed", in, err)
		}
	}
}
