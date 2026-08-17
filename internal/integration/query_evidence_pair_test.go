//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 38X-3A
// atomic Execution Evidence Pair primitive (Issue #34).
// input: context, database/sql, bytes, log, strings, testing, Testcontainers MySQL, internal/model, internal/repository/mysql
// output: TestQueryEvidencePair* (pair commits both rows, rollback on audit/history failure, persistence-failure counter and fixed safe log)
// pos: Proves the repository-owned atomic Execution Evidence Pair and its failure telemetry against real MySQL state
// note: if this file changes, update header and README.md
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"log"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

// queryEvidencePairInsert inserts one pair via the atomic primitive and returns
// the committed execution id, failing the test on error.
func queryEvidencePairInsert(t *testing.T, db *sql.DB, targetID, actorID uint64, status model.QueryExecutionStatus) uint64 {
	t.Helper()
	repo := mysql.NewQueryExecutionRepository(db)
	id, err := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: targetID,
		ActorUserID:      actorID,
		Engine:           "mysql",
		StatementDigest:  "digest:" + string(status),
		StatementPreview: "preview:" + string(status),
		Status:           status,
		RowCount:         7,
	}, "query.executed", "success")
	if err != nil {
		t.Fatalf("insert execution with audit: %v", err)
	}
	return id
}

// TestQueryEvidencePairCommitsBothRows proves the atomic pair persists the
// execution-history row AND the corresponding fixed audit event in one
// transaction, and returns the committed execution id on success.
func TestQueryEvidencePairCommitsBothRows(t *testing.T) {
	db := setupTestDB(t)
	targetID := createQueryTargetResource(t, db, "qe-pair-commit")
	id := queryEvidencePairInsert(t, db, targetID, ownerDBA, model.QueryExecutionSuccess)

	var execRows, auditRows int
	if err := db.QueryRow(`select count(*) from query_executions where id = ?`, id).Scan(&execRows); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if err := db.QueryRow(`select count(*) from audit_events
		where target_resource_id = ? and actor_user_id = ? and event_type = 'query.executed' and result = 'success'`, targetID, ownerDBA).Scan(&auditRows); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if execRows != 1 {
		t.Fatalf("execution rows for id %d = %d, want 1", id, execRows)
	}
	if auditRows != 1 {
		t.Fatalf("audit rows for target %d = %d, want 1", targetID, auditRows)
	}
	if id == 0 {
		t.Fatal("returned execution id = 0, want the committed id")
	}
}

// TestQueryEvidencePairAuditFailureRollsBackBothRows proves the
// no-partial-evidence invariant against real MySQL: when the audit insert fails
// inside the pair transaction, the execution-history row is rolled back too —
// neither row commits. The pre-atomic split-write path left the history row
// orphaned in exactly this scenario (see Issue #34 RED proof).
func TestQueryEvidencePairAuditFailureRollsBackBothRows(t *testing.T) {
	db := setupTestDB(t)
	targetID := createQueryTargetResource(t, db, "qe-pair-rollback")

	// Force every audit_events INSERT to fail deterministically.
	if _, err := db.Exec(`create trigger ch34_force_audit_fail
		before insert on audit_events for each row
		signal sqlstate '45000' set message_text = 'forced audit failure'`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists ch34_force_audit_fail`) })

	repo := mysql.NewQueryExecutionRepository(db)
	_, err := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: targetID,
		ActorUserID:      ownerDBA,
		Engine:           "mysql",
		StatementDigest:  "digest:rollback",
		StatementPreview: "preview:rollback",
		Status:           model.QueryExecutionSuccess,
		RowCount:         1,
	}, "query.executed", "success")
	if err == nil {
		t.Fatal("expected the pair to fail when the audit insert fails")
	}
	// No raw driver/database details may reach the caller: the error must not
	// echo the trigger's message text or statement internals.
	if strings.Contains(err.Error(), "forced audit failure") {
		t.Fatalf("raw database error leaked to caller: %v", err)
	}

	// Database-state proof: neither the history row nor the audit row exists.
	var execRows, auditRows int
	if err := db.QueryRow(`select count(*) from query_executions where target_resource_id = ?`, targetID).Scan(&execRows); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ?`, targetID).Scan(&auditRows); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if execRows != 0 {
		t.Fatalf("execution rows = %d, want 0 (history must be rolled back with the failed audit)", execRows)
	}
	if auditRows != 0 {
		t.Fatalf("audit rows = %d, want 0", auditRows)
	}
}

// TestQueryEvidencePairPersistenceFailureCounterIncrementsOncePerPair proves
// the dimensionless counter increments exactly once per failed pair against
// real MySQL, and the failure telemetry log line is the one fixed safe
// category with no dynamic values.
func TestQueryEvidencePairPersistenceFailureCounterIncrementsOncePerPair(t *testing.T) {
	db := setupTestDB(t)
	targetID := createQueryTargetResource(t, db, "qe-pair-counter")

	if _, err := db.Exec(`create trigger ch34_force_audit_fail_counter
		before insert on audit_events for each row
		signal sqlstate '45000' set message_text = 'forced audit failure'`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists ch34_force_audit_fail_counter`) })

	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(origOut); log.SetFlags(origFlags) })

	repo := mysql.NewQueryExecutionRepository(db)
	before := mysql.QueryEvidencePersistenceFailures.Value()
	for i := 0; i < 2; i++ {
		if _, err := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  "digest:counter",
			StatementPreview: "preview:counter",
			Status:           model.QueryExecutionSuccess,
		}, "query.executed", "success"); err == nil {
			t.Fatalf("pair %d: expected failure under forced audit trigger", i+1)
		}
	}
	after := mysql.QueryEvidencePersistenceFailures.Value()
	if after-before != 2 {
		t.Fatalf("counter incremented by %d, want exactly 2 for two failed pairs", after-before)
	}

	// Exactly one fixed log line per failure, no sensitive values.
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2; output=%q", len(lines), buf.String())
	}
	for _, line := range lines {
		if !strings.Contains(line, "query_evidence_persistence_failed") {
			t.Fatalf("log line must use the fixed category: %q", line)
		}
		if strings.Contains(line, "forced audit failure") || strings.Contains(line, "digest:") || strings.Contains(line, "preview:") || strings.Contains(line, "45000") {
			t.Fatalf("log line leaks raw driver/database or evidence values: %q", line)
		}
	}
}

// TestQueryEvidencePairHistoryInsertFailure proves that when the FIRST write
// (the history row) itself fails, the pair errors and no audit row is left
// behind.
func TestQueryEvidencePairHistoryInsertFailure(t *testing.T) {
	db := setupTestDB(t)
	targetID := createQueryTargetResource(t, db, "qe-pair-historyfail")

	if _, err := db.Exec(`create trigger ch34_force_exec_fail
		before insert on query_executions for each row
		signal sqlstate '45000' set message_text = 'forced execution failure'`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists ch34_force_exec_fail`) })

	repo := mysql.NewQueryExecutionRepository(db)
	_, err := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: targetID,
		ActorUserID:      ownerDBA,
		Engine:           "mysql",
		StatementDigest:  "digest:historyfail",
		StatementPreview: "preview:historyfail",
		Status:           model.QueryExecutionSuccess,
	}, "query.executed", "success")
	if err == nil {
		t.Fatal("expected the pair to fail when the history insert fails")
	}
	var auditRows int
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ?`, targetID).Scan(&auditRows); err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if auditRows != 0 {
		t.Fatalf("audit rows = %d, want 0 (no audit without a history row)", auditRows)
	}
}
