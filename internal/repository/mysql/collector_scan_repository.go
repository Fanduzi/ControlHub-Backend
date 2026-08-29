// Package mysql provides MySQL-backed repository implementations.
// input: context, database/sql, errors, fmt, MySQL driver errors, and collector scan models
// output: caller-transaction collector scan ledger insert-or-compare primitive and typed retry conflict
// pos: Durable idempotency boundary for completed collector scan receipts
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	drivermysql "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
)

type CollectorScanConflictError struct {
	MachinePrincipalID uint64
	CollectorScanID    string
}

func (e *CollectorScanConflictError) Error() string {
	return "collector scan retry conflicts with completed ledger entry"
}

// InsertCollectorScanLedger inserts one terminal scan receipt in tx.
// The caller owns commit and rollback so later ingestion writes can share it.
func InsertCollectorScanLedger(ctx context.Context, tx *sql.Tx, entry model.CollectorScanLedgerEntry) (uint64, error) {
	result, err := tx.ExecContext(ctx, `insert into collector_scan_ledger
		(machine_principal_id, collector_scan_id, payload_hash, result, completed_at)
		values (?, ?, ?, ?, ?)`, entry.MachinePrincipalID, entry.CollectorScanID, entry.PayloadHash[:], string(entry.Result), entry.CompletedAt.UTC())
	if err != nil {
		var mysqlErr *drivermysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return compareCollectorScanRetry(ctx, tx, entry)
		}
		return 0, fmt.Errorf("insert collector scan ledger: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("collector scan ledger last insert id: %w", err)
	}
	return uint64(id), nil
}

func compareCollectorScanRetry(ctx context.Context, tx *sql.Tx, entry model.CollectorScanLedgerEntry) (uint64, error) {
	var (
		id          uint64
		payloadHash []byte
		result      string
	)
	if err := tx.QueryRowContext(ctx, `select id, payload_hash, result from collector_scan_ledger
		where machine_principal_id = ? and collector_scan_id = ? for update`, entry.MachinePrincipalID, entry.CollectorScanID).
		Scan(&id, &payloadHash, &result); err != nil {
		return 0, fmt.Errorf("read existing collector scan ledger: %w", err)
	}
	if len(payloadHash) != len(entry.PayloadHash) {
		return 0, fmt.Errorf("existing collector scan payload hash has length %d", len(payloadHash))
	}
	var storedHash [32]byte
	copy(storedHash[:], payloadHash)
	if !entry.MatchesRetry(storedHash, model.CollectorScanResult(result)) {
		return 0, &CollectorScanConflictError{
			MachinePrincipalID: entry.MachinePrincipalID,
			CollectorScanID:    entry.CollectorScanID,
		}
	}
	return id, nil
}
