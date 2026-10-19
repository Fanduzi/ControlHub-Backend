// Package service — unit tests for the keyed MySQL/TiDB execution attempt policy, ordinary digest contract, and privacy-safe statement metadata.
// input: counting fakes (claim store, evidence pair, executor, resolver) — never a real database
// output: admission/evidence matrix proofs, literal digest goldens, masking and ownership assertions, bare-sentinel and nil-provider outcome proofs, controlled bad-reader saved-snapshot proofs, post-Apply malformed-evidence clearing proofs, and entry-clone fault-injection proofs across reader/claim/persistence seams
// pos: Fast seam-level proof that one TryClaim plus exactly one final evidence write governs every keyed MySQL/TiDB outcome; complements the real-DB HTTP golden test
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// keyedRequest builds the minimal well-formed keyed ordinary request.
func keyedRequest(key string) model.QueryExecuteRequest {
	return model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: key}
}

// assertNoEvidence asserts the full no-write invariant for a failure that
// must leave zero traces: no claim admission, no keyed finalize, no
// execution row, no audit pair.
func assertNoEvidence(t *testing.T, repo *fakeExecRepo) {
	t.Helper()
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 ||
		len(repo.pairCalls) != 0 || len(repo.insertedAttempts) != 0 {
		t.Fatalf("evidence leaked: claims=%d finalizes=%d pairs=%d inserts=%d",
			len(repo.claimInputs), len(repo.finalizeCalls), len(repo.pairCalls), len(repo.insertedAttempts))
	}
}

func TestMySQLAttempt_ShapeFailureWritesNothing(t *testing.T) {
	// WHY: shape and actor validation happen before target access — a
	// malformed request consumes no key, reads no credentials, and writes no
	// evidence. This is what lets an invalid keyed retry stay free.
	cases := []struct {
		name string
		req  model.QueryExecuteRequest
	}{
		{"empty statement", model.QueryExecuteRequest{ClientExecutionID: "k"}},
		{"NUL in statement", model.QueryExecuteRequest{Statement: "select \x00", ClientExecutionID: "k"}},
		{"invalid UTF-8 statement", model.QueryExecuteRequest{Statement: "select \xff", ClientExecutionID: "k"}},
		{"negative maxRows", model.QueryExecuteRequest{Statement: "select 1", MaxRows: -1, ClientExecutionID: "k"}},
		{"database set", model.QueryExecuteRequest{Statement: "select 1", Database: "d", ClientExecutionID: "k"}},
		{"schema set", model.QueryExecuteRequest{Statement: "select 1", Schema: "s", ClientExecutionID: "k"}},
		{"NUL in key", model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "k\x00"}},
		{"invalid UTF-8 key", model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "k\xff"}},
		{"oversized key", model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: strings.Repeat("k", 65)}},
		{"bad pagination", model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "k",
			Pagination: &model.QueryExecutePaginationRequest{Page: 0, PageSize: 10}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, resolver, executor := executionTestScaffold(t)
			_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, tc.req)
			if !errors.Is(err, ErrQueryValidationFailed) {
				t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
			}
			if resolver.called {
				t.Fatal("shape failure must not reach credential resolution")
			}
			if executor.called {
				t.Fatal("shape failure must not reach the executor")
			}
			assertNoEvidence(t, repo)
		})
	}
}

func TestMySQLAttempt_AccessDenialWritesNullKeyPairOnce(t *testing.T) {
	// WHY: access denial happens before claim admission — the occupied key
	// is never consumed, so the refusal lands on a NULL-key rejected pair
	// and a later permitted request may still claim the key.
	svc, repo, _, _ := executionTestScaffold(t)
	delete(repo.credentials, 9001)

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("deny-1"))
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionRejected {
		t.Fatalf("pairs = %+v, want one rejected NULL-key pair", repo.pairCalls)
	}
	if repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("access-denial pair must stay NULL-key, got %v", repo.pairCalls[0].rec.ClientExecutionID)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatalf("claim seam touched on access denial: claims=%d finalizes=%d",
			len(repo.claimInputs), len(repo.finalizeCalls))
	}
}

func TestMySQLAttempt_ClaimStoreFailureWritesNullKeyFailed(t *testing.T) {
	// WHY: a non-duplicate claim-INSERT failure is a backend failure of the
	// control plane — the key never occupied, so the failure records exactly
	// once as a NULL-key failed pair and surfaces a bare backend error.
	svc, repo, _, executor := executionTestScaffold(t)
	repo.claimErr = errors.New("claim insert exploded")

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("cf-1"))
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if executor.called {
		t.Fatal("claim failure must never reach the executor")
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairs = %d, want exactly one NULL-key failed pair", len(repo.pairCalls))
	}
	rec := repo.pairCalls[0].rec
	if rec.ClientExecutionID != nil || rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_backend_error" {
		t.Fatalf("claim-failure pair = %+v, want NULL-key failed/query_backend_error", rec)
	}
	// insertedAttempts mirrors the pair's record — the same single NULL-key
	// row. The keyed finalize arm must stay untouched.
	if len(repo.finalizeCalls) != 0 || len(repo.insertedAttempts) != 1 {
		t.Fatalf("unexpected writes after claim failure: finalizes=%d inserts=%d",
			len(repo.finalizeCalls), len(repo.insertedAttempts))
	}
}

func TestMySQLAttempt_DuplicateAndConflictWriteNothing(t *testing.T) {
	// WHY: an occupied key replays as already-exists and a same-key
	// different-request as conflict — both without writing any new claim,
	// execution, or audit row and without running user SQL again.
	svc, repo, _, executor := executionTestScaffold(t)
	key := "dup-1"
	if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest(key)); err != nil {
		t.Fatalf("first keyed execute: %v", err)
	}
	claims, finalizes, queries := len(repo.claimInputs), len(repo.finalizeCalls), executor.queryCalls

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest(key))
	if !errors.Is(err, ErrQueryExecutionAlreadyExists) {
		t.Fatalf("replay error = %v, want ErrQueryExecutionAlreadyExists", err)
	}
	_, err = svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select 2", ClientExecutionID: key})
	if !errors.Is(err, ErrQueryExecutionConflict) {
		t.Fatalf("conflict error = %v, want ErrQueryExecutionConflict", err)
	}
	if len(repo.claimInputs) != claims || len(repo.finalizeCalls) != finalizes || executor.queryCalls != queries {
		t.Fatalf("replay/conflict wrote or ran: claims=%d finalizes=%d queries=%d",
			len(repo.claimInputs), len(repo.finalizeCalls), executor.queryCalls)
	}
	if len(repo.pairCalls) != 0 || len(repo.insertedAttempts) != 0 {
		t.Fatalf("replay/conflict wrote NULL-key evidence: pairs=%d inserts=%d",
			len(repo.pairCalls), len(repo.insertedAttempts))
	}
}

func TestMySQLAttempt_PostClaimGuardDenyFinalizesKeyedOnce(t *testing.T) {
	// WHY: admission runs before the full guard, so a guard rejection is a
	// post-claim terminal outcome — the sole claimant finalizes the key
	// exactly once as rejected; no NULL-key row may appear alongside.
	svc, repo, _, _ := executionTestScaffold(t)
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "delete from t", ClientExecutionID: "gd-1"})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claims = %d, want 1", len(repo.claimInputs))
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("keyed finalizes = %d, want exactly 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.Status != model.QueryExecutionRejected || rec.ClientExecutionID == nil || *rec.ClientExecutionID != "gd-1" {
		t.Fatalf("keyed finalize = %+v, want rejected under key gd-1", rec)
	}
	if len(repo.pairCalls) != 0 || len(repo.insertedAttempts) != 0 {
		t.Fatalf("NULL-key evidence alongside keyed finalize: pairs=%d inserts=%d",
			len(repo.pairCalls), len(repo.insertedAttempts))
	}
}

func TestMySQLAttempt_TimeoutAndCancelClassify(t *testing.T) {
	// WHY: executor deadline and client cancellation are terminal attempt
	// outcomes classified by the shared taxonomy — timeout and
	// failed/query_canceled respectively — finalized under the owned key.
	t.Run("deadline", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.err = context.DeadlineExceeded
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("to-1"))
		if !errors.Is(err, ErrQueryTimeout) {
			t.Fatalf("error = %v, want ErrQueryTimeout", err)
		}
		if len(repo.finalizeCalls) != 1 || repo.finalizeCalls[0].Status != model.QueryExecutionTimeout {
			t.Fatalf("finalizes = %+v, want one keyed timeout finalize", repo.finalizeCalls)
		}
	})
	t.Run("canceled", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.err = context.Canceled
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("cc-1"))
		if err == nil {
			t.Fatal("canceled execution must error")
		}
		if len(repo.finalizeCalls) != 1 {
			t.Fatalf("finalizes = %d, want 1", len(repo.finalizeCalls))
		}
		rec := repo.finalizeCalls[0]
		if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_canceled" {
			t.Fatalf("finalize = %+v, want failed/query_canceled", rec)
		}
	})
}

func TestMySQLAttempt_SuccessKeyedFinalize(t *testing.T) {
	// WHY: a keyed success links claim to one terminal execution carrying
	// the owned key, plain-empty connection scope, NULL backend PID, empty
	// remote state, and the budget recorded before claim.
	svc, repo, _, _ := executionTestScaffold(t)
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("ok-1"))
	if err != nil {
		t.Fatalf("keyed execute: %v", err)
	}
	if resp.ExecutionID == 0 || resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("response = %+v", resp)
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 || len(repo.pairCalls) != 0 {
		t.Fatalf("writes: claims=%d finalizes=%d pairs=%d, want 1/1/0",
			len(repo.claimInputs), len(repo.finalizeCalls), len(repo.pairCalls))
	}
	in := repo.claimInputs[0]
	if in.ClientExecutionID != "ok-1" || in.DatabaseName != "" || in.SchemaName != "" {
		t.Fatalf("claim input scope = %+v, want key ok-1 with empty conn scope", in)
	}
	if in.AttemptTimeoutMs != 5000 {
		t.Fatalf("claim AttemptTimeoutMs = %d, want 5000 (staging default)", in.AttemptTimeoutMs)
	}
	if len(in.RequestDigest) != 64 {
		t.Fatalf("claim digest length = %d, want 64 hex chars", len(in.RequestDigest))
	}
	rec := repo.finalizeCalls[0]
	if rec.ClientExecutionID == nil || *rec.ClientExecutionID != "ok-1" {
		t.Fatalf("finalized record missing owned key: %+v", rec)
	}
	if rec.DatabaseName != "" || rec.SchemaName != "" || rec.BackendPID != nil || rec.RemoteState != "" {
		t.Fatalf("finalized record scope leak: %+v", rec)
	}
	if repo.finalizeEvents[0] != "query.executed:success" {
		t.Fatalf("audit event = %q, want query.executed:success", repo.finalizeEvents[0])
	}
}

func TestMySQLAttempt_FinalizeFailureNoCompensation(t *testing.T) {
	// WHY: a keyed finalize failure performs no retry and no compensating
	// NULL-key write — the claim stays occupied and the controlled
	// persistence error surfaces; no fake execution ID is invented.
	svc, repo, _, _ := executionTestScaffold(t)
	repo.finalizeErr = errors.New("finalize exploded")
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("ff-1"))
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want the controlled persistence failure", err)
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalizes = %d, want the single attempted finalize", len(repo.finalizeCalls))
	}
	if len(repo.pairCalls) != 0 || len(repo.insertedAttempts) != 0 {
		t.Fatalf("compensating evidence written: pairs=%d inserts=%d",
			len(repo.pairCalls), len(repo.insertedAttempts))
	}
}

func TestMySQLAttempt_MachineActor(t *testing.T) {
	// WHY: the claim protocol carries typed identity — a machine principal
	// claims and finalizes under its own actor slot, never the user column.
	svc, repo, _, _ := executionTestScaffold(t)
	machine := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 9}
	resp, err := svc.Execute(context.Background(), machine, 9001, keyedRequest("mc-1"))
	if err != nil {
		t.Fatalf("machine keyed execute: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %v", resp.Status)
	}
	in := repo.claimInputs[0]
	if in.Identity.Kind != model.QueryExecutionActorMachine || in.Identity.ID != 9 {
		t.Fatalf("claim identity = %+v, want machine 9", in.Identity)
	}
	rec := repo.finalizeCalls[0]
	if rec.ActorUserID != 0 || rec.ActorMachinePrincipalID != 9 {
		t.Fatalf("finalized actor = user %d machine %d, want machine 9",
			rec.ActorUserID, rec.ActorMachinePrincipalID)
	}
}

func TestMySQLAttempt_SnapshotOwnership(t *testing.T) {
	// WHY: the coordinator's delivery snapshot is taken before the evidence
	// write. A producer object mutated at the persistence boundary must not
	// reach the returned columns, rows, or cellTruncated matrix — the gate
	// and response consume only decision-owned data.
	svc, repo, _, executor := executionTestScaffold(t)
	executor.result = QueryDatabaseResult{
		Columns:       []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:          [][]any{{int64(1)}},
		RowCount:      1,
		CellTruncated: [][]bool{{false}},
	}
	repo.finalizeHook = func() {
		executor.result.Columns[0].Name = "mutated"
		executor.result.Rows[0][0] = int64(999)
		executor.result.CellTruncated[0][0] = true
		executor.result.RowCount = 42
	}
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("sn-1"))
	if err != nil {
		t.Fatalf("keyed execute: %v", err)
	}
	if resp.Columns[0].Name != "value" {
		t.Fatalf("response column mutated by producer: %q", resp.Columns[0].Name)
	}
	if resp.Rows[0][0] != int64(1) {
		t.Fatalf("response row mutated by producer: %v", resp.Rows[0][0])
	}
	if resp.CellTruncated[0][0] {
		t.Fatal("response matrix mutated by producer")
	}
	if resp.RowCount != 1 {
		t.Fatalf("response rowCount mutated by producer: %d", resp.RowCount)
	}
}

func TestMySQLAttempt_MissingMatrixIsBackendFailure(t *testing.T) {
	// WHY: a producer result without its cellTruncated matrix is internal
	// damage, not a disclosure refusal or a capability issue — it finalizes
	// as failed/query_backend_error under the key, never as a 409 gate.
	svc, repo, _, executor := executionTestScaffold(t)
	executor.rawResult = true
	executor.result = QueryDatabaseResult{
		Columns:  []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:     [][]any{{int64(1)}},
		RowCount: 1,
		// CellTruncated deliberately absent.
	}
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("mm-1"))
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalizes = %d, want 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_backend_error" {
		t.Fatalf("finalize = %+v, want failed/query_backend_error", rec)
	}
}

func TestMySQLAttempt_PostApplyMalformedMatrixClearsSuccessArtifacts(t *testing.T) {
	// WHY: a post-Apply matrix mismatch is producer corruption at the last
	// possible boundary — the finalize writes exactly one failed
	// query_backend_error record that carries no user-success artifacts
	// (no owner-private full statement, no row count) and no compensating
	// second write.
	t.Run("keyed", func(t *testing.T) {
		svc, repo, _, _, _ := executionScaffoldWithDisclosure(t, &fakeDisclosureService{
			// Apply fabricates a second row the one-row matrix cannot cover.
			applyRows: [][]any{{int64(1)}, {int64(2)}},
		})
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: "select value from t", ClientExecutionID: "pa-1"})
		if !errors.Is(err, ErrQueryBackendFailure) {
			t.Fatalf("post-Apply corruption = %v, want ErrQueryBackendFailure", err)
		}
		if len(repo.finalizeCalls) != 1 || len(repo.pairCalls) != 0 {
			t.Fatalf("post-Apply corruption must land exactly one keyed finalize: finalizes=%d pairs=%d",
				len(repo.finalizeCalls), len(repo.pairCalls))
		}
		rec := repo.finalizeCalls[0]
		if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_backend_error" {
			t.Fatalf("finalize = %+v, want failed/query_backend_error", rec)
		}
		if rec.FullStatement != "" || rec.RowCount != 0 {
			t.Fatalf("failed row carried user-success artifacts: full=%q rows=%d", rec.FullStatement, rec.RowCount)
		}
	})
	t.Run("keyless", func(t *testing.T) {
		svc, repo, _, _, _ := executionScaffoldWithDisclosure(t, &fakeDisclosureService{
			applyRows: [][]any{{int64(1)}, {int64(2)}},
		})
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: "select value from t"})
		if !errors.Is(err, ErrQueryBackendFailure) {
			t.Fatalf("post-Apply corruption = %v, want ErrQueryBackendFailure", err)
		}
		if len(repo.pairCalls) != 1 || len(repo.finalizeCalls) != 0 {
			t.Fatalf("post-Apply corruption must land exactly one pair: pairs=%d finalizes=%d",
				len(repo.pairCalls), len(repo.finalizeCalls))
		}
		rec := repo.pairCalls[0].rec
		if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_backend_error" {
			t.Fatalf("pair = %+v, want failed/query_backend_error", rec)
		}
		if rec.FullStatement != "" || rec.RowCount != 0 {
			t.Fatalf("failed row carried user-success artifacts: full=%q rows=%d", rec.FullStatement, rec.RowCount)
		}
	})
}

func TestMySQLAttempt_SavedKindWithholdsStatement(t *testing.T) {
	// WHY: saved-statement executions must never let the compiled statement
	// reach private history — the entry kind forces digest, preview, and
	// full statement empty even on a user-actor success.
	svc, _, _, _ := executionTestScaffold(t)
	attempt := newMySQLExecutionAttempt(svc, mysqlTarget("Staging"), userExecutionIdentity(7), mysqlEntrySaved, time.Now())
	guarded := &GuardedQuery{
		OriginalStatement: "select 1 /* compiled */",
		StatementDigest:   "select ?",
		StatementPreview:  "select 1 /* compiled */",
	}
	rec := attempt.buildAttemptRecord(guarded, model.QueryExecutionSuccess, 1, "", "")
	if rec.FullStatement != "" || rec.StatementDigest != "" || rec.StatementPreview != "" {
		t.Fatalf("saved evidence leaked statement metadata: %+v", rec)
	}
}

func TestMySQLOrdinaryDigest_Goldens(t *testing.T) {
	// WHY: the canonical v1 wire digest is a frozen contract — these literal
	// hashes pin the resolved-input assembler so a field-order or scope bug
	// cannot silently re-key every claim.
	svc, _, _, _ := executionTestScaffold(t)
	guard := svc.guard
	resolve := func(env string, requested int) int {
		return guard.effectiveMaxRows(clampProductionMaxRows(env, requested))
	}
	cases := []struct {
		name string
		req  model.QueryExecuteRequest
		env  string
		maxR int
		want string
	}{
		{"base unpaged", model.QueryExecuteRequest{Statement: "SELECT id FROM app.orders"}, "Staging", resolve("Staging", 100),
			"fccc3c232102b49313e913ef0cfc2623b2de0c5889b725834244eca730ce6b3b"},
		{"explicit paging", model.QueryExecuteRequest{Statement: "SELECT id FROM app.orders",
			Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 25}}, "Staging", resolve("Staging", 100),
			"675e29122bf78eb55a63091146160d3d44e95e2779a78888b15c35ae34293bd9"},
		{"original bytes preserved", model.QueryExecuteRequest{Statement: " SELECT id FROM app.orders;\n"}, "Staging", resolve("Staging", 100),
			"530869ff9b66dfa1e294d1729c13dc29441a19d00aa6cd57879db29e88569474"},
		{"higher resolved cap", model.QueryExecuteRequest{Statement: "SELECT id FROM app.orders"}, "Staging", resolve("Staging", 500),
			"3d8278ef40c06e65445375d648b202c37011b1975e8bc370a153ce3f54c27f5e"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := svc.mysqlOrdinaryDigest(tc.req, tc.maxR); got != tc.want {
				t.Fatalf("digest = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMySQLOrdinaryDigest_Equivalences(t *testing.T) {
	// WHY: capabilities and the client key are never digest inputs — two
	// requests identical except for those fields occupy the same digest.
	svc, _, _, _ := executionTestScaffold(t)
	base := model.QueryExecuteRequest{Statement: "select 1", MaxRows: 100}
	if a, b := svc.mysqlOrdinaryDigest(base, 100),
		svc.mysqlOrdinaryDigest(model.QueryExecuteRequest{
			Statement: "select 1", MaxRows: 100,
			ClientExecutionID: "k-1", Capabilities: []string{"cellTruncated"},
		}, 100); a != b {
		t.Fatalf("key/capabilities changed the digest: %s vs %s", a, b)
	}

	// Resolved-cap equivalences: the production clamp collapses omitted and
	// oversized requests to the same 100 cap, and the non-production hard
	// cap lands at 500.
	guard := NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	resolve := func(env string, requested int) int {
		return guard.effectiveMaxRows(clampProductionMaxRows(env, requested))
	}
	if got := resolve("Production", 0); got != 100 {
		t.Fatalf("production omitted cap = %d, want 100", got)
	}
	if got := resolve("Production", 700); got != 100 {
		t.Fatalf("production oversized cap = %d, want 100", got)
	}
	if got := resolve("Staging", 700); got != 500 {
		t.Fatalf("staging oversized cap = %d, want hard cap 500", got)
	}
	req := model.QueryExecuteRequest{Statement: "select 1"}
	if a, b := svc.mysqlOrdinaryDigest(req, resolve("Production", 0)),
		svc.mysqlOrdinaryDigest(req, resolve("Production", 700)); a != b {
		t.Fatal("production omitted vs oversized maxRows must digest identically")
	}
}

func TestMySQLStatementMetadata_MasksLiteralsAndComments(t *testing.T) {
	// WHY: statement evidence must carry shape only — literal values and
	// comments are stripped on the sanitized AST, so private markers can
	// never reach digest or preview for user, machine, or failure rows.
	guard := NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	digest, preview := mysqlStatementMetadata(guard, "SELECT 'MYSQL_KEYED_VALUE_A' /*MYSQL_KEYED_COMMENT_B*/ FROM t")
	for _, field := range []string{digest, preview} {
		if strings.Contains(field, "MYSQL_KEYED_VALUE_A") || strings.Contains(field, "MYSQL_KEYED_COMMENT_B") {
			t.Fatalf("metadata leaked marker: %q", field)
		}
	}
	if !strings.Contains(digest, "?") {
		t.Fatalf("digest must carry the ? literal placeholder, got %q", digest)
	}

	// Non-SELECT shapes withhold entirely — never a raw-SQL fallback.
	if d, p := mysqlStatementMetadata(guard, "SHOW TABLES"); d != mysqlStatementWithheld || p != mysqlStatementWithheld {
		t.Fatalf("non-SELECT metadata = (%q,%q), want withheld", d, p)
	}
}

func TestMySQLAttempt_StatementPrivacyEndToEnd(t *testing.T) {
	// WHY: the masked metadata flows through the real guarded chain — a
	// user-actor success keeps only the owner-private full statement while
	// digest/preview stay sanitized, and a machine success stores no full
	// statement at all.
	const stmt = "SELECT 'MYSQL_KEYED_VALUE_A' /*MYSQL_KEYED_COMMENT_B*/ FROM t"

	t.Run("user success keeps owner-only full statement", func(t *testing.T) {
		svc, repo, _, _ := executionTestScaffold(t)
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: stmt, ClientExecutionID: "pv-u"})
		if err != nil {
			t.Fatalf("keyed execute: %v", err)
		}
		rec := repo.finalizeCalls[0]
		if strings.Contains(rec.StatementDigest, "MYSQL_KEYED") || strings.Contains(rec.StatementPreview, "MYSQL_KEYED") {
			t.Fatalf("masked metadata leaked markers: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
		}
		if rec.FullStatement != stmt {
			t.Fatalf("owner private statement = %q, want the verbatim original", rec.FullStatement)
		}
	})
	t.Run("machine success stores no full statement", func(t *testing.T) {
		svc, repo, _, _ := executionTestScaffold(t)
		machine := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 9}
		_, err := svc.Execute(context.Background(), machine, 9001,
			model.QueryExecuteRequest{Statement: stmt, ClientExecutionID: "pv-m"})
		if err != nil {
			t.Fatalf("machine keyed execute: %v", err)
		}
		rec := repo.finalizeCalls[0]
		if rec.FullStatement != "" {
			t.Fatalf("machine success stored a full statement: %q", rec.FullStatement)
		}
	})
	t.Run("failure row keeps masked metadata but no full statement", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.err = context.Canceled
		_, _ = svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: stmt, ClientExecutionID: "pv-f"})
		rec := repo.finalizeCalls[0]
		if strings.Contains(rec.StatementDigest, "MYSQL_KEYED") || strings.Contains(rec.StatementPreview, "MYSQL_KEYED") {
			t.Fatalf("failure row leaked markers: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
		}
		if rec.FullStatement != "" {
			t.Fatalf("failure row stored a full statement: %q", rec.FullStatement)
		}
	})
}

func TestMySQLStatementMetadata_UnsupportedLiteralWithheld(t *testing.T) {
	// WHY: the sanitizer masks *Literal nodes only — the parser's standalone
	// BoolVal and NullVal expressions are literals it does not cover, so any
	// statement containing them withholds rather than leaking unmasked value
	// text into history.
	guard := NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	for _, stmt := range []string{
		"SELECT true FROM t",
		"SELECT false FROM t",
		"SELECT null FROM t",
		"SELECT * FROM t WHERE x = true",
	} {
		d, p := mysqlStatementMetadata(guard, stmt)
		if d != mysqlStatementWithheld || p != mysqlStatementWithheld {
			t.Fatalf("statement %q metadata = (%q,%q), want withheld", stmt, d, p)
		}
	}
}

func TestMySQLAttempt_RequestOwnershipIsolation(t *testing.T) {
	// WHY: the attempt clones the caller-owned capabilities slice and
	// pagination before any external call, so a caller mutating its request
	// while the evidence write runs cannot arm or disarm the delivery gate;
	// and the returned payload is a coordinator-owned snapshot — mutating it
	// must not corrupt the producer's result.
	truncatedResult := QueryDatabaseResult{
		Columns:       []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:          [][]any{{int64(1)}},
		RowCount:      1,
		CellTruncated: [][]bool{{true}},
	}

	t.Run("keyed: disarmed caller slice still delivers to a capable request", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.rawResult = true
		executor.result = truncatedResult
		caps := []string{CapabilityCellTruncated}
		repo.finalizeHook = func() { caps[0] = "unknown-cap" }
		resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "ow-1", Capabilities: caps})
		if err != nil {
			t.Fatalf("capable request must still deliver after caller mutation: %v", err)
		}
		if len(resp.Rows) != 1 {
			t.Fatalf("truncated page must be delivered to the declared capability, rows=%v", resp.Rows)
		}
	})

	t.Run("keyed: armed caller slice cannot unlock an incapable request", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.rawResult = true
		executor.result = truncatedResult
		caps := []string{"unknown-cap"}
		repo.finalizeHook = func() { caps[0] = CapabilityCellTruncated }
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "ow-2", Capabilities: caps})
		if !errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
		}
	})

	t.Run("keyless: pair boundary sees the same cloned request", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.rawResult = true
		executor.result = truncatedResult
		caps := []string{"unknown-cap"}
		repo.pairHook = func() { caps[0] = CapabilityCellTruncated }
		_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: "select 1", Capabilities: caps})
		if !errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
		}
		if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionSuccess {
			t.Fatalf("delivery refusal must keep the committed success pair, pairs=%+v", repo.pairCalls)
		}
	})

	t.Run("consumer mutation cannot corrupt the producer result", func(t *testing.T) {
		svc, _, _, executor := executionTestScaffold(t)
		executor.rawResult = true
		executor.result = truncatedResult
		resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "ow-3", Capabilities: []string{CapabilityCellTruncated}})
		if err != nil {
			t.Fatalf("keyed execute: %v", err)
		}
		resp.Rows[0][0] = int64(999)
		resp.Columns[0].Name = "mutated"
		resp.CellTruncated[0][0] = false
		if executor.result.Rows[0][0] != int64(1) || executor.result.Columns[0].Name != "value" || !executor.result.CellTruncated[0][0] {
			t.Fatal("mutating the returned payload reached the producer's result object")
		}
	})
}

func TestMySQLAttempt_KeylessStatementPrivacy(t *testing.T) {
	// WHY: statement privacy is independent of the key — the keyless legacy
	// path goes through the same ordinary entry, so its persisted metadata
	// must be sanitized identically while the owner-only full statement
	// contract is preserved.
	const stmt = "SELECT 'MYSQL_KEYED_VALUE_A' /*MYSQL_KEYED_COMMENT_B*/ FROM t"

	t.Run("user success", func(t *testing.T) {
		svc, repo, _, _ := executionTestScaffold(t)
		if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: stmt}); err != nil {
			t.Fatalf("keyless execute: %v", err)
		}
		rec := repo.insertedAttempts[0]
		if strings.Contains(rec.StatementDigest, "MYSQL_KEYED") || strings.Contains(rec.StatementPreview, "MYSQL_KEYED") {
			t.Fatalf("keyless metadata leaked markers: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
		}
		if rec.FullStatement != stmt {
			t.Fatalf("owner private statement = %q, want the verbatim original", rec.FullStatement)
		}
	})
	t.Run("machine success stores no full statement", func(t *testing.T) {
		svc, repo, _, _ := executionTestScaffold(t)
		machine := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 9}
		if _, err := svc.Execute(context.Background(), machine, 9001,
			model.QueryExecuteRequest{Statement: stmt}); err != nil {
			t.Fatalf("machine keyless execute: %v", err)
		}
		rec := repo.insertedAttempts[0]
		if strings.Contains(rec.StatementDigest, "MYSQL_KEYED") || strings.Contains(rec.StatementPreview, "MYSQL_KEYED") {
			t.Fatalf("keyless machine metadata leaked markers: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
		}
		if rec.FullStatement != "" {
			t.Fatalf("machine success stored a full statement: %q", rec.FullStatement)
		}
	})
	t.Run("failure row keeps masked metadata but no full statement", func(t *testing.T) {
		svc, repo, _, executor := executionTestScaffold(t)
		executor.err = context.Canceled
		_, _ = svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
			model.QueryExecuteRequest{Statement: stmt})
		rec := repo.insertedAttempts[0]
		if strings.Contains(rec.StatementDigest, "MYSQL_KEYED") || strings.Contains(rec.StatementPreview, "MYSQL_KEYED") {
			t.Fatalf("keyless failure row leaked markers: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
		}
		if rec.FullStatement != "" {
			t.Fatalf("failure row stored a full statement: %q", rec.FullStatement)
		}
	})
}

func TestMySQLAttempt_DisclosureDenialDropsRawDetail(t *testing.T) {
	// WHY: the disclosure service blends its raw error (which may carry
	// SQL text, values, or driver detail) into the blocked wrap — the
	// post-target rejection must surface only the fixed sentinel join and
	// the fixed safe message, with the keyed finalize recorded once.
	markers := []string{"MYSQL_PRIVACY_DETAIL", "secret-dsn-do-not-leak"}
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"wrapped detail", fmt.Errorf("%w: %s", ErrQueryDisclosureBlocked, markers[0])},
		{"joined detail", errors.Join(ErrQueryDisclosureBlocked, errors.New(markers[1]))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _, _ := executionScaffoldWithDisclosure(t,
				&fakeDisclosureService{preflightErr: tc.err})
			_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, keyedRequest("dd-1"))
			if !errors.Is(err, ErrQueryNotAllowed) || !errors.Is(err, ErrQueryDisclosureBlocked) {
				t.Fatalf("error = %v, want ErrQueryNotAllowed+ErrQueryDisclosureBlocked", err)
			}
			// Walk the whole unwrap tree — the fixed sentinel join is the
			// only permitted content; no node may carry the raw detail.
			for _, node := range errorTree(err) {
				for _, m := range markers {
					if strings.Contains(node.Error(), m) {
						t.Fatalf("returned error chain leaked raw detail %q at node %v", m, node)
					}
				}
			}
			if len(repo.finalizeCalls) != 1 {
				t.Fatalf("finalizes = %d, want exactly one keyed rejection", len(repo.finalizeCalls))
			}
			rec := repo.finalizeCalls[0]
			if rec.Status != model.QueryExecutionRejected || rec.ErrorCode != "query_result_disclosure_blocked" {
				t.Fatalf("finalize = %+v, want rejected/query_result_disclosure_blocked", rec)
			}
			for _, m := range markers {
				if strings.Contains(rec.ErrorMessage, m) {
					t.Fatalf("recorded message leaked raw detail %q: %q", m, rec.ErrorMessage)
				}
			}
		})
	}
}

// --- Saved-statement keyed entry ---

func TestMySQLSavedKeyed_SuccessClaimsAndFinalizesOnce(t *testing.T) {
	// WHY: a keyed saved execution admits once with empty connection scope
	// and records exactly one keyed finalize carrying no statement-derived
	// history — never the keyless pair.
	svc, repo, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select id from app.orders where id = :cell_id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "cell_id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	req := templateExecuteRequest(map[string]string{"cell_id": "1"})
	req.ClientExecutionID = "saved-key-1"

	resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
	if err != nil {
		t.Fatalf("keyed saved execute: %v", err)
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 || len(repo.pairCalls) != 0 {
		t.Fatalf("claims=%d finalizes=%d pairs=%d, want 1/1/0", len(repo.claimInputs), len(repo.finalizeCalls), len(repo.pairCalls))
	}
	in := repo.claimInputs[0]
	if in.DatabaseName != "" || in.SchemaName != "" || in.ClientExecutionID != "saved-key-1" {
		t.Fatalf("claim input scope = (%q,%q,%q)", in.DatabaseName, in.SchemaName, in.ClientExecutionID)
	}
	rec := repo.finalizeCalls[0]
	if rec.Status != model.QueryExecutionSuccess || rec.StatementDigest != "" || rec.StatementPreview != "" || rec.FullStatement != "" {
		t.Fatalf("saved finalize must carry no statement history: %+v", rec)
	}
	if executor.templateCalls != 1 {
		t.Fatalf("QueryTemplate calls = %d, want 1", executor.templateCalls)
	}
	if resp.ExecutionID == 0 {
		t.Fatal("keyed saved response must carry the finalized execution ID")
	}
}

func TestMySQLSavedKeyed_ReplayAndConflictWriteNothing(t *testing.T) {
	svc, repo, _, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select 1", nil), nil)
	req := templateExecuteRequest(nil)
	req.ClientExecutionID = "saved-key-2"
	if _, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req); err != nil {
		t.Fatalf("first keyed saved execute: %v", err)
	}
	before := len(repo.finalizeCalls)

	if _, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req); !errors.Is(err, ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key replay = %v, want ErrQueryExecutionAlreadyExists", err)
	}
	other := templateExecuteRequest(nil)
	other.ClientExecutionID = "saved-key-2"
	other.MaxRows = 50
	if _, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, other); !errors.Is(err, ErrQueryExecutionConflict) {
		t.Fatalf("same-key different digest = %v, want ErrQueryExecutionConflict", err)
	}
	if len(repo.finalizeCalls) != before || len(repo.pairCalls) != 0 || len(repo.claimInputs) != 1 {
		t.Fatalf("replay/conflict wrote evidence: finalizes=%d pairs=%d claims=%d",
			len(repo.finalizeCalls), len(repo.pairCalls), len(repo.claimInputs))
	}
}

func TestMySQLSavedKeyed_ValueValidationBeforeClaim(t *testing.T) {
	// WHY: typed-value validation runs before claim admission — an invalid
	// value is a rejected NULL-key pair and must never occupy a claim slot.
	svc, repo, _, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select id from app.orders where id = :cell_id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "cell_id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	req := templateExecuteRequest(map[string]string{"cell_id": `"not-an-int"`})
	req.ClientExecutionID = "saved-key-3"

	var valueErr *TemplateValueValidationError
	if _, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req); !errors.As(err, &valueErr) {
		t.Fatalf("invalid typed value = %v, want TemplateValueValidationError", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatalf("value validation admitted a claim: claims=%d finalizes=%d", len(repo.claimInputs), len(repo.finalizeCalls))
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionRejected {
		t.Fatalf("pairs = %+v, want one rejected NULL-key pair", repo.pairCalls)
	}
	if repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("pre-claim rejection must stay NULL-key, got %v", repo.pairCalls[0].rec.ClientExecutionID)
	}
}

func TestMySQLSavedKeyed_ReadFailureClassifiesNULLPair(t *testing.T) {
	// WHY: a canceled snapshot read classifies through the shared taxonomy
	// into a failed NULL-key pair with a bare sentinel — never a raw driver
	// error.
	svc, repo, _, _ := newTemplateExecutionTestService(model.QuerySavedStatement{}, context.Canceled)
	if _, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, templateExecuteRequest(nil)); !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("canceled read = %v, want bare ErrQueryBackendFailure", err)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionFailed || repo.pairCalls[0].rec.ErrorCode != "query_canceled" {
		t.Fatalf("canceled read pair = %+v, want failed/query_canceled", repo.pairCalls)
	}
	// The actual repository carries exactly one NULL-key row — the pair's —
	// and the claim/finalize arms stay untouched.
	if repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("read-failure pair must stay NULL-key, got %v", repo.pairCalls[0].rec.ClientExecutionID)
	}
	if len(repo.insertedAttempts) != 1 || len(repo.auditEvents) != 1 {
		t.Fatalf("canceled read must persist exactly the pair's one row: inserts=%d audits=%d",
			len(repo.insertedAttempts), len(repo.auditEvents))
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatalf("pre-claim read failure touched the claim store: claims=%d", len(repo.claimInputs))
	}
}

func TestMySQLSavedKeyed_ReadFailurePrecedenceJoinedErrors(t *testing.T) {
	// WHY: context.Canceled and context.DeadlineExceeded are checked BEFORE
	// sql.ErrNoRows on the snapshot read — a joined or wrapped cancellation
	// must classify through the shared taxonomy, never masquerade as a
	// missing statement or leak raw read detail.
	for _, tc := range []struct {
		name       string
		readErr    error
		wantErr    error
		wantStatus model.QueryExecutionStatus
		wantCode   string
	}{
		{"joined canceled", errors.Join(sql.ErrNoRows, context.Canceled),
			ErrQueryBackendFailure, model.QueryExecutionFailed, "query_canceled"},
		{"joined deadline", errors.Join(sql.ErrNoRows, context.DeadlineExceeded),
			ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout"},
		{"wrapped canceled", fmt.Errorf("statement read: %w", context.Canceled),
			ErrQueryBackendFailure, model.QueryExecutionFailed, "query_canceled"},
		{"joined no-rows stays not-found", errors.Join(sql.ErrNoRows, errors.New("driver detail")),
			ErrQuerySavedStatementNotFound, model.QueryExecutionRejected, "saved_statement_not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, executor, _ := newTemplateExecutionTestService(model.QuerySavedStatement{}, tc.readErr)
			req := templateExecuteRequest(nil)
			req.ClientExecutionID = "read-prec-1"

			_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("read error = %v, want %v", err, tc.wantErr)
			}
			// The returned chain is the bare sentinel: exactly one node, no
			// wrapped safe message, no raw driver detail.
			if tree := errorTree(err); len(tree) != 1 {
				t.Fatalf("returned error chain = %d nodes, want the bare sentinel: %v", len(tree), err)
			}
			if len(repo.pairCalls) != 1 {
				t.Fatalf("pairs = %d, want exactly one NULL-key pair", len(repo.pairCalls))
			}
			rec := repo.pairCalls[0].rec
			if rec.Status != tc.wantStatus || rec.ErrorCode != tc.wantCode || rec.ClientExecutionID != nil {
				t.Fatalf("pair = %+v, want %v/%s NULL-key", rec, tc.wantStatus, tc.wantCode)
			}
			if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.templateCalls != 0 {
				t.Fatalf("read failure reached claim/finalize/executor: %d %d %d",
					len(repo.claimInputs), len(repo.finalizeCalls), executor.templateCalls)
			}
		})
	}
}

func TestMySQLSavedKeyed_OwnerMismatchIsInvisibleNotFound(t *testing.T) {
	svc, repo, _, _ := newTemplateExecutionTestService(
		templateStatement(99, model.QuerySavedStatementPersonal, "select 1", nil), nil)
	req := templateExecuteRequest(nil)
	req.ClientExecutionID = "saved-key-4"
	if _, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, req); !errors.Is(err, ErrQuerySavedStatementNotFound) {
		t.Fatalf("non-owner read = %v, want ErrQuerySavedStatementNotFound", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionRejected {
		t.Fatalf("owner mismatch = claims %d pairs %+v", len(repo.claimInputs), repo.pairCalls)
	}
}

func TestMySQLSavedKeyed_SnapshotContractViolationIsInvisibleNotFound(t *testing.T) {
	// WHY: the returned snapshot must be exactly the requested statement on
	// this target carrying a known scope. The production repository's WHERE
	// already scopes by (target, id), so these controlled bad-reader fakes
	// defend the service boundary only — they do NOT claim a production
	// cross-target leak. Every violation is the same invisible not-found:
	// one NULL-key rejected pair, no claim, no finalize, no executor call.
	for _, tc := range []struct {
		name   string
		mutate func(*model.QuerySavedStatement)
	}{
		{"foreign statement id", func(s *model.QuerySavedStatement) { s.ID = 99 }},
		{"foreign target id", func(s *model.QuerySavedStatement) { s.TargetResourceID = 7777 }},
		{"unknown scope", func(s *model.QuerySavedStatement) { s.Scope = model.QuerySavedStatementScope("team") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statement := templateStatement(7, model.QuerySavedStatementPersonal, "select 1", nil)
			tc.mutate(&statement)
			svc, repo, executor, _ := newTemplateExecutionTestService(statement, nil)
			req := templateExecuteRequest(nil)
			req.ClientExecutionID = "saved-contract-1"

			_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
			if !errors.Is(err, ErrQuerySavedStatementNotFound) {
				t.Fatalf("snapshot violation = %v, want ErrQuerySavedStatementNotFound", err)
			}
			if len(repo.pairCalls) != 1 {
				t.Fatalf("pairs = %d, want exactly one NULL-key rejected pair", len(repo.pairCalls))
			}
			rec := repo.pairCalls[0].rec
			if rec.Status != model.QueryExecutionRejected || rec.ErrorCode != "saved_statement_not_found" ||
				rec.ClientExecutionID != nil {
				t.Fatalf("pair = %+v, want rejected saved_statement_not_found NULL-key", rec)
			}
			if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.templateCalls != 0 {
				t.Fatalf("snapshot violation reached claim/finalize/executor: claims=%d finalizes=%d executor=%d",
					len(repo.claimInputs), len(repo.finalizeCalls), executor.templateCalls)
			}
		})
	}
}

func TestMySQLSavedKeyed_InputCopySurvivesClaimSeamMutation(t *testing.T) {
	// WHY: the request's raw values and capabilities are owned at entry —
	// mutating the caller's structures synchronously at the claim seam can
	// never reach the recorded digest or the bound template values.
	svc, repo, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select id from app.orders where id = :cell_id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "cell_id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	req := templateExecuteRequest(map[string]string{"cell_id": "1"})
	req.ClientExecutionID = "saved-key-5"
	req.Capabilities = []string{"cellTruncated"}
	wantDigest := svc.mysqlSavedDigest(7,
		[]TemplateParameterDefinition{{Name: "cell_id", Type: TemplateParameterInteger}},
		map[string]any{"cell_id": int64(1)}, 0, 0, 100)
	repo.claimHook = func() {
		req.Values["cell_id"][0] = '9'
		req.Capabilities[0] = "unknown-cap"
	}
	if _, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req); err != nil {
		t.Fatalf("keyed saved execute: %v", err)
	}
	if repo.claimInputs[0].RequestDigest != wantDigest {
		t.Fatalf("claim digest drifted under seam mutation: %s != %s", repo.claimInputs[0].RequestDigest, wantDigest)
	}
	if executor.gotTemplate == nil || len(executor.gotTemplate.args) != 1 || executor.gotTemplate.args[0] != int64(1) {
		t.Fatalf("bound values drifted under seam mutation: %+v", executor.gotTemplate)
	}
}

func TestMySQLSavedKeyed_InputCopySurvivesReaderSeamMutation(t *testing.T) {
	// WHY: the raw values and capabilities are cloned at request entry —
	// before the statement read — so a caller mutating its own structures
	// inside the reader seam (after cloning, ahead of typed validation and
	// the claim digest) can never reach the bound values, the recorded
	// digest, or the delivery gate. Mutating earlier than the claim seam
	// proves the clone happens at entry, not at admission.
	statement := templateStatement(7, model.QuerySavedStatementPersonal,
		"select id from app.orders where id = :cell_id",
		[]model.QuerySavedStatementParameterDefinition{{Name: "cell_id", Type: model.QuerySavedStatementParameterInteger}})
	svc, repo, executor, _ := newTemplateExecutionTestService(statement, nil)
	reader := &fakeSavedStatementReader{getResp: statement}
	svc.WithTemplateExecution(reader, NewTemplateStatementCompiler())
	executor.rawResult = true
	executor.result = truncatedResult()

	req := templateExecuteRequest(map[string]string{"cell_id": "1"})
	req.ClientExecutionID = "saved-key-6"
	caps := []string{CapabilityCellTruncated}
	req.Capabilities = caps
	wantDigest := svc.mysqlSavedDigest(7,
		[]TemplateParameterDefinition{{Name: "cell_id", Type: TemplateParameterInteger}},
		map[string]any{"cell_id": int64(1)}, 0, 0, 100)
	reader.getHook = func() {
		req.Values["cell_id"][0] = '9'
		caps[0] = "unknown-cap"
	}
	resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
	if err != nil {
		t.Fatalf("capable request must still deliver after reader-seam mutation: %v", err)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("truncated page must deliver to the declared capability, rows=%v", resp.Rows)
	}
	if repo.claimInputs[0].RequestDigest != wantDigest {
		t.Fatalf("claim digest drifted under reader-seam mutation: %s != %s", repo.claimInputs[0].RequestDigest, wantDigest)
	}
	if executor.gotTemplate == nil || len(executor.gotTemplate.args) != 1 || executor.gotTemplate.args[0] != int64(1) {
		t.Fatalf("bound values drifted under reader-seam mutation: %+v", executor.gotTemplate)
	}
}

func TestMySQLSavedKeyed_CapabilityMutationCannotArmOrDisarm(t *testing.T) {
	// WHY: the delivery gate consumes the entry-owned capability clone —
	// mutating the caller's slice at the claim seam must neither unlock an
	// incapable request nor disarm a capable one.
	t.Run("armed caller slice cannot unlock an incapable request", func(t *testing.T) {
		svc, repo, executor, _ := newTemplateExecutionTestService(
			templateStatement(7, model.QuerySavedStatementPersonal, "select 1", nil), nil)
		executor.rawResult = true
		executor.result = truncatedResult()
		req := templateExecuteRequest(nil)
		req.ClientExecutionID = "saved-cap-arm"
		caps := []string{"unknown-cap"}
		req.Capabilities = caps
		repo.claimHook = func() { caps[0] = CapabilityCellTruncated }
		_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
		if !errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
		}
	})
	t.Run("disarmed caller slice still delivers to a capable request", func(t *testing.T) {
		svc, repo, executor, _ := newTemplateExecutionTestService(
			templateStatement(7, model.QuerySavedStatementPersonal, "select 1", nil), nil)
		executor.rawResult = true
		executor.result = truncatedResult()
		req := templateExecuteRequest(nil)
		req.ClientExecutionID = "saved-cap-disarm"
		caps := []string{CapabilityCellTruncated}
		req.Capabilities = caps
		repo.claimHook = func() { caps[0] = "unknown-cap" }
		resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
		if err != nil {
			t.Fatalf("capable request must still deliver after caller mutation: %v", err)
		}
		if len(resp.Rows) != 1 {
			t.Fatalf("truncated page must deliver to the declared capability, rows=%v", resp.Rows)
		}
	})
}

// --- Navigation keyed entry ---

func TestMySQLNavKeyed_SuccessClaimsAndFinalizesOnce(t *testing.T) {
	// WHY: a keyed navigation admits once after trusted FK preparation and
	// records exactly one keyed finalize under related_record_navigation —
	// never a second pair.
	svc, repo, executor, _ := navTestScaffold(t)
	req := validNavRequest()
	req.ClientExecutionID = "nav-key-1"

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("keyed navigation: %v", err)
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 || len(repo.pairCalls) != 0 {
		t.Fatalf("claims=%d finalizes=%d pairs=%d, want 1/1/0", len(repo.claimInputs), len(repo.finalizeCalls), len(repo.pairCalls))
	}
	in := repo.claimInputs[0]
	if in.DatabaseName != "" || in.SchemaName != "" || in.ClientExecutionID != "nav-key-1" {
		t.Fatalf("claim input scope = (%q,%q,%q)", in.DatabaseName, in.SchemaName, in.ClientExecutionID)
	}
	if repo.finalizeEvents[0] != "related_record_navigation:success" {
		t.Fatalf("finalize event = %q, want related_record_navigation:success", repo.finalizeEvents[0])
	}
	rec := repo.finalizeCalls[0]
	if rec.StatementDigest != "nav:orders.orders/fk_order_items_order" || rec.StatementPreview != "related:orders.orders/fk_order_items_order" {
		t.Fatalf("navigation relation evidence = %q/%q", rec.StatementDigest, rec.StatementPreview)
	}
	if rec.FullStatement != "" {
		t.Fatalf("generated SQL must never reach history: %q", rec.FullStatement)
	}
	if executor.gotNavInput == nil || resp.ExecutionID == 0 {
		t.Fatalf("nav input/execution id missing: %+v %d", executor.gotNavInput, resp.ExecutionID)
	}
}

func TestMySQLNavKeyed_ReplayConflictAndSourceSchemaClosed(t *testing.T) {
	svc, repo, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.ClientExecutionID = "nav-key-2"
	if _, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req); err != nil {
		t.Fatalf("first keyed navigation: %v", err)
	}
	before := len(repo.finalizeCalls)

	if _, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req); !errors.Is(err, ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key replay = %v, want ErrQueryExecutionAlreadyExists", err)
	}
	other := validNavRequest()
	other.ClientExecutionID = "nav-key-2"
	other.LocalValues = []string{"43"}
	if _, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, other); !errors.Is(err, ErrQueryExecutionConflict) {
		t.Fatalf("same-key changed locator = %v, want ErrQueryExecutionConflict", err)
	}
	if len(repo.finalizeCalls) != before || len(repo.pairCalls) != 0 || len(repo.claimInputs) != 1 {
		t.Fatalf("replay/conflict wrote evidence: finalizes=%d pairs=%d claims=%d",
			len(repo.finalizeCalls), len(repo.pairCalls), len(repo.claimInputs))
	}

	// source.schema stays closed at the service boundary before any external
	// call — no access resolve, no inspector call, no evidence.
	schemaReq := validNavRequest()
	schemaReq.Source.Schema = "app"
	if _, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, schemaReq); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("source.schema = %v, want ErrQueryValidationFailed", err)
	}
	if len(repo.claimInputs) != 1 || len(repo.pairCalls) != 0 {
		t.Fatalf("source.schema rejection wrote evidence: claims=%d pairs=%d", len(repo.claimInputs), len(repo.pairCalls))
	}
}

func TestMySQLNavKeyed_PreclaimFailuresNULLPair(t *testing.T) {
	// WHY: an ordinary inspector fault before claim admission is a rejected
	// navigation_source_error NULL-key pair — no claim slot is occupied.
	svc, repo, _, inspector := navTestScaffold(t)
	inspector.err = errors.New("sensitive inspector detail")
	req := validNavRequest()
	req.ClientExecutionID = "nav-key-3"
	if _, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req); !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("inspector fault = %v, want ErrNavigationSourceNotFound", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatalf("pre-claim fault touched the claim store: claims=%d finalizes=%d", len(repo.claimInputs), len(repo.finalizeCalls))
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionRejected ||
		repo.pairCalls[0].rec.ErrorCode != "navigation_source_error" {
		t.Fatalf("pairs = %+v, want one rejected navigation_source_error NULL pair", repo.pairCalls)
	}
	if strings.Contains(repo.pairCalls[0].rec.ErrorMessage, "sensitive") {
		t.Fatalf("inspector detail leaked into evidence: %q", repo.pairCalls[0].rec.ErrorMessage)
	}

	// A canceled inspector read is a failed terminal NULL-key pair with a
	// bare sentinel — not a governance rejection.
	svc2, repo2, _, inspector2 := navTestScaffold(t)
	inspector2.err = context.Canceled
	if _, err := svc2.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest()); !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("canceled inspector read = %v, want bare ErrQueryBackendFailure", err)
	}
	if len(repo2.pairCalls) != 1 || repo2.pairCalls[0].rec.ErrorCode != "query_canceled" {
		t.Fatalf("canceled read pair = %+v, want failed/query_canceled", repo2.pairCalls)
	}
}

func TestMySQLNavKeyed_PreclaimReadFailureReturnsBareSentinel(t *testing.T) {
	// WHY: the pre-claim inspector read is preparation — its terminal
	// outcome returns the BARE canonical sentinel (no wrapped safe message
	// on the chain) while the evidence keeps the fixed classification.
	// Wrapped or joined cancellation detail never escapes.
	markers := []string{"SENSITIVE_INSPECTOR_DETAIL"}
	for _, tc := range []struct {
		name       string
		inspectErr error
		wantErr    error
		wantStatus model.QueryExecutionStatus
		wantCode   string
	}{
		{"joined canceled", errors.Join(context.Canceled, errors.New(markers[0])),
			ErrQueryBackendFailure, model.QueryExecutionFailed, "query_canceled"},
		{"joined deadline", errors.Join(context.DeadlineExceeded, errors.New(markers[0])),
			ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout"},
		{"wrapped deadline", fmt.Errorf("inspector read: %w", context.DeadlineExceeded),
			ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, executor, inspector := navTestScaffold(t)
			inspector.err = tc.inspectErr
			req := validNavRequest()
			req.ClientExecutionID = "nav-prep-1"

			_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("inspector read = %v, want %v", err, tc.wantErr)
			}
			// The returned chain is exactly the bare sentinel — one node,
			// no safe message wrap, no raw detail.
			tree := errorTree(err)
			if len(tree) != 1 {
				t.Fatalf("returned error chain = %d nodes, want the bare sentinel: %v", len(tree), err)
			}
			for _, m := range markers {
				if strings.Contains(err.Error(), m) {
					t.Fatalf("returned error leaked raw detail %q: %v", m, err)
				}
			}
			if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.gotNavInput != nil {
				t.Fatalf("pre-claim fault reached claim/finalize/executor: %d %d %v",
					len(repo.claimInputs), len(repo.finalizeCalls), executor.gotNavInput)
			}
			if len(repo.pairCalls) != 1 {
				t.Fatalf("pairs = %d, want exactly one NULL-key pair", len(repo.pairCalls))
			}
			rec := repo.pairCalls[0].rec
			if rec.Status != tc.wantStatus || rec.ErrorCode != tc.wantCode || rec.ClientExecutionID != nil {
				t.Fatalf("pair = %+v, want %v/%s NULL-key", rec, tc.wantStatus, tc.wantCode)
			}
		})
	}
}

func TestMySQLNavKeyed_NilProviderDetailIsControlledRejection(t *testing.T) {
	// WHY: a nil metadata snapshot with a nil error is a provider contract
	// violation — the service fails closed with the controlled source
	// error as one NULL-key rejected pair, never a nil dereference and
	// never fabricated relation metadata.
	svc, repo, executor, inspector := navTestScaffold(t)
	inspector.nilDetail = true
	req := validNavRequest()
	req.ClientExecutionID = "nav-nil-1"

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("nil provider detail = %v, want ErrNavigationSourceNotFound", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.gotNavInput != nil {
		t.Fatalf("nil detail reached claim/finalize/executor: claims=%d finalizes=%d executor=%v",
			len(repo.claimInputs), len(repo.finalizeCalls), executor.gotNavInput)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairs = %d, want exactly one NULL-key rejected pair", len(repo.pairCalls))
	}
	rec := repo.pairCalls[0].rec
	if rec.Status != model.QueryExecutionRejected || rec.ErrorCode != "navigation_source_error" ||
		rec.ClientExecutionID != nil {
		t.Fatalf("pair = %+v, want rejected navigation_source_error NULL-key", rec)
	}
	if strings.Contains(rec.ErrorMessage, "nil") {
		t.Fatalf("provider detail leaked into evidence: %q", rec.ErrorMessage)
	}
}

func TestMySQLNavKeyed_CapabilityMutationCannotArmOrDisarm(t *testing.T) {
	// WHY: the locator values and capabilities are cloned at request entry —
	// mutating the caller's capability slice at the claim seam must neither
	// unlock an incapable request nor disarm a capable one.
	t.Run("armed caller slice cannot unlock an incapable request", func(t *testing.T) {
		svc, repo, executor, _ := navTestScaffold(t)
		executor.rawResult = true
		executor.result = truncatedResult()
		req := validNavRequest()
		req.ClientExecutionID = "nav-cap-arm"
		caps := []string{"unknown-cap"}
		req.Capabilities = caps
		repo.claimHook = func() { caps[0] = CapabilityCellTruncated }
		_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
		if !errors.Is(err, ErrResultContractUpgradeRequired) {
			t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
		}
	})
	t.Run("disarmed caller slice still delivers to a capable request", func(t *testing.T) {
		svc, repo, executor, _ := navTestScaffold(t)
		executor.rawResult = true
		executor.result = truncatedResult()
		req := validNavRequest()
		req.ClientExecutionID = "nav-cap-disarm"
		caps := []string{CapabilityCellTruncated}
		req.Capabilities = caps
		repo.claimHook = func() { caps[0] = "unknown-cap" }
		resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
		if err != nil {
			t.Fatalf("capable request must still deliver after caller mutation: %v", err)
		}
		if len(resp.Rows) != 1 {
			t.Fatalf("truncated page must deliver to the declared capability, rows=%v", resp.Rows)
		}
	})
}

func TestMySQLNavKeyed_ProviderMutationAtClaimSeam(t *testing.T) {
	// WHY: the FK summary and locator values are owned before admission —
	// mutating the provider's column slice or the caller's values at the
	// claim seam can never reach the digest or the bound SQL.
	svc, repo, executor, inspector := navTestScaffold(t)
	req := validNavRequest()
	req.ClientExecutionID = "nav-key-4"
	wantDigest := svc.mysqlNavigationDigest("orders_db", "order_items",
		&FKSummary{Name: "fk_order_items_order", Columns: []FKColumn{
			{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"},
		}}, []string{"42"}, 100)
	repo.claimHook = func() {
		inspector.detail.ForeignKeys[0].Columns[0].ReferencedTable = "poisoned"
		req.LocalValues[0] = "999"
	}
	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("keyed navigation: %v", err)
	}
	if repo.claimInputs[0].RequestDigest != wantDigest {
		t.Fatalf("claim digest drifted under seam mutation: %s != %s", repo.claimInputs[0].RequestDigest, wantDigest)
	}
	if executor.gotNavInput == nil || !strings.Contains(executor.gotNavInput.Statement, "`orders`") ||
		strings.Contains(executor.gotNavInput.Statement, "poisoned") {
		t.Fatalf("bound SQL drifted under provider mutation: %+v", executor.gotNavInput)
	}
	if len(executor.gotNavInput.Values) != 1 || executor.gotNavInput.Values[0] != "42" {
		t.Fatalf("bound values drifted under caller mutation: %+v", executor.gotNavInput.Values)
	}
	if resp.ReferencedObject != "orders" {
		t.Fatalf("response relation metadata drifted: %q", resp.ReferencedObject)
	}
}

// --- PostgreSQL profile stays closed on all three entries ---

func TestMySQLKeyed_PostgreSQLProfileDeniedAllEntries(t *testing.T) {
	// WHY: a configured postgresql target is denied at engine resolution for
	// every governed entry — controlled refusal, zero claims, zero executor
	// calls, and each denial writes exactly one NULL-key rejected pair.
	pg := mysqlTarget("Staging")
	pg.ConnectionContext.Engine = "postgresql"

	build := func() (*QueryExecutionService, *fakeExecRepo, *fakeExecutor) {
		repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}}
		executor := &fakeExecutor{}
		svc := NewQueryExecutionService(
			fakeTargetRepo{targets: []model.QueryTarget{pg}},
			repo, &fakeResolver{dsn: testResolverDSN}, executor,
			NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
			&fakeClock{t: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)},
			&fakeNavSchemaInspector{}, &fakeDisclosureService{},
		)
		svc.WithTemplateExecution(&fakeSavedStatementReader{getResp: model.QuerySavedStatement{}}, NewTemplateStatementCompiler())
		return svc, repo, executor
	}

	identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: 1}
	svc, repo, executor := build()
	if _, err := svc.Execute(context.Background(), identity, 9001, keyedRequest("pg-key-1")); !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("pg ordinary = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.queryCalls != 0 {
		t.Fatalf("pg ordinary touched claims/executor: %d %d %d", len(repo.claimInputs), len(repo.finalizeCalls), executor.queryCalls)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("pg ordinary must write one NULL-key rejected pair: %+v", repo.pairCalls)
	}

	svc, repo, executor = build()
	req := templateExecuteRequest(nil)
	req.ClientExecutionID = "pg-key-2"
	if _, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, req); !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("pg saved = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.templateCalls != 0 {
		t.Fatalf("pg saved touched claims/executor: %d %d %d", len(repo.claimInputs), len(repo.finalizeCalls), executor.templateCalls)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("pg saved must write one NULL-key rejected pair: %+v", repo.pairCalls)
	}

	svc, repo, executor = build()
	navReq := validNavRequest()
	navReq.ClientExecutionID = "pg-key-3"
	if _, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, navReq); !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("pg navigation = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || executor.gotNavInput != nil {
		t.Fatalf("pg navigation touched claims/executor: %d %d", len(repo.claimInputs), len(repo.finalizeCalls))
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("pg navigation must write one NULL-key rejected pair: %+v", repo.pairCalls)
	}
}

// --- Frozen saved/navigation digest goldens (lead-authored vectors) ---

func TestMySQLSavedDigest_Goldens(t *testing.T) {
	// WHY: the canonical v1 wire digest is a frozen contract — literal
	// hashes pin the typed-value assembler so a kind-tag, name-order, or
	// scope bug cannot silently re-key every saved claim.
	svc, _, _, _ := executionTestScaffold(t)
	defs := []TemplateParameterDefinition{
		{Name: "tag", Type: TemplateParameterString},
		{Name: "minimum", Type: TemplateParameterInteger},
		{Name: "amount", Type: TemplateParameterDecimal},
		{Name: "enabled", Type: TemplateParameterBoolean},
	}
	values := map[string]any{"tag": "ok", "minimum": int64(-1), "amount": "3.1400", "enabled": false}
	if got := svc.mysqlSavedDigest(19, defs, values, 0, 0, 100); got != "6767e6d67543f4cf4d5bcf7f9fe61ddfbd61e4709ba54f059bbe7bf10ca6f08e" {
		t.Fatalf("saved_typed_unpaged digest = %s", got)
	}
	if got := svc.mysqlSavedDigest(19, defs, values, 2, 10, 100); got != "93f70fbd94104db53d689ed6d4382f11563cf269a83c02f1f8fd1006d1f6d114" {
		t.Fatalf("saved_paged digest = %s", got)
	}
	strDigest := svc.mysqlSavedDigest(19,
		[]TemplateParameterDefinition{{Name: "n", Type: TemplateParameterString}}, map[string]any{"n": "5"}, 0, 0, 100)
	intDigest := svc.mysqlSavedDigest(19,
		[]TemplateParameterDefinition{{Name: "n", Type: TemplateParameterInteger}}, map[string]any{"n": int64(5)}, 0, 0, 100)
	if strDigest != "b7b8b4db27788bc37361241af99632557ecb6a04356879f3c36ea38d233f4d68" {
		t.Fatalf("saved_string_5 digest = %s", strDigest)
	}
	if intDigest != "23d3f6b3c86ba927d5b4197c58fedd39b587da4ecc4a1143228c4c419133daca" {
		t.Fatalf("saved_integer_5 digest = %s", intDigest)
	}
	if strDigest == intDigest {
		t.Fatal("string \"5\" and integer 5 must digest differently — the declared kind tag is part of identity")
	}
	// Map/definition order never affects the fingerprint — names sort inside.
	reordered := []TemplateParameterDefinition{defs[3], defs[0], defs[2], defs[1]}
	if a, b := svc.mysqlSavedDigest(19, defs, values, 0, 0, 100),
		svc.mysqlSavedDigest(19, reordered, values, 0, 0, 100); a != b {
		t.Fatalf("definition ordering changed the digest: %s vs %s", a, b)
	}
}

func TestMySQLNavigationDigest_Goldens(t *testing.T) {
	// WHY: the canonical v1 wire digest is a frozen contract — literal
	// hashes pin source database, ordered column group, and ordered locator
	// values so an ordering or scope bug cannot silently re-key every nav
	// claim.
	svc, _, _, _ := executionTestScaffold(t)
	fk := &FKSummary{Name: "fk_customer", Columns: []FKColumn{
		{Column: "customer_id", ReferencedSchema: "shop", ReferencedTable: "customers", ReferencedColumn: "id"},
		{Column: "region", ReferencedSchema: "shop", ReferencedTable: "customers", ReferencedColumn: "region"},
	}}
	if got := svc.mysqlNavigationDigest("shop", "orders", fk, []string{"7", "west"}, 100); got != "5167caf811690706fa483b4d97a341034f47e3f4449a20f9adb1f899f9fcc0c7" {
		t.Fatalf("navigation_unpaged digest = %s", got)
	}
	if got := svc.mysqlNavigationDigest("shop2", "orders", fk, []string{"7", "west"}, 100); got != "74b3b8a5b05b82caccd362bd86d0445272e245702f72658fa42384b5f5cbd1ec" {
		t.Fatalf("navigation_source_database digest = %s", got)
	}
	if got := svc.mysqlNavigationDigest("shop", "orders", fk, []string{"west", "7"}, 100); got != "9140ac3c9de731b471844b97bf2dbfa697135e58baa8f921f30da1fddb210d9e" {
		t.Fatalf("navigation_value_order digest = %s", got)
	}
	swapped := &FKSummary{Name: "fk_customer", Columns: []FKColumn{fk.Columns[1], fk.Columns[0]}}
	if got := svc.mysqlNavigationDigest("shop", "orders", swapped, []string{"7", "west"}, 100); got != "0b00f7f697ffbcd971ecfe00bd73233c9c8264bb0a7c71c56e591fc3a020c3c9" {
		t.Fatalf("navigation_column_order digest = %s", got)
	}
}
