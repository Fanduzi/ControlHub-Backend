// Package mysql provides tests for atomic query-evidence persistence.
// input: bytes, context, database/sql, errors, log, strings, testing, time, sqlmock, internal/model
// output: machine identity atomic pair/rollback/history tests plus dimensionless persistence-failure counter and fixed safe log tests
// pos: Proves truthful actor persistence/projection, pair atomicity, and value-free failure telemetry
// note: if this file changes, update header and README.md
package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/model"
)

func TestInsertExecutionWithAuditMachineIdentityCommitsAtomicPair(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`insert into query_executions\s+\(target_resource_id, actor_user_id, actor_machine_principal_id`).
		WithArgs(uint64(22), nil, uint64(91), "mysql", "digest", "preview", nil, "success", 1, int64(7), "", "", nil).
		WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec(`insert into audit_events \(actor_user_id, actor_machine_principal_id, target_resource_id, event_type, result\)`).
		WithArgs(nil, uint64(91), uint64(22), "query.executed", "success").
		WillReturnResult(sqlmock.NewResult(201, 1))
	mock.ExpectCommit()

	id, err := NewQueryExecutionRepository(db).InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID:        22,
		ActorMachinePrincipalID: 91,
		Engine:                  "mysql",
		StatementDigest:         "digest",
		StatementPreview:        "preview",
		Status:                  model.QueryExecutionSuccess,
		RowCount:                1,
		DurationMs:              7,
	}, "query.executed", "success")
	if err != nil || id != 101 {
		t.Fatalf("InsertExecutionWithAudit() = (%d, %v), want (101, nil)", id, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestInsertExecutionWithAuditMachineAuditFailureRollsBackHistory(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`insert into query_executions`).
		WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec(`insert into audit_events`).
		WillReturnError(errors.New("forced audit failure"))
	mock.ExpectRollback()

	_, err = NewQueryExecutionRepository(db).InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID:        22,
		ActorMachinePrincipalID: 91,
		Engine:                  "mysql",
		Status:                  model.QueryExecutionSuccess,
	}, "query.executed", "success")
	if err == nil {
		t.Fatal("InsertExecutionWithAudit() error = nil, want rollback failure")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestListExecutionsProjectsMachinePrincipalIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	createdAt := time.Date(2026, 8, 30, 6, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT qe\.id, qe\.target_resource_id, qe\.actor_user_id, qe\.actor_machine_principal_id.*LEFT JOIN machine_principals mp ON mp\.id = qe\.actor_machine_principal_id`).
		WithArgs(uint64(22), 20).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "target_resource_id", "actor_user_id", "actor_machine_principal_id", "engine",
			"statement_digest", "statement_preview", "status", "row_count", "duration_ms",
			"error_code", "error_message", "created_at", "user_display_name", "machine_name",
		}).AddRow(101, 22, nil, 91, "mysql", "digest", "preview", "success", 1, 7, "", "", createdAt, nil, "inventory-agent"))

	items, _, err := NewQueryExecutionRepository(db).ListExecutions(context.Background(), model.QueryExecutionListQuery{
		TargetResourceID: 22,
		PageSize:         20,
		Mode:             model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	item := items[0]
	if item.ActorUserID != 0 || item.ActorMachinePrincipalID != 91 {
		t.Fatalf("history identity = user:%d machine:%d, want machine principal 91 only", item.ActorUserID, item.ActorMachinePrincipalID)
	}
	if item.Actor.Kind != model.QueryExecutionActorMachine || item.Actor.DisplayName != "inventory-agent" {
		t.Fatalf("history actor = %+v, want machine inventory-agent", item.Actor)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

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
	}, "query.executed", "success")
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
	}, "query.executed", "success")
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
