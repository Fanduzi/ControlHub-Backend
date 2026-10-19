//go:build integration

// Package integration — real cancellation proofs for the PG-MULTI-ENTRY
// claimed entries.
// input: disposable PG16 fixtures (CPU-bound cartesian template for saved;
// 5M-row seqscan target with planner alternatives disabled for navigation),
// real MySQL production dependencies
// output: TestPGMultiEntryCancel_* — the caller cancels only after the real
// server reports the actual result statement active (application_name +
// marker + state; saved adds a 0.25 s query-age barrier, navigation polls
// state/marker with no age floor), and the entry returns genuine evidence:
// real backend pid, UserSQLSent true, cancelled status, observed
// remote_state — never a guessed or fabricated state
// pos: stage 4 packet item 3 — in-flight cancellation of both new entries
// through the production methods; completed-call stats are never used for
// aborted statements
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// pollActiveResultStatement waits until the fixture server reports the
// attempt's result statement in execution — the marker appears only in the
// (rewritten) user statement, never in fixed metadata or touch probes.
// minAgeSecs is a test-side steady-execution barrier for long-running
// statements; fast statements pass 0 so the short window is not filtered
// away.
func pollActiveResultStatement(t *testing.T, admin *pgx.Conn, marker string, minAgeSecs float64, early <-chan string) {
	t.Helper()
	pollCtx, pollCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer pollCancel()
	for {
		var n int64
		// The admin connection itself carries application_name ch-t6 — its
		// own poll query contains the marker literal, so the poll must
		// exclude the caller's backend or it matches itself. The R1-1
		// provenance Parse/Describe carries the same rewritten text —
		// including __chub_w — under the fixed probe prefix, so it must be
		// excluded or the barrier could fire on a Describe instead of the
		// actual result Execute.
		if err := admin.QueryRow(pollCtx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = 'ch-t6' AND state = 'active'
			    AND pid <> pg_backend_pid()
			    AND pg_catalog.strpos(query, $1) > 0
			    AND pg_catalog.strpos(query, '__chub_parameter_probe__') = 0
			    AND clock_timestamp() - query_start >= make_interval(secs => $2)`,
			marker, minAgeSecs).Scan(&n); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("activity poll: %v", err)
		}
		if n > 0 {
			return
		}
		select {
		case diag := <-early:
			t.Fatalf("entry returned before the result statement became active: %s", diag)
		default:
		}
		if pollCtx.Err() != nil {
			t.Fatalf("result statement containing %q never became active", marker)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// keyedCancelledHistory runs the real ListHistory path on a fresh context
// and returns the single keyed row — cancelled status, real pid, and
// remote_state must match the outcome's evidence and the persisted row.
func keyedCancelledHistory(t *testing.T, svc *service.QueryExecutionService, actor model.QueryExecutionIdentity,
	targetID uint64, key string, execID uint64, ev *service.PGAttemptEvidence) {
	t.Helper()
	page, err := svc.ListHistory(context.Background(), actor, "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID, ClientExecutionID: key,
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("list items = %d, want exactly 1", len(page.Items))
	}
	it := page.Items[0]
	if it.Kind != model.QueryExecutionListItemExecution || it.ID == nil || *it.ID != execID {
		t.Fatalf("list item = %+v, want the persisted terminal row %d", it, execID)
	}
	if it.Status != model.QueryExecutionViewStatus(model.QueryExecutionCancelled) ||
		it.Status != model.QueryExecutionViewStatus(ev.Status) {
		t.Fatalf("list status = %v, want cancelled matching evidence %v", it.Status, ev.Status)
	}
	if it.BackendPID == nil || *it.BackendPID != int64(ev.BackendPID) {
		t.Fatalf("list pid = %v, want evidence pid %d", it.BackendPID, ev.BackendPID)
	}
	if it.RemoteState != ev.RemoteState {
		t.Fatalf("list remote_state = %q, want %q", it.RemoteState, ev.RemoteState)
	}
}

// The saved entry's request context cancels a real in-flight attempt: the
// production entry returns the cancelled class with real pid/sent evidence,
// ListHistory on a fresh context shows the keyed cancelled row, and the key
// stays a discoverable duplicate — never a second execution.
func TestPGMultiEntryCancel_Saved(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-cancel-s-"+strings.ReplaceAll(t.Name(), "/", "-"))
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "tag", model.ResultDisclosureRawCopyAllowed)

	stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name: "cartesian count",
		Statement: `SELECT count(*) FROM app.smallvals a, app.smallvals b,
		    app.smallvals c, app.smallvals d, app.smallvals e`,
		Scope: model.QuerySavedStatementPersonal,
	})

	reqCtx, reqCancel := context.WithCancel(ctx)
	type outcome struct {
		out service.PGClaimedOutcome
		err error
	}
	done := make(chan outcome, 1)
	early := make(chan string, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		o, e := svc.ExecutePGSavedStatementClaimed(reqCtx, userIdentity(pgSavedOwner), targetID, stmt.ID,
			savedRequest("pgmulti-cancel-s", nil))
		early <- fmt.Sprintf("err=%v status=%s", e, o.Status)
		done <- outcome{o, e}
	}()
	// The in-flight goroutine must be joined before teardown even when a
	// later assertion fails: the done channel may already be consumed.
	defer func() {
		reqCancel()
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Error("saved attempt goroutine never finished")
		}
	}()

	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	// Test barrier: cancel only while the saved result statement is in
	// steady execution (server-measured 0.25 s query age).
	pollActiveResultStatement(t, admin, "count(*)", 0.25, early)
	reqCancel()

	var res outcome
	select {
	case res = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled saved attempt did not return")
	}
	if res.err == nil || !errors.Is(res.err, context.Canceled) {
		t.Fatalf("saved err = %v, want context.Canceled", res.err)
	}
	if res.out.Evidence == nil || !res.out.Evidence.UserSQLSent || res.out.Evidence.BackendPID == 0 {
		t.Fatalf("saved evidence = %+v, want sent sql + real pid", res.out.Evidence)
	}
	if res.out.Status != model.QueryExecutionCancelled {
		t.Fatalf("saved status = %q, want cancelled", res.out.Status)
	}
	if res.out.Evidence.RemoteState != model.QueryExecutionRemoteStopped &&
		res.out.Evidence.RemoteState != model.QueryExecutionRemoteUnknown {
		t.Fatalf("remote_state = %q, want the observed stopped or unknown", res.out.Evidence.RemoteState)
	}

	// The keyed cancelled row is readable through ListHistory on a fresh
	// context; the key stays occupied — a retry is the discoverable
	// duplicate.
	keyedCancelledHistory(t, svc, userIdentity(pgSavedOwner), targetID, "pgmulti-cancel-s", res.out.ExecutionID, res.out.Evidence)
	status, code := keyedSavedRow(t, db, targetID, "pgmulti-cancel-s")
	if status != string(model.QueryExecutionCancelled) || code != "query_cancelled" ||
		status != string(res.out.Status) || status != string(res.out.Evidence.Status) {
		t.Fatalf("keyed row = %q/%q vs outcome/evidence %q/%q, want cancelled/query_cancelled",
			status, code, res.out.Status, res.out.Evidence.Status)
	}
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
		savedRequest("pgmulti-cancel-s", nil)); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key after cancel = %v, want already-exists", err)
	}

	// Bounded convergence: the cancelled backend drains — the caller's own
	// backend is excluded so the poll cannot match itself.
	deadline := time.Now().Add(20 * time.Second)
	for {
		var n int64
		if err := admin.QueryRow(ctx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = 'ch-t6' AND state = 'active'
			    AND pid <> pg_backend_pid()
			    AND pg_catalog.strpos(query, 'count(*)') > 0`).Scan(&n); err == nil && n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled backend still active")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The navigation entry cancels mid-seqscan on a dedicated slow target: the
// restricted role's test-only planner settings force a sequential scan over
// 5M rows so the __chub_w-marked result statement is genuinely active when
// the caller cancels. Missing locator 999999999 is a legal no-match request.
func TestPGMultiEntryCancel_Navigation(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	ctx := context.Background()

	// Dedicated slow target — 5M rows plus a source FK pointing at it. The
	// role-level planner overrides apply only to this disposable fixture's
	// role settings and only for connections opened after the ALTER.
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	for _, stmt := range []string{
		`CREATE TABLE app.nav_slow (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.nav_slow SELECT g, 'note-' || g::text FROM generate_series(1, 5000000) g`,
		`CREATE TABLE app.nav_source (id bigint PRIMARY KEY, sid bigint,
		    CONSTRAINT fk_ns FOREIGN KEY (sid) REFERENCES app.nav_slow(id))`,
		`GRANT SELECT ON app.nav_slow TO ` + pgDiscRole,
		`GRANT SELECT ON app.nav_source TO ` + pgDiscRole,
		`ALTER ROLE ` + pgDiscRole + ` SET enable_indexscan = off`,
		`ALTER ROLE ` + pgDiscRole + ` SET enable_indexonlyscan = off`,
		`ALTER ROLE ` + pgDiscRole + ` SET enable_bitmapscan = off`,
		`ALTER ROLE ` + pgDiscRole + ` SET max_parallel_workers_per_gather = 0`,
	} {
		execPG(t, admin, stmt)
	}

	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-cancel-n-"+strings.ReplaceAll(t.Name(), "/", "-"))
	navPolicy(t, disc, targetID, "discdb", "app", "nav_slow", "id", "note")

	reqCtx, reqCancel := context.WithCancel(ctx)
	type outcome struct {
		out service.PGClaimedNavigationOutcome
		err error
	}
	done := make(chan outcome, 1)
	early := make(chan string, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		o, e := svc.NavigatePGRelatedRecordsClaimed(reqCtx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-cancel-n", navSource("discdb", "app", "nav_source", "fk_ns"), []string{"999999999"}))
		early <- fmt.Sprintf("err=%v status=%s", e, o.Status)
		done <- outcome{o, e}
	}()
	// The in-flight goroutine must be joined before teardown even when a
	// later assertion fails: the done channel may already be consumed.
	defer func() {
		reqCancel()
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Error("navigation attempt goroutine never finished")
		}
	}()

	// The __chub_w witness marker appears only inside the rewritten result
	// statement — metadata probes and touch reads never carry it. The test
	// barrier polls state/marker at 2 ms intervals with no age floor; the
	// seqscan window is short, so the marker itself is the barrier.
	pollActiveResultStatement(t, admin, "__chub_w", 0, early)
	reqCancel()

	var res outcome
	select {
	case res = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled navigation attempt did not return")
	}
	if res.err == nil || !errors.Is(res.err, context.Canceled) {
		t.Fatalf("nav err = %v, want context.Canceled", res.err)
	}
	if res.out.Evidence == nil || !res.out.Evidence.UserSQLSent || res.out.Evidence.BackendPID == 0 {
		t.Fatalf("nav evidence = %+v, want sent sql + real pid", res.out.Evidence)
	}
	if res.out.Status != model.QueryExecutionCancelled {
		t.Fatalf("nav status = %q, want cancelled", res.out.Status)
	}
	if res.out.Evidence.RemoteState != model.QueryExecutionRemoteStopped &&
		res.out.Evidence.RemoteState != model.QueryExecutionRemoteUnknown {
		t.Fatalf("remote_state = %q, want the observed stopped or unknown", res.out.Evidence.RemoteState)
	}
	if res.out.Relation != nil {
		t.Fatal("cancelled navigation carried relation metadata")
	}
	keyedCancelledHistory(t, svc, userIdentity(claimUserActor), targetID, "pgmulti-cancel-n", res.out.ExecutionID, res.out.Evidence)
	status, code := keyedSavedRow(t, db, targetID, "pgmulti-cancel-n")
	if status != string(model.QueryExecutionCancelled) || code != "query_cancelled" ||
		status != string(res.out.Status) || status != string(res.out.Evidence.Status) {
		t.Fatalf("nav keyed row = %q/%q vs outcome/evidence %q/%q, want cancelled/query_cancelled",
			status, code, res.out.Status, res.out.Evidence.Status)
	}
	if _, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-cancel-n", navSource("discdb", "app", "nav_source", "fk_ns"), []string{"999999999"})); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("nav same-key after cancel = %v, want already-exists", err)
	}
}
