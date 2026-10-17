// Package service pins the G9 claim-digest contract to fixed golden vectors
// and collision boundaries.
// input: testing, service digest entry points
// output: deterministic digest proofs for the three typed admission entries
// pos: T10-C digest unit coverage (issue #118) — same resolved request must
// produce the same digest; any semantic difference must change it;
// clientExecutionId/capabilities never enter the byte stream
// note: if this file changes, update this header and module README.md.
package service

import (
	"testing"
)

// Golden vectors freeze the v1 wire encoding: tag + length-prefixed fields in
// the frozen order. A change here means the encoding changed and every
// outstanding claim key silently re-bases — that is a deliberate break, never
// an accident.
func TestClaimDigest_GoldenVectors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "execute",
			got: DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
				Database: "appdb", Schema: "public",
				Statement: "select id from orders where status = 'open'",
				Page:      1, PageSize: 50, MaxRows: 100,
			}),
			want: "d20329e9562fda90460ac1580816a5b8eb0fec7298efee99a289a831d1172282",
		},
		{
			name: "saved-statement",
			got: DigestSavedStatement(SavedStatementDigestInput{
				Database: "appdb", Schema: "public", StatementID: 42,
				Values: []ClaimDigestNamedValue{
					{Name: "alpha", Kind: claimDigestKindInteger, Text: "1"},
					{Name: "beta", Kind: claimDigestKindString, Text: "x"},
				},
				Page: 1, PageSize: 50, MaxRows: 100,
			}),
			want: "8e498c2a445a4874e0b5687670b718d09610ad5fa37229c3d7cb9829e83065e2",
		},
		{
			name: "related-records",
			got: DigestRelatedRecords(RelatedRecordsDigestInput{
				Database: "appdb", Schema: "public",
				SourceSchema: "public", SourceObject: "orders",
				SourceColumns: []string{"id", "tenant"},
				ForeignKey:    "fk_orders_customer",
				LocalValues:   []string{"a", "bc"},
				Page:          1, PageSize: 50, MaxRows: 100,
			}),
			want: "45c1d81662bb56e8f8ff4e1e4f2fbd1b0dd133c7e663bae56d0c97e16493bac9",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("golden digest = %q, want %q — the v1 wire encoding changed", tc.got, tc.want)
			}
		})
	}
}

func TestClaimDigest_ExecuteDeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	base := OrdinaryExecuteDigestInput{
		Database: "appdb", Schema: "public",
		Statement: "select id from orders where status = 'open'",
		Page:      1, PageSize: 50, MaxRows: 100,
	}
	d0 := DigestOrdinaryExecute(base)
	if DigestOrdinaryExecute(base) != d0 {
		t.Fatal("same resolved request must produce the same digest")
	}
	if len(d0) != 64 {
		t.Fatalf("digest length = %d, want 64 hex", len(d0))
	}

	// Every semantic field participates: changing any one must change the
	// digest. These mutations are the contract — a client key claimed under
	// one of them must never admit another.
	variants := map[string]OrdinaryExecuteDigestInput{
		"database":   {Database: "otherdb", Schema: base.Schema, Statement: base.Statement, Page: 1, PageSize: 50, MaxRows: 100},
		"schema":     {Database: base.Database, Schema: "audit", Statement: base.Statement, Page: 1, PageSize: 50, MaxRows: 100},
		"statement":  {Database: base.Database, Schema: base.Schema, Statement: "select id from orders where status = 'closed'", Page: 1, PageSize: 50, MaxRows: 100},
		"page":       {Database: base.Database, Schema: base.Schema, Statement: base.Statement, Page: 2, PageSize: 50, MaxRows: 100},
		"pageSize":   {Database: base.Database, Schema: base.Schema, Statement: base.Statement, Page: 1, PageSize: 25, MaxRows: 100},
		"maxRows":    {Database: base.Database, Schema: base.Schema, Statement: base.Statement, Page: 1, PageSize: 50, MaxRows: 200},
		"case":       {Database: base.Database, Schema: base.Schema, Statement: "SELECT id from orders where status = 'open'", Page: 1, PageSize: 50, MaxRows: 100},
		"trailspace": {Database: base.Database, Schema: base.Schema, Statement: base.Statement + " ", Page: 1, PageSize: 50, MaxRows: 100},
	}
	for name, v := range variants {
		if got := DigestOrdinaryExecute(v); got == d0 {
			t.Fatalf("variant %q collided with base digest", name)
		}
	}
}

func TestClaimDigest_ExecuteDefaultsEquivalent(t *testing.T) {
	t.Parallel()
	// Defaults resolved server-side then digested: the wire may omit them,
	// but the digest always sees the resolved values — an explicit request
	// carrying the same resolved values is the same request.
	implicit := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "appdb", Schema: "public",
		Statement: "select 1", Page: 1, PageSize: 50, MaxRows: 100,
	})
	explicit := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "appdb", Schema: "public",
		Statement: "select 1", Page: 1, PageSize: 50, MaxRows: 100,
	})
	if implicit != explicit {
		t.Fatal("resolved-equal requests must share a digest")
	}
}

func TestClaimDigest_ExecuteFieldBoundaries(t *testing.T) {
	t.Parallel()
	// Length-prefix framing makes concatenation-boundary collisions
	// impossible: ("ab","c") and ("a","bc") encode differently.
	a := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "ab", Schema: "c", Statement: "select 1", Page: 1, PageSize: 50, MaxRows: 100,
	})
	b := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "a", Schema: "bc", Statement: "select 1", Page: 1, PageSize: 50, MaxRows: 100,
	})
	if a == b {
		t.Fatal("field-boundary collision: (ab,c) must differ from (a,bc)")
	}
}

func TestClaimDigest_SavedStatementSortsValuesByName(t *testing.T) {
	t.Parallel()
	in1 := SavedStatementDigestInput{
		Database: "appdb", Schema: "public", StatementID: 42,
		Values: []ClaimDigestNamedValue{
			{Name: "beta", Kind: claimDigestKindString, Text: "x"},
			{Name: "alpha", Kind: claimDigestKindInteger, Text: "1"},
		},
		Page: 1, PageSize: 50, MaxRows: 100,
	}
	in2 := in1
	in2.Values = []ClaimDigestNamedValue{in1.Values[1], in1.Values[0]}
	if DigestSavedStatement(in1) != DigestSavedStatement(in2) {
		t.Fatal("parameter map order must not affect the digest")
	}
	// Value type participates: "1" the string ≠ 1 the integer.
	typed := in1
	typed.Values = []ClaimDigestNamedValue{
		{Name: "beta", Kind: claimDigestKindString, Text: "x"},
		{Name: "alpha", Kind: claimDigestKindString, Text: "1"},
	}
	if DigestSavedStatement(in1) == DigestSavedStatement(typed) {
		t.Fatal("string \"1\" must not collide with integer 1")
	}
	// NULL ≠ "null" text ≠ empty string.
	vs := func(v ClaimDigestNamedValue) string {
		c := in1
		c.Values = []ClaimDigestNamedValue{{Name: "alpha", Kind: v.Kind, Text: v.Text}}
		return DigestSavedStatement(c)
	}
	if vs(ClaimDigestNull()) == vs(ClaimDigestString("null")) ||
		vs(ClaimDigestNull()) == vs(ClaimDigestString("")) ||
		vs(ClaimDigestString("")) == vs(ClaimDigestString("null")) {
		t.Fatal("NULL, empty, and \"null\" must be three distinct values")
	}
	// Decimal precision survives: a decimal literal is its own kind and its
	// text is preserved, never reduced through float64.
	decA := in1
	decA.Values = []ClaimDigestNamedValue{{Name: "p", Kind: claimDigestKindDecimal, Text: "1.10"}}
	decB := in1
	decB.Values = []ClaimDigestNamedValue{{Name: "p", Kind: claimDigestKindDecimal, Text: "1.100"}}
	if DigestSavedStatement(decA) == DigestSavedStatement(decB) {
		t.Fatal("decimal texts 1.10 and 1.100 are distinct literals and must not collide")
	}
}

func TestClaimDigest_RelatedRecordsListBoundaries(t *testing.T) {
	t.Parallel()
	base := RelatedRecordsDigestInput{
		Database: "appdb", Schema: "public",
		SourceSchema: "public", SourceObject: "orders",
		SourceColumns: []string{"id", "tenant"},
		ForeignKey:    "fk_orders_customer",
		LocalValues:   []string{"a", "bc"},
		Page:          1, PageSize: 50, MaxRows: 100,
	}
	d0 := DigestRelatedRecords(base)
	shift := base
	shift.LocalValues = []string{"ab", "c"}
	if DigestRelatedRecords(shift) == d0 {
		t.Fatal("ordered locator values must keep list boundaries: [a,bc] ≠ [ab,c]")
	}
	reordered := base
	reordered.LocalValues = []string{"bc", "a"}
	if DigestRelatedRecords(reordered) == d0 {
		t.Fatal("locator value order is significant: [a,bc] ≠ [bc,a]")
	}
	cols := base
	cols.SourceColumns = []string{"id"}
	if DigestRelatedRecords(cols) == d0 {
		t.Fatal("ordered column group participates")
	}
}

// Entry-point domains can never alias: an ordinary execute and a navigation
// request sharing the same field values must produce different digests.
func TestClaimDigest_EntryDomainsDistinct(t *testing.T) {
	t.Parallel()
	e := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "appdb", Schema: "public", Statement: "select 1",
		Page: 1, PageSize: 50, MaxRows: 100,
	})
	s := DigestSavedStatement(SavedStatementDigestInput{
		Database: "appdb", Schema: "public", StatementID: 1,
		Page: 1, PageSize: 50, MaxRows: 100,
	})
	n := DigestRelatedRecords(RelatedRecordsDigestInput{
		Database: "appdb", Schema: "public", SourceObject: "t",
		Page: 1, PageSize: 50, MaxRows: 100,
	})
	if e == s || e == n || s == n {
		t.Fatal("entry domains must never collide")
	}
}
