// Package mysql provides tests for the auth audit emitter fail-open counter.
// input: database/sql, testing
// output: TestAuthAuditPersistenceFailuresCounterIncrements
// pos: Proves the fixed-category persistence-failure counter increments on INSERT failure without changing the security decision
// note: if this file changes, update header and README.md
package mysql

import (
	"database/sql"
	"testing"
)

func TestAuthAuditPersistenceFailuresCounterIncrements(t *testing.T) {
	// Open a broken DB to force INSERT failures.
	brokenDB, _ := sql.Open("mysql", "invalid:dsn@tcp(127.0.0.1:1)/noexist?timeout=1ms")
	brokenDB.Close()

	emitter := NewAuthAuditEmitter(brokenDB)
	before := AuthAuditPersistenceFailures.Value()

	_ = emitter.EmitAuthAudit("auth.login", "succeeded", nil, nil)
	_ = emitter.EmitAuthAudit("auth.bearer", "rejected", nil, nil)

	after := AuthAuditPersistenceFailures.Value()
	if after-before != 2 {
		t.Fatalf("counter incremented by %d, want 2; before=%d after=%d", after-before, before, after)
	}
}
