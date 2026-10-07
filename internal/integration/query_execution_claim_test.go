//go:build integration

// Package integration provides Testcontainers-backed tests for the G9 execution-claim storage protocol (issue #118, T10-A).
// input: context, database/sql, errors, fmt, strings, sync, testing, time, Testcontainers MySQL, internal/model, internal/repository/mysql
// output: TestClaim* suite covering acquisition/dup/conflict, terminal finalize atomicity + failure injection, ownership isolation, concurrent readers, NULL-key coexistence, cancellation-durable window, and exact key collation
// pos: Proves the claim storage protocol against real MySQL without PostgreSQL user SQL or T7 executor wiring
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
)

const (
	claimUserActor   = 70007
	claimMachineID   = 70007 // same numeric id as claimUserActor: user:7 vs machine:7 must stay distinct
	claimOtherUser   = 70008
	claimTestDB      = "claimdb"
	claimTestDBOther = "claimdb_other"
	claimTestSchema  = "app"
)

func claimDigest(seed string) string {
	// 64-char deterministic stand-in for the canonical digest — the claim
	// layer stores and compares it, never computes it.
	d := fmt.Sprintf("%x", seed)
	if len(d) > 64 {
		return d[:64]
	}
	return d + strings.Repeat("0", 64-len(d))
}

func userIdentity(id uint64) model.QueryExecutionIdentity {
	return model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: id}
}
func machineIdentity(id uint64) model.QueryExecutionIdentity {
	return model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: id}
}

func newClaimInput(targetID uint64, key string, id model.QueryExecutionIdentity, database, digest string) model.QueryExecutionClaimInput {
	return model.QueryExecutionClaimInput{
		TargetResourceID:  targetID,
		ClientExecutionID: key,
		Identity:          id,
		DatabaseName:      database,
		SchemaName:        claimTestSchema,
		RequestDigest:     digest,
		ClaimedAt:         time.Now().UTC(),
	}
}

func keyedRecord(targetID uint64, id model.QueryExecutionIdentity, key, database string, status model.QueryExecutionStatus) model.QueryExecutionRecord {
	rec := model.QueryExecutionRecord{
		TargetResourceID:  targetID,
		Engine:            "postgresql",
		DatabaseName:      database,
		SchemaName:        claimTestSchema,
		StatementDigest:   "digest:" + key,
		StatementPreview:  "preview:" + key,
		FullStatement:     "SELECT 1 -- " + key,
		Status:            status,
		RowCount:          3,
		DurationMs:        12,
		ClientExecutionID: &key,
	}
	if id.Kind == model.QueryExecutionActorUser {
		rec.ActorUserID = id.ID
	} else {
		rec.ActorMachinePrincipalID = id.ID
	}
	return rec
}

func countWhere(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("count query: %v", err)
	}
	return n
}

func claimExecCounts(t *testing.T, db *sql.DB, targetID uint64, key string) (execs, audits, linked int) {
	t.Helper()
	execs = countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`, targetID, key)
	audits = countWhere(t, db,
		`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID)
	linked = countWhere(t, db,
		`SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = ? AND execution_id IS NOT NULL`, targetID, key)
	return
}

// ---------------------------------------------------------------------------
// Vertical slice: acquire → read unlinked → finalize → read linked.
// ---------------------------------------------------------------------------

func TestClaim_VerticalFlow(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-vertical")
	in := newClaimInput(targetID, "vert-key-1", userIdentity(claimUserActor), claimTestDB, claimDigest("a"))

	if err := repo.TryClaimExecution(ctx, in); err != nil {
		t.Fatalf("claim: %v", err)
	}
	view, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, "vert-key-1", userIdentity(claimUserActor))
	if err != nil {
		t.Fatalf("read unlinked claim: %v", err)
	}
	if view.Execution != nil || view.Claim.ExecutionID != 0 {
		t.Fatalf("fresh claim must be unlinked, got exec=%+v id=%d", view.Execution, view.Claim.ExecutionID)
	}
	if view.Claim.RequestDigest != in.RequestDigest || view.Claim.ActorUserID != claimUserActor {
		t.Fatalf("claim identity mismatch: %+v", view.Claim)
	}

	rec := keyedRecord(targetID, userIdentity(claimUserActor), "vert-key-1", claimTestDB, model.QueryExecutionSuccess)
	pid := int64(4242)
	rec.BackendPID = &pid
	rec.RemoteState = model.QueryExecutionRemoteCompleted
	execID, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success")
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if execID == 0 {
		t.Fatal("finalize returned id 0")
	}

	view, err = repo.GetClaimWithExecution(ctx, targetID, claimTestDB, "vert-key-1", userIdentity(claimUserActor))
	if err != nil {
		t.Fatalf("read linked claim: %v", err)
	}
	if view.Execution == nil || view.Execution.ID != execID || view.Claim.ExecutionID != execID {
		t.Fatalf("linked view mismatch: claim=%+v exec=%+v", view.Claim, view.Execution)
	}
	if view.Execution.Status != model.QueryExecutionSuccess ||
		view.Execution.DatabaseName != claimTestDB ||
		view.Execution.SchemaName != claimTestSchema ||
		view.Execution.BackendPID == nil || *view.Execution.BackendPID != pid ||
		view.Execution.RemoteState != model.QueryExecutionRemoteCompleted ||
		view.Execution.ClientExecutionID == nil || *view.Execution.ClientExecutionID != "vert-key-1" {
		t.Fatalf("keyed execution fields not persisted: %+v", view.Execution)
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, "vert-key-1"); execs != 1 || linked != 1 {
		t.Fatalf("execs=%d linked=%d want 1/1", execs, linked)
	}
	// Success + user actor → full SQL persists (private evidence rule).
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE id = ? AND full_statement IS NOT NULL`, execID); got != 1 {
		t.Fatal("successful user execution must retain full_statement")
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.executed' AND result = 'success' AND actor_user_id = ? AND actor_machine_principal_id IS NULL`,
		targetID, claimUserActor); got != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", got)
	}
}

// ---------------------------------------------------------------------------
// A. Concurrent acquisition: exactly one Acquired; digest classifies the rest.
// ---------------------------------------------------------------------------

func TestClaim_ConcurrentAcquisition(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	targetID := createQueryTargetResource(t, db, "qe-claim-race")

	t.Run("same digest admits exactly one", func(t *testing.T) {
		const n = 8
		start := make(chan struct{})
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start // barrier: all callers contend on the same release
				errs[i] = repo.TryClaimExecution(context.Background(),
					newClaimInput(targetID, "race-same", userIdentity(claimUserActor), claimTestDB, claimDigest("same")))
			}(i)
		}
		close(start)
		wg.Wait()

		var acquired, dup, conflict, other int
		for _, err := range errs {
			switch {
			case err == nil:
				acquired++
			case errors.Is(err, mysql.ErrQueryExecutionClaimExists):
				dup++
			case errors.Is(err, mysql.ErrQueryExecutionClaimConflict):
				conflict++
			default:
				other++
			}
		}
		if acquired != 1 || dup != n-1 || conflict != 0 || other != 0 {
			t.Fatalf("acquired=%d dup=%d conflict=%d other=%d (errs=%v)", acquired, dup, conflict, other, errs)
		}
	})

	t.Run("different digest conflicts", func(t *testing.T) {
		const n = 8
		start := make(chan struct{})
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				errs[i] = repo.TryClaimExecution(context.Background(),
					newClaimInput(targetID, "race-mixed", userIdentity(claimUserActor), claimTestDB, claimDigest(fmt.Sprint(i))))
			}(i)
		}
		close(start)
		wg.Wait()

		var acquired, conflict, other int
		for _, err := range errs {
			switch {
			case err == nil:
				acquired++
			case errors.Is(err, mysql.ErrQueryExecutionClaimConflict):
				conflict++
			default:
				other++
			}
		}
		if acquired != 1 || conflict != n-1 || other != 0 {
			t.Fatalf("acquired=%d conflict=%d other=%d (errs=%v)", acquired, conflict, other, errs)
		}
		// Admission refusal wrote no history and no audit.
		if execs, _, _ := claimExecCounts(t, db, targetID, "race-mixed"); execs != 0 {
			t.Fatalf("conflict must not write history, got %d rows", execs)
		}
	})
}

// ---------------------------------------------------------------------------
// B. A finalized claim stays occupied forever: no release, no expiry, no key
// recycling after success/cancelled/failed — and none while still unlinked.
// ---------------------------------------------------------------------------

func TestClaim_TerminalClaimStaysOccupied(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-occupied")

	for _, status := range []model.QueryExecutionStatus{
		model.QueryExecutionSuccess, model.QueryExecutionCancelled, model.QueryExecutionFailed,
	} {
		key := "occ-" + string(status)
		in := newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest("occ"))
		if err := repo.TryClaimExecution(ctx, in); err != nil {
			t.Fatalf("%s claim: %v", status, err)
		}
		rec := keyedRecord(targetID, userIdentity(claimUserActor), key, claimTestDB, status)
		if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", string(status)); err != nil {
			t.Fatalf("%s finalize: %v", status, err)
		}
		// Same key can never be re-acquired — neither same digest…
		if err := repo.TryClaimExecution(ctx, in); !errors.Is(err, mysql.ErrQueryExecutionClaimExists) {
			t.Fatalf("%s re-claim same digest = %v, want ErrQueryExecutionClaimExists", status, err)
		}
		// …nor a different digest.
		in.RequestDigest = claimDigest("other")
		if err := repo.TryClaimExecution(ctx, in); !errors.Is(err, mysql.ErrQueryExecutionClaimConflict) {
			t.Fatalf("%s re-claim other digest = %v, want ErrQueryExecutionClaimConflict", status, err)
		}
	}
	// An unlinked claim is occupied too: time passing must not release it.
	old := newClaimInput(targetID, "occ-pending", userIdentity(claimUserActor), claimTestDB, claimDigest("p"))
	old.ClaimedAt = time.Now().UTC().Add(-72 * time.Hour) // ancient claim: still occupied
	if err := repo.TryClaimExecution(ctx, old); err != nil {
		t.Fatalf("pending claim: %v", err)
	}
	old.RequestDigest = claimDigest("p")
	if err := repo.TryClaimExecution(ctx, old); !errors.Is(err, mysql.ErrQueryExecutionClaimExists) {
		t.Fatalf("ancient pending claim re-claim = %v, want ErrQueryExecutionClaimExists", err)
	}
}

// ---------------------------------------------------------------------------
// C. Ownership/scope isolation: user:7 ≠ machine:7; database scope; target
// scope. Wrong identity/context reads nothing and finalizes nothing.
// ---------------------------------------------------------------------------

func TestClaim_OwnershipAndScopeIsolation(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-scope")
	otherTarget := createQueryTargetResource(t, db, "qe-claim-scope2")

	// user:7 and machine:7 with the same numeric id are distinct occupants.
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "iso-user", userIdentity(claimUserActor), claimTestDB, claimDigest("u"))); err != nil {
		t.Fatalf("user claim: %v", err)
	}
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "iso-machine", machineIdentity(claimMachineID), claimTestDB, claimDigest("m"))); err != nil {
		t.Fatalf("machine claim: %v", err)
	}
	// machine:7 must not see user:7's claim and vice versa.
	if _, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, "iso-user", machineIdentity(claimMachineID)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("machine reading user claim = %v, want ErrNoRows", err)
	}
	if _, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, "iso-machine", userIdentity(claimUserActor)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("user reading machine claim = %v, want ErrNoRows", err)
	}
	// Same key, different database under one target: no cross-read.
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "iso-db", userIdentity(claimUserActor), claimTestDB, claimDigest("d"))); err != nil {
		t.Fatalf("db claim: %v", err)
	}
	if _, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDBOther, "iso-db", userIdentity(claimUserActor)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other-database read = %v, want ErrNoRows", err)
	}
	// Same key on a different target is an independent occupancy.
	if err := repo.TryClaimExecution(ctx, newClaimInput(otherTarget, "iso-db", userIdentity(claimUserActor), claimTestDB, claimDigest("d"))); err != nil {
		t.Fatalf("same key on other target should acquire: %v", err)
	}
	// A non-occupant cannot finalize: wrong actor.
	rec := keyedRecord(targetID, userIdentity(claimOtherUser), "iso-user", claimTestDB, model.QueryExecutionSuccess)
	if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, mysql.ErrQueryExecutionClaimNotLinked) {
		t.Fatalf("wrong-actor finalize = %v, want ErrQueryExecutionClaimNotLinked", err)
	}
	// Wrong database scope.
	rec = keyedRecord(targetID, userIdentity(claimUserActor), "iso-user", claimTestDBOther, model.QueryExecutionSuccess)
	if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, mysql.ErrQueryExecutionClaimNotLinked) {
		t.Fatalf("wrong-database finalize = %v, want ErrQueryExecutionClaimNotLinked", err)
	}
	// Nothing was written by the refused finalizes.
	if execs, _, _ := claimExecCounts(t, db, targetID, "iso-user"); execs != 0 {
		t.Fatalf("refused finalize wrote %d executions", execs)
	}
	// Missing claim cannot be finalized at all.
	rec = keyedRecord(targetID, userIdentity(claimUserActor), "no-such-claim", claimTestDB, model.QueryExecutionSuccess)
	if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, mysql.ErrQueryExecutionClaimNotLinked) {
		t.Fatalf("finalize without claim = %v, want ErrQueryExecutionClaimNotLinked", err)
	}
}

// ---------------------------------------------------------------------------
// D. Finalize persists the keyed evidence row faithfully and preserves the
// full-SQL privacy rules (user+success only).
// ---------------------------------------------------------------------------

func TestClaim_FinalizeKeyedColumnsAndPrivacy(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-cols")

	// Machine occupant: full SQL must never persist even on success.
	key := "cols-machine"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, machineIdentity(claimMachineID), claimTestDB, claimDigest("mc"))); err != nil {
		t.Fatalf("machine claim: %v", err)
	}
	mrec := keyedRecord(targetID, machineIdentity(claimMachineID), key, claimTestDB, model.QueryExecutionSuccess)
	execID, err := repo.FinalizeClaimWithAudit(ctx, mrec, "query.executed", "success")
	if err != nil {
		t.Fatalf("machine finalize: %v", err)
	}
	var (
		dbName, schema, status string
		fullStmt               sql.NullString
		machID                 sql.NullInt64
		userID                 sql.NullInt64
	)
	if err := db.QueryRow(`SELECT database_name, schema_name, status, full_statement, actor_machine_principal_id, actor_user_id
		FROM query_executions WHERE id = ?`, execID).Scan(&dbName, &schema, &status, &fullStmt, &machID, &userID); err != nil {
		t.Fatalf("scan keyed execution: %v", err)
	}
	if dbName != claimTestDB || schema != claimTestSchema || status != "success" {
		t.Fatalf("keyed dims wrong: db=%q schema=%q status=%q", dbName, schema, status)
	}
	if fullStmt.Valid {
		t.Fatal("machine execution must never store full_statement")
	}
	if !machID.Valid || machID.Int64 != claimMachineID || userID.Valid {
		t.Fatalf("machine actor columns wrong: mach=%v user=%v", machID, userID)
	}
	// Claim still present and linked.
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = ? AND execution_id = ?`,
		targetID, key, execID); got != 1 {
		t.Fatalf("claim link rows = %d, want 1", got)
	}
	// Non-success user execution: full_statement stays NULL.
	key2 := "cols-failed"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key2, userIdentity(claimUserActor), claimTestDB, claimDigest("f"))); err != nil {
		t.Fatalf("failed claim: %v", err)
	}
	frec := keyedRecord(targetID, userIdentity(claimUserActor), key2, claimTestDB, model.QueryExecutionFailed)
	frec.ErrorCode = "42P01"
	frec.ErrorMessage = "relation missing"
	fid, err := repo.FinalizeClaimWithAudit(ctx, frec, "query.executed", "failure")
	if err != nil {
		t.Fatalf("failed finalize: %v", err)
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE id = ? AND full_statement IS NULL AND status = 'failed' AND error_code = '42P01'`, fid); got != 1 {
		t.Fatal("non-success keyed row must keep full_statement NULL")
	}
	// Audit result reflects the recorded outcome.
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.executed' AND result = 'failure'`, targetID); got != 1 {
		t.Fatalf("failure audit rows = %d", got)
	}
}

// ---------------------------------------------------------------------------
// E. Failure injection at each of the three transaction stages — via real
// database triggers, not mocks. Every stage failure must roll back the whole
// pair: no execution row, no audit row, claim remains unlinked.
// ---------------------------------------------------------------------------

func TestClaim_FinalizeFailureInjection(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()

	stages := []struct {
		name    string
		trigger string
	}{
		{"history insert", `CREATE TRIGGER fail_claim_history BEFORE INSERT ON query_executions
			FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject history failure'`},
		{"audit insert", `CREATE TRIGGER fail_claim_audit BEFORE INSERT ON audit_events
			FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject audit failure'`},
		{"claim update", `CREATE TRIGGER fail_claim_link BEFORE UPDATE ON query_execution_claims
			FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject link failure'`},
	}
	for i, st := range stages {
		t.Run(st.name, func(t *testing.T) {
			targetID := createQueryTargetResource(t, db, fmt.Sprintf("qe-claim-fail-%d", i))
			key := "fail-" + st.name
			if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest("x"))); err != nil {
				t.Fatalf("claim: %v", err)
			}
			if _, err := db.ExecContext(ctx, st.trigger); err != nil {
				t.Fatalf("install trigger: %v", err)
			}
			defer db.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s", strings.Fields(st.trigger)[2]))

			failuresBefore := repo.QueryEvidencePersistenceFailures()
			rec := keyedRecord(targetID, userIdentity(claimUserActor), key, claimTestDB, model.QueryExecutionSuccess)
			_, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success")
			if err == nil || errors.Is(err, mysql.ErrQueryExecutionClaimNotLinked) {
				t.Fatalf("injected %s failure = %v, want controlled persistence error", st.name, err)
			}
			if strings.Contains(err.Error(), "inject") || strings.Contains(err.Error(), "45000") {
				t.Fatalf("injected error details leaked into safe error: %v", err)
			}
			if got := repo.QueryEvidencePersistenceFailures(); got != failuresBefore+1 {
				t.Fatalf("failure counter delta = %d, want exactly 1", got-failuresBefore)
			}
			// All-or-nothing: claim present, unlinked; zero history; zero audit
			// for this target beyond what other tests wrote (target is fresh).
			if execs, audits, linked := claimExecCounts(t, db, targetID, key); execs != 0 || audits != 0 || linked != 0 {
				t.Fatalf("partial evidence after %s failure: execs=%d audits=%d linked=%d", st.name, execs, audits, linked)
			}
		})
	}
}

// sqlmock supplements the real-DB injection with a commit-stage failure.

// ---------------------------------------------------------------------------
// F. Duplicate/concurrent finalize: at most one pair; mismatched occupancy
// cannot finalize.
// ---------------------------------------------------------------------------

func TestClaim_DuplicateAndConcurrentFinalize(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-dupfin")
	key := "dupfin"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest("z"))); err != nil {
		t.Fatalf("claim: %v", err)
	}
	rec := keyedRecord(targetID, userIdentity(claimUserActor), key, claimTestDB, model.QueryExecutionSuccess)

	// Sequential double finalize.
	if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); err != nil {
		t.Fatalf("first finalize: %v", err)
	}
	if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, mysql.ErrQueryExecutionClaimNotLinked) {
		t.Fatalf("second finalize = %v, want ErrQueryExecutionClaimNotLinked", err)
	}
	if execs, audits, _ := claimExecCounts(t, db, targetID, key); execs != 1 || audits != 1 {
		t.Fatalf("duplicate finalize wrote extra pair: execs=%d audits=%d", execs, audits)
	}

	// Concurrent finalize on a fresh claim: barrier-release, at most one pair.
	key2 := "dupfin-race"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key2, userIdentity(claimUserActor), claimTestDB, claimDigest("z2"))); err != nil {
		t.Fatalf("claim2: %v", err)
	}
	_, auditsBefore, _ := claimExecCounts(t, db, targetID, key2)
	const n = 6
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = repo.FinalizeClaimWithAudit(ctx,
				keyedRecord(targetID, userIdentity(claimUserActor), key2, claimTestDB, model.QueryExecutionSuccess),
				"query.executed", "success")
		}(i)
	}
	close(start)
	wg.Wait()

	var ok, notLinked, other int
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, mysql.ErrQueryExecutionClaimNotLinked):
			notLinked++
		default:
			other++
		}
	}
	if ok != 1 || notLinked != n-1 || other != 0 {
		t.Fatalf("concurrent finalize: ok=%d notLinked=%d other=%d (errs=%v)", ok, notLinked, other, errs)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, key2); execs != 1 || audits != auditsBefore+1 || linked != 1 {
		t.Fatalf("concurrent finalize wrote execs=%d auditsDelta=%d linked=%d", execs, audits-auditsBefore, linked)
	}
}

// ---------------------------------------------------------------------------
// G. Readers racing finalize observe only pre-finalize or complete
// post-finalize state — never a torn pair, never a fake id.
// ---------------------------------------------------------------------------

func TestClaim_ConcurrentReadVsFinalize(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-snapshot")
	key := "snap"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest("s"))); err != nil {
		t.Fatalf("claim: %v", err)
	}
	rec := keyedRecord(targetID, userIdentity(claimUserActor), key, claimTestDB, model.QueryExecutionSuccess)

	const readers = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < 25; j++ {
				v, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, key, userIdentity(claimUserActor))
				if err != nil {
					errCh <- fmt.Errorf("read: %w", err)
					return
				}
				if v.Execution == nil {
					if v.Claim.ExecutionID != 0 {
						errCh <- fmt.Errorf("unlinked claim shows execution_id=%d", v.Claim.ExecutionID)
						return
					}
					continue
				}
				// Linked: the join row must be the real terminal execution.
				if v.Execution.ID != v.Claim.ExecutionID || v.Execution.Status != model.QueryExecutionSuccess {
					errCh <- fmt.Errorf("torn view: claim.exec=%d exec=%+v", v.Claim.ExecutionID, v.Execution)
					return
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "success"); err != nil {
			errCh <- fmt.Errorf("finalize: %w", err)
		}
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, key); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("post-race state execs=%d audits=%d linked=%d", execs, audits, linked)
	}

	// Reads are write-free: repeated reads change no row counts.
	before := countWhere(t, db, `SELECT (SELECT COUNT(*) FROM query_executions) + (SELECT COUNT(*) FROM audit_events) + (SELECT COUNT(*) FROM query_execution_claims)`)
	for i := 0; i < 10; i++ {
		if _, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, key, userIdentity(claimUserActor)); err != nil {
			t.Fatalf("re-read: %v", err)
		}
	}
	if after := countWhere(t, db, `SELECT (SELECT COUNT(*) FROM query_executions) + (SELECT COUNT(*) FROM audit_events) + (SELECT COUNT(*) FROM query_execution_claims)`); after != before {
		t.Fatalf("reads mutated tables: before=%d after=%d", before, after)
	}

	// A linked claim whose execution row vanished is an evidence error, not a
	// fabricated running state.
	key2 := "broken"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key2, userIdentity(claimUserActor), claimTestDB, claimDigest("b"))); err != nil {
		t.Fatalf("claim2: %v", err)
	}
	if _, err := db.Exec(`UPDATE query_execution_claims SET execution_id = 999999999 WHERE target_resource_id = ? AND client_execution_id = ?`, targetID, key2); err != nil {
		t.Fatalf("forge broken link: %v", err)
	}
	if _, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, key2, userIdentity(claimUserActor)); !errors.Is(err, mysql.ErrQueryExecutionClaimBrokenLink) {
		t.Fatalf("broken link read = %v, want ErrQueryExecutionClaimBrokenLink", err)
	}
}

// ---------------------------------------------------------------------------
// H. Pre-claim NULL-key evidence pairs coexist and never consume a key;
// refused admissions write nothing.
// ---------------------------------------------------------------------------

func TestClaim_NullKeyEvidenceCoexists(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-nullkey")

	// Two NULL-key pairs on different PG databases (e.g. access denial before
	// claim, claim-insert failure) coexist, each preserving its resolved
	// connection scope — the unique index tolerates repeated NULL keys.
	pairs := []struct {
		database, schema, errCode string
		status                    model.QueryExecutionStatus
	}{
		{claimTestDB, "tenant_a", "E0", model.QueryExecutionRejected},
		{claimTestDBOther, "tenant_b", "E1", model.QueryExecutionFailed},
	}
	for _, p := range pairs {
		if _, err := repo.InsertExecutionWithAudit(ctx, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      claimUserActor,
			Engine:           "postgresql",
			DatabaseName:     p.database,
			SchemaName:       p.schema,
			Status:           p.status,
			ErrorCode:        p.errCode,
		}, "query.executed", "failure"); err != nil {
			t.Fatalf("null-key pair %s: %v", p.errCode, err)
		}
	}
	// Each pair landed with its real scope and an explicit NULL key.
	for _, p := range pairs {
		var dbName, schema, status string
		var key sql.NullString
		if err := db.QueryRow(`SELECT database_name, schema_name, status, client_execution_id
			FROM query_executions WHERE target_resource_id = ? AND error_code = ?`, targetID, p.errCode).
			Scan(&dbName, &schema, &status, &key); err != nil {
			t.Fatalf("read null-key row %s: %v", p.errCode, err)
		}
		if dbName != p.database || schema != p.schema || status != string(p.status) || key.Valid {
			t.Fatalf("null-key row %s = db:%q schema:%q status:%q key:%v", p.errCode, dbName, schema, status, key)
		}
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.executed' AND result = 'failure'`, targetID); got != 2 {
		t.Fatalf("null-key audit rows = %d, want 2", got)
	}
	// Legacy keyless evidence (empty scope, machine actor) keeps old semantics.
	if _, err := repo.InsertExecutionWithAudit(ctx, model.QueryExecutionRecord{
		TargetResourceID:        targetID,
		ActorMachinePrincipalID: claimMachineID,
		Engine:                  "mysql",
		Status:                  model.QueryExecutionFailed,
		FullStatement:           "SELECT machine_secret",
	}, "query.executed", "failure"); err != nil {
		t.Fatalf("machine null-key pair: %v", err)
	}
	var fs sql.NullString
	var mach sql.NullInt64
	var usr sql.NullInt64
	if err := db.QueryRow(`SELECT full_statement, actor_machine_principal_id, actor_user_id, database_name, schema_name, client_execution_id
		FROM query_executions WHERE target_resource_id = ? AND engine = 'mysql'`, targetID).
		Scan(&fs, &mach, &usr, new(string), new(string), new(sql.NullString)); err != nil {
		t.Fatalf("read machine null-key row: %v", err)
	}
	if fs.Valid || !mach.Valid || mach.Int64 != claimMachineID || usr.Valid {
		t.Fatalf("machine evidence leaked: fs=%v mach=%v usr=%v", fs, mach, usr)
	}
	// The NULL-key pairs did not occupy any key.
	key := "freed-after-denial"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest("aa"))); err != nil {
		t.Fatalf("key consumed by null-key evidence: %v", err)
	}
	// The keyless path must refuse a keyed record outright.
	rec := keyedRecord(targetID, userIdentity(claimUserActor), "must-not-write", claimTestDB, model.QueryExecutionSuccess)
	if _, err := repo.InsertExecutionWithAudit(ctx, rec, "query.executed", "success"); !errors.Is(err, mysql.ErrQueryExecutionClaimInvalid) {
		t.Fatalf("keyed record via keyless path = %v, want ErrQueryExecutionClaimInvalid", err)
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'must-not-write'`, targetID); got != 0 {
		t.Fatal("keyless path wrote a keyed row")
	}
}

// ---------------------------------------------------------------------------
// I. Cancellation-durable evidence: a cancelled request ctx cannot finalize
// (repo honors ctx), while a detached bounded window still commits the pair.
// ---------------------------------------------------------------------------

func TestClaim_ContextCancellationBoundary(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-ctx")
	key := "ctx-key"
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest("c"))); err != nil {
		t.Fatalf("claim: %v", err)
	}
	rec := keyedRecord(targetID, userIdentity(claimUserActor), key, claimTestDB, model.QueryExecutionSuccess)

	// A cancelled request context is honored: finalize cannot proceed.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.FinalizeClaimWithAudit(cancelled, rec, "query.executed", "success"); err == nil {
		t.Fatal("finalize on cancelled ctx must fail")
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, key); execs != 0 || linked != 0 {
		t.Fatalf("cancelled finalize wrote evidence: execs=%d linked=%d", execs, linked)
	}

	// The evidence window detaches from request cancellation (the pattern
	// persistEvidencePair uses): WithoutCancel + a bounded timeout still
	// commits after the request ctx is dead.
	windowCtx, cancelWin := context.WithTimeout(context.WithoutCancel(cancelled), 2*time.Second)
	defer cancelWin()
	if _, err := repo.FinalizeClaimWithAudit(windowCtx, rec, "query.executed", "success"); err != nil {
		t.Fatalf("detached finalize after request cancel: %v", err)
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, key); execs != 1 || linked != 1 {
		t.Fatalf("detached finalize did not commit: execs=%d linked=%d", execs, linked)
	}

	// An already-expired window fails controlled — no queue, no retry.
	expired, cancelExpired := context.WithTimeout(ctx, 1*time.Nanosecond)
	defer cancelExpired()
	time.Sleep(5 * time.Millisecond)
	if _, err := repo.FinalizeClaimWithAudit(expired, keyedRecord(targetID, userIdentity(claimUserActor), "ctx-expired", claimTestDB, model.QueryExecutionSuccess), "query.executed", "success"); err == nil {
		t.Fatal("expired window finalize must fail")
	}
}

// ---------------------------------------------------------------------------
// R1-1: the point read returns the linked execution's real identity and
// rejects any link whose stored identity does not match the claim.
// ---------------------------------------------------------------------------

func TestClaim_PointReadVerifiesLinkedIdentity(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-linkid")

	// Real user and machine links return the real typed actor ids.
	for _, id := range []model.QueryExecutionIdentity{userIdentity(claimUserActor), machineIdentity(claimMachineID)} {
		key := "lid-" + string(id.Kind)
		if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, id, claimTestDB, claimDigest("aa"))); err != nil {
			t.Fatalf("claim %s: %v", key, err)
		}
		execID, err := repo.FinalizeClaimWithAudit(ctx, keyedRecord(targetID, id, key, claimTestDB, model.QueryExecutionSuccess), "query.executed", "success")
		if err != nil {
			t.Fatalf("finalize %s: %v", key, err)
		}
		view, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, key, id)
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		if view.Execution == nil || view.Execution.ID != execID {
			t.Fatalf("%s exec missing", key)
		}
		switch id.Kind {
		case model.QueryExecutionActorUser:
			if view.Execution.ActorUserID != id.ID || view.Execution.ActorMachinePrincipalID != 0 {
				t.Fatalf("user exec identity wrong: %+v", view.Execution)
			}
		default:
			if view.Execution.ActorMachinePrincipalID != id.ID || view.Execution.ActorUserID != 0 {
				t.Fatalf("machine exec identity wrong: %+v", view.Execution)
			}
		}
		if view.Execution.TargetResourceID != targetID {
			t.Fatalf("exec target = %d, want %d", view.Execution.TargetResourceID, targetID)
		}
	}

	// Forge a link from claim B to claim A's real execution row — every
	// mismatched dimension (key/actor/scope) must surface a structural error,
	// never a fabricated view.
	realExec := countWhere(t, db,
		`SELECT execution_id FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = 'lid-user'`, targetID)
	forge := func(key string, id model.QueryExecutionIdentity, database string) {
		if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, id, database, claimDigest("bb"))); err != nil {
			t.Fatalf("forge claim %s: %v", key, err)
		}
		mustExec(t, db, `UPDATE query_execution_claims SET execution_id = ?
			WHERE target_resource_id = ? AND client_execution_id = ?`, realExec, targetID, key)
	}
	forge("forge-key", userIdentity(claimUserActor), claimTestDB)        // key differs
	forge("forge-actor", machineIdentity(claimMachineID), claimTestDB)   // actor type differs
	forge("forge-scope", userIdentity(claimUserActor), claimTestDBOther) // database differs
	for _, c := range []struct {
		key string
		id  model.QueryExecutionIdentity
		db  string
	}{{"forge-key", userIdentity(claimUserActor), claimTestDB},
		{"forge-actor", machineIdentity(claimMachineID), claimTestDB},
		{"forge-scope", userIdentity(claimUserActor), claimTestDBOther}} {
		view, err := repo.GetClaimWithExecution(ctx, targetID, c.db, c.key, c.id)
		if !errors.Is(err, mysql.ErrQueryExecutionClaimBrokenLink) {
			t.Fatalf("%s mismatched link = %v (view %+v), want ErrQueryExecutionClaimBrokenLink", c.key, err, view)
		}
		if view.Execution != nil {
			t.Fatalf("%s forged link leaked an execution", c.key)
		}
	}

	// A linked row whose status is not a stored terminal enum is also broken
	// evidence, not a running state.
	badRes, err := db.Exec(`INSERT INTO query_executions
		(target_resource_id, actor_user_id, engine, database_name, schema_name, statement_digest, statement_preview, status, client_execution_id)
		VALUES (?, ?, 'postgresql', 'claimdb', 'app', 'd', 'p', 'running', 'bad-status')`, targetID, claimUserActor)
	if err != nil {
		t.Fatalf("seed illegal-status row: %v", err)
	}
	badID, _ := badRes.LastInsertId()
	if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, "bad-status", userIdentity(claimUserActor), claimTestDB, claimDigest("cc"))); err == nil {
		mustExec(t, db, `UPDATE query_execution_claims SET execution_id = ? WHERE target_resource_id = ? AND client_execution_id = 'bad-status'`, badID, targetID)
	}
	if _, err := repo.GetClaimWithExecution(ctx, targetID, claimTestDB, "bad-status", userIdentity(claimUserActor)); !errors.Is(err, mysql.ErrQueryExecutionClaimBrokenLink) {
		t.Fatalf("illegal-status link = %v, want ErrQueryExecutionClaimBrokenLink", err)
	}
}

func TestClaim_AdmissionValidation(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-validate")
	base := newClaimInput(targetID, "valid-key", userIdentity(claimUserActor), claimTestDB, claimDigest("v"))

	cases := map[string]func(model.QueryExecutionClaimInput) model.QueryExecutionClaimInput{
		"zero target": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.TargetResourceID = 0
			return in
		},
		"empty key": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.ClientExecutionID = ""
			return in
		},
		"oversize key": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.ClientExecutionID = strings.Repeat("k", 65)
			return in
		},
		"NUL key": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.ClientExecutionID = "a\x00b"
			return in
		},
		"no actor": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.Identity = model.QueryExecutionIdentity{}
			return in
		},
		"bad actor kind": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.Identity = model.QueryExecutionIdentity{Kind: "service", ID: 1}
			return in
		},
		"short digest": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.RequestDigest = "abc"
			return in
		},
		"long digest": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.RequestDigest = strings.Repeat("a", 65)
			return in
		},
		"non-hex digest": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.RequestDigest = strings.Repeat("g", 64)
			return in
		},
		"unicode digest": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.RequestDigest = strings.Repeat("中", 64)
			return in
		},
		"trailing-space digest": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.RequestDigest = strings.Repeat("a", 63) + " "
			return in
		},
		"zero claimed_at": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.ClaimedAt = time.Time{}
			return in
		},
		"oversize database": func(in model.QueryExecutionClaimInput) model.QueryExecutionClaimInput {
			in.DatabaseName = strings.Repeat("d", 129)
			return in
		},
	}
	for name, mutate := range cases {
		if err := repo.TryClaimExecution(ctx, mutate(base)); !errors.Is(err, mysql.ErrQueryExecutionClaimInvalid) {
			t.Fatalf("%s: got %v, want ErrQueryExecutionClaimInvalid", name, err)
		}
	}
	// Finalize rejects non-terminal / derived statuses and missing key.
	for _, status := range []model.QueryExecutionStatus{"running", "unknown", ""} {
		rec := keyedRecord(targetID, userIdentity(claimUserActor), "st-"+string(status), claimTestDB, status)
		if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "x"); !errors.Is(err, mysql.ErrQueryExecutionClaimInvalid) {
			t.Fatalf("finalize status %q = %v, want ErrQueryExecutionClaimInvalid", status, err)
		}
	}
	rec := keyedRecord(targetID, userIdentity(claimUserActor), "", claimTestDB, model.QueryExecutionSuccess)
	*rec.ClientExecutionID = ""
	if _, err := repo.FinalizeClaimWithAudit(ctx, rec, "query.executed", "x"); !errors.Is(err, mysql.ErrQueryExecutionClaimInvalid) {
		t.Fatalf("finalize empty key = %v, want ErrQueryExecutionClaimInvalid", err)
	}

	// Hex digests are valid in either case — stored verbatim, never normalized.
	for _, d := range []string{
		strings.Repeat("a", 64),
		strings.Repeat("A", 64),
		strings.Repeat("0123456789abcdefABCDEF", 3)[:64],
	} {
		in := base
		in.ClientExecutionID = "hex-" + d[:8] + fmt.Sprint(len(d))
		in.RequestDigest = d
		if err := repo.TryClaimExecution(ctx, in); err != nil {
			t.Fatalf("valid hex digest rejected: %v", err)
		}
	}
	// An unknown remote-state enum is refused before the transaction and
	// writes nothing.
	in := base
	in.ClientExecutionID = "bad-remote"
	in.RequestDigest = strings.Repeat("e", 64)
	if err := repo.TryClaimExecution(ctx, in); err != nil {
		t.Fatalf("claim for remote-state test: %v", err)
	}
	badRemote := keyedRecord(targetID, userIdentity(claimUserActor), "bad-remote", claimTestDB, model.QueryExecutionSuccess)
	badRemote.RemoteState = "teleported"
	if _, err := repo.FinalizeClaimWithAudit(ctx, badRemote, "query.executed", "success"); !errors.Is(err, mysql.ErrQueryExecutionClaimInvalid) {
		t.Fatalf("bad remote_state finalize = %v, want ErrQueryExecutionClaimInvalid", err)
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, "bad-remote"); execs != 0 || linked != 0 {
		t.Fatalf("bad remote_state touched the store: execs=%d linked=%d", execs, linked)
	}
}

// Exact key identity: the claim PK and the keyed unique index must not fold
// case or strip trailing space — opaque keys compare byte-exact.
func TestClaim_KeyCollationIsExact(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-claim-collation")

	for _, c := range [][2]string{
		{"query_execution_claims", "client_execution_id"},
		{"query_execution_claims", "database_name"},
		{"query_execution_claims", "schema_name"},
		{"query_execution_claims", "request_digest"},
		{"query_executions", "client_execution_id"},
	} {
		var coll string
		if err := db.QueryRow(`SELECT COLLATION_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, c[0], c[1]).Scan(&coll); err != nil {
			t.Fatalf("%s.%s collation lookup: %v", c[0], c[1], err)
		}
		if coll != "utf8mb4_0900_bin" {
			t.Fatalf("%s.%s collation=%s, want utf8mb4_0900_bin", c[0], c[1], coll)
		}
	}
	// Case-distinct and trailing-space keys are independent occupancies.
	for _, key := range []string{"CaseKey", "casekey", "trailkey", "trailkey "} {
		if err := repo.TryClaimExecution(ctx, newClaimInput(targetID, key, userIdentity(claimUserActor), claimTestDB, claimDigest(key))); err != nil {
			t.Fatalf("key %q must be a distinct occupancy: %v", key, err)
		}
	}
	if got := countWhere(t, db,
		`SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ?`, targetID); got != 4 {
		t.Fatalf("claim rows = %d, want 4 distinct keys", got)
	}
}
