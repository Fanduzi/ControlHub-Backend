// Package mysql provides tests for the Issue #34 (38X-3A) query-evidence
// persistence-failure telemetry: the dimensionless counter and the single
// fixed safe log category emitted by the atomic Execution Evidence Pair.
// input: bytes, database/sql, log, strings, testing
// output: TestQueryEvidencePersistenceFailuresCounterIncrementsOnce, TestQueryEvidencePersistenceFailureLogIsFixedAndSafe
// pos: Proves the failure telemetry is dimensionless and value-free (no actor, target, statement, value, credential, DSN, request, or raw error)
// note: if this file changes, update header and README.md
package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// TestQueryEvidencePersistenceFailuresCounterIncrementsOnce proves the pair
// primitive increments the dimensionless counter exactly once per failed pair
// and returns the failure so callers surface the existing controlled backend
// error. A broken DB forces a begin-transaction failure — no live MySQL needed.
func TestQueryEvidencePersistenceFailuresCounterIncrementsOnce(t *testing.T) {
	brokenDB, _ := sql.Open("mysql", "invalid:dsn@tcp(127.0.0.1:1)/noexist?timeout=1ms")
	brokenDB.Close()

	repo := NewQueryExecutionRepository(brokenDB)
	before := QueryEvidencePersistenceFailures.Value()

	_, err := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: 1,
		ActorUserID:      7,
		Engine:           "mysql",
		Status:           model.QueryExecutionSuccess,
	}, "success")
	if err == nil {
		t.Fatal("expected the pair to fail against a broken DB")
	}

	after := QueryEvidencePersistenceFailures.Value()
	if after-before != 1 {
		t.Fatalf("counter incremented by %d, want exactly 1; before=%d after=%d", after-before, before, after)
	}
}

// TestQueryEvidencePersistenceFailureLogIsFixedAndSafe captures the failure log
// line and proves it is the ONE fixed category with no dynamic sensitive value:
// no actor, target resource, statement, value, credential, DSN, request data,
// or raw database error text may appear.
func TestQueryEvidencePersistenceFailureLogIsFixedAndSafe(t *testing.T) {
	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(origOut); log.SetFlags(origFlags) })

	brokenDB, _ := sql.Open("mysql", "invalid:dsn@tcp(127.0.0.1:1)/noexist?timeout=1ms")
	brokenDB.Close()
	repo := NewQueryExecutionRepository(brokenDB)
	_, err := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: 1,
		ActorUserID:      7,
		Engine:           "mysql",
		StatementDigest:  "digest-leak-check",
		StatementPreview: "preview-leak-check",
		Status:           model.QueryExecutionSuccess,
	}, "success")
	if err == nil {
		t.Fatal("expected the pair to fail")
	}

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1 fixed line; output=%q", len(lines), buf.String())
	}
	line := lines[0]
	if !strings.Contains(line, "query_evidence_persistence_failed") {
		t.Fatalf("log line must use the fixed category query_evidence_persistence_failed: %q", line)
	}
	for _, forbidden := range []string{
		"digest-leak-check", "preview-leak-check", "1", "7", "invalid:dsn", "noexist",
		"actor", "target", "statement", "credential", "dsn", "sqlstate", "45000",
	} {
		if strings.Contains(line, forbidden) {
			t.Fatalf("log line leaks forbidden value %q: %q", forbidden, line)
		}
	}
}
