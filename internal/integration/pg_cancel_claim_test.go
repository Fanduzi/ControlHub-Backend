//go:build integration

// Package integration — real-MySQL claim + real-PG16 cancel vertical proof for T10-B.
// input: shared MySQL fixture (real claim repository + real PG target
// resource), a disposable PostgreSQL 16 with a restricted role, and the
// production ExecutePGGovernedOutcome entry
// output: the G-group acceptance — a claimed execution is cancelled for
// real, and the executor's internal evidence (backend pid, backend_start,
// remote_state, cancelled class) is consumed by the real
// FinalizeClaimWithAudit path on a cancellation-durable window
// pos: T10-B evidence consumption proof (issue #118, frozen G9) — a test
// composition of real components, NOT the production three-entry claim
// orchestration; no finalize SQL is duplicated and the T10-A protocol is
// unchanged
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// cancelFrameCounter counts control dials whose first write is a real
// protocol CancelRequest frame (length + code 80877102) — packet shape only,
// pid/secret bytes are never stored.
type cancelFrameCounter struct {
	packets atomic.Int64
}

type cancelFrameConn struct {
	net.Conn
	seen *atomic.Int64
	once sync.Once
}

func (c *cancelFrameConn) Write(b []byte) (int, error) {
	c.once.Do(func() {
		if len(b) >= 8 &&
			binary.BigEndian.Uint32(b[0:4]) == uint32(len(b)) &&
			binary.BigEndian.Uint32(b[4:8]) == 80877102 {
			c.seen.Add(1)
		}
	})
	return c.Conn.Write(b)
}

// TestClaim_RealPGCancelEvidenceFlow drives the T10-B vertical: a real claim
// occupies the keyed slot, the production governed PG entry runs a long query
// on the restricted role, the caller cancels mid-execution, and the returned
// internal evidence is finalized through the real MySQL claim repository on a
// WithoutCancel evidence window. The point read must return the real backend
// pid, the real observed remote_state, and the cancelled terminal class —
// nothing fabricated.
func TestClaim_RealPGCancelEvidenceFlow(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createPGInstance(t, db, "t10b-cancel-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	key := "t10b-cancel-key"

	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), "discdb", claimDigest("t10b"))); err != nil {
		t.Fatalf("claim: %v", err)
	}

	cfg, err := pgx.ParseConfig(fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=ch-t10b-claim",
		fix.host, fix.port, fix.db, pgDiscRole, pgStmtPassword))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	frames := &cancelFrameCounter{}
	innerDial := cfg.DialFunc
	cfg.DialFunc = func(dctx context.Context, network, addr string) (net.Conn, error) {
		c, err := innerDial(dctx, network, addr)
		if err != nil {
			return nil, err
		}
		return &cancelFrameConn{Conn: c, seen: &frames.packets}, nil
	}
	// The user statement is a CPU-bound cartesian count over the fixture
	// table: every executor iteration reaches an interrupt point, so a
	// delivered CancelRequest kills it in milliseconds. (A FROM-clause
	// generate_series is avoided on purpose — PostgreSQL materialises
	// RangeFunction SRFs into a tuplestore that can spill to pgsql_tmp.)
	in := service.PGExecuteInput{
		ConnConfig:   cfg,
		Database:     fix.db,
		PinnedSchema: "app",
		Statement:    `SELECT count(*) FROM smallvals a, smallvals b, smallvals c, smallvals d, smallvals e`,
		Page:         pgsql.PageOptions{Page: 1, PageSize: 25, MaxRows: 100},
		Timeout:      30 * time.Second,
	}

	reqCtx, reqCancel := context.WithCancel(ctx)
	type outcome struct {
		ev  *service.PGAttemptEvidence
		err error
	}
	service.PGExecProbeLogForTest(func(s string) { t.Logf("probe: %s", s) })
	t.Cleanup(func() { service.PGExecProbeLogForTest(nil) })

	done := make(chan outcome, 1)
	go func() {
		res, ev, err := service.ExecutePGGovernedOutcome(reqCtx, in)
		if res != nil {
			err = fmt.Errorf("rows delivered on cancelled attempt: %w", err)
		}
		done <- outcome{ev, err}
	}()

	// Deterministic barrier: cancel only after the server reports OUR user
	// statement active on the exec application's backend. The count(*)
	// marker appears only in the (rewritten) user statement — internal
	// probes never carry it.
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	pollCtx, pollCancel := context.WithTimeout(ctx, 10*time.Second)
	defer pollCancel()
	deadline := time.Now().Add(8 * time.Second)
	for {
		var n int64
		if err := admin.QueryRow(pollCtx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = 'ch-t10b-claim' AND state = 'active'
			    AND position('count(*)' in query) > 0`).Scan(&n); err != nil {
			t.Fatalf("poll backend: %v", err)
		}
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("user statement never became active on the exec backend")
		}
		time.Sleep(20 * time.Millisecond)
	}
	reqCancel()

	var out outcome
	select {
	case out = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled attempt did not return")
	}
	if out.err == nil || !errors.Is(out.err, context.Canceled) {
		t.Fatalf("executor err = %v, want context.Canceled", out.err)
	}
	ev := out.ev
	if ev == nil || !ev.UserSQLSent || ev.BackendPID == 0 {
		t.Fatalf("evidence = %+v, want sent sql + real backend pid", ev)
	}
	if ev.Status != model.QueryExecutionCancelled {
		t.Fatalf("evidence status = %q, want cancelled", ev.Status)
	}
	if frames.packets.Load() != 1 {
		t.Fatalf("cancel frames on wire = %d, want 1", frames.packets.Load())
	}
	// The 2s probe window is frozen: when the server's cancel dispatch
	// outlives it, the honest answer is unknown — the strict stopped proof
	// lives in the service-level cancel matrix. What the claim path must
	// guarantee is that the OBSERVED verdict (whichever it was) round-trips
	// through the real repository unchanged.
	if ev.RemoteState != model.QueryExecutionRemoteStopped &&
		ev.RemoteState != model.QueryExecutionRemoteUnknown {
		t.Fatalf("remote_state = %q, want stopped or unknown", ev.RemoteState)
	}
	// Bounded convergence: the destroyed conn must not leave a backend.
	// Budget exceeds pgx's asyncClose drain deadline (15s) — the conn-EOF
	// backstop for a cancel the server never processed.
	goneDeadline := time.Now().Add(20 * time.Second)
	for {
		var n int64
		if err := admin.QueryRow(ctx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE application_name = 'ch-t10b-claim'`).Scan(&n); err == nil && n == 0 {
			break
		}
		if time.Now().After(goneDeadline) {
			t.Fatal("backend still alive 10s after cancel")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Consume the real evidence through the real claim repository on a
	// detached evidence window — the cancelled request ctx must not block
	// the bounded write.
	pid := int64(ev.BackendPID)
	rec := model.QueryExecutionRecord{
		TargetResourceID:  targetID,
		Engine:            "postgresql",
		DatabaseName:      "discdb",
		SchemaName:        "app",
		StatementDigest:   claimDigest("t10b"),
		StatementPreview:  "SELECT count(*) FROM smallvals",
		Status:            ev.Status,
		DurationMs:        0,
		ErrorCode:         "query_canceled",
		ErrorMessage:      "query canceled",
		BackendPID:        &pid,
		RemoteState:       ev.RemoteState,
		ClientExecutionID: &key,
		ActorUserID:       claimUserActor,
	}
	winCtx, winCancel := context.WithTimeout(context.WithoutCancel(reqCtx), 2*time.Second)
	defer winCancel()
	execID, err := repo.FinalizeClaimWithAudit(winCtx, rec, "query.executed", "cancelled")
	if err != nil {
		t.Fatalf("finalize with real evidence: %v", err)
	}
	if execID == 0 {
		t.Fatal("finalize returned id 0 — fabricated execution id")
	}

	view, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", key, userIdentity(claimUserActor))
	if err != nil {
		t.Fatalf("read linked claim: %v", err)
	}
	if view.Execution == nil || view.Execution.ID != execID || view.Claim.ExecutionID != execID {
		t.Fatalf("linked view mismatch: claim=%+v exec=%+v", view.Claim, view.Execution)
	}
	got := view.Execution
	if got.Status != model.QueryExecutionCancelled ||
		got.BackendPID == nil || *got.BackendPID != pid ||
		got.RemoteState != ev.RemoteState ||
		got.ClientExecutionID == nil || *got.ClientExecutionID != key {
		t.Fatalf("persisted evidence mismatch: %+v vs ev=%+v", got, ev)
	}
	if got.ActorUserID != claimUserActor || got.ActorMachinePrincipalID != 0 {
		t.Fatalf("typed actor not preserved: %+v", got)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, key); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link counts = %d/%d/%d, want exactly 1/1/1", execs, audits, linked)
	}
	// Cancelled status must not persist the full statement text (the
	// success+user-actor privacy rule).
	if got.HasFullStatement {
		t.Fatal("cancelled execution must not persist full statement")
	}
}
