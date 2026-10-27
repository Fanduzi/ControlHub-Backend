// Package service provides the PostgreSQL Explain runner for governed
// EXPLAIN (FORMAT JSON). Explain reads only optimizer metadata — the bare
// SELECT is never executed (ANALYZE is never requested) — so it reuses the
// read-only transaction + pinned search_path + statement_timeout session
// contract from execution without claims, witness injection, or evidence
// rows.
// input: attempt context with deadline, T2-validated *pgx.ConnConfig, pinned schema, guarded SELECT text
// output: PGExplainRunner.Explain — bounded raw plan tree or a controlled sentinel; DSNs and driver text never surface
// pos: PG sibling of MySQLExplainExecutor — same sealed-statement discipline: the runner only ever sends "EXPLAIN (FORMAT JSON) " + guarded select
// note: if this file changes, update this header and module README.md.

package service

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgExplainMaxStatementBytes bounds the wrapped EXPLAIN text sent to the
// target. GuardPG already bounds the analyzed statement; this second cap is
// a defensive bound on the wire payload.
const pgExplainMaxStatementBytes = 1 << 20 // 1 MiB

// PGExplainExecutor is the narrow runner seam the service holds. It takes
// the already-validated ConnConfig, pinned schema, and guarded SELECT, and
// returns the raw plan tree — there is no path to pass arbitrary SQL or an
// unwrapped SELECT.
type PGExplainExecutor interface {
	Explain(ctx context.Context, cfg *pgx.ConnConfig, pinnedSchema, statement string) (ExplainRawPlan, error)
}

// PGExplainRunner runs EXPLAIN (FORMAT JSON) against a PostgreSQL target
// inside a read-only transaction that is always rolled back. The pool is
// opened per request from the T2-validated ConnConfig — the same discipline
// OpenPostgresPool applies to governed execution — and closed on return.
type PGExplainRunner struct {
	openPool func(ctx context.Context, cfg *pgx.ConnConfig) (*pgxpool.Pool, error)
}

// NewPGExplainRunner builds the runner with the production pool factory.
func NewPGExplainRunner() *PGExplainRunner {
	return &PGExplainRunner{openPool: OpenPostgresPool}
}

// Explain wraps the guarded SELECT in EXPLAIN (FORMAT JSON) and returns the
// parsed plan tree. pinnedSchema is the resolved connection schema; it is
// bound as a set_config value (never spliced) so unqualified names resolve
// exactly as governed execution resolves them. Errors map to the shared
// sentinels: ErrQueryTimeout, ErrQueryBackendFailure, or
// ErrQueryExplainNotSupported for an unreadable plan cell.
func (r *PGExplainRunner) Explain(ctx context.Context, cfg *pgx.ConnConfig, pinnedSchema, statement string) (ExplainRawPlan, error) {
	if cfg == nil {
		return ExplainRawPlan{}, ErrQueryBackendFailure
	}
	wrapped := "EXPLAIN (FORMAT JSON) " + statement
	if len(wrapped) > pgExplainMaxStatementBytes {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}

	pool, err := r.openPool(ctx, cfg)
	if err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Frozen session contract, same shape as governed execution: pinned
	// search_path bound as a value, UTC, then the statement budget armed
	// from the remaining attempt deadline.
	if _, err := tx.Exec(ctx,
		`SELECT pg_catalog.set_config('search_path', $1, true)`,
		pgQuoteIdent(pinnedSchema)+",pg_catalog"); err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	if _, err := tx.Exec(ctx,
		`SELECT pg_catalog.set_config('TimeZone', 'UTC', true)`); err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	if err := armExplainStatementTimeout(ctx, tx); err != nil {
		return ExplainRawPlan{}, err
	}

	rows, err := tx.Query(ctx, wrapped)
	if err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	defer rows.Close()

	if !rows.Next() {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	if err := rows.Err(); err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	if len(raw) == 0 || len(raw) > MaxExplainPlanBytes {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	var tree interface{}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	return ExplainRawPlan{tree: tree}, nil
}

// armExplainStatementTimeout issues SET LOCAL statement_timeout from the
// attempt deadline, identical to pgAttemptBudget.arm. A spent budget is
// classified as timeout before anything else is sent.
func armExplainStatementTimeout(ctx context.Context, tx pgx.Tx) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return ErrQueryBackendFailure
	}
	ms, ok := pgRemainingStatementTimeoutMs(deadline, time.Now())
	if !ok {
		return ErrQueryTimeout
	}
	_, err := tx.Exec(ctx,
		`SELECT pg_catalog.set_config('statement_timeout', $1, true)`,
		strconv.FormatInt(ms, 10)+"ms")
	if err != nil {
		return classifyExplainDriverError(err)
	}
	return nil
}
