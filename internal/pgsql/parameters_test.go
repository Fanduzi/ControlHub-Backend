// Package pgsql proves the trusted text-parameter contract offline.
// input: GuardPG results over $k-bearing and bare statements, stub resolvers
// output: ValidateParameterBindings coverage/matrix tests and rewrite
// parameter-set integrity proofs (qualify/inject/round-trip keep the same
// numeric set; duplicated references stay legal)
// pos: PG-MULTI-ENTRY stage 1 — parameter numbers bind by $k identity, never
// traversal order; an unbound count permits no ParamRef at all
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func guardForTest(t *testing.T, stmt string) *GuardResult {
	t.Helper()
	gr, err := GuardPG(stmt)
	if err != nil {
		t.Fatalf("GuardPG(%q): %v", stmt, err)
	}
	return gr
}

// A bound plan owes a dense {1..count} reference set: every declared number
// must appear, no number may exceed the bound set, and an unbound statement
// must carry no ParamRef at all.
func TestValidateParameterBindings(t *testing.T) {
	t.Parallel()

	dense := guardForTest(t, `SELECT $1::text, $2::int`)
	if err := ValidateParameterBindings(dense, 2); err != nil {
		t.Fatalf("dense {1,2} with count 2 rejected: %v", err)
	}
	if err := ValidateParameterBindings(dense, 1); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("$2 beyond bound set: err = %v, want evidence-unavailable", err)
	}
	if err := ValidateParameterBindings(dense, 3); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("unused $3: err = %v, want evidence-unavailable", err)
	}

	// A gap inside the range fails the same way as one beyond it — $1 must
	// still be bound even when $2 is present.
	gap := guardForTest(t, `SELECT $2::text`)
	if err := ValidateParameterBindings(gap, 2); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("gap at $1: err = %v, want evidence-unavailable", err)
	}

	// An ordinary statement carries no parameters: count 0 passes, and any
	// native $k is an out-of-range reference that fails closed before wire.
	bare := guardForTest(t, `SELECT 1`)
	if err := ValidateParameterBindings(bare, 0); err != nil {
		t.Fatalf("bare statement with count 0 rejected: %v", err)
	}
	if err := ValidateParameterBindings(bare, 1); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("unused bound value: err = %v, want evidence-unavailable", err)
	}
	native := guardForTest(t, `SELECT $1::text`)
	if err := ValidateParameterBindings(native, 0); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("native $1 unbound: err = %v, want evidence-unavailable", err)
	}
}

// Duplicate references to one number are legitimate — the contract is the
// numeric identity set, never a node count.
func TestValidateParameterBindings_DuplicateReferences(t *testing.T) {
	t.Parallel()

	dup := guardForTest(t, `SELECT $1::text AS a, $1::text AS b`)
	if err := ValidateParameterBindings(dup, 1); err != nil {
		t.Fatalf("duplicated $1 with count 1 rejected: %v", err)
	}
	mixed := guardForTest(t, `SELECT $1::text, $2::int, $1::text`)
	if err := ValidateParameterBindings(mixed, 2); err != nil {
		t.Fatalf("duplicated $1 inside {1,2} rejected: %v", err)
	}
}

// The binding check revalidates the GuardResult's immutable fingerprint — a
// nil or mutated result can never stand in for the analysis of record.
func TestValidateParameterBindings_GuardIntegrity(t *testing.T) {
	t.Parallel()

	if err := ValidateParameterBindings(nil, 0); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("nil guard: err = %v, want evidence-unavailable", err)
	}
	if err := ValidateParameterBindings(guardForTest(t, `SELECT 1`), -1); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("negative count: err = %v, want evidence-unavailable", err)
	}
	gr := guardForTest(t, `SELECT $1::text`)
	other := guardForTest(t, `SELECT 2`)
	gr.Tree = other.Tree
	if err := ValidateParameterBindings(gr, 1); !errors.Is(err, ErrEvidenceUnavailable) {
		t.Fatalf("swapped tree: err = %v, want evidence-unavailable", err)
	}
}

// paramRefMultiset counts ParamRef occurrences by number — the traversal
// view the contract deliberately never compares on.
func paramRefMultiset(t *testing.T, stmt string) map[int32]int {
	t.Helper()
	gr := guardForTest(t, stmt)
	out := map[int32]int{}
	walkTree(gr.Tree.ProtoReflect(), func(ctx *walkCtx, m protoreflect.Message) bool {
		if pr, ok := m.Interface().(*pg.ParamRef); ok {
			out[pr.GetNumber()]++
		}
		return true
	})
	return out
}

// Parameters survive the full rewrite pipeline at every structural position
// that can hold them — CTE bodies, JOIN conditions, DISTINCT Q+D, and ORDER
// BY — with the same numeric set reaching the transport text. Node counts
// may grow through legitimate duplication (set identity only); no number may
// appear or vanish.
func TestRewrite_ParameterSetIntegrity(t *testing.T) {
	t.Parallel()

	res := stubResolver{
		"app.orders":    {"id", "item", "qty"},
		"app.smallvals": {"id", "tag"},
	}
	for _, tc := range []struct {
		name string
		stmt string
		want map[int32]struct{}
	}{
		{"constant params", `SELECT $1::text, $2::int`, map[int32]struct{}{1: {}, 2: {}}},
		{"order by ordinal", `SELECT $1::text AS v ORDER BY 1`, map[int32]struct{}{1: {}}},
		{"order by param", `SELECT $1::int AS v ORDER BY $1::int`, map[int32]struct{}{1: {}}},
		{"cte body", `WITH c AS (SELECT $1::text AS v) SELECT c.v FROM c`, map[int32]struct{}{1: {}}},
		{"distinct q+d", `SELECT DISTINCT $1::text`, map[int32]struct{}{1: {}}},
		{"distinct q+d with where", `SELECT DISTINCT $1::text AS v FROM orders o WHERE o.qty > $2::int`,
			map[int32]struct{}{1: {}, 2: {}}},
		{"join condition", `SELECT o.id FROM orders o JOIN smallvals s ON s.id = o.id AND s.tag = $1::text`,
			map[int32]struct{}{1: {}}},
		{"same param twice", `SELECT $1::text AS a, $1::text AS b`, map[int32]struct{}{1: {}}},
		{"set-op leaves", `SELECT $1::text AS v UNION ALL SELECT $1::text`, map[int32]struct{}{1: {}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srcSet := paramRefMultiset(t, tc.stmt)
			if len(srcSet) != len(tc.want) {
				t.Fatalf("source param set = %v, want %v", srcSet, tc.want)
			}
			rw, err := Rewrite(tc.stmt, "app", res)
			if err != nil {
				t.Fatalf("Rewrite: %v", err)
			}
			gotSet := paramRefMultiset(t, rw.SQL)
			if len(gotSet) != len(tc.want) {
				t.Fatalf("transport param set = %v, want numbers %v", gotSet, tc.want)
			}
			for n := range tc.want {
				if gotSet[n] < 1 {
					t.Fatalf("transport lost parameter %d: %v", n, gotSet)
				}
			}
			for n := range gotSet {
				if _, ok := tc.want[n]; !ok {
					t.Fatalf("transport invented parameter %d: %v", n, gotSet)
				}
			}
			// Generated $k must deparse to the same binding syntax — the
			// transport text is what the server binds, not :name markers.
			for n := range tc.want {
				marker := fmt.Sprintf("$%d", n)
				if !strings.Contains(rw.SQL, marker) {
					t.Fatalf("transport SQL lacks %s: %q", marker, rw.SQL)
				}
			}
		})
	}
}
