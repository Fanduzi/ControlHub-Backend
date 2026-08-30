// Package mysql provides tests for private full-statement execution persistence and retrieval.
// input: context, database/sql, errors, testing, sqlmock, internal/model
// output: successful-user-only full-statement insert arguments, owner-only retrieval, and mismatch denial tests
// pos: SQL privacy boundary for reusable successful user execution statements
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/fan/controlhub/internal/model"
)

func TestInsertExecutionWithAuditWritesFullStatementButNotAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectExec(`insert into query_executions\s+\(target_resource_id, actor_user_id, actor_machine_principal_id, engine, statement_digest, statement_preview, full_statement`).
		WithArgs(uint64(22), uint64(7), nil, "mysql", "digest", "preview", "\n SELECT  1 \t", "success", 1, int64(7), "", "", nil).
		WillReturnResult(sqlmock.NewResult(101, 1))
	mock.ExpectExec(`insert into audit_events \(actor_user_id, actor_machine_principal_id, target_resource_id, event_type, result\)`).
		WithArgs(uint64(7), nil, uint64(22), "query.executed", "success").
		WillReturnResult(sqlmock.NewResult(201, 1))
	mock.ExpectCommit()

	_, err = NewQueryExecutionRepository(db).InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: 22,
		ActorUserID:      7,
		Engine:           "mysql",
		StatementDigest:  "digest",
		StatementPreview: "preview",
		FullStatement:    "\n SELECT  1 \t",
		Status:           model.QueryExecutionSuccess,
		RowCount:         1,
		DurationMs:       7,
	}, "query.executed", "success")
	if err != nil {
		t.Fatalf("InsertExecutionWithAudit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestExecutionRecordArgsDropsFullStatementOutsideSuccessfulUserExecutions(t *testing.T) {
	tests := []struct {
		name   string
		record model.QueryExecutionRecord
	}{
		{name: "machine", record: model.QueryExecutionRecord{ActorMachinePrincipalID: 9, Status: model.QueryExecutionSuccess}},
		{name: "failed", record: model.QueryExecutionRecord{ActorUserID: 7, Status: model.QueryExecutionFailed}},
		{name: "rejected", record: model.QueryExecutionRecord{ActorUserID: 7, Status: model.QueryExecutionRejected}},
		{name: "timeout", record: model.QueryExecutionRecord{ActorUserID: 7, Status: model.QueryExecutionTimeout}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := "SELECT private_" + tt.name
			tt.record.FullStatement = secret
			args, err := executionRecordArgs(tt.record)
			if err != nil {
				t.Fatalf("executionRecordArgs: %v", err)
			}
			if args[6] != nil {
				t.Fatalf("full_statement arg = %q, want nil", args[6])
			}
			for _, arg := range args {
				if arg == secret {
					t.Fatal("execution insert args contain private full statement")
				}
			}
		})
	}
}

func TestGetSuccessfulExecutionStatementUsesExactOwnerOnlyPredicatesAndArgs(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(`SELECT full_statement FROM query_executions WHERE id = \? AND target_resource_id = \? AND actor_user_id = \? AND actor_machine_principal_id IS NULL AND status = 'success' AND full_statement IS NOT NULL`).
		WithArgs(uint64(91), uint64(22), uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"full_statement"}).AddRow("SELECT *\nFROM orders"))

	response, err := NewQueryExecutionRepository(db).GetSuccessfulExecutionStatement(context.Background(), 91, 22, 7)
	if err != nil || response.Statement != "SELECT *\nFROM orders" {
		t.Fatalf("GetSuccessfulExecutionStatement() = (%+v, %v)", response, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestGetSuccessfulExecutionStatementDeniesEveryMismatchAsNoRows(t *testing.T) {
	for _, name := range []string{"other user", "machine actor", "failed execution", "legacy null statement", "other target"} {
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()

			mock.ExpectQuery(`SELECT full_statement FROM query_executions`).
				WithArgs(uint64(91), uint64(22), uint64(7)).
				WillReturnError(sql.ErrNoRows)
			_, err = NewQueryExecutionRepository(db).GetSuccessfulExecutionStatement(context.Background(), 91, 22, 7)
			if !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("error = %v, want sql.ErrNoRows", err)
			}
		})
	}
}
