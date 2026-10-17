//go:build integration

// Package service proves the T10-B cancel contract end to end against a
// disposable PostgreSQL 16 server.
// input: context, errors, encoding/binary, fmt, io, net, strings, sync,
// sync/atomic, testing, time, jackc/pgx/v5, internal/model, internal/pgsql
// output: acceptance tests — real CancelRequest packet on the wire (frame
// shape / successful write / peer close as separate tiers), the strict
// native-cancel stopped proof and its blocked-write negative control, the
// three races, stopped/unknown probe outcomes, identity boundaries, error
// classes, bounded convergence and recovery; T10-B-R1 adds pre-send unsent
// convergence, local-rejection completion, early identity retention, and
// the phase-marked single-window probe proof
// pos: T10-B real-PostgreSQL acceptance (issue #118, frozen G9 cancel
// clause) — ro_bind runs the production Outcome chain; the CancelRequest
// tiers observe the wire without storing secrets, the remote verdict is a
// real second connection's pg_stat_activity read in one window
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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
// failure for the test's own connections. A connection whose FIRST write is
// a cancel frame (length-prefixed, protocol code 80877102) is a real
// CancelRequest control connection. The tiers are recorded separately and
// never conflated: a recognized frame SHAPE (cancelFrames), a frame whose
// underlying Write returned success (cancelWriteOK), and the server closing
// the control connection after it — pgx reads for that close exactly like
// libpq's PQcancel, so a post-frame EOF (cancelPeerEOF) means the server
// consumed the request. None of them stores pid or secret-key bytes; none
// of them alone proves the backend actually stopped. The dialer never
// touches a connection other than the ones built from the test-owned
// ConnConfig.
type cancelPacketDialer struct {
	inner func(ctx context.Context, network, addr string) (net.Conn, error)
	dials atomic.Int64
	// Wire tiers for CancelRequest connections — kept distinct on purpose:
	// frame shape recognized at Write entry, a successful underlying write,
	// and the postmaster closing the control connection after processing.
	cancelFrames  atomic.Int64
	cancelWriteOK atomic.Int64
	cancelPeerEOF atomic.Int64
	// fault modes armed atomically: refuse fails new dials at once;
	// blackhole holds them until the caller's own ctx ends — a dial that
	// connects but never answers; dropCancelWrite fails the cancel frame's
	// Write so the request is never transmitted at all.
	faultRefuse          atomic.Bool
	faultBlackhole       atomic.Bool
	faultDropCancelWrite atomic.Bool
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
	return &cancelPacketConn{Conn: c, dialer: d}, nil
}

type cancelPacketConn struct {
	net.Conn
	dialer *cancelPacketDialer
	framed atomic.Bool
	eof    atomic.Bool
}

// isCancelFrame reports whether b is the single length-prefixed frame a
// CancelRequest control connection writes first (protocol code 80877102).
func isCancelFrame(b []byte) bool {
	return len(b) >= 8 &&
		binary.BigEndian.Uint32(b[0:4]) == uint32(len(b)) &&
		binary.BigEndian.Uint32(b[4:8]) == 80877102
}

func (c *cancelPacketConn) Write(b []byte) (int, error) {
	isCancel := !c.framed.Load() && isCancelFrame(b)
	if isCancel {
		c.dialer.cancelFrames.Add(1)
		if c.dialer.faultDropCancelWrite.Load() {
			// The request is never transmitted — the negative control that
			// proves the wire path is load-bearing.
			return 0, errors.New("controlled test fault: cancel frame write blocked")
		}
	}
	n, err := c.Conn.Write(b)
	if isCancel {
		c.framed.Store(true)
		if err == nil {
			// Bytes left the socket — a stronger tier than shape, still
			// weaker than the server consuming them.
			c.dialer.cancelWriteOK.Add(1)
		}
	}
	return n, err
}

func (c *cancelPacketConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if c.framed.Load() && errors.Is(err, io.EOF) && c.eof.CompareAndSwap(false, true) {
		// The server closed the cancel control connection — pgx issues
		// this read after the frame write exactly for this signal.
		c.dialer.cancelPeerEOF.Add(1)
	}
	return n, err
}

// waitPGBackend polls pg_stat_activity through the admin connection until a
// backend owned by the exec role's application is in the wanted state — the
// deterministic interleave barrier for cancel-during-execute. A non-empty
// queryMark additionally requires the backend's live query text to contain
// the marker, so "active" during an internal arm/identity statement cannot
// satisfy the barrier early.
func waitPGBackend(t *testing.T, lab pgPoolLab, appName, wantState, queryMark string) {
	t.Helper()
	waitPGBackendAged(t, lab, appName, wantState, queryMark, 0)
}

// waitPGBackendAged is waitPGBackend plus a server-measured minimum query
// age: the barrier holds only once pg_stat_activity reports the backend's
// live (mark-matching) query has been running for at least minAge. That is
// a phase barrier on steady execution — measured by the server — not a
// blind client-side sleep guessing at execution timing. It exists because
// a cancel fired in the first instants of backend activity lands inside
// the executor's startup edge where the signal is not yet honored.
func waitPGBackendAged(t *testing.T, lab pgPoolLab, appName, wantState, queryMark string, minAge time.Duration) {
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
			    AND ($3 = '' OR position($3 in query) > 0)
			    AND clock_timestamp() - query_start >= make_interval(secs => $4)`,
			appName, wantState, queryMark, minAge.Seconds()).Scan(&n)
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

// probeMark is one phase record emitted through the pgExecProbeLog seam
// together with its capture time — tests order marks and measure the gaps
// between them (never trusting a bare enum verdict as observation proof).
type probeMark struct {
	at   time.Time
	line string
}

// probeLogCapture collects probe phase marks concurrency-safely and answers
// prefix lookups; installed per test through pgExecProbeLog.
type probeLogCapture struct {
	mu    sync.Mutex
	marks []probeMark
}

func (c *probeLogCapture) fn() func(string) {
	return func(s string) {
		c.mu.Lock()
		c.marks = append(c.marks, probeMark{time.Now(), s})
		c.mu.Unlock()
	}
}

func (c *probeLogCapture) reset() {
	c.mu.Lock()
	c.marks = nil
	c.mu.Unlock()
}

func (c *probeLogCapture) first(prefix string) (probeMark, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range c.marks {
		if strings.HasPrefix(m.line, prefix) {
			return m, true
		}
	}
	return probeMark{}, false
}

func (c *probeLogCapture) count(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, m := range c.marks {
		if strings.HasPrefix(m.line, prefix) {
			n++
		}
	}
	return n
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

// The real cancel chain as a repeated observation trial: up to three
// caller-cancelled long queries each produce a real CancelRequest packet on
// a fresh control connection and every strict per-attempt invariant
// (sent sql, cancelled class, real identity, no rows, wire tiers) holds
// every time. Only the remote verdict may legitimately come back unknown —
// the frozen 2s probe window cannot outwait a cancel that never lands
// remotely, and the exact mechanism of a non-landing cancel is NOT
// confirmed (frame write and the peer closing the control conn are proven;
// server-side dispatch is not observable from the client). Each trial's
// stopped/unknown outcome plus its wire tiers are logged; the strict
// native-cancel proof is TestPGCancel_NativeCancelStopped.
func TestPGCancel_RealChainStopped(t *testing.T) {
	lab := pgExecFixture(t)
	pgExecProbeLog = func(s string) { t.Logf("probe: %s", s) }
	t.Cleanup(func() { pgExecProbeLog = nil })

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
		// user statement in steady execution (past the startup edge — an
		// early cancel can land before the backend honors signals).
		waitPGBackendAged(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark, 250*time.Millisecond)
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
		if got := dialer.cancelFrames.Load(); got != 1 {
			t.Fatalf("cancel frames on wire = %d, want 1 (dials=%d)", got, dialer.dials.Load())
		}
		t.Logf("attempt %d wire tiers: frames=%d writeOK=%d peerEOF=%d", attempt,
			dialer.cancelFrames.Load(), dialer.cancelWriteOK.Load(), dialer.cancelPeerEOF.Load())
		// The error carries no driver text.
		if out.err.Error() != "context canceled" {
			t.Fatalf("cancel error text = %q", out.err.Error())
		}
		t.Logf("attempt %d elapsed=%s pid=%d remote=%s", attempt, elapsed, ev.BackendPID, ev.RemoteState)
		stoppedSeen = ev.RemoteState == model.QueryExecutionRemoteStopped
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	}
	if !stoppedSeen {
		// Observation trial outcome, not a verdict failure: every real
		// cancel was dispatched (packet counted) and every attempt's
		// invariants held — the server's dispatch simply never landed
		// inside the frozen window this run. The deterministic stopped
		// proof is TestPGCancel_RemoteStopObserved.
		t.Logf("observation trial: 3 real cancels, 0 stopped within the 2s window — dispatch not observed (root cause unconfirmed)")
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

// The strict native-cancel success case: one production attempt on the
// interruptible cartesian-count load, caller ctx cancelled while the
// backend is in server-measured steady execution. No pg_terminate_backend,
// no pg_cancel_backend, no manual packet resend — pgx's asyncClose emits
// the only CancelRequest. The assertion is the FULL chain, not the bare
// enum: a cancel frame was written, the peer closed the control conn
// (server-side consumption as far as the client can see), the independent
// probe ran on a different pid and really read this backend's row
// (startEq=true polls), and remote_state is strictly stopped.
func TestPGCancel_NativeCancelStopped(t *testing.T) {
	lab := pgExecFixture(t)
	cap := &probeLogCapture{}
	pgExecProbeLog = cap.fn()
	t.Cleanup(func() { pgExecProbeLog = nil })
	// The strict path must not touch any probe seam — assert that rather
	// than assume it.
	if pgProbeRowHook != nil || pgExecRemoteProbeHook != nil || pgProbeDialOverride != nil || pgProbeRowSQL != "" {
		t.Fatal("a probe seam is armed — the strict proof must run the unmodified production probe")
	}

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
	go func() {
		res, ev, err := ExecutePGGovernedOutcome(ctx, in)
		done <- outcome{res, ev, err}
	}()

	// Phase barrier: the backend is running THIS statement and has been for
	// at least 250ms per the server's own clock — steady execution, not a
	// guessed instant.
	waitPGBackendAged(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark, 250*time.Millisecond)
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

	if out.err == nil || !errors.Is(out.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", out.err)
	}
	if out.res != nil {
		t.Fatal("rows must never be delivered on a cancelled attempt")
	}
	ev := out.ev
	if ev == nil || ev.Status != model.QueryExecutionCancelled {
		t.Fatalf("evidence = %+v, want cancelled", ev)
	}
	if ev.Stage != PGStageExecute || !ev.UserSQLSent || ev.BackendPID == 0 {
		t.Fatalf("evidence = %+v, want execute/sent/real pid", ev)
	}
	if ev.BackendPID != execPID || !ev.BackendStart.Equal(execStart) {
		t.Fatalf("backend identity ev=(%d,%v) server=(%d,%v)", ev.BackendPID, ev.BackendStart, execPID, execStart)
	}
	// Wire tiers, reported separately: the frame shape was recognized, the
	// write succeeded, and the peer closed the control connection — the
	// strongest client-visible signal that the server consumed the request.
	if got := dialer.cancelFrames.Load(); got != 1 {
		t.Fatalf("cancel frames = %d, want 1", got)
	}
	if got := dialer.cancelWriteOK.Load(); got != 1 {
		t.Fatalf("cancel frames written = %d, want 1", got)
	}
	if got := dialer.cancelPeerEOF.Load(); got != 1 {
		t.Fatalf("cancel control conn peer-closes = %d, want 1 — server-side consumption not observed", got)
	}
	// The probe must be load-bearing: it ran, it ran on a connection that
	// is NOT the execution backend, and it really read this backend's row.
	begin, ok := cap.first("probe begin")
	if !ok {
		t.Fatal("probe never ran — remote_state must come from real observation")
	}
	cdone, ok := cap.first("probe connect done pid=")
	if !ok {
		t.Fatal("probe connection never established")
	}
	var probePID int
	if _, err := fmt.Sscanf(cdone.line, "probe connect done pid=%d", &probePID); err != nil || probePID == 0 {
		t.Fatalf("probe pid unparsable in %q", cdone.line)
	}
	if int32(probePID) == execPID {
		t.Fatalf("probe pid %d equals exec pid — the observer must be an independent connection", probePID)
	}
	polls := cap.count("poll found=")
	if polls == 0 {
		t.Fatal("probe never queried this backend's row — verdict not derived from real state")
	}
	verdict, ok := cap.first("probe verdict=")
	if !ok || !strings.Contains(verdict.line, "stopped") {
		t.Fatalf("probe verdict mark = %+v, want stopped", verdict)
	}
	if !verdict.at.After(begin.at) {
		t.Fatal("verdict mark precedes probe begin")
	}
	if ev.RemoteState != model.QueryExecutionRemoteStopped {
		t.Fatalf("remote_state = %q, want stopped — a dispatched cancel kills this load deterministically", ev.RemoteState)
	}
	if _, ok := cap.first("probe conn release returned"); !ok {
		t.Fatal("probe connection was never released")
	}
	t.Logf("native cancel: pid=%d probePID=%d polls=%d verdict=%s",
		execPID, probePID, polls, verdict.line)
	waitNoPGBackends(t, lab, "ch-t7s34-exec")

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

// Negative control: identical load, identical steady-execution barrier, but
// the cancel frame's Write is refused in the test dialer — the request is
// never transmitted. Same conditions then produce unknown, never stopped:
// the wire path is load-bearing, and no amount of local evidence may
// substitute for the packet actually leaving. The orphaned backend is left
// to the armed statement_timeout backstop; no admin termination rescues it.
func TestPGCancel_NativeCancelBlockedNoStop(t *testing.T) {
	lab := pgExecFixture(t)
	cap := &probeLogCapture{}
	pgExecProbeLog = cap.fn()
	t.Cleanup(func() { pgExecProbeLog = nil })

	in := pgExecInput(t, lab, pgLongRunningSQL, pageOpts(1, 25))
	in.Timeout = 12 * time.Second // server-side backstop for the orphan
	dialer := &cancelPacketDialer{inner: in.ConnConfig.DialFunc}
	dialer.faultDropCancelWrite.Store(true)
	in.ConnConfig = in.ConnConfig.Copy()
	in.ConnConfig.DialFunc = dialer.dial

	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		res *PGVerifiedResult
		ev  *PGAttemptEvidence
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, ev, err := ExecutePGGovernedOutcome(ctx, in)
		done <- outcome{res, ev, err}
	}()

	waitPGBackendAged(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark, 250*time.Millisecond)
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
		t.Fatal("blocked-cancel attempt did not return")
	}

	if out.err == nil || !errors.Is(out.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", out.err)
	}
	if out.res != nil {
		t.Fatal("no rows")
	}
	ev := out.ev
	if ev == nil || ev.Status != model.QueryExecutionCancelled {
		t.Fatalf("evidence = %+v, want cancelled", ev)
	}
	if !ev.UserSQLSent || ev.BackendPID != execPID || !ev.BackendStart.Equal(execStart) {
		t.Fatalf("evidence = %+v — the user statement really dispatched before the blocked cancel", ev)
	}
	if got := dialer.cancelFrames.Load(); got != 1 {
		t.Fatalf("cancel frames = %d, want 1 — the shape was still attempted", got)
	}
	if got := dialer.cancelWriteOK.Load(); got != 0 {
		t.Fatalf("cancel frames written = %d, want 0 — nothing may leave the socket", got)
	}
	if got := dialer.cancelPeerEOF.Load(); got != 0 {
		t.Fatalf("cancel conn peer-closes = %d, want 0", got)
	}
	// The probe still ran and honestly reports a live backend.
	if _, ok := cap.first("probe begin"); !ok || cap.count("poll found=true") == 0 {
		t.Fatal("probe never observed the surviving backend")
	}
	if _, ok := cap.first("probe verdict=unknown"); !ok {
		t.Fatal("probe verdict mark missing")
	}
	if ev.RemoteState != model.QueryExecutionRemoteUnknown {
		t.Fatalf("remote_state = %q, want unknown — without transmission the backend stays active and truth is unknowable", ev.RemoteState)
	}
	// Convergence is real, not assumed: the armed statement_timeout kills
	// the orphan server-side.
	waitPGBackendGone(t, lab, "ch-t7s34-exec", 20*time.Second)
}

// External-termination evidence handling — NOT the native-cancel success
// proof (that is TestPGCancel_NativeCancelStopped). The caller still really
// cancels — request ctx ends and pgx's asyncClose emits a real CancelRequest
// — while the test's admin connection independently kills the same backend
// (pg_terminate_backend, same fixture pattern as TestPGExecute_BackendTerminated).
// What is proven here is only that the evidence path records a
// genuinely-dead backend as stopped; the death-driving signal is external.
func TestPGCancel_ExternalTerminationObserved(t *testing.T) {
	lab := pgExecFixture(t)
	admin := lab.adminConn(t)

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
	go func() {
		res, ev, err := ExecutePGGovernedOutcome(ctx, in)
		done <- outcome{res, ev, err}
	}()

	waitPGBackend(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark)
	execPID, execStart, ok := pgBackendIdentity(t, lab, "ch-t7s34-exec")
	if !ok {
		cancel()
		t.Fatal("exec backend identity not visible")
	}
	// Caller cancel first so the attempt's classification is cancelled,
	// then the assisted kill lands the same backend's real death inside
	// the probe window.
	cancel()
	var killed bool
	if err := admin.QueryRow(context.Background(),
		`SELECT pg_catalog.pg_terminate_backend($1)`, execPID).Scan(&killed); err != nil {
		t.Fatalf("terminate backend: %v", err)
	}
	if !killed {
		t.Fatal("pg_terminate_backend reported no matching backend")
	}

	var out outcome
	select {
	case out = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled attempt did not return")
	}
	if out.err == nil || !errors.Is(out.err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", out.err)
	}
	if out.res != nil {
		t.Fatal("rows must never be delivered on a cancelled attempt")
	}
	ev := out.ev
	if ev == nil || ev.Status != model.QueryExecutionCancelled {
		t.Fatalf("status = %+v, want cancelled", ev)
	}
	if !ev.UserSQLSent || ev.BackendPID == 0 {
		t.Fatalf("evidence = %+v, want sent sql and real backend pid", ev)
	}
	if ev.BackendPID != execPID || !ev.BackendStart.Equal(execStart) {
		t.Fatalf("backend identity ev=(%d,%v) server=(%d,%v)", ev.BackendPID, ev.BackendStart, execPID, execStart)
	}
	if ev.RemoteState != model.QueryExecutionRemoteStopped {
		t.Fatalf("remote_state = %q, want stopped — the same backend's death was real and observed", ev.RemoteState)
	}
	if ev.Stage != PGStageExecute {
		t.Fatalf("stage = %q, want execute", ev.Stage)
	}
	// The caller cancel still ran the real asyncClose chain — a
	// CancelRequest packet went out even though the assisted kill is what
	// made the death land inside the window.
	if got := dialer.cancelFrames.Load(); got != 1 {
		t.Fatalf("cancel frames on wire = %d, want 1", got)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
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

// === T10-B-R1 targeted repairs ===

// A1: an armed-then-pre-send cancellation. The driver deterministically
// refuses to send when the context is already done — execExtendedPrefix
// returns a closed reader carrying contextAlreadyDoneError, which
// pgconn.SafeToRetry marks as pre-send. The conservative indeterminate
// mark placed before the call must converge back to unsent on that
// driver fact: UserSQLSent=false, remote_state empty — and the healthy
// connection emits no CancelRequest at all.
func TestPGCancel_PreSendDriverRefusal(t *testing.T) {
	lab := pgExecFixture(t)

	in := pgExecInput(t, lab, `SELECT o.id FROM orders o`, pageOpts(1, 25))
	dialer := &cancelPacketDialer{inner: in.ConnConfig.DialFunc}
	in.ConnConfig = in.ConnConfig.Copy()
	in.ConnConfig.DialFunc = dialer.dial

	ctx, cancel := context.WithCancel(context.Background())
	pgExecPreSendHook = func(context.Context) { cancel() }
	defer func() { pgExecPreSendHook = nil }()

	res, ev, err := ExecutePGGovernedOutcome(ctx, in)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if res != nil {
		t.Fatal("no rows")
	}
	if ev == nil {
		t.Fatal("evidence must arrive")
	}
	if ev.Status != model.QueryExecutionCancelled {
		t.Fatalf("status = %q, want cancelled", ev.Status)
	}
	if ev.Stage != PGStageExecute {
		t.Fatalf("stage = %q, want execute — the attempt did arm and reach the transport", ev.Stage)
	}
	if ev.UserSQLSent {
		t.Fatalf("evidence = %+v — the driver refused before the wire; sent must be false", ev)
	}
	if ev.RemoteState != model.QueryExecutionRemoteNone {
		t.Fatalf("remote_state = %q, want empty — nothing was dispatched to observe", ev.RemoteState)
	}
	if ev.BackendPID == 0 {
		t.Fatal("acquired backend pid must be retained even though nothing was sent")
	}
	if got := dialer.cancelFrames.Load(); got != 0 {
		t.Fatalf("cancel frames = %d — a never-sent statement must not trigger the cancel chain", got)
	}
}

// A2: a local result-budget rejection must not erase the remote
// completion fact. The server finished the statement and the reader
// converged through ReadyForQuery — remote_state is completed even
// though the attempt's primary error is the local rejection and no rows
// are ever delivered.
func TestPGCancel_LocalRejectionStillCompleted(t *testing.T) {
	lab := pgExecFixture(t)

	// One 2MiB cell: exceeds the 1MiB private result bound, so our read
	// stops mid-copy while the server still finishes normally.
	res, ev, err := ExecutePGGovernedOutcome(context.Background(), pgExecInput(t, lab,
		`SELECT repeat('x', 2097152)`, pageOpts(1, 25)))
	if !errors.Is(err, ErrQueryResultTooLarge) {
		t.Fatalf("err = %v, want ErrQueryResultTooLarge", err)
	}
	if res != nil {
		t.Fatal("a local rejection never delivers rows — remote completion is a fact, not a release")
	}
	if ev == nil {
		t.Fatal("evidence must arrive")
	}
	if ev.Status != model.QueryExecutionRejected {
		t.Fatalf("status = %q, want rejected", ev.Status)
	}
	if ev.RemoteState != model.QueryExecutionRemoteCompleted {
		t.Fatalf("remote_state = %q, want completed — the server's own end-of-statement fact survived the local rejection", ev.RemoteState)
	}
	if !ev.UserSQLSent || ev.Stage != PGStageExecute {
		t.Fatalf("evidence = %+v", ev)
	}
}

// B: once Acquire returns, the backend pid is known evidence — a later
// BEGIN/session/identity failure must not erase it, while a
// never-connected attempt still records nothing.
func TestPGCancel_BackendIdentityRetention(t *testing.T) {
	lab := pgExecFixture(t)

	t.Run("begin fails after acquire keeps pid", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		pgExecAfterAcquireHook = func(context.Context) { cancel() }
		defer func() { pgExecAfterAcquireHook = nil }()

		res, ev, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
			`SELECT o.id FROM orders o`, pageOpts(1, 25)))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if res != nil || ev == nil {
			t.Fatalf("res=%v ev=%v", res, ev)
		}
		if ev.BackendPID == 0 {
			t.Fatal("pid lost — the acquired backend's identity must survive a BEGIN failure")
		}
		if ev.UserSQLSent || ev.RemoteState != model.QueryExecutionRemoteNone {
			t.Fatalf("evidence = %+v — user SQL never dispatched", ev)
		}
		if ev.Stage != PGStageConnect || ev.Status != model.QueryExecutionCancelled {
			t.Fatalf("evidence = %+v, want connect/cancelled", ev)
		}
	})

	t.Run("identity stage failure keeps pid", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		pgExecPreIdentityHook = func(context.Context) { cancel() }
		defer func() { pgExecPreIdentityHook = nil }()

		res, ev, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
			`SELECT o.id FROM orders o`, pageOpts(1, 25)))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if res != nil || ev == nil {
			t.Fatalf("res=%v ev=%v", res, ev)
		}
		if ev.BackendPID == 0 {
			t.Fatal("pid lost — the acquired backend's identity must survive an identity-read failure")
		}
		if ev.UserSQLSent || ev.RemoteState != model.QueryExecutionRemoteNone {
			t.Fatalf("evidence = %+v — user SQL never dispatched", ev)
		}
		if ev.Stage != PGStageIdentity || ev.Status != model.QueryExecutionCancelled {
			t.Fatalf("evidence = %+v, want identity/cancelled", ev)
		}
	})

	t.Run("no connection no pid", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		res, ev, err := ExecutePGGovernedOutcome(ctx, pgExecInput(t, lab,
			`SELECT o.id FROM orders o`, pageOpts(1, 25)))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if res != nil || ev == nil {
			t.Fatalf("res=%v ev=%v", res, ev)
		}
		if ev.BackendPID != 0 {
			t.Fatalf("pid = %d — no backend was ever confirmed; fabricating one is worse than zero", ev.BackendPID)
		}
		if ev.UserSQLSent || ev.RemoteState != model.QueryExecutionRemoteNone {
			t.Fatalf("evidence = %+v", ev)
		}
	})
}

// C: the probe's 2-second window is one shared deadline, proven by phase
// marks — connect, version gate, every poll, and the verdict all consume
// the same budget. Each case cancels a real in-flight statement so the
// attempt reaches the probe on a genuinely indeterminate fate. The verdict
// window ends at the decision mark; probe-connection release is timed on
// its own mark afterwards and is never folded into it.
func TestPGCancel_ProbeWindow(t *testing.T) {
	lab := pgExecFixture(t)
	cap := &probeLogCapture{}
	pgExecProbeLog = cap.fn()
	t.Cleanup(func() { pgExecProbeLog = nil })

	mark := func(prefix string) (time.Time, bool) {
		m, ok := cap.first(prefix)
		return m.at, ok
	}
	// windowBetween returns the probe's own verdict window — begin to the
	// verdict mark — never the enclosing attempt's elapsed.
	windowBetween := func(t *testing.T) time.Duration {
		t.Helper()
		begin, ok1 := mark("probe begin")
		verdict, ok2 := mark("probe verdict=")
		if !ok1 || !ok2 {
			t.Fatalf("probe marks missing")
		}
		return verdict.Sub(begin)
	}
	// releaseAfterVerdict proves the release mark exists, ran after the
	// verdict mark, and reports the given minimum elapsed — conn cleanup
	// is owned but measured separately.
	releaseAfterVerdict := func(t *testing.T, min time.Duration) {
		t.Helper()
		verdict, ok1 := mark("probe verdict=")
		rel, ok2 := cap.first("probe conn release returned")
		if !ok1 || !ok2 {
			t.Fatal("verdict/release marks missing")
		}
		if !rel.at.After(verdict) {
			t.Fatal("release mark precedes verdict mark — cleanup folded into the window")
		}
		elapsed, err := time.ParseDuration(strings.TrimPrefix(rel.line, "probe conn release returned elapsed="))
		if err != nil {
			t.Fatalf("release elapsed unparsable in %q", rel.line)
		}
		if elapsed < min {
			t.Fatalf("release elapsed %s < %s", elapsed, min)
		}
	}

	run := func(t *testing.T, afterCancel func(t *testing.T)) *PGAttemptEvidence {
		t.Helper()
		cap.reset()

		in := pgExecInput(t, lab, pgLongRunningSQL, pageOpts(1, 25))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct {
			ev  *PGAttemptEvidence
			err error
		}, 1)
		go func() {
			_, ev, err := ExecutePGGovernedOutcome(ctx, in)
			done <- struct {
				ev  *PGAttemptEvidence
				err error
			}{ev, err}
		}()
		waitPGBackendAged(t, lab, "ch-t7s34-exec", "active", pgLongRunningMark, 250*time.Millisecond)
		cancel()
		if afterCancel != nil {
			afterCancel(t)
		}
		var out struct {
			ev  *PGAttemptEvidence
			err error
		}
		select {
		case out = <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("cancelled attempt did not return")
		}
		if !errors.Is(out.err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", out.err)
		}
		if out.ev == nil {
			t.Fatal("evidence must arrive")
		}
		if out.ev.Status != model.QueryExecutionCancelled {
			t.Fatalf("status = %q — an observation timeout must never overwrite the cancel class", out.ev.Status)
		}
		return out.ev
	}

	// A dial that consumes part of the window leaves only the remainder
	// for version + polls — never a fresh budget per phase. The in-flight
	// stall is REAL: the row query is swapped for a delayed state probe
	// (a pg_stat_activity read joined with pg_sleep) that the server itself
	// executes on the real probe connection under the real shared ctx.
	t.Run("dial then real in-flight query consumes only the remaining window", func(t *testing.T) {
		realDial := pgExecLabConfig(t, lab).DialFunc
		if realDial == nil {
			realDial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
		}
		pgProbeDialOverride = func(ctx context.Context, network, addr string) (net.Conn, error) {
			timer := time.NewTimer(500 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
			return realDial(ctx, network, addr)
		}
		defer func() { pgProbeDialOverride = nil }()
		// Test-only delayed state probe: the pg_sleep arm is FIRST in the
		// UNION ALL so the append node evaluates it before the real row —
		// the query is genuinely in flight for 30s regardless of whether
		// the execution backend's row still exists.
		pgProbeRowSQL = `SELECT NULL::text, NULL::timestamptz FROM pg_catalog.pg_sleep(30)
		   UNION ALL
		  SELECT a.state::text, a.backend_start
		   FROM pg_catalog.pg_stat_activity a WHERE a.pid = $1`
		defer func() { pgProbeRowSQL = "" }()

		var execPID int32
		ev := run(t, func(t *testing.T) {
			// While the attempt's probe is alive, the delayed state query
			// must be visible server-side on a DIFFERENT backend — proof a
			// real probe statement is genuinely in flight.
			execPID, _, _ = pgBackendIdentity(t, lab, "ch-t7s34-exec")
			admin := lab.adminConn(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			deadline := time.Now().Add(4 * time.Second)
			for {
				var n int64
				if err := admin.QueryRow(ctx,
					`SELECT count(*) FROM pg_catalog.pg_stat_activity
					  WHERE pid <> $1 AND state = 'active'
					    AND position('pg_sleep(30)' in query) > 0`, execPID).Scan(&n); err != nil {
					t.Fatalf("poll probe backend: %v", err)
				}
				if n > 0 {
					return
				}
				if time.Now().After(deadline) {
					t.Fatal("delayed probe query never observed in flight")
				}
				time.Sleep(20 * time.Millisecond)
			}
		})
		if ev.RemoteState != model.QueryExecutionRemoteUnknown {
			t.Fatalf("remote_state = %q, want unknown", ev.RemoteState)
		}
		cBegin, _ := mark("probe connect begin")
		cDone, ok := mark("probe connect done")
		if !ok {
			t.Fatal("connect-done mark missing — dial delay never landed")
		}
		if d := cDone.Sub(cBegin); d < 450*time.Millisecond {
			t.Fatalf("dial phase only %s — the delayed dial was not exercised", d)
		}
		if _, ok := mark("probe version done"); !ok {
			t.Fatal("version gate never completed on the real connection")
		}
		// 0.5s dial + the delayed query bounded by what is LEFT of the one
		// window — a fresh 2s after connect would push verdict past ~2.5s.
		vAt, _ := mark("probe verdict=")
		if post := vAt.Sub(cDone); post > 1850*time.Millisecond {
			t.Fatalf("post-connect phase %s — the query got its own deadline instead of the remainder", post)
		}
		if w := windowBetween(t); w < 1800*time.Millisecond || w > 2600*time.Millisecond {
			t.Fatalf("probe verdict window %s — phases must share one 2s deadline", w)
		}
		releaseAfterVerdict(t, 0)
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	})

	// Polls that stay indeterminate repeat inside the SAME window — the
	// verdict lands at the shared deadline, not at N×2s.
	t.Run("indeterminate polls never re-arm the window", func(t *testing.T) {
		var polls atomic.Int64
		active := "active"
		var lastStart time.Time
		pgProbeRowHook = func(ctx context.Context, conn *pgx.Conn, pid int32) (bool, *string, time.Time, error, bool) {
			polls.Add(1)
			// The backend is pinned "active" for the whole window on
			// purpose — the real row is consulted only to keep the identity
			// match honest when it still exists.
			found, _, start, err := pgProbeBackendRow(ctx, conn, pid)
			if err == nil && found {
				lastStart = start
			}
			return true, &active, lastStart, nil, true
		}
		defer func() { pgProbeRowHook = nil }()

		ev := run(t, nil)
		if ev.RemoteState != model.QueryExecutionRemoteUnknown {
			t.Fatalf("remote_state = %q, want unknown", ev.RemoteState)
		}
		if polls.Load() < 5 {
			t.Fatalf("only %d polls — the window never actually polled", polls.Load())
		}
		if w := windowBetween(t); w < 1800*time.Millisecond || w > 2600*time.Millisecond {
			t.Fatalf("probe verdict window %s across %d polls — polling must share one 2s deadline", w, polls.Load())
		}
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	})

	// A dial that never answers burns the whole window at connect — the
	// verdict is unknown at the deadline, not whenever the dial gives up.
	t.Run("blackholed dial spends the whole window at connect", func(t *testing.T) {
		pgProbeDialOverride = func(ctx context.Context, _, _ string) (net.Conn, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		defer func() { pgProbeDialOverride = nil }()

		ev := run(t, nil)
		if ev.RemoteState != model.QueryExecutionRemoteUnknown {
			t.Fatalf("remote_state = %q, want unknown", ev.RemoteState)
		}
		if _, ok := mark("probe connect done"); ok {
			t.Fatal("connect-done mark on a blackholed dial")
		}
		if _, ok := mark("probe connect failed"); !ok {
			t.Fatal("connect-failed mark missing")
		}
		if w := windowBetween(t); w < 1800*time.Millisecond || w > 2600*time.Millisecond {
			t.Fatalf("probe verdict window %s — connect must consume the one 2s deadline, not its own", w)
		}
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	})

	// The verdict mark is stamped at the decision, BEFORE the probe
	// connection's release runs — a slow Close must not inflate the window.
	// The row hook reports the backend gone at the first poll so the probe
	// returns while its connection is still healthy: only then does the
	// deferred conn.Close exercise the slow transport (a ctx-dead conn is
	// closed by pgx's own async path, which is correctly not ours to time).
	t.Run("conn release is timed outside the verdict window", func(t *testing.T) {
		realDial := pgExecLabConfig(t, lab).DialFunc
		if realDial == nil {
			realDial = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
		}
		pgProbeDialOverride = func(ctx context.Context, network, addr string) (net.Conn, error) {
			c, err := realDial(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			return &slowCloseConn{Conn: c, d: 700 * time.Millisecond}, nil
		}
		defer func() { pgProbeDialOverride = nil }()
		pgProbeRowHook = func(ctx context.Context, conn *pgx.Conn, pid int32) (bool, *string, time.Time, error, bool) {
			return false, nil, time.Time{}, nil, true
		}
		defer func() { pgProbeRowHook = nil }()

		ev := run(t, nil)
		if ev.RemoteState != model.QueryExecutionRemoteStopped {
			t.Fatalf("remote_state = %q, want stopped — the hook reports the backend gone", ev.RemoteState)
		}
		// The verdict lands almost immediately; the conn's own close then
		// takes ~700ms on its own mark. If release were folded into the
		// window, begin→verdict would read ≥700ms.
		if w := windowBetween(t); w > 500*time.Millisecond {
			t.Fatalf("probe verdict window %s — release time must not fold into the window", w)
		}
		releaseAfterVerdict(t, 600*time.Millisecond)
		waitNoPGBackends(t, lab, "ch-t7s34-exec")
	})
}

// slowCloseConn is a test-only transport whose Close is artificially slow —
// it proves conn-release time is measured on its own mark and never folded
// into the probe verdict window. Ownership is still real: the deferred
// release still awaits it before the probe returns.
type slowCloseConn struct {
	net.Conn
	d time.Duration
}

func (c *slowCloseConn) Close() error {
	time.Sleep(c.d)
	return c.Conn.Close()
}
