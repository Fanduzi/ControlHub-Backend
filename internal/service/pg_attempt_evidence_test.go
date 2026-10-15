// Package service unit-tests the T10-B attempt evidence: the status fold and
// the remote-state verdict table. No database is needed — the verdict is a
// pure function over one pg_stat_activity row shape.
// input: constructed errors, pg_stat_activity row shapes
// output: TestPGAttemptStatus*, TestPGRemoteStateVerdict*
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
		// When the attempt never captured backend_start, identity cannot be
		// disambiguated beyond the pid row itself.
		{"no start recorded, idle", true, &idle, now, time.Time{}, model.QueryExecutionRemoteStopped},
		{"no start recorded, active", true, &active, now, time.Time{}, model.QueryExecutionRemoteUnknown},
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
