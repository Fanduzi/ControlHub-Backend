// Package service tests the keyed PostgreSQL orchestration contract.
// input: testing, time, context, errors, os, internal/model fixtures, internal/pgsql warm-up
// output: TestExecutePGClaimed_* — admission order, evidence-pair boundary,
// duplicate/conflict classification, keyed finalize, and deriveClaimViewStatus
// pos: T10-C claim-orchestration unit coverage for issue #118 — every test
// encodes the frozen G9 order; no real PostgreSQL or MySQL is involved, the
// governed chain either rejects at the guard or fails at the DSN endpoint
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// TestMain warms the wazero-backed pg_query compiler once per test binary.
// The first GuardPG parse compiles the WASM module — a multi-second,
// process-wide cost that runs inside the frozen attempt budget when a test
// reaches the governed chain. Under -race that compile can consume the whole
// budget and turn a deterministic driver failure into a timeout; warming it
// here keeps the attempt deadline for what it governs.
func TestMain(m *testing.M) {
	if _, err := pgsql.GuardPG("select 1"); err != nil {
		panic("pg guard warm-up: " + err.Error())
	}
	os.Exit(m.Run())
}

// pgClaimFixture builds the smallest valid keyed-PG world: a PostgreSQL
// target bound to a local endpoint, an enabled postgresql credential row for
// database "appdb" with pinned default_schema "public", and a resolver whose
// DSN binds to the target. The DSN port 1 refuses instantly so the governed
// chain's driver-failure path runs without a real server.
func pgClaimFixture() (model.QueryTarget, model.QueryCredentialMetadata, string) {
	target := model.QueryTarget{
		ResourceID: 9002,
		ConnectionContext: model.QueryTargetConnectionContext{
			Engine:      "postgresql",
			Environment: "staging",
			Host:        "127.0.0.1",
			Port:        1,
		},
	}
	cred := model.QueryCredentialMetadata{
		ResourceID:        9002,
		Engine:            "postgresql",
		DatabaseName:      "appdb",
		DefaultSchema:     "public",
		CredentialRef:     "ORDER_PG_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	return target, cred, "postgres://u:p@127.0.0.1:1/appdb"
}

// newClaimedService wires a keyed-PG service with the given repo so tests can
// inject claim/finalize outcomes; the disclosure reader is present so the
// orchestration precondition holds.
func newClaimedService(repo *fakeExecRepo, resolver *fakeResolver) *QueryExecutionService {
	target, _, _ := pgClaimFixture()
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{target}},
		repo, resolver, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)},
		nil, &fakeDisclosureService{},
	)
	return svc.WithPGClaimOrchestration(&fakePGPolicyReader{})
}

func claimedPGRepo(t *testing.T) *fakeExecRepo {
	t.Helper()
	_, cred, _ := pgClaimFixture()
	return &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9002: cred}}
}

func validClaimedRequest() PGClaimedExecuteRequest {
	return PGClaimedExecuteRequest{
		ClientExecutionID: "req-1",
		Database:          "appdb",
		Statement:         "select id from orders",
	}
}

// Request-shape failures happen before access work: no claim, no pair.
func TestExecutePGClaimed_RequestShapeFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*PGClaimedExecuteRequest){
		"missing key":      func(r *PGClaimedExecuteRequest) { r.ClientExecutionID = "" },
		"oversized key":    func(r *PGClaimedExecuteRequest) { r.ClientExecutionID = strings.Repeat("x", 65) },
		"empty statement":  func(r *PGClaimedExecuteRequest) { r.Statement = "   " },
		"half pagination":  func(r *PGClaimedExecuteRequest) { r.Page = 2 },
		"negative maxRows": func(r *PGClaimedExecuteRequest) { r.MaxRows = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
			req := validClaimedRequest()
			mutate(&req)
			_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
			if !errors.Is(err, ErrQueryValidationFailed) {
				t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
			}
			if len(repo.claimInputs) != 0 {
				t.Fatalf("claim attempted on shape failure: %+v", repo.claimInputs)
			}
			if len(repo.pairCalls) != 0 {
				t.Fatalf("evidence pair written on shape failure")
			}
		})
	}
}

// Invalid trusted identity fails before everything else.
func TestExecutePGClaimed_InvalidIdentity(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), model.QueryExecutionIdentity{}, 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("claim or evidence pair attempted on invalid identity")
	}
}

// Empty database names no connection identity: shape failure, no pair.
func TestExecutePGClaimed_EmptyDatabase(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	req.Database = ""
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("empty database occupied the key or wrote evidence")
	}
}

// Unknown target: nothing to attribute — no claim, no pair.
func TestExecutePGClaimed_TargetNotFound(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 4242, validClaimedRequest())
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("error = %v, want ErrQueryTargetNotFound", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("unknown target occupied the key or wrote evidence")
	}
}

// A resolved target with refused access writes exactly one NULL-key rejected
// pair and leaves the key unoccupied.
func TestExecutePGClaimed_AccessDeniedNullKeyPair(t *testing.T) {
	t.Parallel()
	_, cred, dsn := pgClaimFixture()
	cred.Enabled = false
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9002: cred}}
	svc := newClaimedService(repo, &fakeResolver{dsn: dsn})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("access denial occupied the key")
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want 1", len(repo.pairCalls))
	}
	rec := repo.pairCalls[0].rec
	if rec.Status != model.QueryExecutionRejected || rec.ErrorCode != "query_not_allowed" {
		t.Fatalf("rejected record = %+v", rec)
	}
	if rec.ClientExecutionID != nil {
		t.Fatalf("NULL-key pair carried client key %q", *rec.ClientExecutionID)
	}
	if rec.ActorUserID != 7 {
		t.Fatalf("actor = %d, want user 7", rec.ActorUserID)
	}
}

// Same key + same digest → already-exists; nothing executes, nothing written.
func TestExecutePGClaimed_ClaimExistsClassification(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.claimErr = model.ErrQueryExecutionClaimExists
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryExecutionAlreadyExists) {
		t.Fatalf("error = %v, want ErrQueryExecutionAlreadyExists", err)
	}
	if len(repo.pairCalls) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatal("duplicate admission wrote evidence or finalized")
	}
}

// Same key + different digest → conflict; nothing executes, nothing written.
func TestExecutePGClaimed_ClaimConflictClassification(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.claimErr = model.ErrQueryExecutionClaimConflict
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryExecutionConflict) {
		t.Fatalf("error = %v, want ErrQueryExecutionConflict", err)
	}
	if len(repo.pairCalls) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatal("conflicted admission wrote evidence or finalized")
	}
}

// A non-duplicate claim-store failure writes one NULL-key failed pair — the
// client key never masquerades as the occupant.
func TestExecutePGClaimed_ClaimStoreFailure(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.claimErr = errors.New("connection reset")
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want 1", len(repo.pairCalls))
	}
	rec := repo.pairCalls[0].rec
	if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_backend_error" {
		t.Fatalf("failed record = %+v", rec)
	}
	if rec.ClientExecutionID != nil {
		t.Fatal("claim-failure pair carried the client key")
	}
}

// Guard rejection after a successful claim: exactly one keyed finalize with
// rejected status and the client key on the record — never a NULL-key pair.
func TestExecutePGClaimed_GuardRejectFinalizes(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	req.Statement = "delete from orders"
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
	if err == nil {
		t.Fatal("guard rejection must surface an error")
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claims = %d, want 1", len(repo.claimInputs))
	}
	claim := repo.claimInputs[0]
	if claim.AttemptTimeoutMs != defaultQueryTimeout.Milliseconds() {
		t.Fatalf("attempt timeout = %d, want %d", claim.AttemptTimeoutMs, defaultQueryTimeout.Milliseconds())
	}
	if claim.RequestDigest != DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "appdb", Schema: "public", Statement: "delete from orders",
		Page: 1, PageSize: pgClaimDefaultPageSize, MaxRows: pgClaimDefaultMaxRows,
	}) {
		t.Fatal("claim digest does not match resolved defaults")
	}
	if len(repo.pairCalls) != 0 {
		t.Fatal("post-claim rejection wrote a NULL-key pair")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalize calls = %d, want exactly 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.Status != model.QueryExecutionRejected {
		t.Fatalf("finalized status = %v, want rejected", rec.Status)
	}
	if rec.ClientExecutionID == nil || *rec.ClientExecutionID != "req-1" {
		t.Fatal("keyed finalize lost the client key")
	}
	if rec.StatementDigest != claim.RequestDigest {
		t.Fatal("terminal statement digest diverged from claim digest")
	}
}

// Driver failure after claim: keyed finalize with failed status; evidence
// carries the executor's classification, never fabricated scalars.
func TestExecutePGClaimed_DriverFailureFinalizes(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if err == nil {
		t.Fatal("connect failure must surface an error")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalize calls = %d, want 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.Status != model.QueryExecutionFailed {
		t.Fatalf("finalized status = %v, want failed", rec.Status)
	}
	if rec.ErrorCode == "" || rec.ErrorMessage == "" {
		t.Fatal("failed finalize lost the fixed evidence code")
	}
}

// A machine actor's keyed finalize lands on actor_machine_principal_id —
// user:7 and machine:7 never collapse into one identity.
func TestExecutePGClaimed_MachineIdentityFinalize(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 7}
	_, err := svc.ExecutePGClaimed(context.Background(), identity, 9002, validClaimedRequest())
	if err == nil {
		t.Fatal("connect failure must surface an error")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalize calls = %d, want 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.ActorUserID != 0 || rec.ActorMachinePrincipalID != 7 {
		t.Fatalf("machine actor projected wrongly: user=%d machine=%d", rec.ActorUserID, rec.ActorMachinePrincipalID)
	}
	if rec.FullStatement != "" {
		t.Fatal("machine actor must never carry a saved full statement")
	}
}

// Finalize failure: no result, safe persistence error, no compensating pair.
func TestExecutePGClaimed_FinalizeFailure(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.finalizeErr = errors.New("deadlock")
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("error = %v, want errPersistAttempt", err)
	}
	if len(repo.pairCalls) != 0 {
		t.Fatal("finalize failure wrote a compensating NULL-key pair")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalize calls = %d, want 1", len(repo.finalizeCalls))
	}
}

// An explicit schema outside the pinned connection identity is refused as
// access denial — the key stays unoccupied.
func TestExecutePGClaimed_SchemaOutsidePinnedIdentity(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	req.Schema = "other"
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("unpinned schema occupied the key")
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want 1", len(repo.pairCalls))
	}
}

// The derived view status is a function of claimed_at + recorded budget:
// unexpired → running, expired → unknown, absent budget → running forever.
func TestDeriveClaimViewStatus(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	claimed := now.Add(-10 * time.Second)
	if got := deriveClaimViewStatus(claimed, 60_000, now); got != model.QueryExecutionViewRunning {
		t.Fatalf("unexpired claim = %v, want running", got)
	}
	stale := now.Add(-2 * time.Minute)
	if got := deriveClaimViewStatus(stale, 5_000, now); got != model.QueryExecutionViewUnknown {
		t.Fatalf("expired claim = %v, want unknown", got)
	}
	// claimed_at + timeout + 60s is the boundary: exactly at it is expired.
	at := now.Add(-(5*time.Second + 60*time.Second))
	if got := deriveClaimViewStatus(at, 5_000, now); got != model.QueryExecutionViewUnknown {
		t.Fatalf("boundary claim = %v, want unknown", got)
	}
	// NULL budget (pre-T10-C row) can never expire.
	if got := deriveClaimViewStatus(stale, 0, now); got != model.QueryExecutionViewRunning {
		t.Fatalf("legacy claim = %v, want running", got)
	}
}

func mustDSN(t *testing.T) string {
	t.Helper()
	_, _, dsn := pgClaimFixture()
	return dsn
}

func timePtr(t time.Time) *time.Time { return &t }
func strPtr(s string) *string        { return &s }

// Unlinked claims surface as derived entries: unexpired → running, expired →
// unknown, and a NULL budget (pre-T10-C) can never expire. The read writes
// nothing — no claim, no finalize, no evidence pair.
func TestListHistory_ClaimDerivedStates(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	now := time.Date(2026, 6, 22, 9, 0, 0, 0, time.UTC)
	svc.clock = &fakeClock{t: now}
	repo.claimItems = []model.QueryExecutionListItem{
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
			ClientExecutionID: strPtr("fresh"), ClaimedAt: timePtr(now.Add(-10 * time.Second)),
			ClaimAttemptTimeoutMs: 60_000, DatabaseName: "appdb", SchemaName: "public",
		},
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
			ClientExecutionID: strPtr("stale"), ClaimedAt: timePtr(now.Add(-2 * time.Minute)),
			ClaimAttemptTimeoutMs: 5_000, DatabaseName: "appdb", SchemaName: "public",
		},
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
			ClientExecutionID: strPtr("legacy"), ClaimedAt: timePtr(now.Add(-2 * time.Minute)),
			ClaimAttemptTimeoutMs: 0, DatabaseName: "appdb", SchemaName: "public",
		},
	}
	page, err := svc.ListHistory(context.Background(), userExecutionIdentity(7), "user", 9002, model.QueryExecutionListQuery{TargetResourceID: 9002})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	got := map[string]model.QueryExecutionViewStatus{}
	for _, it := range page.Items {
		if it.Kind != model.QueryExecutionListItemClaim {
			t.Fatalf("unexpected execution item %+v", it)
		}
		if it.ID != nil {
			t.Fatal("claim-derived item fabricated an execution id")
		}
		got[*it.ClientExecutionID] = it.Status
	}
	if got["fresh"] != model.QueryExecutionViewRunning {
		t.Fatalf("fresh claim = %v, want running", got["fresh"])
	}
	if got["stale"] != model.QueryExecutionViewUnknown {
		t.Fatalf("stale claim = %v, want unknown", got["stale"])
	}
	if got["legacy"] != model.QueryExecutionViewRunning {
		t.Fatalf("legacy claim = %v, want running", got["legacy"])
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("a read wrote claim, finalize, or evidence rows")
	}
}

// A flagged broken link is an evidence fault: the read fails closed rather
// than rendering a fabricated running entry.
func TestListHistory_BrokenClaimLinkFailsClosed(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	repo.claimItems = []model.QueryExecutionListItem{{
		Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
		ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
		ClientExecutionID: strPtr("broken"), ClaimedAt: timePtr(time.Now()),
		ClaimLinkBroken: true, DatabaseName: "appdb",
	}}
	_, err := svc.ListHistory(context.Background(), userExecutionIdentity(7), "user", 9002, model.QueryExecutionListQuery{TargetResourceID: 9002})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
}

// A stored-status filter can only match terminal rows — claim-derived entries
// are excluded, never surfaced as a matching status.
func TestListHistory_StatusFilterExcludesClaims(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	success := model.QueryExecutionSuccess
	repo.claimItems = []model.QueryExecutionListItem{{
		Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
		ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
		ClientExecutionID: strPtr("c1"), ClaimedAt: timePtr(time.Now()),
		ClaimAttemptTimeoutMs: 60_000, DatabaseName: "appdb",
	}}
	page, err := svc.ListHistory(context.Background(), userExecutionIdentity(7), "user", 9002, model.QueryExecutionListQuery{
		TargetResourceID: 9002, Status: &success,
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("status filter leaked %d claim-derived items", len(page.Items))
	}
}

// The clientExecutionId filter narrows the mixed set to the keyed row.
func TestListHistory_ClientExecutionIDFilter(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	repo.claimItems = []model.QueryExecutionListItem{
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
			ClientExecutionID: strPtr("wanted"), ClaimedAt: timePtr(time.Now()),
			ClaimAttemptTimeoutMs: 60_000, DatabaseName: "appdb",
		},
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
			ClientExecutionID: strPtr("other"), ClaimedAt: timePtr(time.Now()),
			ClaimAttemptTimeoutMs: 60_000, DatabaseName: "appdb",
		},
	}
	page, err := svc.ListHistory(context.Background(), userExecutionIdentity(7), "user", 9002, model.QueryExecutionListQuery{
		TargetResourceID: 9002, ClientExecutionID: "wanted",
	})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 1 || *page.Items[0].ClientExecutionID != "wanted" {
		t.Fatalf("filter returned %+v", page.Items)
	}
}

// Non-admin reads see only their own typed actor's claims — user:7 and
// machine:7 never mix.
func TestListHistory_TypedActorIsolation(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	repo.claimItems = []model.QueryExecutionListItem{
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser},
			ClientExecutionID: strPtr("user-claim"), ClaimedAt: timePtr(time.Now()),
			ClaimAttemptTimeoutMs: 60_000, DatabaseName: "appdb",
		},
		{
			Kind: model.QueryExecutionListItemClaim, TargetResourceID: 9002,
			ActorMachinePrincipalID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorMachine},
			ClientExecutionID: strPtr("machine-claim"), ClaimedAt: timePtr(time.Now()),
			ClaimAttemptTimeoutMs: 60_000, DatabaseName: "appdb",
		},
	}
	// user:7 sees only the user claim.
	page, err := svc.ListHistory(context.Background(), userExecutionIdentity(7), "user", 9002, model.QueryExecutionListQuery{TargetResourceID: 9002})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 1 || *page.Items[0].ClientExecutionID != "user-claim" {
		t.Fatalf("user scope leaked items: %+v", page.Items)
	}
	// machine:7 sees only the machine claim — same numeric id, different actor.
	machine := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 7}
	page, err = svc.ListHistory(context.Background(), machine, "machine", 9002, model.QueryExecutionListQuery{TargetResourceID: 9002})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(page.Items) != 1 || *page.Items[0].ClientExecutionID != "machine-claim" {
		t.Fatalf("machine scope leaked items: %+v", page.Items)
	}
}
