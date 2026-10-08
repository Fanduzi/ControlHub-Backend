// Package mysql provides MySQL-backed repository implementations.
// input: context, database/sql driver stubs, errors, testing, time, sqlmock, go-sql-driver/mysql, internal/model
// output: unit tests for claim admission validation, finalize stage failures (commit/rollback), and duplicate-key classification
// pos: Mocked-DB coverage of claim-protocol error paths that real-MySQL injection cannot reach (commit error, driver-level dup classification)
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	driver "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
)

func claimTestInput() model.QueryExecutionClaimInput {
	return model.QueryExecutionClaimInput{
		TargetResourceID:  42,
		ClientExecutionID: "claim-key",
		Identity:          model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: 7},
		DatabaseName:      "claimdb",
		SchemaName:        "app",
		RequestDigest:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ClaimedAt:         time.Now().UTC(),
	}
}

func keyedClaimRecord(key string) model.QueryExecutionRecord {
	k := key
	return model.QueryExecutionRecord{
		TargetResourceID:  42,
		ActorUserID:       7,
		Engine:            "postgresql",
		DatabaseName:      "claimdb",
		SchemaName:        "app",
		StatementDigest:   "d",
		StatementPreview:  "p",
		Status:            model.QueryExecutionSuccess,
		ClientExecutionID: &k,
	}
}

// Validation failures return the invalid sentinel before any SQL is issued.
func TestClaimValidationSkipsDatabase(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQueryExecutionRepository(db)
	in := claimTestInput()
	in.RequestDigest = "short"
	if err := repo.TryClaimExecution(context.Background(), in); !errors.Is(err, ErrQueryExecutionClaimInvalid) {
		t.Fatalf("invalid claim = %v, want ErrQueryExecutionClaimInvalid", err)
	}
	rec := keyedClaimRecord("")
	if _, err := repo.FinalizeClaimWithAudit(context.Background(), rec, "query.executed", "success"); !errors.Is(err, ErrQueryExecutionClaimInvalid) {
		t.Fatalf("keyless finalize = %v, want ErrQueryExecutionClaimInvalid", err)
	}
	rec = keyedClaimRecord("x")
	rec.Status = "running"
	if _, err := repo.FinalizeClaimWithAudit(context.Background(), rec, "query.executed", "success"); !errors.Is(err, ErrQueryExecutionClaimInvalid) {
		t.Fatalf("running finalize = %v, want ErrQueryExecutionClaimInvalid", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("validation must not reach the database: %v", err)
	}
}

// Claim-store failures return a fixed safe error: injected markers and driver
// details never reach the caller, while context cancellation stays
// classifiable and is never disguised as Exists/Conflict/ErrNoRows.
func TestClaimStoreErrorsAreSafeAndClassifiable(t *testing.T) {
	const marker = "SECRET-dsn-payload-9f3"
	in := claimTestInput()
	ctx := context.Background()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQueryExecutionRepository(db)

	// 1. Non-duplicate INSERT failure carrying a marker AND a wrapped ctx
	// sentinel — classification must survive without leaking the marker text.
	mock.ExpectExec(`INSERT INTO query_execution_claims`).
		WillReturnError(fmt.Errorf("conn reset %s: %w", marker, context.DeadlineExceeded))
	if err := repo.TryClaimExecution(ctx, in); err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("insert failure leaked details or succeeded: %v", err)
	} else if errors.Is(err, ErrQueryExecutionClaimExists) || errors.Is(err, ErrQueryExecutionClaimConflict) || errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("store failure disguised as admission outcome: %v", err)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("insert failure lost deadline classification: %v", err)
	}

	// 2. Digest-read failure after a PK conflict.
	dup := &driver.MySQLError{Number: 1062, Message: "dup"}
	mock.ExpectExec(`INSERT INTO query_execution_claims`).WillReturnError(dup)
	mock.ExpectQuery(`SELECT request_digest`).
		WillReturnError(fmt.Errorf("timeout %s: %w", marker, context.DeadlineExceeded))
	if err := repo.TryClaimExecution(ctx, in); err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("conflict-read failure leaked details or succeeded: %v", err)
	} else if errors.Is(err, ErrQueryExecutionClaimExists) || errors.Is(err, ErrQueryExecutionClaimConflict) {
		t.Fatalf("conflict-read failure disguised as admission outcome: %v", err)
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("conflict-read lost deadline classification: %v", err)
	}

	// 3. Point-read query/scan failure — empty view, marker-free, classified.
	mock.ExpectQuery(`SELECT .* FROM query_execution_claims`).
		WillReturnError(fmt.Errorf("driver exploded %s: %w", marker, context.Canceled))
	view, err := repo.GetClaimWithExecution(ctx, 42, "claimdb", "k", model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: 7})
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatalf("read failure leaked details or succeeded: %v", err)
	} else if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("read failure disguised as absence: %v", err)
	} else if !errors.Is(err, context.Canceled) {
		t.Fatalf("read failure lost cancel classification: %v", err)
	}
	if view.Execution != nil || view.Claim.ExecutionID != 0 || view.Claim.ClientExecutionID != "" {
		t.Fatalf("failed read returned a non-empty view: %+v", view)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}

	// Cancellation/deadline stay classifiable through the safe wrapper.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := repo.TryClaimExecution(cancelled, in); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled claim = %v, want classifiable context.Canceled", err)
	}
	expired, stop := context.WithTimeout(ctx, time.Nanosecond)
	defer stop()
	time.Sleep(time.Millisecond)
	if _, err := repo.GetClaimWithExecution(expired, 42, "claimdb", "k", model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: 7}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired read = %v, want classifiable DeadlineExceeded", err)
	}
}

// claimStoreError must never carry a raw lower-layer error in its unwrap
// chain: a marker-bearing error that merely WRAPS a context sentinel is still
// a leak. Only the bare sentinel may be re-wrapped for classification.
func TestClaimStoreErrorDropsWrappedContextText(t *testing.T) {
	const marker = "SECRET-marker-9f3"
	errMarked := errors.New("dial tcp dsn=" + marker)
	live := context.Background()

	cases := []struct {
		name      string
		ctx       context.Context
		err       error
		wantIs    error // errors.Is must match
		wantIsNot error // errors.Is must NOT match
	}{
		{"plain marker error", live, errMarked, nil, errMarked},
		{"direct canceled", live, context.Canceled, context.Canceled, errMarked},
		{"direct deadline", live, context.DeadlineExceeded, context.DeadlineExceeded, errMarked},
		{"wrapped deadline w/ marker", live,
			fmt.Errorf("driver %w", fmt.Errorf("net %s: %w", marker, context.DeadlineExceeded)),
			context.DeadlineExceeded, errMarked},
		{"wrapped canceled w/ marker", live,
			fmt.Errorf("outer %w", fmt.Errorf("%s %w", marker, context.Canceled)),
			context.Canceled, errMarked},
		{"joined marker + canceled", live,
			errors.Join(errMarked, context.Canceled),
			context.Canceled, errMarked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimStoreError(tc.ctx, tc.err)
			if strings.Contains(got.Error(), marker) {
				t.Fatalf("marker leaked in error text: %q", got.Error())
			}
			if !errors.Is(got, errQueryExecutionClaimStore) {
				t.Fatalf("missing store sentinel: %v", got)
			}
			if tc.wantIs != nil && !errors.Is(got, tc.wantIs) {
				t.Fatalf("lost %v classification: %v", tc.wantIs, got)
			}
			if errors.Is(got, tc.wantIsNot) {
				t.Fatalf("raw marked error still in unwrap chain: %v", got)
			}
		})
	}

	// Cancelled/expired parent ctx keeps priority and stays classifiable.
	cancelled, cancel := context.WithCancel(live)
	cancel()
	got := claimStoreError(cancelled, errMarked)
	if !errors.Is(got, context.Canceled) || strings.Contains(got.Error(), marker) {
		t.Fatalf("cancelled parent ctx = %v", got)
	}
	expired, stop := context.WithTimeout(live, time.Nanosecond)
	defer stop()
	time.Sleep(time.Millisecond)
	got = claimStoreError(expired, errors.New("boom "+marker))
	if !errors.Is(got, context.DeadlineExceeded) || strings.Contains(got.Error(), marker) {
		t.Fatalf("expired parent ctx = %v", got)
	}
}

// A duplicate-key claim insert reads the stored digest: same → Exists,
// different → Conflict; a failed conflict read is a safe internal error.
func TestTryClaimExecutionDuplicateClassification(t *testing.T) {
	dup := &driver.MySQLError{Number: 1062, Message: "Duplicate entry"}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQueryExecutionRepository(db)
	ctx := context.Background()
	in := claimTestInput()

	mock.ExpectExec(`INSERT INTO query_execution_claims`).WillReturnError(dup)
	mock.ExpectQuery(`SELECT request_digest FROM query_execution_claims`).
		WithArgs(in.TargetResourceID, in.ClientExecutionID).
		WillReturnRows(sqlmock.NewRows([]string{"request_digest"}).AddRow(in.RequestDigest))
	if err := repo.TryClaimExecution(ctx, in); !errors.Is(err, ErrQueryExecutionClaimExists) {
		t.Fatalf("same digest = %v, want ErrQueryExecutionClaimExists", err)
	}

	mock.ExpectExec(`INSERT INTO query_execution_claims`).WillReturnError(dup)
	mock.ExpectQuery(`SELECT request_digest FROM query_execution_claims`).
		WithArgs(in.TargetResourceID, in.ClientExecutionID).
		WillReturnRows(sqlmock.NewRows([]string{"request_digest"}).AddRow("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if err := repo.TryClaimExecution(ctx, in); !errors.Is(err, ErrQueryExecutionClaimConflict) {
		t.Fatalf("different digest = %v, want ErrQueryExecutionClaimConflict", err)
	}

	mock.ExpectExec(`INSERT INTO query_execution_claims`).WillReturnError(dup)
	mock.ExpectQuery(`SELECT request_digest FROM query_execution_claims`).
		WillReturnError(errors.New("conn reset"))
	if err := repo.TryClaimExecution(ctx, in); err == nil ||
		errors.Is(err, ErrQueryExecutionClaimExists) || errors.Is(err, ErrQueryExecutionClaimConflict) {
		t.Fatalf("conflict-read failure = %v, want safe internal error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Each finalize stage failure rolls back; a commit failure reports the pair
// sentinel without retrying; a zero-row link refuses as non-occupant.
func TestFinalizeClaimWithAuditStageFailures(t *testing.T) {
	ctx := context.Background()
	key := "k"
	rec := keyedClaimRecord(key)

	setupClaimGate := func(mock sqlmock.Sqlmock) {
		mock.ExpectQuery(`SELECT .* FROM query_execution_claims .* FOR UPDATE`).
			WithArgs(rec.TargetResourceID, key).
			WillReturnRows(sqlmock.NewRows(
				[]string{"actor_user_id", "actor_machine_principal_id", "database_name", "schema_name", "execution_id"}).
				AddRow(7, nil, "claimdb", "app", nil))
	}

	stages := []struct {
		name  string
		build func(sqlmock.Sqlmock)
	}{
		{"history insert", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			setupClaimGate(m)
			m.ExpectExec(`insert into query_executions`).WillReturnError(errors.New("history boom"))
			m.ExpectRollback()
		}},
		{"audit insert", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			setupClaimGate(m)
			m.ExpectExec(`insert into query_executions`).WillReturnResult(sqlmock.NewResult(9, 1))
			m.ExpectExec(`insert into audit_events`).WillReturnError(errors.New("audit boom"))
			m.ExpectRollback()
		}},
		{"claim update", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			setupClaimGate(m)
			m.ExpectExec(`insert into query_executions`).WillReturnResult(sqlmock.NewResult(9, 1))
			m.ExpectExec(`insert into audit_events`).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec(`UPDATE query_execution_claims`).WillReturnError(errors.New("link boom"))
			m.ExpectRollback()
		}},
		{"commit", func(m sqlmock.Sqlmock) {
			m.ExpectBegin()
			setupClaimGate(m)
			m.ExpectExec(`insert into query_executions`).WillReturnResult(sqlmock.NewResult(9, 1))
			m.ExpectExec(`insert into audit_events`).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectExec(`UPDATE query_execution_claims`).WillReturnResult(sqlmock.NewResult(0, 1))
			m.ExpectCommit().WillReturnError(errors.New("commit boom"))
		}},
	}
	for _, st := range stages {
		t.Run(st.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			repo := NewQueryExecutionRepository(db)
			failuresBefore := repo.QueryEvidencePersistenceFailures()
			st.build(mock)
			if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); err == nil {
				t.Fatalf("%s failure must return an error", st.name)
			}
			if got := repo.QueryEvidencePersistenceFailures(); got != failuresBefore+1 {
				t.Fatalf("%s: counter delta = %d, want 1", st.name, got-failuresBefore)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("%s: %v", st.name, err)
			}
		})
	}

	t.Run("already-linked claim refuses without counting", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		repo := NewQueryExecutionRepository(db)
		failuresBefore := repo.QueryEvidencePersistenceFailures()
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .* FROM query_execution_claims .* FOR UPDATE`).
			WithArgs(rec.TargetResourceID, key).
			WillReturnRows(sqlmock.NewRows(
				[]string{"actor_user_id", "actor_machine_principal_id", "database_name", "schema_name", "execution_id"}).
				AddRow(7, nil, "claimdb", "app", 55))
		mock.ExpectRollback()
		if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, ErrQueryExecutionClaimNotLinked) {
			t.Fatalf("linked claim = %v, want ErrQueryExecutionClaimNotLinked", err)
		}
		if got := repo.QueryEvidencePersistenceFailures(); got != failuresBefore {
			t.Fatal("admission refusal must not count as persistence failure")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})

	// The keyless primitive rejects keyed records before touching the DB.
	t.Run("keyless path rejects keyed record", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		repo := NewQueryExecutionRepository(db)
		if _, err := repo.InsertExecutionWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, ErrQueryExecutionClaimInvalid) {
			t.Fatalf("keyless keyed insert = %v, want ErrQueryExecutionClaimInvalid", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
