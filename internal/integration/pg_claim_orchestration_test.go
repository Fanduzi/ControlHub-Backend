//go:build integration

// Package integration — real PG16 + real MySQL vertical for T10-C keyed claim
// orchestration and mixed state reads (issue #118, frozen G9).
// input: shared MySQL fixture (real target/credential/claim/execution repos),
// a disposable PostgreSQL 16 with a restricted role, the production
// ExecutePGClaimed entry, and the real ListHistory read path
// output: TestPGClaimed* — vertical flow, concurrent admission, digest
// conflict, evidence-boundary rows, defaults/capabilities digest equivalence,
// cancel finalize, finalize-failure rollback, derived running/unknown reads,
// typed-actor scope, and the real HTTP list read
// pos: T10-C production-orchestration proof — requests travel the real
// service entry; no test hand-composes claim/finalize calls for the covered
// paths. The HTTP POST gate stays closed; reads are the only HTTP surface.
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const pgClaimCredRef = "PG_T10C_RO"

// newPGClaimedService wires the production keyed-PG entry exactly as the
// server would: real MySQL repos, the env credential resolver, and the real
// disclosure-policy reader for the provenance preflight.
func newPGClaimedService(db *sql.DB) *service.QueryExecutionService {
	return newExecutionService(db).WithPGClaimOrchestration(mysqlrepo.NewQueryDisclosureRepository(db))
}

// setupPGClaimTarget creates the real PG target + enabled postgresql
// credential row (pinned default_schema app) and binds the lab DSN through
// the env resolver — the same chain ResolvePGExecution walks in production.
func setupPGClaimTarget(t *testing.T, db *sql.DB, fix pgDiscFixture, name string) uint64 {
	t.Helper()
	targetID := createPGInstance(t, db, name, envStaging, fix.host, fix.port)
	insertPGCredential(t, db, targetID, fix.db, "app", pgClaimCredRef, true, string(model.QueryEnvPolicyNonProdOnly))
	return targetID
}

func setupPGClaimEnv(t *testing.T, fix pgDiscFixture) {
	t.Helper()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+pgClaimCredRef,
		pgKeywordDSN(fix.host, fix.port, fix.db, pgDiscRole, pgStmtPassword, 5))
}

// createPGSecondDB adds a second real database on the fixture so a request
// can resolve access for a different database identity under the same target
// — the only way a same-key cross-database request reaches claim admission.
func createPGSecondDB(t *testing.T, fix pgDiscFixture, name string) {
	t.Helper()
	boot := connectPG(t, fix.host, fix.port, "postgres", pgMetaAdminUser, pgMetaAdminPassword)
	execPG(t, boot, `CREATE DATABASE `+name)
	execPG(t, boot, `GRANT CONNECT ON DATABASE `+name+` TO `+pgDiscRole)
	admin := connectPG(t, fix.host, fix.port, name, pgMetaAdminUser, pgMetaAdminPassword)
	execPG(t, admin, `CREATE SCHEMA app`)
	execPG(t, admin, `GRANT USAGE ON SCHEMA app TO `+pgDiscRole)
}

func claimedRequest(key, stmt string) service.PGClaimedExecuteRequest {
	return service.PGClaimedExecuteRequest{
		ClientExecutionID: key,
		Database:          "discdb",
		Statement:         stmt,
	}
}

func claimOccupied(t *testing.T, db *sql.DB, targetID uint64, key string) bool {
	t.Helper()
	return countWhere(t, db,
		`SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = ?`,
		targetID, key) == 1
}

// ---------------------------------------------------------------------------
// Vertical: real request → access → claim → governed execute → keyed finalize
// → consistent read back through both the point view and the mixed list.
// ---------------------------------------------------------------------------

func TestPGClaimed_VerticalFlow(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "t10c-vert-"+strings.ReplaceAll(t.Name(), "/", "-"))
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)

	out, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-vert-key", `SELECT id FROM app.orders ORDER BY id`))
	if err != nil {
		t.Fatalf("ExecutePGClaimed: %v", err)
	}
	if out.ExecutionID == 0 || out.Status != model.QueryExecutionSuccess || out.Result == nil {
		t.Fatalf("outcome = %+v, want real execution id + disclosed result", out)
	}
	if out.Evidence == nil || out.Evidence.BackendPID == 0 {
		t.Fatalf("evidence = %+v, want real backend pid", out.Evidence)
	}

	// The keyed point read returns the claim linked to the real terminal row.
	view, err := repo.GetClaimWithExecution(ctx, targetID, "discdb", "t10c-vert-key", userIdentity(claimUserActor))
	if err != nil {
		t.Fatalf("point read: %v", err)
	}
	if view.Execution == nil || view.Execution.ID != out.ExecutionID || view.Claim.ExecutionID != out.ExecutionID {
		t.Fatalf("linked view mismatch: claim=%+v exec=%+v", view.Claim, view.Execution)
	}
	if view.Execution.Status != model.QueryExecutionSuccess ||
		view.Execution.ClientExecutionID == nil || *view.Execution.ClientExecutionID != "t10c-vert-key" {
		t.Fatalf("terminal evidence mismatch: %+v", view.Execution)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "t10c-vert-key"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}

	// The mixed list returns the terminal row — never a duplicate claim entry.
	page, err := svc.ListHistory(ctx, userIdentity(claimUserActor), "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID, ClientExecutionID: "t10c-vert-key",
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("list items = %d, want exactly 1 (no claim/terminal duplication)", len(page.Items))
	}
	it := page.Items[0]
	if it.Kind != model.QueryExecutionListItemExecution || it.ID == nil || *it.ID != out.ExecutionID {
		t.Fatalf("list item = %+v, want the real terminal row", it)
	}
	if it.Status != model.QueryExecutionViewStatus(model.QueryExecutionSuccess) {
		t.Fatalf("list status = %v, want success", it.Status)
	}
}

// ---------------------------------------------------------------------------
// A. Concurrent same-key admission: exactly one request executes real SQL;
// the losers get already-exists with no history, no audit, no execution.
// ---------------------------------------------------------------------------

func TestPGClaimed_ConcurrentSameKey(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-race-"+strings.ReplaceAll(t.Name(), "/", "-"))

	const n = 6
	start := make(chan struct{})
	errs := make([]error, n)
	outs := make([]service.PGClaimedOutcome, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outs[i], errs[i] = svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
				claimedRequest("t10c-race-key", `SELECT 1 AS one`))
		}(i)
	}
	close(start)
	wg.Wait()

	var succeeded, dup, other int
	for i, err := range errs {
		switch {
		case err == nil && outs[i].ExecutionID != 0:
			succeeded++
		case errors.Is(err, service.ErrQueryExecutionAlreadyExists):
			dup++
		default:
			other++
			t.Logf("unexpected outcome[%d]: out=%+v err=%v", i, outs[i], err)
		}
	}
	if succeeded != 1 || dup != n-1 || other != 0 {
		t.Fatalf("succeeded=%d dup=%d other=%d, want 1/%d/0", succeeded, dup, other, n-1)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "t10c-race-key"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link = %d/%d/%d, want exactly 1/1/1", execs, audits, linked)
	}

	// A retry after the terminal row exists still executes nothing.
	_, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-race-key", `SELECT 1 AS one`))
	if !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("post-terminal retry = %v, want already_exists", err)
	}
	if execs, _, _ := claimExecCounts(t, db, targetID, "t10c-race-key"); execs != 1 {
		t.Fatalf("retry wrote %d executions, want still 1", execs)
	}
}

// ---------------------------------------------------------------------------
// B. Evidence boundary rows: shape failures write nothing; access refusal
// writes one NULL-key rejected pair and leaves the key unoccupied; a post-
// claim guard rejection finalizes keyed evidence and keeps the key occupied.
// ---------------------------------------------------------------------------

func TestPGClaimed_EvidenceBoundary(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-bound-"+strings.ReplaceAll(t.Name(), "/", "-"))

	execRows := func() int {
		return countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID)
	}
	auditRows := func() int {
		return countWhere(t, db, `SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.executed'`, targetID)
	}

	// Shape failure: no occupancy, no evidence.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		service.PGClaimedExecuteRequest{ClientExecutionID: "", Database: "discdb", Statement: "select 1"}); !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("missing key = %v, want validation_failed", err)
	}
	if execRows() != 0 || auditRows() != 0 {
		t.Fatal("shape failure wrote evidence")
	}

	// Unknown target: nothing attributable.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID+99999,
		claimedRequest("t10c-b-missing", `SELECT 1`)); !errors.Is(err, service.ErrQueryTargetNotFound) {
		t.Fatalf("unknown target = %v, want target_not_found", err)
	}
	if claimOccupied(t, db, targetID+99999, "t10c-b-missing") {
		t.Fatal("unknown target occupied a key")
	}

	// Disabled credential: one NULL-key rejected pair; key stays free.
	disabledTarget := createPGInstance(t, db, "t10c-disabled-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	insertPGCredential(t, db, disabledTarget, "discdb", "app", pgClaimCredRef, false, string(model.QueryEnvPolicyNonProdOnly))
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), disabledTarget,
		claimedRequest("t10c-b-denied", `SELECT 1`)); !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("disabled credential = %v, want not_allowed", err)
	}
	if claimOccupied(t, db, disabledTarget, "t10c-b-denied") {
		t.Fatal("access denial occupied the key")
	}
	deniedRows := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NULL AND status = 'rejected' AND error_code = 'query_not_allowed'`,
		disabledTarget)
	if deniedRows != 1 {
		t.Fatalf("NULL-key rejected rows = %d, want 1", deniedRows)
	}

	// Guard rejection after claim: keyed rejected row, claim stays linked.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-b-guard", `DELETE FROM app.orders`)); err == nil {
		t.Fatal("guard rejection must surface an error")
	}
	if !claimOccupied(t, db, targetID, "t10c-b-guard") {
		t.Fatal("claimed key vanished after post-claim rejection")
	}
	guarded := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 't10c-b-guard' AND status = 'rejected'`,
		targetID)
	if guarded != 1 {
		t.Fatalf("keyed rejected rows = %d, want 1", guarded)
	}
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-b-guard", `DELETE FROM app.orders`)); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("retry of finalized key = %v, want already_exists", err)
	}
}

// ---------------------------------------------------------------------------
// C. Resolved-default and capability equivalence: identical resolved requests
// share one digest — the second admission is already-exists; a different
// statement under the same key conflicts. PK is (target, key): a different
// database under the same key can never take a second admission.
// ---------------------------------------------------------------------------

func TestPGClaimed_DigestAndDefaults(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-digest-"+strings.ReplaceAll(t.Name(), "/", "-"))

	// Explicit defaults resolve identically to absent ones.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-d-defaults", `SELECT 1 AS one`)); err != nil {
		t.Fatalf("first execute: %v", err)
	}
	explicit := claimedRequest("t10c-d-defaults", `SELECT 1 AS one`)
	explicit.Page, explicit.PageSize, explicit.MaxRows = 1, 50, 100
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID, explicit); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("explicit-default retry = %v, want already_exists", err)
	}
	// Capabilities never enter the digest.
	caps := claimedRequest("t10c-d-defaults", `SELECT 1 AS one`)
	caps.Capabilities = []string{"csv"}
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID, caps); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("capabilities retry = %v, want already_exists", err)
	}
	// A statement change under the same key is a conflict, not an execution.
	conflict := claimedRequest("t10c-d-defaults", `SELECT 2 AS two`)
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID, conflict); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("digest mismatch = %v, want conflict", err)
	}
	if execs, _, _ := claimExecCounts(t, db, targetID, "t10c-d-defaults"); execs != 1 {
		t.Fatalf("conflict wrote %d executions, want 1", execs)
	}

	// Same key, different database: both connection identities resolve, the
	// digest differs, and the (target,key) PK refuses the second admission.
	createPGSecondDB(t, fix, "discdb2")
	insertPGCredential(t, db, targetID, "discdb2", "app", "PG_T10C_DB2", true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_PG_T10C_DB2",
		pgKeywordDSN(fix.host, fix.port, "discdb2", pgDiscRole, pgStmtPassword, 5))
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-d-dbscope", `SELECT 1 AS one`)); err != nil {
		t.Fatalf("first dbscope execute: %v", err)
	}
	otherDB := claimedRequest("t10c-d-dbscope", `SELECT 1 AS one`)
	otherDB.Database = "discdb2"
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID, otherDB); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("cross-database same-key = %v, want conflict", err)
	}
}

// ---------------------------------------------------------------------------
// D. Request cancellation still lands a real keyed terminal row: cancelled
// status, real backend pid, observed remote_state, claim linked — readable
// through the mixed history.
// ---------------------------------------------------------------------------

func TestPGClaimed_CancelFinalize(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	// A dedicated application_name keeps the activity probe unambiguous.
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+pgClaimCredRef, fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=disable connect_timeout=5 application_name=ch-t10c-cancel",
		fix.host, fix.port, fix.db, pgDiscRole, pgStmtPassword))
	svc := newPGClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-cancel-"+strings.ReplaceAll(t.Name(), "/", "-"))
	// count(*) expands over the whole-row slot — every smallvals column needs
	// a policy before the disclosed chain will send the statement.
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "smallvals", "tag", model.ResultDisclosureRawCopyAllowed)

	reqCtx, reqCancel := context.WithCancel(ctx)
	type outcome struct {
		out service.PGClaimedOutcome
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		// The CPU-bound cartesian count keeps the backend interruptible; the
		// count(*) text marker identifies our rewritten statement in
		// pg_stat_activity (pg_sleep itself is guard-forbidden).
		out, err := svc.ExecutePGClaimed(reqCtx, userIdentity(claimUserActor), targetID,
			claimedRequest("t10c-cancel-key", `SELECT count(*) FROM app.smallvals a, app.smallvals b, app.smallvals c, app.smallvals d, app.smallvals e`))
		done <- outcome{out, err}
	}()

	// Cancel only after the exec backend reports the sleeping user statement
	// — the barrier keeps the cancel out of the startup edge.
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	pollCtx, pollCancel := context.WithTimeout(ctx, 10*time.Second)
	defer pollCancel()
	deadline := time.Now().Add(8 * time.Second)
	var res outcome
	for {
		select {
		case res = <-done:
			t.Fatalf("attempt returned before the cancel barrier: out=%+v err=%v", res.out, res.err)
		default:
		}
		var n int64
		if err := admin.QueryRow(pollCtx,
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = 'ch-t10c-cancel' AND state = 'active'
			    AND position('count(*)' in query) > 0
			    AND clock_timestamp() - query_start >= make_interval(secs => 0.25)`).Scan(&n); err != nil {
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

	select {
	case res = <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("cancelled claim execution did not return")
	}
	if res.err == nil || !errors.Is(res.err, context.Canceled) {
		t.Fatalf("outcome err = %v, want context.Canceled", res.err)
	}

	// The keyed terminal row is linked and carries the observed evidence.
	row := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 't10c-cancel-key' AND status = 'cancelled' AND backend_pid IS NOT NULL`,
		targetID)
	if row != 1 {
		t.Fatalf("keyed cancelled rows = %d, want 1 with real backend pid", row)
	}
	if !claimOccupied(t, db, targetID, "t10c-cancel-key") {
		t.Fatal("cancelled claim link missing")
	}
	page, err := svc.ListHistory(ctx, userIdentity(claimUserActor), "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID, ClientExecutionID: "t10c-cancel-key",
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Status != model.QueryExecutionViewStatus(model.QueryExecutionCancelled) {
		t.Fatalf("cancelled history items = %+v", page.Items)
	}
}

// ---------------------------------------------------------------------------
// E. Finalize failure: a real trigger rolls the whole keyed transaction back —
// no execution row, no audit, the claim stays discoverable and unlinked, and
// no compensating NULL-key pair appears.
// ---------------------------------------------------------------------------

func TestPGClaimed_FinalizeFailureRollback(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-finfail-"+strings.ReplaceAll(t.Name(), "/", "-"))

	mustExec(t, db, `CREATE TRIGGER t10c_fail_link BEFORE UPDATE ON query_execution_claims
		FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject link failure'`)
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP TRIGGER IF EXISTS t10c_fail_link`); err != nil {
			t.Logf("drop trigger: %v", err)
		}
	})

	_, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-fail-key", `SELECT 1 AS one`))
	if err == nil {
		t.Fatal("finalize failure must surface an error")
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "t10c-fail-key"); execs != 0 || audits != 0 || linked != 0 {
		t.Fatalf("rolled-back finalize left execs=%d audits=%d linked=%d, want 0/0/0", execs, audits, linked)
	}
	if !claimOccupied(t, db, targetID, "t10c-fail-key") {
		t.Fatal("claim must stay discoverable after finalize failure")
	}
	// The claim is still occupied — a retry cannot re-execute.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-fail-key", `SELECT 1 AS one`)); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("retry after finalize failure = %v, want already_exists", err)
	}
}

// ---------------------------------------------------------------------------
// F. Unlinked claims derive display states on read: fresh → running, past
// claimed_at + recorded budget + grace → unknown. The read writes nothing.
// ---------------------------------------------------------------------------

func TestPGClaimed_DerivedStateRead(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-derive-"+strings.ReplaceAll(t.Name(), "/", "-"))

	fresh := newClaimInput(targetID, "t10c-fresh", userIdentity(claimUserActor), "discdb", claimDigest("fresh"))
	fresh.AttemptTimeoutMs = 60_000
	if err := repo.TryClaimExecution(ctx, fresh); err != nil {
		t.Fatalf("fresh claim: %v", err)
	}
	stale := newClaimInput(targetID, "t10c-stale", userIdentity(claimUserActor), "discdb", claimDigest("stale"))
	stale.AttemptTimeoutMs = 1_000
	stale.ClaimedAt = time.Now().UTC().Add(-3 * time.Minute)
	if err := repo.TryClaimExecution(ctx, stale); err != nil {
		t.Fatalf("stale claim: %v", err)
	}

	execBefore := countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID)
	page, err := svc.ListHistory(ctx, userIdentity(claimUserActor), "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	execAfter := countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID)
	if execBefore != execAfter {
		t.Fatal("a read wrote execution rows")
	}

	got := map[string]model.QueryExecutionViewStatus{}
	for _, it := range page.Items {
		if it.Kind != model.QueryExecutionListItemClaim || it.ID != nil {
			t.Fatalf("unexpected item kind/id: %+v", it)
		}
		got[*it.ClientExecutionID] = it.Status
	}
	if got["t10c-fresh"] != model.QueryExecutionViewRunning {
		t.Fatalf("fresh = %v, want running", got["t10c-fresh"])
	}
	if got["t10c-stale"] != model.QueryExecutionViewUnknown {
		t.Fatalf("stale = %v, want unknown", got["t10c-stale"])
	}
}

// ---------------------------------------------------------------------------
// G. Typed-actor scope: user:7 and machine:7 claims are distinct occupants;
// non-admin reads see only their own typed actor's items.
// ---------------------------------------------------------------------------

func TestPGClaimed_TypedActorScope(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-scope-"+strings.ReplaceAll(t.Name(), "/", "-"))

	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "t10c-user-claim", userIdentity(claimUserActor), "discdb", claimDigest("u"))); err != nil {
		t.Fatalf("user claim: %v", err)
	}
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "t10c-machine-claim", machineIdentity(claimMachineID), "discdb", claimDigest("m"))); err != nil {
		t.Fatalf("machine claim: %v", err)
	}

	// user:7's non-admin read must not see machine:7's claim.
	page, err := svc.ListHistory(ctx, userIdentity(claimUserActor), "editor", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
	})
	if err != nil {
		t.Fatalf("user ListHistory: %v", err)
	}
	if len(page.Items) != 1 || *page.Items[0].ClientExecutionID != "t10c-user-claim" {
		t.Fatalf("user scope leaked: %+v", page.Items)
	}
	page, err = svc.ListHistory(ctx, machineIdentity(claimMachineID), "editor", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
	})
	if err != nil {
		t.Fatalf("machine ListHistory: %v", err)
	}
	if len(page.Items) != 1 || *page.Items[0].ClientExecutionID != "t10c-machine-claim" {
		t.Fatalf("machine scope leaked: %+v", page.Items)
	}
	// Admin sees both.
	page, err = svc.ListHistory(ctx, userIdentity(claimUserActor), "admin", targetID, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
	})
	if err != nil {
		t.Fatalf("admin ListHistory: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("admin items = %d, want 2", len(page.Items))
	}
}

// ---------------------------------------------------------------------------
// H. Real HTTP read: router → auth → service → MySQL UNION. Filters pass
// through; claim-derived items serialize with null id and a derived status.
// ---------------------------------------------------------------------------

func TestPGClaimed_HTTPMixedRead(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "t10c-http-"+strings.ReplaceAll(t.Name(), "/", "-"))

	// One terminal row (via the production entry) + one unlinked claim.
	svc := newPGClaimedService(db)
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("t10c-http-done", `SELECT 1 AS one`)); err != nil {
		t.Fatalf("terminal execute: %v", err)
	}
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "t10c-http-open", userIdentity(claimUserActor), "discdb", claimDigest("open"))); err != nil {
		t.Fatalf("open claim: %v", err)
	}

	adminID := insertAuthzTestUser(t, db, "t10c-http-admin@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, adminID) })
	router := api.NewRouter(api.Dependencies{
		AuthService:           service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuthAuditEmitter:      mysqlrepo.NewAuthAuditEmitter(db),
		QueryExecutionAuth:    api.QueryExecutionAuthConfig{Clock: time.Now},
		QueryExecutionService: svc,
	})
	token := mustLogin(t, router, "t10c-http-admin@example.com", "secret123")

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/query-targets/%d/executions?database=discdb", targetID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Items []struct {
			Kind              string  `json:"kind"`
			ID                *uint64 `json:"id"`
			ClientExecutionID *string `json:"clientExecutionId"`
			Status            string  `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v; body=%s", err, rec.Body.String())
	}
	byKey := map[string]struct {
		kind   string
		id     *uint64
		status string
	}{}
	for _, it := range page.Items {
		if it.ClientExecutionID == nil {
			continue
		}
		byKey[*it.ClientExecutionID] = struct {
			kind   string
			id     *uint64
			status string
		}{it.Kind, it.ID, it.Status}
	}
	terminal, ok := byKey["t10c-http-done"]
	if !ok || terminal.kind != "execution" || terminal.id == nil || *terminal.id == 0 || terminal.status != "success" {
		t.Fatalf("terminal item wrong: %+v", terminal)
	}
	open, ok := byKey["t10c-http-open"]
	if !ok || open.kind != "claim" || open.id != nil || open.status != "running" {
		t.Fatalf("claim item wrong: %+v (id must serialize null, status running)", open)
	}

	// The clientExecutionId filter narrows to the one keyed row.
	req = httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/query-targets/%d/executions?clientExecutionId=t10c-http-open", targetID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("filtered status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode filtered page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ClientExecutionID == nil || *page.Items[0].ClientExecutionID != "t10c-http-open" {
		t.Fatalf("filtered items = %+v", page.Items)
	}
}
