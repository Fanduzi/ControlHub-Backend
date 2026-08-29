// Package mysql tests collector scan ledger persistence.
// input: context, crypto/sha256, database/sql, errors, testing, time, sqlmock, and collector scan models
// output: caller-owned transaction, ledger idempotency/conflict, and collector CI state transition coverage
// pos: Focused SQL contract for the idempotent collector ledger/state transaction boundary
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	drivermysql "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
)

func TestApplyCollectorScanExactRetryDoesNotRewindNewerState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	entryA := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-a",
		PayloadHash:        sha256.Sum256([]byte("payload-a")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt,
	}
	entryB := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-b",
		PayloadHash:        sha256.Sum256([]byte("payload-b")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt.Add(time.Minute),
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WithArgs(uint64(7), "scan-a", entryA.PayloadHash[:], "complete", entryA.CompletedAt).
		WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}))
	mock.ExpectExec("insert into collector_ci_scan_states").
		WithArgs(uint64(7), uint64(11), uint8(0), "scan-a", "scan-a", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WithArgs(uint64(7), "scan-b", entryB.PayloadHash[:], "complete", entryB.CompletedAt).
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}).
			AddRow(11, 0, "scan-a", "scan-a", nil))
	mock.ExpectExec("update collector_ci_scan_states").
		WithArgs(uint8(0), "scan-b", "scan-b", nil, uint64(7), uint64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WillReturnError(&drivermysql.MySQLError{Number: 1062, Message: "duplicate"})
	mock.ExpectQuery("select id, payload_hash, result from collector_scan_ledger").
		WithArgs(uint64(7), "scan-a").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload_hash", "result"}).
			AddRow(41, entryA.PayloadHash[:], "complete"))
	mock.ExpectCommit()

	apply := func(entry model.CollectorScanLedgerEntry) uint64 {
		t.Helper()
		tx, err := db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		ledgerID, err := ApplyCollectorScan(t.Context(), tx, entry, []uint64{11})
		if err != nil {
			t.Fatalf("ApplyCollectorScan(%s): %v", entry.CollectorScanID, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("Commit: %v", err)
		}
		return ledgerID
	}

	if ledgerID := apply(entryA); ledgerID != 41 {
		t.Fatalf("scan A ledger ID = %d, want 41", ledgerID)
	}
	if ledgerID := apply(entryB); ledgerID != 42 {
		t.Fatalf("scan B ledger ID = %d, want 42", ledgerID)
	}
	if ledgerID := apply(entryA); ledgerID != 41 {
		t.Fatalf("scan A retry ledger ID = %d, want prior ID 41", ledgerID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanReturnsInsertedIDInCallerTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-1",
		PayloadHash:        sha256.Sum256([]byte("normalized payload")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt,
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WithArgs(uint64(7), "scan-1", entry.PayloadHash[:], "complete", completedAt).
		WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}))
	mock.ExpectCommit()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	ledgerID, err := ApplyCollectorScan(t.Context(), tx, entry, nil)
	if err != nil {
		t.Fatalf("ApplyCollectorScan: %v", err)
	}
	if ledgerID != 41 {
		t.Fatalf("ledger ID = %d, want 41", ledgerID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanInsertFailureLeavesRollbackToCaller(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-1",
		PayloadHash:        sha256.Sum256([]byte("normalized payload")),
		Result:             model.CollectorScanResultFailed,
		CompletedAt:        time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	_, err = ApplyCollectorScan(t.Context(), tx, entry, nil)
	if !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("insert error = %v, want sql.ErrConnDone", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanConflictingRetryIsTypedAndCallerRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-1",
		PayloadHash:        sha256.Sum256([]byte("different normalized payload")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC),
	}
	storedHash := sha256.Sum256([]byte("original normalized payload"))

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WillReturnError(&drivermysql.MySQLError{Number: 1062, Message: "duplicate"})
	mock.ExpectQuery("select id, payload_hash, result from collector_scan_ledger").
		WithArgs(uint64(7), "scan-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload_hash", "result"}).
			AddRow(41, storedHash[:], "complete"))
	mock.ExpectRollback()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	_, err = ApplyCollectorScan(t.Context(), tx, entry, nil)
	var conflict *CollectorScanConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("conflicting retry error = %v, want *CollectorScanConflictError", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanExactRetryReturnsPriorIDWithoutStateSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-1",
		PayloadHash:        sha256.Sum256([]byte("normalized payload")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt,
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WillReturnError(&drivermysql.MySQLError{Number: 1062, Message: "duplicate"})
	mock.ExpectQuery("select id, payload_hash, result from collector_scan_ledger").
		WithArgs(uint64(7), "scan-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "payload_hash", "result"}).
			AddRow(41, entry.PayloadHash[:], "complete"))
	mock.ExpectCommit()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	ledgerID, err := ApplyCollectorScan(t.Context(), tx, entry, nil)
	if err != nil {
		t.Fatalf("exact retry: %v", err)
	}
	if ledgerID != 41 {
		t.Fatalf("ledger ID = %d, want prior ID 41", ledgerID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanStatesRediscoveryClearsMissingState(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	missingSince := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	completedAt := missingSince.Add(time.Hour)
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-new",
		PayloadHash:        sha256.Sum256([]byte("payload")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt,
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WithArgs(uint64(7), "scan-new", entry.PayloadHash[:], "complete", completedAt).
		WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}).
			AddRow(11, 3, "scan-old", "scan-old", missingSince))
	mock.ExpectExec("update collector_ci_scan_states").
		WithArgs(uint8(0), "scan-new", "scan-new", nil, uint64(7), uint64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	_, err = ApplyCollectorScan(t.Context(), tx, entry, []uint64{11})
	if err != nil {
		t.Fatalf("ApplyCollectorScan: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanStatesThirdCompleteOmissionBecomesMissing(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-3",
		PayloadHash:        sha256.Sum256([]byte("payload")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt,
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WithArgs(uint64(7), "scan-3", entry.PayloadHash[:], "complete", completedAt).
		WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}).
			AddRow(11, 2, "scan-seen", "scan-2", nil))
	mock.ExpectExec("update collector_ci_scan_states").
		WithArgs(uint8(3), "scan-seen", "scan-3", completedAt, uint64(7), uint64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	_, err = ApplyCollectorScan(t.Context(), tx, entry, nil)
	if err != nil {
		t.Fatalf("ApplyCollectorScan: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestApplyCollectorScanStatesUnseenIncompleteAndFailedDoNotChangeState(t *testing.T) {
	for _, result := range []model.CollectorScanResult{model.CollectorScanResultIncomplete, model.CollectorScanResultFailed} {
		t.Run(string(result), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock.New: %v", err)
			}
			defer db.Close()
			completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
			entry := model.CollectorScanLedgerEntry{
				MachinePrincipalID: 7,
				CollectorScanID:    "scan-new",
				PayloadHash:        sha256.Sum256([]byte("payload")),
				Result:             result,
				CompletedAt:        completedAt,
			}

			mock.ExpectBegin()
			mock.ExpectExec("insert into collector_scan_ledger").
				WithArgs(uint64(7), "scan-new", entry.PayloadHash[:], string(result), completedAt).
				WillReturnResult(sqlmock.NewResult(41, 1))
			mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
				WithArgs(uint64(7)).
				WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}).
					AddRow(11, 2, "scan-seen", "scan-2", nil))
			mock.ExpectCommit()
			tx, err := db.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatalf("BeginTx: %v", err)
			}

			_, err = ApplyCollectorScan(t.Context(), tx, entry, nil)
			if err != nil {
				t.Fatalf("ApplyCollectorScan: %v", err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet expectations: %v", err)
			}
		})
	}
}

func TestApplyCollectorScanStatesDeduplicatesAndSortsBeforeInsertError(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	completedAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	entry := model.CollectorScanLedgerEntry{
		MachinePrincipalID: 7,
		CollectorScanID:    "scan-new",
		PayloadHash:        sha256.Sum256([]byte("payload")),
		Result:             model.CollectorScanResultComplete,
		CompletedAt:        completedAt,
	}

	mock.ExpectBegin()
	mock.ExpectExec("insert into collector_scan_ledger").
		WithArgs(uint64(7), "scan-new", entry.PayloadHash[:], "complete", completedAt).
		WillReturnResult(sqlmock.NewResult(41, 1))
	mock.ExpectQuery("select resource_id, consecutive_complete_scan_omissions, last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since").
		WithArgs(uint64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"resource_id", "consecutive_complete_scan_omissions", "last_seen_collector_scan_id", "last_completed_collector_scan_id", "missing_since"}))
	mock.ExpectExec("insert into collector_ci_scan_states").
		WithArgs(uint64(7), uint64(3), uint8(0), "scan-new", "scan-new", nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("insert into collector_ci_scan_states").
		WithArgs(uint64(7), uint64(7), uint8(0), "scan-new", "scan-new", nil).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	tx, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}

	_, err = ApplyCollectorScan(t.Context(), tx, entry, []uint64{9, 3, 7, 3})
	if !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("insert error = %v, want sql.ErrConnDone", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
