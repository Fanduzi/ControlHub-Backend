// Package service unit-tests the T10-B attempt evidence: the status fold,
// the remote-state verdict table, and the transport fate fold. No database
// is needed — the verdicts are pure functions over one pg_stat_activity
// row shape and the transport read/close error pair.
// input: constructed errors, pg_stat_activity row shapes
// output: TestPGAttemptStatus*, TestPGRemoteStateVerdict*,
// TestPGStmtFateAfterRead
// pos: proves evidence classification and remote-state semantics without a
// live server; the real-server proof lives in the integration matrix
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

func TestPGAttemptStatus(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want model.QueryExecutionStatus
	}{
		{"success", nil, model.QueryExecutionSuccess},
		{"reject", &pgsql.RejectError{Code: "query_not_allowed"}, model.QueryExecutionRejected},
		{"reject wrapped", fmt.Errorf("outer: %w", &pgsql.RejectError{Code: "x"}), model.QueryExecutionRejected},
		{"validation", ErrQueryValidationFailed, model.QueryExecutionRejected},
		{"too large", ErrQueryResultTooLarge, model.QueryExecutionRejected},
		{"disclosure blocked", ErrQueryDisclosureBlocked, model.QueryExecutionRejected},
		{"timeout sentinel", ErrQueryTimeout, model.QueryExecutionTimeout},
		{"deadline", context.DeadlineExceeded, model.QueryExecutionTimeout},
		{"cancelled", context.Canceled, model.QueryExecutionCancelled},
		{"backend failure", ErrQueryBackendFailure, model.QueryExecutionFailed},
		{"disclosure backend", ErrQueryDisclosureBackendFailure, model.QueryExecutionFailed},
		{"internal sentinel", errPGExecInternal, model.QueryExecutionFailed},
		{"unknown driver", errors.New("driver text must not reach this layer"), model.QueryExecutionFailed},
		// A context error wrapped inside other text still classifies by the
		// sentinel — never by message content.
		{"wrapped cancel", fmt.Errorf("ctx: %w", context.Canceled), model.QueryExecutionCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pgAttemptStatus(tc.err); got != tc.want {
				t.Fatalf("pgAttemptStatus(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestPGRemoteStateVerdict(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Minute)
	idle := "idle"
	idleTx := "idle in transaction"
	idleAbort := "idle in transaction (aborted)"
	active := "active"
	fastpath := "fastpath function call"

	cases := []struct {
		name      string
		found     bool
		state     *string
		start     time.Time
		wantStart time.Time
		want      model.QueryExecutionRemoteState
	}{
		{"backend gone", false, nil, time.Time{}, now, model.QueryExecutionRemoteStopped},
		{"idle same session", true, &idle, now, now, model.QueryExecutionRemoteStopped},
		{"idle in tx", true, &idleTx, now, now, model.QueryExecutionRemoteStopped},
		{"idle aborted tx", true, &idleAbort, now, now, model.QueryExecutionRemoteStopped},
		{"still active", true, &active, now, now, model.QueryExecutionRemoteUnknown},
		{"fastpath", true, &fastpath, now, now, model.QueryExecutionRemoteUnknown},
		{"state invisible", true, nil, now, now, model.QueryExecutionRemoteUnknown},
		{"pid reused", true, &idle, earlier, now, model.QueryExecutionRemoteUnknown},
		// When the attempt never captured backend_start, a found row can
		// never be attributed to our session — not even an idle one. Only a
		// missing row still proves our backend is gone (nothing remains to
		// confuse with a reused pid).
		{"no start recorded, idle", true, &idle, now, time.Time{}, model.QueryExecutionRemoteUnknown},
		{"no start recorded, active", true, &active, now, time.Time{}, model.QueryExecutionRemoteUnknown},
		{"no start recorded, gone", false, nil, time.Time{}, time.Time{}, model.QueryExecutionRemoteStopped},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pgRemoteStateFromObservation(tc.found, tc.state, tc.start, tc.wantStart)
			if got != tc.want {
				t.Fatalf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

// fakeSafeRetryError mimics the driver's own "occurred before any data was
// sent" mark — pgconn.SafeToRetry unwraps to a SafeToRetry() bool, so this
// stands in for contextAlreadyDoneError without needing unexported types.
type fakeSafeRetryError struct{ msg string }

func (e *fakeSafeRetryError) Error() string     { return e.msg }
func (e *fakeSafeRetryError) SafeToRetry() bool { return true }

// The remote-fate fact table is independent of which error becomes the
// attempt's primary error: a delivered ErrorResponse is always server-
// confirmed termination, a clean close is always completion (even when a
// local budget error already ended our read), and only the driver's own
// pre-send mark may collapse indeterminate back to unsent.
func TestPGStmtFateAfterRead(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}
	localBudget := ErrQueryResultTooLarge
	localCtx := context.Canceled
	connDead := errors.New("conn closed mid-read")
	preSend := &fakeSafeRetryError{msg: "context already done"}

	cases := []struct {
		name     string
		readErr  error
		closeErr error
		want     pgStmtFate
	}{
		{"clean stream", nil, nil, pgStmtCompleted},
		{"server error at close", nil, pgErr, pgStmtErrored},
		{"server error during read", pgErr, nil, pgStmtErrored},
		{"server error beats local", localBudget, pgErr, pgStmtErrored},
		{"local reject + clean close", localBudget, nil, pgStmtCompleted},
		{"local reject + clean close 2", localCtx, nil, pgStmtCompleted},
		{"local reject + dead conn", localBudget, connDead, pgStmtIndeterminate},
		{"ctx cancel + dead conn", localCtx, connDead, pgStmtIndeterminate},
		{"bare ctx error alone never unsent", localCtx, localCtx, pgStmtIndeterminate},
		{"driver pre-send mark", nil, preSend, pgStmtUnsent},
		{"driver pre-send mark over local", preSend, preSend, pgStmtUnsent},
		{"clean close wins over retry mark", preSend, nil, pgStmtCompleted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pgStmtFateAfterRead(tc.readErr, tc.closeErr); got != tc.want {
				t.Fatalf("fate = %v, want %v", got, tc.want)
			}
		})
	}
}
