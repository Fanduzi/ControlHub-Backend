// Package service unit-covers the deterministic parts of the T7-S2 binding
// gate without a database.
// input: testing, context, time, errors, jackc/pgx/v5/pgconn
// output: unit tests for remaining-budget millisecond math, context deadline
// derivation, the cancelled/timeout/internal error classifier, and identifier
// quoting used by probe SQL
// pos: offline guarantee proofs for T7-S2 — the budget math must never emit a
// zero/disabling timeout, cancellation must never be reported as a remote
// stop, and quoted identifiers must stay byte-exact for catalog functions
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/pgsql"
)

func TestPGRemainingStatementTimeoutMs(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		deadline time.Time
		wantMs   int64
		wantOK   bool
	}{
		{"spent", now.Add(-time.Millisecond), 0, false},
		{"exactly now", now, 0, false},
		{"sub-millisecond rounds up", now.Add(1500 * time.Microsecond), 2, true},
		{"fractional ms rounds up", now.Add(1500*time.Millisecond + 1), 1501, true},
		{"integral ms", now.Add(2 * time.Second), 2000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ms, ok := pgRemainingStatementTimeoutMs(tc.deadline, now)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, want %v", ok, tc.wantOK)
			}
			if ok && ms != tc.wantMs {
				t.Fatalf("ms=%d, want %d", ms, tc.wantMs)
			}
			if ok && ms <= 0 {
				t.Fatal("positive remainder must never yield a zero timeout")
			}
		})
	}
}

func TestPGAttemptBudgetFromContext(t *testing.T) {
	if _, err := pgAttemptBudgetFromContext(context.Background()); err == nil {
		t.Fatal("context without a deadline must fail closed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	b, err := pgAttemptBudgetFromContext(ctx)
	if err != nil {
		t.Fatalf("deadline context: %v", err)
	}
	if b.deadline.IsZero() {
		t.Fatal("budget must carry the context deadline")
	}
}

func TestPGBindingGateFailClassifies(t *testing.T) {
	t.Run("reject passes through", func(t *testing.T) {
		g := &pgBindingGate{ctx: context.Background()}
		re := &pgsql.RejectError{Code: "query_object_not_found"}
		got := g.fail(re)
		var out *pgsql.RejectError
		if !errors.As(got, &out) || out.Code != "query_object_not_found" {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("57014 is timeout while context alive", func(t *testing.T) {
		g := &pgBindingGate{ctx: context.Background()}
		got := g.fail(&pgconn.PgError{Code: "57014"})
		if !errors.Is(got, ErrQueryTimeout) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("context deadline is timeout", func(t *testing.T) {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		g := &pgBindingGate{ctx: ctx}
		got := g.fail(errors.New("driver noise must not leak"))
		if !errors.Is(got, ErrQueryTimeout) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("caller cancel is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		g := &pgBindingGate{ctx: ctx}
		// Even a 57014 under a locally-canceled context reports cancellation,
		// never a claim the remote server stopped the statement.
		got := g.fail(&pgconn.PgError{Code: "57014"})
		if !errors.Is(got, context.Canceled) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("driver error is internal", func(t *testing.T) {
		g := &pgBindingGate{ctx: context.Background()}
		got := g.fail(errors.New("password=supersecret dsn detail"))
		if !errors.Is(got, ErrQueryBackendFailure) {
			t.Fatalf("got %v", got)
		}
		if got.Error() == "password=supersecret dsn detail" {
			t.Fatal("driver text leaked")
		}
	})
	t.Run("nil stays nil", func(t *testing.T) {
		g := &pgBindingGate{ctx: context.Background()}
		if g.fail(nil) != nil {
			t.Fatal("nil error must stay nil")
		}
	})
}

func TestPGIdentifierQuoting(t *testing.T) {
	if got := pgQualifiedName("app", "orders"); got != `"app"."orders"` {
		t.Fatalf("got %q", got)
	}
	if got := pgQualifiedName(`we"ird`, `ta"ble`); got != `"we""ird"."ta""ble"` {
		t.Fatalf("got %q", got)
	}
	if got := pgQuoteIdent(`a"b`); got != `"a""b"` {
		t.Fatalf("got %q", got)
	}
}

func TestPGBindResultApprovedDedupAndSort(t *testing.T) {
	g := &pgBindingGate{dbOID: 100}
	shared := &pgBoundRelation{oid: 7, shared: true}
	local := &pgBoundRelation{oid: 9}
	g.refs = map[pgsql.RefID]*PGBoundRef{
		1: {Identity: g.identityOf(shared)},
		2: {Identity: g.identityOf(local)},
		3: {Identity: g.identityOf(local)}, // duplicate approved identity
	}
	g.deps = []PGViewDependency{{RelationOID: 9}, {RelationOID: 10}}
	g.inherits = []PGInheritEdge{{ParentOID: 9, ChildOID: 11}}
	res := g.result()
	want := []PGRelationIdentity{
		{DatabaseOID: 0, RelationOID: 7},
		{DatabaseOID: 100, RelationOID: 9},
		{DatabaseOID: 100, RelationOID: 10},
		{DatabaseOID: 100, RelationOID: 11},
	}
	if len(res.Approved) != len(want) {
		t.Fatalf("approved=%v, want %v", res.Approved, want)
	}
	for i := range want {
		if res.Approved[i] != want[i] {
			t.Fatalf("approved[%d]=%v, want %v", i, res.Approved[i], want[i])
		}
	}
}
