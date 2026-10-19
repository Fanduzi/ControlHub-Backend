//go:build integration

// Package integration — second half of the keyed multi-entry acceptance matrix: keyed live-path coverage for the MySQL ordinary/saved/navigation entries.
// input: shared disposable MySQL container (setupTestDB) + km_* fixture tables
// output: TestMySQLKeyedMatrix{B,E,F,G,I}_* — live admission, abort semantics, atomicity, privacy, and result-contract proofs
// pos: concurrency, real-HTTP abort, injected storage faults, privacy/authorization, and the result-contract boundary across ordinary/saved/navigation entries; the deterministic single-attempt groups live in mysql_keyed_matrix_test.go.
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// ---------------------------------------------------------------------------
// Group B — concurrency: N simultaneous same-key requests admit exactly one
// winner and one evidence path; losers classify deterministically against the
// already-committed claim without retrying or executing a second result
// command. Wire observation proves no second result command ever runs.
// ---------------------------------------------------------------------------

// kmConcurrentPost fires n identical POSTs and collects their responses.
func kmConcurrentPost(t *testing.T, h http.Handler, path, token string, bodies []string) []*httptest.ResponseRecorder {
	t.Helper()
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, len(bodies))
	start := make(chan struct{})
	for i, body := range bodies {
		wg.Add(1)
		go func(i int, body string) {
			defer wg.Done()
			<-start
			recs[i] = httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			h.ServeHTTP(recs[i], req)
		}(i, body)
	}
	close(start)
	wg.Wait()
	return recs
}

// kmTallyCodes buckets responses into (status, error-code) pairs.
func kmTallyCodes(t *testing.T, recs []*httptest.ResponseRecorder) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, r := range recs {
		code := "ok"
		if r.Code != http.StatusOK {
			code = kmErrorCode(t, r)
		}
		out[fmt.Sprintf("%d:%s", r.Code, code)]++
	}
	return out
}

func TestMySQLKeyedMatrixB_SixWaySameKey(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmCells})
	key := "km-b-six"

	// The winner parks inside its single result command; the five losers
	// then classify against the already-committed claim row.
	arrived, release := fx.observer.HoldNextResult(t)
	defer release()
	bodies := make([]string, 6)
	for i := range bodies {
		bodies[i] = `{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"` + key + `"}`
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 6)
	for i := range bodies {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			results <- fx.kmPost(t, fx.executePath, body)
		}(bodies[i])
	}
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("no result command reached the wire barrier")
	}
	// The five losers complete while the winner is still parked: they never
	// wait on the winner's outcome — the claim row alone decides. (Waiting
	// for all six would deadlock: the parked winner cannot return.)
	recs := make([]*httptest.ResponseRecorder, 0, 6)
	deadline := time.After(15 * time.Second)
	for len(recs) < 5 {
		select {
		case r := <-results:
			recs = append(recs, r)
		case <-deadline:
			t.Fatalf("losers did not classify while the winner was parked (done=%d)", len(recs))
		}
	}
	release()
	select {
	case r := <-results:
		recs = append(recs, r)
	case <-deadline:
		t.Fatal("winner did not complete after the barrier released")
	}
	wg.Wait()

	tally := kmTallyCodes(t, recs)
	if tally["200:ok"] != 1 || tally["409:execution_already_exists"] != 5 {
		t.Fatalf("six-way tally = %v, want 1x200 + 5x409 already-exists", tally)
	}
	// Exactly one result command and one reply ever crossed the wire.
	if c := fx.observer.Counts(); c.Results() != 1 || c.Replies != 1 {
		t.Fatalf("wire counts = %+v, want exactly 1 result + 1 reply", c)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

func TestMySQLKeyedMatrixB_SixWayConflictingBodies(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	key := "km-b-conflict"

	// Six different statements, same key: one wins, the rest conflict on the
	// stored digest — the digest difference is decided without executing.
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 6)
	start := make(chan struct{})
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			recs[i] = fx.kmPost(t, fx.executePath, fmt.Sprintf(
				`{"statement":"select id, note from query_e2e_aux.km_cells where id = %d","clientExecutionId":%q}`, 9-i, key))
		}(i)
	}
	close(start)
	wg.Wait()

	tally := kmTallyCodes(t, recs)
	outcomes := tally["200:ok"] + tally["409:execution_already_exists"] + tally["409:client_execution_id_conflict"]
	if outcomes != 6 || tally["200:ok"] != 1 {
		t.Fatalf("conflicting six-way tally = %v, want 1 winner + 5 classified losers", tally)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

func TestMySQLKeyedMatrixB_SameKeyCrossEntry(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	key := "km-b-xentry"

	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`))
	// The saved entry digests a different domain: same key → conflict, not
	// a second execution.
	stmtID := fx.kmCreateSaved(t,
		`{"name":"km-b-saved","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
	rec := fx.kmPost(t, fx.kmSavedExecPath(stmtID),
		`{"values":{"cell_id":9},"clientExecutionId":"`+key+`"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
		t.Fatalf("saved cross-entry = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	// Navigation the same: the related-records digest domain differs.
	rec = fx.kmPost(t, fx.navigationPath, kmNavBody("2", key))
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
		t.Fatalf("nav cross-entry = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

func TestMySQLKeyedMatrixB_ActorTypedIsolation(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	key := "km-b-actors"

	// user:A occupies the key; user:B and a machine actor replaying the
	// identical request still classify as already-exists — digest has no
	// actor input, so the key is occupied target-wide.
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`))
	rec := fx.kmPostAs(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`, fx.otherToken)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("other-user same-key replay = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	issued, err := fx.machineSvc.Create(t.Context(), service.AuthenticatedUser{ID: fx.adminUserID, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "km-b-machine", Scopes: []model.MachineScope{model.MachineScopeGovernedSelect},
	})
	if err != nil {
		t.Fatalf("create machine credential: %v", err)
	}
	rec = fx.kmPostAs(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`, issued.Secret)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("machine same-key replay = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	// The claim row keeps the original user-typed actor.
	_, _, _, actorUser, actorMachine := fx.kmClaimRow(t, key)
	if !actorUser.Valid || actorUser.Int64 != int64(fx.adminUserID) || actorMachine.Valid {
		t.Fatalf("claim actors = user:%v machine:%v, want user:%d only", actorUser, actorMachine, fx.adminUserID)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

func TestMySQLKeyedMatrixB_FinalizeFailureKeepsOccupied(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmCells, wrapRepo: true})
	key := "km-b-fault"

	// Park the winner at its result command — the claim is already
	// committed — then arm the finalize fault so the post-execute evidence
	// write fails.
	arrived, release := fx.observer.HoldNextResult(t)
	defer release()
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 6)
	body := `{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"` + key + `"}`
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recs[i] = fx.kmPost(t, fx.executePath, body)
		}(i)
	}
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("winner result command never reached the wire barrier")
	}
	fx.faultRepo.failClaimFinalize.Store(true)
	release()
	wg.Wait()

	tally := kmTallyCodes(t, recs)
	if tally["502:query_backend_error"] != 1 || tally["409:execution_already_exists"] != 5 {
		t.Fatalf("finalize-fault tally = %v, want 1x502 + 5x409", tally)
	}
	// The aborted finalize left no pair and no link — the claim alone
	// survives as the occupancy record.
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Claims: 1})
	// The key stays occupied: a same-key retry classifies already-exists
	// rather than re-running, and a new key executes cleanly.
	rec := fx.kmPost(t, fx.executePath, body)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("post-fault same-key retry = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	// A new key executes normally once the finalize fault is disarmed.
	fx.faultRepo.failClaimFinalize.Store(false)
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"km-b-fault-new"}`))
	if c := fx.observer.Counts(); c.Results() != 2 {
		t.Fatalf("result commands = %d, want 2 (fault attempt + new key)", c.Results())
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 2, Linked: 1})
}

func TestMySQLKeyedMatrixB_SavedConcurrent(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmCells})
	stmtID := fx.kmCreateSaved(t,
		`{"name":"km-b-sv6","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
	key := "km-b-sv6"
	arrived, release := fx.observer.HoldNextResult(t)
	defer release()
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 6)
	body := `{"values":{"cell_id":9},"clientExecutionId":"` + key + `"}`
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recs[i] = fx.kmPost(t, fx.kmSavedExecPath(stmtID), body)
		}(i)
	}
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("saved result command never reached the wire barrier")
	}
	release()
	wg.Wait()
	tally := kmTallyCodes(t, recs)
	if tally["200:ok"] != 1 || tally["409:execution_already_exists"] != 5 {
		t.Fatalf("saved six-way tally = %v", tally)
	}
	if c := fx.observer.Counts(); c.Results() != 1 || c.Prepares != 1 || c.Executes != 1 {
		t.Fatalf("saved wire counts = %+v, want 1 prepare + 1 execute", c)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

func TestMySQLKeyedMatrixB_NavConcurrent(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmParent})
	key := "km-b-nav6"
	arrived, release := fx.observer.HoldNextResult(t)
	defer release()
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, 6)
	body := kmNavBody("2", key)
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recs[i] = fx.kmPost(t, fx.navigationPath, body)
		}(i)
	}
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("nav result command never reached the wire barrier")
	}
	release()
	wg.Wait()
	tally := kmTallyCodes(t, recs)
	if tally["200:ok"] != 1 || tally["409:execution_already_exists"] != 5 {
		t.Fatalf("nav six-way tally = %v", tally)
	}
	if c := fx.observer.Counts(); c.Results() != 1 {
		t.Fatalf("nav wire results = %d, want 1", c.Results())
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

// ---------------------------------------------------------------------------
// Group E — real HTTP abort semantics: a client disconnect mid-flight is a
// wire/transport fact; the persistence fact is the claim-linked
// failed/query_canceled row written by the detached finalize window — the
// two must never be conflated.
// ---------------------------------------------------------------------------

// kmPostOverHTTP issues a real-network POST through a test server and
// returns a cancelable request plus its async completion channel.
func kmPostOverHTTP(t *testing.T, srv *httptest.Server, path, token, body string) (context.CancelFunc, <-chan error) {
	t.Helper()
	reqCtx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		resp, err := srv.Client().Do(req)
		if err == nil {
			resp.Body.Close()
		}
		done <- err
	}()
	return cancel, done
}

func TestMySQLKeyedMatrixE_AbortMidReply(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmCells})
	key := "km-e-ord"
	srv := httptest.NewServer(fx.router)
	defer srv.Close()

	// Hold the FIRST reply frame: MySQL executed the result command and the
	// first response bytes are on the wire — then the client vanishes.
	arrived, release := fx.observer.HoldNextResultReply(t)
	defer release()
	cancel, done := kmPostOverHTTP(t, srv, fx.executePath, fx.token,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`)
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("first result reply never reached the wire barrier")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("aborted keyed POST unexpectedly completed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("aborted keyed POST never observed the cancel")
	}

	// Persistence fact: the detached window still finalizes — one keyed
	// failed/query_canceled row linked to the claim, no retry.
	kmAwaitExecRows(t, fx.db, fx.targetID, 1, 10*time.Second)
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
	var status, code string
	var pid sql.NullInt64
	var remote sql.NullString
	if err := fx.db.QueryRow(`SELECT status, error_code, backend_pid, remote_state FROM query_executions WHERE target_resource_id = ?`, fx.targetID).
		Scan(&status, &code, &pid, &remote); err != nil {
		t.Fatalf("aborted row read: %v", err)
	}
	if status != string(model.QueryExecutionFailed) || code != "query_canceled" {
		t.Fatalf("abort evidence = %s/%s, want failed/query_canceled", status, code)
	}
	if pid.Valid {
		t.Fatalf("aborted MySQL row backend_pid = %v, want NULL", pid)
	}
	if remote.Valid && remote.String != "" {
		t.Fatalf("aborted MySQL row remote_state = %q, want empty", remote.String)
	}
	// Same-key retry classifies already-exists: the aborted attempt owns
	// the key; a new key still executes.
	rec := fx.kmPost(t, fx.executePath, `{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("post-abort same-key retry = %d %q", rec.Code, kmErrorCode(t, rec))
	}
}

func TestMySQLKeyedMatrixE_AbortBeforeForward(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmCells})
	key := "km-e-pre"
	srv := httptest.NewServer(fx.router)
	defer srv.Close()

	// Hold BEFORE the result command is forwarded: the claim is committed
	// but MySQL never receives the query — a distinct wire fact from the
	// mid-reply abort, same persistence fact.
	arrived, release := fx.observer.HoldNextResult(t)
	defer release()
	cancel, done := kmPostOverHTTP(t, srv, fx.executePath, fx.token,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`)
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("result command never reached the pre-forward barrier")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("pre-forward-aborted POST unexpectedly completed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pre-forward-aborted POST never observed the cancel")
	}
	kmAwaitExecRows(t, fx.db, fx.targetID, 1, 10*time.Second)
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
	var code string
	if err := fx.db.QueryRow(`SELECT error_code FROM query_executions WHERE target_resource_id = ?`, fx.targetID).Scan(&code); err != nil {
		t.Fatalf("pre-forward aborted row: %v", err)
	}
	if code != "query_canceled" {
		t.Fatalf("pre-forward abort error_code = %q, want query_canceled", code)
	}
	// Wire fact: zero selected replies — MySQL never answered.
	if c := fx.observer.Counts(); c.Replies != 0 {
		t.Fatalf("pre-forward abort replies = %d, want 0", c.Replies)
	}
}

func TestMySQLKeyedMatrixE_AbortNavigation(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmParent})
	key := "km-e-nav"
	srv := httptest.NewServer(fx.router)
	defer srv.Close()

	arrived, release := fx.observer.HoldNextResultReply(t)
	defer release()
	cancel, done := kmPostOverHTTP(t, srv, fx.navigationPath, fx.token, kmNavBody("2", key))
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("nav first reply never reached the wire barrier")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("aborted nav POST unexpectedly completed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("aborted nav POST never observed the cancel")
	}
	kmAwaitExecRows(t, fx.db, fx.targetID, 1, 10*time.Second)
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
	var status, code string
	if err := fx.db.QueryRow(`SELECT status, error_code FROM query_executions WHERE target_resource_id = ?`, fx.targetID).Scan(&status, &code); err != nil {
		t.Fatalf("aborted nav row: %v", err)
	}
	if status != string(model.QueryExecutionFailed) || code != "query_canceled" {
		t.Fatalf("nav abort evidence = %s/%s, want failed/query_canceled", status, code)
	}
}

// ---------------------------------------------------------------------------
// Group F — injected storage faults: a real failure inside the finalize
// transaction (execution insert, audit insert, claim link) rolls the whole
// pair back atomically — the claim survives unlinked and the key stays
// occupied. No partial evidence is ever observable.
// ---------------------------------------------------------------------------

func TestMySQLKeyedMatrixF_StorageFaults(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger string
	}{
		{"execution insert aborts the pair", "exec_insert"},
		{"audit insert aborts the pair", "audit_insert"},
		{"claim link aborts the pair", "claim_link"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
			key := "km-f-" + tc.trigger
			kmFaultTrigger(t, fx.db, "km_f_"+tc.trigger, tc.trigger, fx.targetID)

			rec := fx.kmPost(t, fx.executePath,
				`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`)
			if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
				t.Fatalf("%s fault = %d %q", tc.trigger, rec.Code, kmErrorCode(t, rec))
			}
			// Atomic rollback: no execution row, no audit row, one unlinked
			// claim. Partial evidence would be a correctness violation.
			fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Claims: 1})
			// The body is the fixed envelope — the trigger message is an
			// internal detail that must never cross the boundary.
			body := rec.Body.String()
			if strings.Contains(body, "keyed matrix fault") || strings.Contains(body, "TRIGGER") {
				t.Fatalf("fault detail leaked in response: %s", body)
			}
			// Occupied: same-key retry classifies already-exists.
			rec = fx.kmPost(t, fx.executePath,
				`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`)
			if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
				t.Fatalf("%s same-key retry = %d %q", tc.trigger, rec.Code, kmErrorCode(t, rec))
			}
			fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Claims: 1})
			mustExec(t, fx.db, "DROP TRIGGER IF EXISTS km_f_"+tc.trigger)
			// A new key executes normally once the fault clears.
			kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
				`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`-new"}`))
			fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 2, Linked: 1})
		})
	}
}

func TestMySQLKeyedMatrixF_StorageFaultsSavedAndNav(t *testing.T) {
	t.Run("saved claim-link fault rolls back atomically", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
		stmtID := fx.kmCreateSaved(t,
			`{"name":"km-f-saved","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
		kmFaultTrigger(t, fx.db, "km_f_saved_link", "claim_link", fx.targetID)
		rec := fx.kmPost(t, fx.kmSavedExecPath(stmtID),
			`{"values":{"cell_id":9},"clientExecutionId":"km-f-sav"}`)
		if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
			t.Fatalf("saved link fault = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Claims: 1})
	})

	t.Run("nav claim-link fault rolls back atomically", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
		kmFaultTrigger(t, fx.db, "km_f_nav_link", "claim_link", fx.targetID)
		rec := fx.kmPost(t, fx.navigationPath, kmNavBody("2", "km-f-nav"))
		if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
			t.Fatalf("nav link fault = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Claims: 1})
	})
}

// ---------------------------------------------------------------------------
// Group G — privacy and authorization: sanitized evidence fields never carry
// literal/comment markers, saved/navigation evidence suppresses statement
// material and values entirely, actors are typed, and machine credentials
// stay inside their governed-select scope.
// ---------------------------------------------------------------------------

func TestMySQLKeyedMatrixG_StatementPrivacy(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{statementAPI: true})

	// Literals and comments live in the request; the persisted preview/digest
	// render the masked AST — markers must not survive into evidence fields
	// or any history list output.
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9 and 'ZXLIT' = 'ZXLIT' /* ZXCMT */","clientExecutionId":"km-g-ord"}`))
	var digest, preview string
	var full sql.NullString
	if err := fx.db.QueryRow(`SELECT statement_digest, statement_preview, full_statement FROM query_executions WHERE target_resource_id = ?`, fx.targetID).
		Scan(&digest, &preview, &full); err != nil {
		t.Fatalf("privacy row read: %v", err)
	}
	if strings.Contains(digest, "ZX") || strings.Contains(preview, "ZX") {
		t.Fatalf("markers leaked into evidence: digest=%q preview=%q", digest, preview)
	}
	if !full.Valid || !strings.Contains(full.String, "ZXLIT") {
		t.Fatalf("internal full_statement = %v, want the verbatim original", full)
	}
	// The history list never exposes full_statement or the markers.
	rec := fx.kmGet(t, fx.historyPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("history list = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "ZX") {
		t.Fatalf("history response leaked markers: %s", rec.Body.String())
	}

	// The verbatim statement is reachable only through the owner-scoped
	// statement read — other actors are refused.
	var execID uint64
	if err := fx.db.QueryRow(`SELECT id FROM query_executions WHERE target_resource_id = ?`, fx.targetID).Scan(&execID); err != nil {
		t.Fatalf("exec id read: %v", err)
	}
	stmtPath := fmt.Sprintf("%s/%d/statement", fx.historyPath, execID)
	rec = fx.kmGet(t, stmtPath)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ZXLIT") {
		t.Fatalf("owner statement read = %d %s, want 200 with verbatim text", rec.Code, rec.Body.String())
	}
	rec = fx.kmGetWithToken(t, stmtPath, fx.otherToken)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("other-actor statement read = %d, want 404", rec.Code)
	}
}

func TestMySQLKeyedMatrixG_SavedNavPrivacy(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})

	// Saved evidence suppresses every statement-derived field: no compiled
	// SQL, no template id, no parameter value — not even masked.
	stmtID := fx.kmCreateSaved(t,
		`{"name":"km-g-saved","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id and note <> :tag","parameters":[{"name":"cell_id","type":"integer"},{"name":"tag","type":"string"}],"scope":"personal"}`)
	kmDecodeSuccess(t, fx.kmPost(t, fx.kmSavedExecPath(stmtID),
		`{"values":{"cell_id":9,"tag":"ZXSAVEDVAL"},"clientExecutionId":"km-g-sav"}`))
	var digest, preview string
	var full sql.NullString
	if err := fx.db.QueryRow(`SELECT statement_digest, statement_preview, full_statement FROM query_executions WHERE target_resource_id = ?`, fx.targetID).
		Scan(&digest, &preview, &full); err != nil {
		t.Fatalf("saved privacy row: %v", err)
	}
	if digest != "" || preview != "" || full.Valid {
		t.Fatalf("saved evidence fields = (%q, %q, %v), want fully suppressed", digest, preview, full)
	}

	// Navigation evidence carries the trusted relation marker only — never
	// the generated SELECT or the locator value.
	kmDecodeSuccess(t, fx.kmPost(t, fx.navigationPath, kmNavBody("2", "km-g-nav")))
	if err := fx.db.QueryRow(`SELECT statement_digest, statement_preview, full_statement FROM query_executions WHERE target_resource_id = ? ORDER BY id DESC LIMIT 1`, fx.targetID).
		Scan(&digest, &preview, &full); err != nil {
		t.Fatalf("nav privacy row: %v", err)
	}
	if !strings.HasPrefix(digest, "nav:") || !strings.HasPrefix(preview, "related:") {
		t.Fatalf("nav evidence = (%q, %q), want relation markers", digest, preview)
	}
	if strings.Contains(digest, "SELECT") || strings.Contains(preview, "SELECT") || full.Valid {
		t.Fatalf("nav evidence leaked generated SQL: (%q, %q, %v)", digest, preview, full)
	}
	// Locator values and referenced-row data never enter the relation marker.
	if strings.Contains(digest, "P_BETA") || strings.Contains(preview, "P_BETA") {
		t.Fatalf("nav evidence leaked locator data: (%q, %q)", digest, preview)
	}
}

func TestMySQLKeyedMatrixG_MachineScope(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	issued, err := fx.machineSvc.Create(t.Context(), service.AuthenticatedUser{ID: fx.adminUserID, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "km-g-machine", Scopes: []model.MachineScope{model.MachineScopeGovernedSelect},
	})
	if err != nil {
		t.Fatalf("create machine credential: %v", err)
	}

	// Machine actors execute the governed route under their typed identity.
	kmDecodeSuccess(t, fx.kmPostAs(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"km-g-mach"}`, issued.Secret))
	_, _, _, actorUser, actorMachine := fx.kmClaimRow(t, "km-g-mach")
	if actorUser.Valid || !actorMachine.Valid || actorMachine.Int64 != int64(issued.Principal.ID) {
		t.Fatalf("machine claim actors = user:%v machine:%v, want machine:%d only", actorUser, actorMachine, issued.Principal.ID)
	}
	// Machine rows never carry a restorable full statement.
	var full sql.NullString
	var actorUserID sql.NullInt64
	if err := fx.db.QueryRow(`SELECT full_statement, actor_user_id FROM query_executions WHERE target_resource_id = ?`, fx.targetID).
		Scan(&full, &actorUserID); err != nil {
		t.Fatalf("machine row read: %v", err)
	}
	if full.Valid {
		t.Fatalf("machine execution stored a full statement: %v", full)
	}
	// User-only surfaces refuse the machine credential.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, fx.historyPath, ""},
		{http.MethodPost, fx.navigationPath, kmNavBody("2", "km-g-m2")},
		{http.MethodGet, fx.savedPath, ""},
	} {
		rec := fx.kmPostAs(t, tc.path, tc.body, issued.Secret)
		if tc.method == http.MethodGet {
			rec = fx.kmGetWithToken(t, tc.path, issued.Secret)
		}
		if rec.Code != http.StatusForbidden || kmErrorCode(t, rec) != "machine_scope_denied" {
			t.Fatalf("machine %s %s = %d %q, want 403 machine_scope_denied", tc.method, tc.path, rec.Code, kmErrorCode(t, rec))
		}
	}
	// The conflict response itself is privacy-safe: a same-key replay gets
	// the bare envelope — no digest, claim detail, or stored request data.
	rec := fx.kmPostAs(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"km-g-mach"}`, issued.Secret)
	if rec.Code != http.StatusConflict {
		t.Fatalf("machine same-key replay = %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "digest") || strings.Contains(rec.Body.String(), "ZX") {
		t.Fatalf("conflict envelope leaked claim internals: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Group I — result contracts on the keyed path: byte-boundary truncation is
// recorded as success evidence before the capability gate refuses delivery;
// declared capabilities never reach the digest; masking preserves the
// truncation flag; zero-row results carry no residual matrix.
// ---------------------------------------------------------------------------

func TestMySQLKeyedMatrixI_ResultContract(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{maskedSecret: true})
	post := func(body string) *httptest.ResponseRecorder {
		return fx.kmPost(t, fx.executePath, body)
	}

	// Byte boundary: 8192 bytes is clean; 8193 truncates — keyed rows carry
	// the same cellTruncated contract as keyless.
	resp := kmDecodeSuccess(t, post(
		`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 1","capabilities":["cellTruncated"],"clientExecutionId":"km-i-8192"}`))
	if resp.Rows[0][1] != strings.Repeat("a", 8192) || resp.CellTruncated[0][1] {
		t.Fatalf("8192-byte cell = (%v, %v), want intact + flag false", len(fmt.Sprint(resp.Rows[0][1])), resp.CellTruncated)
	}
	resp = kmDecodeSuccess(t, post(
		`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 2","capabilities":["cellTruncated"],"clientExecutionId":"km-i-8193"}`))
	if resp.Rows[0][1] != strings.Repeat("b", 8192) || !resp.CellTruncated[0][1] {
		t.Fatalf("8193-byte cell = (len %d, %v), want truncated + flag true", len(fmt.Sprint(resp.Rows[0][1])), resp.CellTruncated)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2, Claims: 2, Linked: 2})

	// Without the capability the oversized result refuses delivery — but
	// the success evidence was already committed exactly once.
	rec := post(`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 2","clientExecutionId":"km-i-nocap"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "result_contract_upgrade_required" {
		t.Fatalf("keyed truncation refusal = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	if got := kmLastStatus(t, fx); got != string(model.QueryExecutionSuccess) {
		t.Fatalf("refused keyed attempt status = %s, want success", got)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 3, Linked: 3})

	// Declaring the capability on the SAME key replays as already-exists —
	// delivery options never enter the claim digest.
	rec = post(`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 2","capabilities":["cellTruncated"],"clientExecutionId":"km-i-nocap"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("capability-only change = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 3, Linked: 3})

	// Masked + oversized: the masked sentinel replaces the value while the
	// truncation flag survives masking.
	resp = kmDecodeSuccess(t, post(
		`{"statement":"select id, secret from query_e2e_aux.km_cells where id = 2","capabilities":["cellTruncated"],"clientExecutionId":"km-i-mask"}`))
	if resp.Rows[0][1] != "[MASKED]" || !resp.CellTruncated[0][1] {
		t.Fatalf("masked oversized cell = (%v, %v), want [MASKED] + flag true", resp.Rows[0][1], resp.CellTruncated)
	}
	if strings.Contains(fmt.Sprint(resp.Rows[0][1]), strings.Repeat("s", 64)) {
		t.Fatalf("masked cell leaked secret material")
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 4, Audits: 4, Claims: 4, Linked: 4})

	// Native NULL / empty string / text 'NULL' stay distinct.
	resp = kmDecodeSuccess(t, post(
		`{"statement":"select id, pad from query_e2e_aux.km_cells where id in (6, 7, 8) order by id","capabilities":["cellTruncated"],"clientExecutionId":"km-i-nulls"}`))
	if resp.Rows[0][1] != "" || resp.Rows[1][1] != nil || resp.Rows[2][1] != "NULL" {
		t.Fatalf("NULL/empty/text cells = %v", [][]any{resp.Rows[0], resp.Rows[1], resp.Rows[2]})
	}

	// Zero-row keyed results carry no residual truncation matrix.
	resp = kmDecodeSuccess(t, post(
		`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 999","capabilities":["cellTruncated"],"clientExecutionId":"km-i-empty"}`))
	if len(resp.Rows) != 0 || len(resp.CellTruncated) != 0 {
		t.Fatalf("zero-row result = rows %v matrix %v", resp.Rows, resp.CellTruncated)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 6, Audits: 6, Claims: 6, Linked: 6})
}

func TestMySQLKeyedMatrixI_ResultContractSavedNav(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})

	// Saved: the same contract applies through the template entry — keyed
	// refusal commits success evidence, then the same key replays.
	stmtID := fx.kmCreateSaved(t,
		`{"name":"km-i-saved","statement":"select id, pad from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
	rec := fx.kmPost(t, fx.kmSavedExecPath(stmtID),
		`{"values":{"cell_id":2},"clientExecutionId":"km-i-sav"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "result_contract_upgrade_required" {
		t.Fatalf("saved keyed refusal = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	if got := kmLastStatus(t, fx); got != string(model.QueryExecutionSuccess) {
		t.Fatalf("saved refused attempt status = %s", got)
	}
	rec = fx.kmPost(t, fx.kmSavedExecPath(stmtID),
		`{"values":{"cell_id":2},"capabilities":["cellTruncated"],"clientExecutionId":"km-i-sav"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("saved capability replay = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})

	// Navigation: oversized pad on parent id=1 refuses without the
	// capability; evidence commits; same key replays.
	rec = fx.kmPost(t, fx.navigationPath, kmNavBody("1", "km-i-nav"))
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "result_contract_upgrade_required" {
		t.Fatalf("nav keyed refusal = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	if got := kmLastStatus(t, fx); got != string(model.QueryExecutionSuccess) {
		t.Fatalf("nav refused attempt status = %s", got)
	}
	resp := kmDecodeSuccess(t, fx.kmPost(t, fx.navigationPath,
		`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["1"],"capabilities":["cellTruncated"],"clientExecutionId":"km-i-nav2"}`))
	if len(resp.Rows) != 1 || !resp.CellTruncated[0][2] {
		t.Fatalf("nav truncated row = %v matrix %v", resp.Rows, resp.CellTruncated)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 3, Linked: 3})
}

func TestMySQLKeyedMatrixI_FinalizeFaultBeatsRefusal(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{wrapRepo: true})
	fx.faultRepo.failClaimFinalize.Store(true)

	// An oversized result plus a finalize fault: the storage failure is the
	// terminal outcome — 502, never the delivery-refusal 409.
	rec := fx.kmPost(t, fx.executePath,
		`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 2","clientExecutionId":"km-i-fault"}`)
	if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
		t.Fatalf("finalize-fault + refusal = %d %q, want 502 query_backend_error", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Claims: 1})
}
