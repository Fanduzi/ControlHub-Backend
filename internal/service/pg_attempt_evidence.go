// Package service produces the bounded internal evidence for one governed
// PostgreSQL attempt, including cancellation remote-state observation.
// input: attempt identity (backend pid + backend_start), the attempt's stage
// and statement fate facts, the final classified error, T2-validated
// *pgx.ConnConfig copy
// output: PGAttemptEvidence, PGAttemptStage, pgAttemptStatus,
// observeRemoteState/pgProbeBackendState — scalars only: stage, status,
// backend identity, remote_state, and whether user SQL reached the wire;
// never a connection, transaction, resolver, secret key, DSN, statement
// text, parameter, cell value, or raw driver error
// pos: T10-B cancel/remote-state evidence (issue #118, frozen G9 cancel
// clause + G4 ownership) — pgx v5.11.0 turns context cancel into a real
// CancelRequest via DeadlineContextWatcherHandler + asyncClose on a NEW
// control connection; pgxpool destroys the unsynchronized conn. Remote
// truth comes only from the bounded independent pg_stat_activity probe.
// T10-B-R1: pgStmtFateAfterRead folds remote facts (ErrorResponse / clean
// close / driver pre-send mark) independently of the primary error; the
// probe marks connect/version/poll/verdict phases and exposes test-only
// dial/row seams
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// pgRemoteProbeWindow is the total observation window for one remote-state
// probe: connect, authentication, the version gate, and every retry share
// the same deadline — no retry may claim a fresh window.
const pgRemoteProbeWindow = 2 * time.Second

// pgRemoteProbePoll is the retry interval between definitive-state polls
// inside the single window; a still-active backend may transition to idle or
// disappear while the window is alive.
const pgRemoteProbePoll = 50 * time.Millisecond

// PGAttemptStage names the furthest governed stage an attempt reached. It is
// internal evidence vocabulary, not a user-facing phase.
type PGAttemptStage string

const (
	PGStageInput     PGAttemptStage = "input"
	PGStageGuard     PGAttemptStage = "guard"
	PGStageConnect   PGAttemptStage = "connect"
	PGStageIdentity  PGAttemptStage = "identity"
	PGStageBind      PGAttemptStage = "bind"
	PGStageRewrite   PGAttemptStage = "rewrite"
	PGStagePreflight PGAttemptStage = "preflight"
	PGStageExecute   PGAttemptStage = "execute"
	PGStageVerify    PGAttemptStage = "verify"
	PGStageAudit     PGAttemptStage = "audit"
	PGStageDeliver   PGAttemptStage = "deliver"
	// PGStageTransform marks the post-attempt disclosure transform — an
	// attempt can return a verified result yet still fail delivery there.
	PGStageTransform PGAttemptStage = "transform"
)

// PGAttemptEvidence is the narrow internal record one governed attempt hands
// to later orchestration on every exit — success, rejection, timeout,
// cancellation, and failure alike. It carries only scalars: backend identity
// (pid plus backend_start, which disambiguates pid reuse), the observed
// remote state, the classified terminal status, and two attempt facts —
// the furthest stage reached and whether the user statement was dispatched
// to the wire. Fields absent from an attempt stay at their zero values; a
// zero BackendPID means no execution backend was ever confirmed, not that a
// backend 0 was observed.
type PGAttemptEvidence struct {
	Stage        PGAttemptStage
	Status       model.QueryExecutionStatus
	BackendPID   int32
	BackendStart time.Time
	RemoteState  model.QueryExecutionRemoteState
	UserSQLSent  bool
}

// pgAttemptStatus folds the attempt's final classified error onto the shared
// execution-status vocabulary. The error here is already the safe terminal
// error — this classifier never inspects or preserves raw driver text.
func pgAttemptStatus(err error) model.QueryExecutionStatus {
	var re *pgsql.RejectError
	switch {
	case err == nil:
		return model.QueryExecutionSuccess
	case errors.As(err, &re),
		errors.Is(err, ErrQueryValidationFailed),
		errors.Is(err, ErrQueryResultTooLarge),
		errors.Is(err, ErrQueryDisclosureBlocked):
		return model.QueryExecutionRejected
	case errors.Is(err, ErrQueryTimeout), errors.Is(err, context.DeadlineExceeded):
		return model.QueryExecutionTimeout
	case errors.Is(err, context.Canceled):
		return model.QueryExecutionCancelled
	default:
		return model.QueryExecutionFailed
	}
}

// pgStmtFate is what the attempt knows about the user statement's remote
// termination without probing.
type pgStmtFate int

const (
	// pgStmtUnsent — the transport statement was never dispatched.
	pgStmtUnsent pgStmtFate = iota
	// pgStmtIndeterminate — dispatched, but the attempt neither consumed a
	// clean result stream nor received a server ErrorResponse. The remote
	// side may still be running; only observation can tell.
	pgStmtIndeterminate
	// pgStmtCompleted — the reader closed cleanly through ReadyForQuery:
	// the statement finished remotely. Says nothing about verification,
	// audit, or delivery success.
	pgStmtCompleted
	// pgStmtErrored — a server ErrorResponse was delivered: the server
	// itself confirmed the statement terminated (statement_timeout, syntax
	// or runtime error, and the cancel-acknowledging 57014 alike).
	pgStmtErrored
)

// pgStmtFateAfterRead folds the transport read/close outcome into the
// statement's remote-fate fact — independent of which error is chosen as
// the attempt's primary error. Priority:
//  1. a delivered server ErrorResponse (*pgconn.PgError) in either error —
//     the server itself confirmed termination → errored;
//  2. a clean Close through ReadyForQuery → completed, even when a local
//     budget or context error already ended our read;
//  3. the driver's own "occurred before any data was sent" mark
//     (pgconn.SafeToRetry) → unsent — consumed as evidence only, never a
//     retry trigger and never part of the returned error chain;
//  4. everything else stays indeterminate for the independent probe.
func pgStmtFateAfterRead(readErr, closeErr error) pgStmtFate {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(readErr, &pgErr), errors.As(closeErr, &pgErr):
		return pgStmtErrored
	case closeErr == nil:
		return pgStmtCompleted
	case pgconn.SafeToRetry(readErr) || pgconn.SafeToRetry(closeErr):
		return pgStmtUnsent
	default:
		return pgStmtIndeterminate
	}
}

// pgRemoteStateFromObservation is the frozen verdict over one probed
// pg_stat_activity row (or its absence). Only a missing row or an
// identity-matching idle session proves stopped; an active backend, an
// invisible state, or a backend_start mismatch (possible pid reuse) is
// always unknown.
func pgRemoteStateFromObservation(found bool, state *string, backendStart, wantStart time.Time) model.QueryExecutionRemoteState {
	if !found {
		// No backend at all under our pid — the session that ran the
		// statement is gone.
		return model.QueryExecutionRemoteStopped
	}
	if wantStart.IsZero() || !backendStart.Equal(wantStart) {
		// A missing expected start can never attribute this row to our
		// session, and a mismatched one is pid reuse — either way the row
		// is not evidence about our backend.
		return model.QueryExecutionRemoteUnknown
	}
	if state == nil {
		// Another role's backend masks state to NULL — invisible, never
		// optimistically "no active row".
		return model.QueryExecutionRemoteUnknown
	}
	switch *state {
	case "idle", "idle in transaction", "idle in transaction (aborted)":
		return model.QueryExecutionRemoteStopped
	default:
		// active, fastpath, disabled, or anything new — still running or
		// indeterminate.
		return model.QueryExecutionRemoteUnknown
	}
}

// pgExecRemoteProbeHook is a test-only seam that replaces the real
// pg_stat_activity probe result — used only where a controlled observer
// failure must prove the attempt's primary classification survives an
// unknown remote state. Production leaves it nil.
var pgExecRemoteProbeHook func() (model.QueryExecutionRemoteState, bool)

// pgExecProbeLog is a test-only diagnostic seam: when set, each probe step
// reports a short safe trace line (row presence, state value, identity
// match, error class) — never query text, secrets, or connection details.
// Production leaves it nil.
var pgExecProbeLog func(string)

// PGExecProbeLogForTest installs the probe diagnostic seam from tests in
// other packages; nil restores silence. Test-only — production never calls
// it.
func PGExecProbeLogForTest(f func(string)) { pgExecProbeLog = f }

// pgProbeDialOverride is a test-only seam for the probe connection's
// transport dial — it lets a test slow or refuse the probe's own dial to
// prove the shared window without touching the execution connection's
// identity. Production leaves it nil.
var pgProbeDialOverride pgconn.DialFunc

// pgProbeRowHook is a test-only seam over the pg_stat_activity row read:
// when it reports handled=true its synthetic result replaces the real
// query, letting a test hold the backend "active" or stall the read
// mid-flight to prove the single-window contract. Production leaves it
// nil; handled=false falls through to the real query.
var pgProbeRowHook func(ctx context.Context, conn *pgx.Conn, pid int32) (found bool, state *string, start time.Time, err error, handled bool)

// observeRemoteState resolves this attempt's remote_state once per attempt,
// inside the release path. It never touches the execution pool again: the
// probe runs on its own bounded connection after the execution transaction's
// locks are already released. Direct protocol evidence wins without a probe:
// unsent stays empty, a cleanly consumed result is completed, and a
// delivered ErrorResponse is stopped.
func (a *pgExecAttempt) observeRemoteState() {
	a.evidence.BackendPID = a.backendPID
	a.evidence.BackendStart = a.backendStart
	a.evidence.UserSQLSent = a.stmtFate != pgStmtUnsent
	switch a.stmtFate {
	case pgStmtUnsent:
		a.evidence.RemoteState = model.QueryExecutionRemoteNone
	case pgStmtCompleted:
		a.evidence.RemoteState = model.QueryExecutionRemoteCompleted
	case pgStmtErrored:
		a.evidence.RemoteState = model.QueryExecutionRemoteStopped
	default:
		a.evidence.RemoteState = pgProbeBackendState(a.ctx, a.cfg, a.backendPID, a.backendStart)
	}
}

// pgProbeBackendState asks the real server whether our recorded backend is
// still running, through a fresh authenticated connection — never the
// MaxConns=1 execution pool. The probe derives from the attempt context with
// cancellation stripped and spends one total pgRemoteProbeWindow across
// dial, auth, the version gate, and polling. Any failure to establish or
// read the truth is unknown, never stopped.
func pgProbeBackendState(ctx context.Context, cfg *pgx.ConnConfig, pid int32, backendStart time.Time) model.QueryExecutionRemoteState {
	if pgExecRemoteProbeHook != nil {
		if state, ok := pgExecRemoteProbeHook(); ok {
			return state
		}
	}
	if cfg == nil || pid == 0 {
		return model.QueryExecutionRemoteUnknown
	}
	probeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pgRemoteProbeWindow)
	defer cancel()
	logf := pgExecProbeLog
	start := time.Now()
	if logf != nil {
		logf("probe begin")
	}

	verdict := pgProbeBackendStateInner(probeCtx, cfg, pid, backendStart, logf)
	if logf != nil {
		// The verdict window is the observation budget only — the probe
		// connection's own release is logged separately inside the inner
		// call and is never folded into this number.
		logf(fmt.Sprintf("probe verdict=%s window=%s", verdict, time.Since(start)))
	}
	return verdict
}

// pgProbeBackendStateInner is the observable half of the probe: connect,
// version gate, then polls — all inside the caller's single probeCtx. Phase
// marks go to the test log seam so a test can time dial/gate/poll/verdict
// without seeing credentials or query text.
func pgProbeBackendStateInner(probeCtx context.Context, cfg *pgx.ConnConfig, pid int32, backendStart time.Time, logf func(string)) model.QueryExecutionRemoteState {
	pcfg := cfg.Copy()
	if pcfg.ConnectTimeout <= 0 || pcfg.ConnectTimeout > pgRemoteProbeWindow {
		// The observation copy may tighten operational timeouts; identity —
		// endpoint, database, user, TLS — is never altered.
		pcfg.ConnectTimeout = pgRemoteProbeWindow
	}
	if pgProbeDialOverride != nil {
		pcfg.DialFunc = pgProbeDialOverride
	}
	if logf != nil {
		logf("probe connect begin")
	}
	conn, err := pgx.ConnectConfig(probeCtx, pcfg)
	if err != nil {
		if logf != nil {
			logf("probe connect failed")
		}
		return model.QueryExecutionRemoteUnknown
	}
	if logf != nil {
		logf("probe connect done")
	}
	defer func() {
		// Close is a local release; the probe budget must not be spent
		// waiting on an already-dead socket.
		cctx, ccancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer ccancel()
		_ = conn.Close(cctx)
		if logf != nil {
			logf("probe conn released")
		}
	}()

	if logf != nil {
		logf("probe version begin")
	}
	version, err := readPGServerVersionNum(probeCtx, conn)
	if err != nil || !pgServerVersionSupported(version) {
		if logf != nil {
			logf("probe version gate failed")
		}
		return model.QueryExecutionRemoteUnknown
	}
	if logf != nil {
		logf("probe version done")
	}

	for {
		found, state, start, err := pgProbeBackendRowSeamed(probeCtx, conn, pid)
		if err != nil {
			if logf != nil {
				logf("probe row read failed")
			}
			return model.QueryExecutionRemoteUnknown
		}
		verdict := pgRemoteStateFromObservation(found, state, start, backendStart)
		if logf != nil {
			st := "<nil>"
			if state != nil {
				st = *state
			}
			logf(fmt.Sprintf("poll found=%v state=%s startEq=%v verdict=%s", found, st, start.Equal(backendStart), verdict))
		}
		if verdict != model.QueryExecutionRemoteUnknown {
			return verdict
		}
		// Still running or indeterminate: poll within the SAME window —
		// retrying never re-arms the 2-second budget.
		if probeCtx.Err() != nil {
			return model.QueryExecutionRemoteUnknown
		}
		select {
		case <-probeCtx.Done():
			return model.QueryExecutionRemoteUnknown
		case <-time.After(pgRemoteProbePoll):
		}
	}
}

// pgProbeBackendRowSeamed routes the row read through the test hook before
// falling back to the real query.
func pgProbeBackendRowSeamed(ctx context.Context, conn *pgx.Conn, pid int32) (bool, *string, time.Time, error) {
	if pgProbeRowHook != nil {
		if found, state, start, err, handled := pgProbeRowHook(ctx, conn, pid); handled {
			return found, state, start, err
		}
	}
	return pgProbeBackendRow(ctx, conn, pid)
}

// pgProbeBackendRow reads the single pg_stat_activity row for our pid. It
// requests only state and backend_start — never the query text.
func pgProbeBackendRow(ctx context.Context, conn *pgx.Conn, pid int32) (found bool, state *string, start time.Time, err error) {
	err = conn.QueryRow(ctx,
		`SELECT a.state::text, a.backend_start
		   FROM pg_catalog.pg_stat_activity a
		  WHERE a.pid = $1`, pid).Scan(&state, &start)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, time.Time{}, nil
	}
	if err != nil {
		return false, nil, time.Time{}, err
	}
	return true, state, start, nil
}
