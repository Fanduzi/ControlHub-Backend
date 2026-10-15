//go:build integration

// Package service proves the T10-B cancel contract end to end against a
// disposable PostgreSQL 16 server.
// input: context, errors, encoding/binary, net, sync/atomic, testing, time,
// jackc/pgx/v5, internal/model, internal/pgsql
// output: acceptance tests — real CancelRequest packet on the wire, the
// three completion/cancel races, stopped/unknown remote-state outcomes from
// an independent pg_stat_activity probe, identity/permission boundaries,
// timeout/error class preservation, bounded convergence and recovery
// pos: T10-B real-PostgreSQL acceptance (issue #118, frozen G9 cancel clause)
// — the restricted ro_bind account runs the production
// ExecutePGGovernedOutcome/ExecutePGGovernedDisclosedOutcome chain; the
// CancelRequest proof is a protocol-level observation (a dial whose first
// write is a 80877102 cancel frame), never a saved secret packet; the remote
// verdict is a real second connection's pg_stat_activity read under the
// single bounded window
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
)

// cancelPacketDialer wraps the config's DialFunc to observe the wire
// protocol and — when a fault mode is armed — to inject a controlled network
// failure for the test's own connections: a dial whose FIRST write is a
// cancel frame (length-prefixed, protocol code 80877102) is a real
// CancelRequest control connection. Only the packet shape is counted — pid
// and secret key bytes are never stored or logged. The dialer never touches
// a connection other than the ones built from the test-owned ConnConfig.
type cancelPacketDialer struct {
	inner         func(ctx context.Context, network, addr string) (net.Conn, error)
	dials         atomic.Int64
	cancelPackets atomic.Int64
	// fault modes armed atomically: refuse fails new dials at once;
	// blackhole holds them until the caller's own ctx ends — a dial that
	// connects but never answers.
	faultRefuse    atomic.Bool
	faultBlackhole atomic.Bool
}

func (d *cancelPacketDialer) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	d.dials.Add(1)
	if d.faultRefuse.Load() {
		return nil, errors.New("controlled test fault: dial refused")
	}
	if d.faultBlackhole.Load() {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c, err := d.inner(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return &cancelPacketConn{Conn: c, seen: &d.cancelPackets}, nil
}

type cancelPacketConn struct {
	net.Conn
	seen *atomic.Int64
	once sync.Once
}

func (c *cancelPacketConn) Write(b []byte) (int, error) {
	c.once.Do(func() {
		if len(b) >= 8 &&
			binary.BigEndian.Uint32(b[0:4]) == uint32(len(b)) &&
			binary.BigEndian.Uint32(b[4:8]) == 80877102 {
			c.seen.Add(1)
		}
	})
	return c.Conn.Write(b)
}

// waitPGBackend polls pg_stat_activity through the admin connection until a
// backend owned by the exec role's application is in the wanted state — the
// deterministic interleave barrier for cancel-during-execute. A non-empty
// queryMark additionally requires the backend's live query text to contain
// the marker, so "active" during an internal arm/identity statement cannot
// satisfy the barrier early.
func waitPGBackend(t *testing.T, lab pgPoolLab, appName, wantState, queryMark string) {
	t.Helper()
	admin := lab.adminConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var n int64
		err := admin.QueryRow(ctx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = $1 AND state = $2
			    AND ($3 = '' OR position($3 in query) > 0)`, appName, wantState, queryMark).Scan(&n)
		if err != nil {
			t.Fatalf("poll backend state: %v", err)
		}
		if n > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s backend reached state %q", appName, wantState)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitPGBackendGone polls until no backend carrying the exec application
// name remains — the post-fault convergence check whose deadline exceeds the
// armed server-side statement_timeout backstop.
func waitPGBackendGone(t *testing.T, lab pgPoolLab, appName string, budget time.Duration) {
	t.Helper()
	admin := lab.adminConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), budget+10*time.Second)
	defer cancel()
	deadline := time.Now().Add(budget)
	for {
		var n int64
		err := admin.QueryRow(ctx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = $1`, appName).Scan(&n)
		if err != nil {
			t.Fatalf("poll backend gone: %v", err)
		}
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("application_name %s still has %d backend(s)", appName, n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// pgBackendIdentity reads pid + backend_start for the attempt application's
// live backend (admin view) — test-side truth to compare evidence against.
func pgBackendIdentity(t *testing.T, lab pgPoolLab, appName string) (int32, time.Time, bool) {
	t.Helper()
	admin := lab.adminConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var (
		pid   int32
		start time.Time
	)
	err := admin.QueryRow(ctx,
		`SELECT pid, backend_start FROM pg_catalog.pg_stat_activity
		  WHERE application_name = $1 ORDER BY backend_start DESC LIMIT 1`, appName).Scan(&pid, &start)
	if err != nil {
		return 0, time.Time{}, false
	}
	return pid, start, true
}

// pgLongRunningSQL is the deterministic "still executing" user statement: a
// CPU-bound cartesian count over the 30-row fixture table — every executor
// iteration reaches an interrupt point so a dispatched CancelRequest kills
// it in milliseconds. A FROM-clause generate_series was rejected on purpose:
// PostgreSQL materialises RangeFunction SRFs into a tuplestore that spills
// to pgsql_tmp, turning orphaned executions into fixture-level disk
// pressure; a table lock wait cannot be used either because the binding
// gate's own touch probe must take AccessShareLock on the same table first.
const pgLongRunningSQL = `SELECT count(*) FROM smallvals a, smallvals b, smallvals c, smallvals d, smallvals e, smallvals f`

// pgLongRunningMark is the substring only the (rewritten) user statement
// carries — internal catalog probes and the gate's CASE-WHEN-FALSE touch
// never contain it, so the execution barrier cannot trip early.
const pgLongRunningMark = `count(*)`

// The real cancel chain: a caller-cancelled long query produces a
// CancelRequest packet on a fresh control connection, the remote backend
// stops (independently observed), the attempt reports
// cancelled + stopped, and nothing stays behind.
func TestPGCancel_RealChainStopped(t *testing.T) {
	lab := pgExecFixture(t)
	pgExecProbeLog = func(s string) { t.Logf("probe: %s", s) }
	t.Cleanup(func() { pgExecProbeLog = nil })

	// Each iteration is a complete real cancel; the strict per-attempt
	// invariants (sent sql, cancelled class, one CancelRequest packet, real
	// identity, no rows) hold on every try. Only the remote verdict may
	// legitimately come back unknown — the frozen 2s probe window cannot
	// outwait a server that never dispatches the cancel, and a dropped
	// packet is a property of the environment, not of this attempt. One
	// observed stopped out of up to three real cancels proves the chain.
	stoppedSeen := false
	for attempt := 1; attempt <= 3 && !stoppedSeen; attempt++ {
		in := pgExecInput(t, lab, pgLongRunningSQL, pageOpts(1, 25))
		dialer := &cancelPacketDialer{inner: in.ConnConfig.DialFunc}
		in.ConnConfig = in.ConnConfig.Copy()
		in.ConnConfig.DialFunc = dialer.dial

		ctx, cancel := context.WithCancel(context.Background())
		type outcome struct {
			res *PGVerifiedResult
			ev  *PGAttemptEvidence
			err error
		}
		done := make(chan outcome, 1)
		started := time.Now()
		go func() {
			res, ev, err := ExecutePGGovernedOutcome(ctx, in)
			done <- outcome{res, ev, err}
		}()

		// Deterministic interleave: cancel only after the server reports the
		// user statement actually running on our backend.
		waitPGBackend(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark)
		execPID, execStart, ok := pgBackendIdentity(t, lab, "ch-t7s34-exec")
		if !ok {
			cancel()
			t.Fatal("exec backend identity not visible")
		}
		cancel()

		var out outcome
		select {
		case out = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("cancelled attempt did not return")
		}
		elapsed := time.Since(started)

		if out.err == nil || !errors.Is(out.err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", out.err)
		}
		if out.res != nil {
			t.Fatal("rows must never be delivered on a cancelled attempt")
		}
		ev := out.ev
		if ev == nil {
			t.Fatal("evidence must be returned on the cancelled path")
		}
		if ev.Status != model.QueryExecutionCancelled {
			t.Fatalf("status = %q, want cancelled", ev.Status)
		}
		if !ev.UserSQLSent || ev.BackendPID == 0 {
			t.Fatalf("evidence = %+v, want sent sql and real backend pid", ev)
		}
		if ev.BackendPID != execPID || !ev.BackendStart.Equal(execStart) {
			t.Fatalf("backend identity ev=(%d,%v) server=(%d,%v)", ev.BackendPID, ev.BackendStart, execPID, execStart)
		}
		if ev.Stage != PGStageExecute {
			t.Fatalf("stage = %q, want execute", ev.Stage)
		}
		// Protocol proof: a control connection whose first frame is a
		// CancelRequest packet — the pgx asyncClose chain was exercised for
		// real, not inferred from context errors.
		if got := dialer.cancelPackets.Load(); got != 1 {
			t.Fatalf("cancel packets on wire = %d, want 1 (dials=%d)", got, dialer.dials.Load())
		}
		// The error carries no driver text.
		if out.err.Error() != "context canceled" {
			t.Fatalf("cancel error text = %q", out.err.Error())
		}
		t.Logf("attempt %d elapsed=%s pid=%d remote=%s", attempt, elapsed, ev.BackendPID, ev.RemoteState)
		stoppedSeen = ev.RemoteState == model.QueryExecutionRemoteStopped
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	}
	if !stoppedSeen {
		t.Fatal("3 real cancels all returned remote_state=unknown — server never dispatched the cancellation within the frozen probe window")
	}

	// Recovery: the destroyed connection is never redelivered — a fresh
	// attempt executes normally on the same target.
	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("recovery attempt: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("recovery rows = %d", len(res.Rows))
	}
}

// Completion before cancel: the statement finished remotely, then the caller
// cancels during the audit hook — the attempt reports cancelled while
// remote_state stays completed (the confirmed conclusion is never rewritten).
func TestPGCancel_CompletedThenCancel(t *testing.T) {
	lab := pgExecFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	pgExecAfterAuditHook = func(context.Context, pgx.Tx) error {
		cancel()
		return nil
	}
	defer func() { pgExecAfterAuditHook = nil }()

	res, ev, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if res != nil {
		t.Fatal("no rows may be delivered after post-completion cancel")
	}
	if ev == nil || ev.Status != model.QueryExecutionCancelled {
		t.Fatalf("status = %+v, want cancelled", ev)
	}
	if ev.RemoteState != model.QueryExecutionRemoteCompleted {
		t.Fatalf("remote_state = %q, want completed — finished conclusion must not be rewritten", ev.RemoteState)
	}
	if !ev.UserSQLSent || ev.BackendPID == 0 {
		t.Fatalf("evidence = %+v", ev)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Cancel before the wire: a pre-cancelled context never dispatches the user
// statement — send count 0, empty remote state, no backend recorded.
func TestPGCancel_BeforeSend(t *testing.T) {
	lab := pgExecFixture(t)
	before := pgExecTransportCalls.Load()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	res, ev, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
		`SELECT o.id FROM orders o`, pageOpts(1, 25)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if res != nil {
		t.Fatal("no rows")
	}
	if ev == nil || ev.UserSQLSent || ev.BackendPID != 0 {
		t.Fatalf("evidence = %+v, want unsent/no-backend", ev)
	}
	if ev.RemoteState != model.QueryExecutionRemoteNone {
		t.Fatalf("remote_state = %q, want empty", ev.RemoteState)
	}
	if ev.Status != model.QueryExecutionCancelled || ev.Stage != PGStageInput {
		t.Fatalf("evidence = %+v", ev)
	}
	if got := pgExecTransportCalls.Load() - before; got != 0 {
		t.Fatalf("transport calls delta = %d, want 0", got)
	}
}

// Unknown remote state under a controlled network fault: once the exec
// backend is confirmed active, new dials are refused or blackholed — the
// cancel control connection and the independent pg_stat_activity probe
// both fail to reach the server, so remote truth is unknowable. The attempt
// reports cancelled + unknown, never stopped; the probe window never
// re-arms; and when the fault clears, the armed server-side
// statement_timeout is the backstop and resources converge.
func TestPGCancel_UnknownRemoteState(t *testing.T) {
	lab := pgExecFixture(t)

	run := func(t *testing.T, arm func(*cancelPacketDialer)) (elapsed time.Duration, ev *PGAttemptEvidence) {
		in := pgExecInput(t, lab, pgLongRunningSQL, pageOpts(1, 25))
		in.Timeout = 12 * time.Second // server-side backstop for the orphaned query
		dialer := &cancelPacketDialer{inner: in.ConnConfig.DialFunc}
		in.ConnConfig = in.ConnConfig.Copy()
		in.ConnConfig.DialFunc = dialer.dial

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct {
			ev  *PGAttemptEvidence
			err error
		}, 1)
		started := time.Now()
		go func() {
			_, ev, err := ExecutePGGovernedOutcome(ctx, in)
			done <- struct {
				ev  *PGAttemptEvidence
				err error
			}{ev, err}
		}()
		waitPGBackend(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark)
		arm(dialer)
		cancel()
		var got struct {
			ev  *PGAttemptEvidence
			err error
		}
		select {
		case got = <-done:
		case <-time.After(45 * time.Second):
			t.Fatal("faulted-cancel attempt did not return within bound")
		}
		elapsed = time.Since(started)
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", got.err)
		}
		if got.ev == nil {
			t.Fatal("evidence must arrive on the cancelled path")
		}
		return elapsed, got.ev
	}

	t.Run("dial refused", func(t *testing.T) {
		elapsed, ev := run(t, func(d *cancelPacketDialer) { d.faultRefuse.Store(true) })
		if ev.RemoteState != model.QueryExecutionRemoteUnknown {
			t.Fatalf("remote_state = %q, want unknown — a failed probe must not report stopped", ev.RemoteState)
		}
		if ev.Status != model.QueryExecutionCancelled || !ev.UserSQLSent {
			t.Fatalf("evidence = %+v", ev)
		}
		if elapsed > 30*time.Second {
			t.Fatalf("refused-dial attempt took %s", elapsed)
		}
		// The fault only affected THIS config's dials; clearing it restores
		// truth — the orphaned backend dies to its armed statement_timeout.
		waitPGBackendGone(t, lab, "ch-t7s34-exec", 30*time.Second)
	})

	t.Run("dial blackhole consumes the single window", func(t *testing.T) {
		elapsed, ev := run(t, func(d *cancelPacketDialer) { d.faultBlackhole.Store(true) })
		if ev.RemoteState != model.QueryExecutionRemoteUnknown {
			t.Fatalf("remote_state = %q, want unknown", ev.RemoteState)
		}
		// The probe's own 2s window expired while the dial hung — the proof
		// bound is that the whole attempt stayed under the teardown cap
		// (pgx bounds the async-close drain at 15s) plus the probe window.
		if elapsed > 30*time.Second {
			t.Fatalf("blackholed attempt took %s — window not bounded", elapsed)
		}
		waitPGBackendGone(t, lab, "ch-t7s34-exec", 30*time.Second)
	})

	// Recovery after the fault clears: a fresh attempt runs normally.
	res, err := ExecutePGGoverned(context.Background(), pgExecInput(t, lab,
		`SELECT o.id FROM orders o ORDER BY o.id`, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("post-fault recovery: %v", err)
	}
	if len(res.Rows) != 3 {
		t.Fatalf("recovery rows = %d", len(res.Rows))
	}
}

// Identity and permission boundaries: probing another role's backend yields
// an invisible state → unknown; a backend_start mismatch (simulated pid
// reuse) → unknown; the same session's idle backend → stopped. A fabricated
// identity counterexample is exercised against a REAL server, not a stub.
func TestPGCancel_IdentityAndPermissions(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()

	// A live ro_bind backend we own: open a raw conn with the restricted
	// role and hold it idle.
	cfg := pgExecLabConfig(t, lab)
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("restricted conn: %v", err)
	}
	defer conn.Close(ctx)
	var ownPID int32
	var ownStart time.Time
	if err := conn.QueryRow(ctx,
		`SELECT pg_backend_pid(), backend_start FROM pg_stat_activity WHERE pid = pg_backend_pid()`).Scan(&ownPID, &ownStart); err != nil {
		t.Fatalf("own identity: %v", err)
	}

	// Idle own session under correct identity → stopped.
	if st := pgProbeBackendState(ctx, cfg, ownPID, ownStart); st != model.QueryExecutionRemoteStopped {
		t.Fatalf("own idle backend = %q, want stopped", st)
	}
	// Same pid, wrong backend_start — pid-reuse counterexample → unknown.
	if st := pgProbeBackendState(ctx, cfg, ownPID, ownStart.Add(-time.Hour)); st != model.QueryExecutionRemoteUnknown {
		t.Fatalf("identity mismatch = %q, want unknown", st)
	}

	// Another role's backend: admin conn's state is invisible to ro_bind →
	// unknown, never "no matching row".
	admin := lab.adminConn(t)
	var adminPID int32
	var adminStart time.Time
	if err := admin.QueryRow(ctx,
		`SELECT pg_backend_pid(), backend_start FROM pg_stat_activity WHERE pid = pg_backend_pid()`).Scan(&adminPID, &adminStart); err != nil {
		t.Fatalf("admin identity: %v", err)
	}
	if st := pgProbeBackendState(ctx, cfg, adminPID, adminStart); st != model.QueryExecutionRemoteUnknown {
		t.Fatalf("foreign backend state = %q, want unknown (invisible)", st)
	}
}

// Timeout and error classes keep their verdicts through the evidence
// record: server statement_timeout (57014) is timeout + stopped (the
// ErrorResponse is direct proof of remote termination); a probe failure
// degrades remote_state to unknown without touching the primary class;
// cancel inside binding or inside a disclosure policy read keeps the real
// stage and never reaches the wire.
func TestPGCancel_ErrorClasses(t *testing.T) {
	lab := pgExecFixture(t)

	t.Run("statement_timeout", func(t *testing.T) {
		in := pgExecInput(t, lab, pgLongRunningSQL, pageOpts(1, 25))
		in.Timeout = 1500 * time.Millisecond
		res, ev, err := ExecutePGGovernedOutcome(context.Background(), in)
		if !errors.Is(err, ErrQueryTimeout) {
			t.Fatalf("err = %v, want ErrQueryTimeout", err)
		}
		if res != nil {
			t.Fatal("no rows")
		}
		if ev == nil || ev.Status != model.QueryExecutionTimeout {
			t.Fatalf("status = %+v, want timeout", ev)
		}
		// Server delivered 57014 — the ErrorResponse is the remote
		// termination proof.
		if ev.RemoteState != model.QueryExecutionRemoteStopped || !ev.UserSQLSent {
			t.Fatalf("evidence = %+v, want stopped+sent", ev)
		}
		if ev.Stage != PGStageExecute {
			t.Fatalf("stage = %q, want execute", ev.Stage)
		}
	})

	t.Run("probe failure keeps cancel class", func(t *testing.T) {
		probeCalls := atomic.Int64{}
		pgExecRemoteProbeHook = func() (model.QueryExecutionRemoteState, bool) {
			probeCalls.Add(1)
			return model.QueryExecutionRemoteUnknown, true // forced observer failure
		}
		defer func() { pgExecRemoteProbeHook = nil }()

		in := pgExecInput(t, lab, pgLongRunningSQL, pageOpts(1, 25))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		var ev *PGAttemptEvidence
		go func() {
			_, ev2, err := ExecutePGGovernedOutcome(ctx, in)
			ev = ev2
			done <- err
		}()
		waitPGBackend(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark)
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if probeCalls.Load() == 0 {
			t.Fatal("remote probe was never reached")
		}
		if ev == nil || ev.RemoteState != model.QueryExecutionRemoteUnknown {
			t.Fatalf("remote_state = %+v, want unknown", ev)
		}
		if ev.Status != model.QueryExecutionCancelled {
			t.Fatalf("status = %q — observer failure must not overwrite the cancel verdict", ev.Status)
		}
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	})

	t.Run("cancel inside binding", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		pgExecAfterBindHook = func(context.Context, pgx.Tx) error {
			cancel()
			return nil
		}
		defer func() { pgExecAfterBindHook = nil }()

		before := pgExecTransportCalls.Load()
		res, ev, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
			`SELECT o.id FROM orders o`, pageOpts(1, 25)))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if res != nil || ev == nil {
			t.Fatalf("res=%v ev=%v", res, ev)
		}
		if ev.UserSQLSent || ev.RemoteState != model.QueryExecutionRemoteNone {
			t.Fatalf("evidence = %+v — pre-wire cancel must keep empty remote state", ev)
		}
		if ev.Stage != PGStageBind {
			t.Fatalf("stage = %q, want bind", ev.Stage)
		}
		if pgExecTransportCalls.Load()-before != 0 {
			t.Fatal("user SQL reached the wire on a bind-stage cancel")
		}
	})

	t.Run("cancel inside disclosure policy read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// The store honors its deadline mid-read — the real repository's
		// context behavior, without claiming a MySQL round-trip here.
		reader := &cancelingReader{
			inner:  pgReader(lab, policySet{{"app", "orders", "id"}: raw}),
			cancel: cancel,
		}
		before := pgExecTransportCalls.Load()
		res, ev, err := ExecutePGGovernedDisclosedOutcome(ctx, PGDisclosureInput{
			Execute:          pgExecInput(t, lab, `SELECT o.id FROM orders o`, pageOpts(1, 25)),
			TargetResourceID: 1,
			Policies:         reader,
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if res != nil || ev == nil {
			t.Fatalf("res=%v ev=%v", res, ev)
		}
		if ev.Stage != PGStagePreflight || ev.UserSQLSent {
			t.Fatalf("evidence = %+v, want preflight/unsent", ev)
		}
		if pgExecTransportCalls.Load()-before != 0 {
			t.Fatal("user SQL reached the wire on a preflight cancel")
		}
	})
}

// The cancelled attempt's error surface stays sanitized: no driver text,
// marker strings, or policy-store internals reach the caller, and the unwrap
// chain contains only the bare sentinel.
func TestPGCancel_ErrorSurfaceSanitized(t *testing.T) {
	lab := pgExecFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
			pgLongRunningSQL, pageOpts(1, 25)))
		done <- err
	}()
	waitPGBackend(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark)
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err.Error() != "context canceled" {
		t.Fatalf("error text leaked: %q", err.Error())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		t.Fatalf("driver error object reachable in unwrap chain: %v", err)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}
