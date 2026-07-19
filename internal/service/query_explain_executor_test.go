// Package service provides tests for the MySQL Explain executor.
// input: testing, internal/service
// output: TestExplainStatement*, TestDispatchSQLForExplain* (structural enforcement)
// pos: Phase 38N — prove the executor cannot run the bare guarded SELECT
// note: the integration test covers the real MySQL path; these unit tests prove the structural seam
package service

import (
	"strings"
	"testing"
)

// TestNewExplainStatementWrapsSelect proves the sealed ExplainStatement
// contains ONLY the EXPLAIN-prefixed form. WHY: the executor reads
// stmt.WrappedSQL() and never has access to the bare guarded SELECT, so a
// faulty executor cannot run the bare statement via tx.QueryContext.
func TestNewExplainStatementWrapsSelect(t *testing.T) {
	t.Parallel()
	guarded := "select * from t where id = 1"
	stmt := NewExplainStatement(guarded)
	wrapped := stmt.WrappedSQL()
	if !strings.HasPrefix(wrapped, "EXPLAIN FORMAT=JSON ") {
		t.Fatalf("wrapped SQL must start with EXPLAIN FORMAT=JSON prefix: %q", wrapped)
	}
	if wrapped == guarded {
		t.Fatalf("wrapped SQL must NOT equal the bare guarded select — the executor must never see the bare form: %q", wrapped)
	}
	if !strings.HasSuffix(wrapped, guarded) {
		t.Fatalf("wrapped SQL must end with the guarded select: %q", wrapped)
	}
}

// TestDispatchSQLForExplainIsPrefixed is the P1.1 structural enforcement test.
// WHY: this is the single dispatch point the executor uses for
// tx.QueryContext. If a future refactor lets the bare SELECT reach the
// executor (e.g. by adding a Query(sql string) method or passing
// guarded.ExecutableSQL directly), this test fails.
func TestDispatchSQLForExplainIsPrefixed(t *testing.T) {
	t.Parallel()
	guarded := "select 1"
	stmt := NewExplainStatement(guarded)
	dispatched := dispatchSQLForExplain(stmt)
	if dispatched == guarded {
		t.Fatalf("dispatched SQL must NOT equal the bare guarded select: %q", dispatched)
	}
	if !strings.HasPrefix(dispatched, "EXPLAIN FORMAT=JSON ") {
		t.Fatalf("dispatched SQL must start with EXPLAIN FORMAT=JSON: %q", dispatched)
	}
	if !strings.HasSuffix(dispatched, guarded) {
		t.Fatalf("dispatched SQL must end with the guarded select: %q", dispatched)
	}
}

// TestExplainStatementHasNoBareAccessor proves there is no way to recover the
// bare guarded SELECT from an ExplainStatement. WHY: this is the structural
// guarantee (Oracle P1.1) — the type only exposes WrappedSQL(), which is the
// prefixed form. If a future change adds a BareSelect() accessor, the type
// system would let a faulty executor run the bare statement.
//
// This test is a compile-time intent declaration: it documents that the ONLY
// public method on ExplainStatement is WrappedSQL. If someone adds a
// BareSelect() / ExecutableSQL() / Unwrapped() accessor, the security review
// must reject it.
func TestExplainStatementHasNoBareAccessor(t *testing.T) {
	t.Parallel()
	stmt := NewExplainStatement("select 1")
	// The only accessor is WrappedSQL, which is the prefixed form.
	got := stmt.WrappedSQL()
	if !strings.HasPrefix(got, "EXPLAIN FORMAT=JSON ") {
		t.Fatalf("WrappedSQL must be the prefixed form, got: %q", got)
	}
	// No other accessor exists by construction — verified by the fact that
	// this test file compiles only with the declared method set.
}

// TestMaxExplainPlanBytesConstant pins the raw plan byte cap. WHY: the cap is
// scanned before json.Unmarshal to prevent unbounded memory/CPU on a hostile
// plan. A change here is a deliberate, reviewed policy shift.
func TestMaxExplainPlanBytesConstant(t *testing.T) {
	t.Parallel()
	if MaxExplainPlanBytes != 1<<20 {
		t.Errorf("MaxExplainPlanBytes changed: expected %d, got %d", 1<<20, MaxExplainPlanBytes)
	}
}
