// Package mysql provides MySQL-backed repository implementations.
// input: context, database/sql, errors, fmt, slices, MySQL driver errors, and collector scan models
// output: caller-transaction collector ledger plus idempotent per-principal/per-CI state application
// pos: Durable idempotency and lifecycle-state boundary for completed collector scans
// note: if this file changes, update this header and module README.md.
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

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

// ApplyCollectorScan records one terminal scan and applies its CI state changes once.
// The caller owns commit and rollback; exact retries return the prior ledger ID without state SQL.
func ApplyCollectorScan(ctx context.Context, tx *sql.Tx, entry model.CollectorScanLedgerEntry, seenResourceIDs []uint64) (uint64, error) {
	if tx == nil {
		return 0, fmt.Errorf("transaction is required")
	}
	ledgerID, inserted, err := insertCollectorScanLedger(ctx, tx, entry)
	if err != nil || !inserted {
		return ledgerID, err
	}
	if err := applyCollectorScanStates(ctx, tx, entry.MachinePrincipalID, model.CollectorScan{
		ID:          entry.CollectorScanID,
		Result:      entry.Result,
		CompletedAt: entry.CompletedAt,
	}, seenResourceIDs); err != nil {
		return 0, err
	}
	return ledgerID, nil
}

func insertCollectorScanLedger(ctx context.Context, tx *sql.Tx, entry model.CollectorScanLedgerEntry) (uint64, bool, error) {
	result, err := tx.ExecContext(ctx, `insert into collector_scan_ledger
		(machine_principal_id, collector_scan_id, payload_hash, result, completed_at)
		values (?, ?, ?, ?, ?)`, entry.MachinePrincipalID, entry.CollectorScanID, entry.PayloadHash[:], string(entry.Result), entry.CompletedAt.UTC())
	if err != nil {
		var mysqlErr *drivermysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			id, err := compareCollectorScanRetry(ctx, tx, entry)
			return id, false, err
		}
		return 0, false, fmt.Errorf("insert collector scan ledger: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, fmt.Errorf("collector scan ledger last insert id: %w", err)
	}
	return uint64(id), true, nil
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

func applyCollectorScanStates(ctx context.Context, tx *sql.Tx, machinePrincipalID uint64, scan model.CollectorScan, seenResourceIDs []uint64) error {
	if machinePrincipalID == 0 {
		return fmt.Errorf("machine principal ID is required")
	}
	if _, err := model.ApplyCollectorScan(model.CollectorCIState{}, scan, false); err != nil {
		return err
	}
	scan.CompletedAt = scan.CompletedAt.UTC()
	seenResourceIDs = append([]uint64(nil), seenResourceIDs...)
	for _, resourceID := range seenResourceIDs {
		if resourceID == 0 {
			return fmt.Errorf("seen resource IDs must be positive")
		}
	}
	slices.Sort(seenResourceIDs)
	seenResourceIDs = slices.Compact(seenResourceIDs)

	rows, err := tx.QueryContext(ctx, `select resource_id, consecutive_complete_scan_omissions,
		last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since
		from collector_ci_scan_states where machine_principal_id = ? order by resource_id for update`, machinePrincipalID)
	if err != nil {
		return fmt.Errorf("lock collector CI states: %w", err)
	}
	var states []model.CollectorCIState
	for rows.Next() {
		state := model.CollectorCIState{MachinePrincipalID: machinePrincipalID}
		var lastCompletedScanID sql.NullString
		var missingSince sql.NullTime
		if err := rows.Scan(&state.ResourceID, &state.ConsecutiveCompleteScanOmissions, &state.LastSeenScanID, &lastCompletedScanID, &missingSince); err != nil {
			rows.Close()
			return fmt.Errorf("scan collector CI state: %w", err)
		}
		state.LastCompletedScanID = lastCompletedScanID.String
		if missingSince.Valid {
			state.MissingSince = &missingSince.Time
		}
		states = append(states, state)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close collector CI states: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read collector CI states: %w", err)
	}

	seen := make(map[uint64]bool, len(seenResourceIDs))
	for _, resourceID := range seenResourceIDs {
		seen[resourceID] = true
	}
	existing := make(map[uint64]bool, len(states))
	for _, state := range states {
		existing[state.ResourceID] = true
		next, err := model.ApplyCollectorScan(state, scan, seen[state.ResourceID])
		if err != nil {
			return fmt.Errorf("apply collector scan to resource %d: %w", state.ResourceID, err)
		}
		if next == state {
			continue
		}
		var lastCompletedScanID, missingSince any
		if next.LastCompletedScanID != "" {
			lastCompletedScanID = next.LastCompletedScanID
		}
		if next.MissingSince != nil {
			missingSince = next.MissingSince.UTC()
		}
		if _, err := tx.ExecContext(ctx, `update collector_ci_scan_states set
			consecutive_complete_scan_omissions = ?, last_seen_collector_scan_id = ?,
			last_completed_collector_scan_id = ?, missing_since = ?
			where machine_principal_id = ? and resource_id = ?`, next.ConsecutiveCompleteScanOmissions,
			next.LastSeenScanID, lastCompletedScanID, missingSince, machinePrincipalID, next.ResourceID); err != nil {
			return fmt.Errorf("update collector CI state for resource %d: %w", next.ResourceID, err)
		}
	}
	for _, resourceID := range seenResourceIDs {
		if existing[resourceID] {
			continue
		}
		state, err := model.ApplyCollectorScan(model.CollectorCIState{
			MachinePrincipalID: machinePrincipalID,
			ResourceID:         resourceID,
		}, scan, true)
		if err != nil {
			return fmt.Errorf("apply collector scan to resource %d: %w", resourceID, err)
		}
		var lastCompletedScanID any
		if state.LastCompletedScanID != "" {
			lastCompletedScanID = state.LastCompletedScanID
		}
		if _, err := tx.ExecContext(ctx, `insert into collector_ci_scan_states
			(machine_principal_id, resource_id, consecutive_complete_scan_omissions,
			last_seen_collector_scan_id, last_completed_collector_scan_id, missing_since)
			values (?, ?, ?, ?, ?, ?)`, machinePrincipalID, resourceID, state.ConsecutiveCompleteScanOmissions,
			state.LastSeenScanID, lastCompletedScanID, nil); err != nil {
			return fmt.Errorf("insert collector CI state for resource %d: %w", resourceID, err)
		}
	}
	return nil
}
