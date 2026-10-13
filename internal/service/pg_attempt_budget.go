// Package service bounds every governed PostgreSQL attempt statement by the
// attempt's remaining time, never a fresh full budget.
// input: context, strconv, time, jackc/pgx/v5
// output: pgAttemptBudget, pgAttemptBudgetFromContext, pgRemainingStatementTimeoutMs — per-statement SET LOCAL statement_timeout arm computing remaining milliseconds from the attempt deadline
// pos: G4 step-5 remaining-budget mechanism for T7-S2 — the resolution gate, touch, attribute/dependency/type probes all re-arm before each statement inside the caller's transaction; a spent budget fails closed before any statement is sent
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// pgAttemptBudget is one governed attempt's DB-side statement budget. The
// attempt context carries the deadline; every probe statement re-arms
// statement_timeout to the remaining time so no single statement may claim a
// fresh full budget. Lock waits are covered by the same per-statement cap.
type pgAttemptBudget struct {
	deadline time.Time
}

// pgAttemptBudgetFromContext derives the attempt budget from the attempt
// context's deadline. A context without a deadline is a caller defect —
// governed execution always runs under a finite budget, so this fails closed.
func pgAttemptBudgetFromContext(ctx context.Context) (pgAttemptBudget, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return pgAttemptBudget{}, ErrQueryBackendFailure
	}
	return pgAttemptBudget{deadline: deadline}, nil
}

// pgRemainingStatementTimeoutMs converts the time left until deadline into a
// positive statement_timeout millisecond value. ok=false means the budget is
// already spent; the caller must classify timeout without sending anything.
// A positive sub-millisecond remainder rounds up to 1ms — statement_timeout 0
// disables the cap entirely, which must never happen here.
func pgRemainingStatementTimeoutMs(deadline, now time.Time) (int64, bool) {
	rem := deadline.Sub(now)
	if rem <= 0 {
		return 0, false
	}
	ms := rem.Milliseconds()
	if rem%time.Millisecond != 0 {
		ms++
	}
	if ms < 1 {
		ms = 1
	}
	return ms, true
}

// arm re-issues SET LOCAL statement_timeout = <remaining ms> inside the
// caller's transaction. It runs under the previously armed value, so the arm
// itself is bounded too. The "ms" suffix keeps the value unambiguous for
// set_config's free-form text input.
func (b pgAttemptBudget) arm(ctx context.Context, tx pgx.Tx) error {
	ms, ok := pgRemainingStatementTimeoutMs(b.deadline, time.Now())
	if !ok {
		return ErrQueryTimeout
	}
	_, err := tx.Exec(ctx,
		`SELECT pg_catalog.set_config('statement_timeout', $1, true)`,
		strconv.FormatInt(ms, 10)+"ms")
	return err
}
