// Package service tests the keyed PostgreSQL orchestration contract.
// input: testing, time, context, errors, os, internal/model fixtures, internal/pgsql warm-up
// output: TestExecutePGClaimed_* — admission order, evidence-pair boundary,
// duplicate/conflict classification, keyed finalize, and deriveClaimViewStatus
// pos: T10-C claim-orchestration unit coverage for issue #118 — every test
// encodes the frozen G9 order; no real PostgreSQL or MySQL is involved, the
// governed chain either rejects at the guard or fails at the DSN endpoint;
// T8-C-R1 adds the delivery-snapshot ownership proofs (bidirectional
// isolation plus finalize-boundary mutation via the fake's finalizeHook)
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"math"
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
	return newClaimedServiceOn(repo, resolver, fakeTargetRepo{targets: []model.QueryTarget{target}})
}

// newClaimedServiceOn wires the same keyed-PG service over a caller-supplied
// target repository so tests can count access-resolution work (shape failures
// must never reach it).
func newClaimedServiceOn(repo *fakeExecRepo, resolver *fakeResolver, targets QueryTargetRepository) *QueryExecutionService {
	svc := NewQueryExecutionService(
		targets,
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

// Request-shape failures happen before access work: no target lookup, no
// credential resolution, no claim, no evidence pair, no user SQL.
func TestExecutePGClaimed_RequestShapeFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*PGClaimedExecuteRequest){
		"missing key":     func(r *PGClaimedExecuteRequest) { r.ClientExecutionID = "" },
		"oversized key":   func(r *PGClaimedExecuteRequest) { r.ClientExecutionID = strings.Repeat("x", 65) },
		"key with NUL":    func(r *PGClaimedExecuteRequest) { r.ClientExecutionID = "a\x00b" },
		"empty statement": func(r *PGClaimedExecuteRequest) { r.Statement = "   " },
		"oversized statement": func(r *PGClaimedExecuteRequest) {
			r.Statement = strings.Repeat("s", model.MaxStatementLength+1)
		},
		"oversized unicode statement": func(r *PGClaimedExecuteRequest) {
			// The bound counts runes: 65537 multi-byte characters fail the
			// same check even though the byte count is far larger.
			r.Statement = strings.Repeat("é", model.MaxStatementLength+1)
		},
		"missing database": func(r *PGClaimedExecuteRequest) { r.Database = "" },
		"oversized database": func(r *PGClaimedExecuteRequest) {
			r.Database = strings.Repeat("d", model.MaxIdentifierLength+1)
		},
		"database with NUL": func(r *PGClaimedExecuteRequest) { r.Database = "app\x00db" },
		"oversized schema": func(r *PGClaimedExecuteRequest) {
			r.Schema = strings.Repeat("s", model.MaxIdentifierLength+1)
		},
		"schema with NUL":      func(r *PGClaimedExecuteRequest) { r.Schema = "pub\x00lic" },
		"half pagination":      func(r *PGClaimedExecuteRequest) { r.Page = 2 },
		"disallowed page size": func(r *PGClaimedExecuteRequest) { r.Page, r.PageSize = 1, 11 },
		"negative maxRows":     func(r *PGClaimedExecuteRequest) { r.MaxRows = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			target, _, _ := pgClaimFixture()
			var targetQueries []model.QueryTargetListQuery
			resolver := &fakeResolver{dsn: mustDSN(t)}
			svc := newClaimedServiceOn(repo, resolver, fakeTargetRepo{targets: []model.QueryTarget{target}, queries: &targetQueries})
			req := validClaimedRequest()
			mutate(&req)
			_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
			if !errors.Is(err, ErrQueryValidationFailed) {
				t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
			}
			if len(targetQueries) != 0 {
				t.Fatalf("shape failure reached target access: %+v", targetQueries)
			}
			if resolver.calls != 0 {
				t.Fatalf("shape failure resolved credentials %d times", resolver.calls)
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

// The 65536-rune boundary statement passes shape and reaches the keyed chain
// — claim admitted, exactly one keyed finalize. Trailing spaces, case, and
// Unicode in names stay verbatim: only shape is checked.
func TestExecutePGClaimed_ShapeBoundaryAccepted(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	req.Statement = "SELECT " + strings.Repeat("é", model.MaxStatementLength-7)
	req.Schema = "MiXeD Case Schema "
	if _, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req); err == nil {
		t.Fatal("boundary statement must terminate through the keyed chain")
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claimInputs = %d, want 1 — the boundary shape must pass", len(repo.claimInputs))
	}
	if repo.claimInputs[0].SchemaName != "MiXeD Case Schema " {
		t.Fatalf("schema = %q, want the explicit name verbatim (no trim/fold)", repo.claimInputs[0].SchemaName)
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalizeCalls = %d, want exactly one keyed finalize", len(repo.finalizeCalls))
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
// pair and leaves the key unoccupied. The normalized connection sentinels
// keep their own identity for the transport (R1-1): the transport publishes
// query_connection_disabled/not_found and dsn_binding_mismatch while the
// persisted record stays the generic query_not_allowed denial contract —
// the record's error code is deliberately not per-category so history stays
// uniform with every other access refusal.
func TestExecutePGClaimed_AccessDeniedNullKeyPair(t *testing.T) {
	t.Parallel()
	_, cred, dsn := pgClaimFixture()
	cred.Enabled = false
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9002: cred}}
	svc := newClaimedService(repo, &fakeResolver{dsn: dsn})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrSchemaConnectionDisabled) {
		t.Fatalf("error = %v, want ErrSchemaConnectionDisabled", err)
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

// Sentinel classification through the shared rejection exit (R1-1): the
// three normalized connection sentinels surface their own identity —
// verbatim, with no unwrap of driver text — while policy, secret, and
// unexpected internal refusals stay the generic ErrQueryNotAllowed denial.
// Every case writes the same single NULL-key query_not_allowed pair.
func TestExecutePGClaimed_AccessDenialSentinels(t *testing.T) {
	t.Parallel()
	target, cred, dsn := pgClaimFixture()
	prodTarget := target
	prodTarget.ConnectionContext.Environment = "production"

	cases := map[string]struct {
		cred    *model.QueryCredentialMetadata
		dsn     string
		resErr  error
		target  model.QueryTarget
		wantErr error
	}{
		"missing connection row": {wantErr: ErrSchemaConnectionNotFound, dsn: dsn},
		"disabled connection": {
			cred:    credPtr(cred, func(c *model.QueryCredentialMetadata) { c.Enabled = false }),
			dsn:     dsn,
			wantErr: ErrSchemaConnectionDisabled,
		},
		"dsn binding mismatch": {
			cred:    &cred,
			dsn:     "postgres://u:p@127.0.0.1:1/otherdb",
			wantErr: ErrSchemaBindingMismatch,
		},
		"environment policy blocked": {
			cred:    &cred,
			dsn:     dsn,
			target:  prodTarget,
			wantErr: ErrQueryNotAllowed,
		},
		"secret resolve failure": {
			cred:    &cred,
			resErr:  errors.New("vault unreachable"),
			wantErr: ErrQueryNotAllowed,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tgt := tc.target
			if tgt.ResourceID == 0 {
				tgt = target
			}
			repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{}}
			if tc.cred != nil {
				repo.credentials[9002] = *tc.cred
			}
			svc := newClaimedServiceOn(repo, &fakeResolver{dsn: tc.dsn, err: tc.resErr},
				fakeTargetRepo{targets: []model.QueryTarget{tgt}})
			_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if errors.Is(err, ErrSchemaConnectionNotFound) || errors.Is(err, ErrSchemaConnectionDisabled) ||
				errors.Is(err, ErrSchemaBindingMismatch) {
				if !errors.Is(err, tc.wantErr) || errors.Is(err, ErrQueryNotAllowed) {
					t.Fatalf("connection sentinel wrapped under not-allowed: %v", err)
				}
			}
			if len(repo.claimInputs) != 0 {
				t.Fatal("access denial occupied the key")
			}
			if len(repo.pairCalls) != 1 {
				t.Fatalf("pairCalls = %d, want 1", len(repo.pairCalls))
			}
			rec := repo.pairCalls[0].rec
			if rec.Status != model.QueryExecutionRejected || rec.ErrorCode != "query_not_allowed" ||
				rec.ClientExecutionID != nil {
				t.Fatalf("rejected record = %+v, want NULL-key query_not_allowed", rec)
			}
		})
	}
}

// A persistence failure during the rejection write still outranks the access
// verdict itself — the transport publishes 502, never the connection code.
func TestExecutePGClaimed_AccessDenialPersistFailure(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{}}
	repo.pairErr = errors.New("history db down")
	svc := newClaimedService(repo, &fakeResolver{dsn: "postgres://u:p@127.0.0.1:1/appdb"})
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (persist attempt outranks access verdict)", err)
	}
	if errors.Is(err, ErrSchemaConnectionNotFound) {
		t.Fatalf("persistence failure masked as access verdict: %v", err)
	}
}

func credPtr(c model.QueryCredentialMetadata, mutate func(*model.QueryCredentialMetadata)) *model.QueryCredentialMetadata {
	mutate(&c)
	return &c
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

// An explicit schema is no longer a connection-identity whitelist: it claims
// the key, runs the governed chain under the pinned explicit name, and lands
// exactly one keyed finalize (here the dead DSN surfaces as a keyed driver
// failure) — never a pre-claim NULL-key denial. An omitted schema still
// resolves the credential's default_schema into the same digest/claim scope.
func TestExecutePGClaimed_ExplicitSchemaClaimsAndFinalizes(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	req.Schema = "analytics"
	out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
	if err == nil {
		t.Fatalf("explicit schema with dead DSN = %+v, want classified failure", out)
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("explicit schema claimInputs = %d, want 1 — the key must be occupied", len(repo.claimInputs))
	}
	if repo.claimInputs[0].SchemaName != "analytics" {
		t.Fatalf("claim schema = %q, want the explicit name verbatim", repo.claimInputs[0].SchemaName)
	}
	if len(repo.pairCalls) != 0 {
		t.Fatalf("post-claim outcome wrote %d NULL-key pairs, want 0", len(repo.pairCalls))
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalizeCalls = %d, want exactly one keyed finalize", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.SchemaName != "analytics" || rec.ClientExecutionID == nil || *rec.ClientExecutionID != "req-1" {
		t.Fatalf("keyed record scope = %q/%v, want analytics/req-1", rec.SchemaName, rec.ClientExecutionID)
	}
}

// The omitted/explicit-default schema equivalence keeps its digest contract:
// explicit "public" under the same key is already-exists, not a second
// admission, while a different explicit schema conflicts.
func TestExecutePGClaimed_SchemaDefaultEquivalence(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	if _, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req); err == nil {
		t.Fatal("first execute must terminate (keyed finalize)")
	}
	explicit := validClaimedRequest()
	explicit.Schema = "public" // the credential's default_schema
	if _, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, explicit); !errors.Is(err, ErrQueryExecutionAlreadyExists) {
		t.Fatalf("explicit-default retry = %v, want already_exists", err)
	}
	different := validClaimedRequest()
	different.Schema = "analytics"
	if _, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, different); !errors.Is(err, ErrQueryExecutionConflict) {
		t.Fatalf("different-schema same-key = %v, want conflict", err)
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 {
		t.Fatalf("claims/finalizes = %d/%d, want 1/1 — no second admission", len(repo.claimInputs), len(repo.finalizeCalls))
	}
}

// The derived view status is a function of claimed_at + recorded budget:
// unexpired → running, expired → unknown. A missing or unusable budget is an
// evidence gap → unknown — never an invented expiry window, and never a
// fabricated "still running".
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
	// NULL budget (pre-T10-C row): evidence missing → unknown even when fresh.
	if got := deriveClaimViewStatus(claimed, 0, now); got != model.QueryExecutionViewUnknown {
		t.Fatalf("legacy claim = %v, want unknown", got)
	}
	// A corrupt budget — negative, or too large to form a duration — must
	// not overflow into a seemingly valid expiry window either way.
	if got := deriveClaimViewStatus(stale, -5, now); got != model.QueryExecutionViewUnknown {
		t.Fatalf("negative budget = %v, want unknown", got)
	}
	if got := deriveClaimViewStatus(claimed, math.MaxInt64, now); got != model.QueryExecutionViewUnknown {
		t.Fatalf("overflowing budget = %v, want unknown", got)
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
// unknown, and a NULL budget (pre-T10-C) derives unknown — the budget evidence
// is missing, not a proven expiry. The read writes nothing — no claim, no
// finalize, no evidence pair.
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
	if got["legacy"] != model.QueryExecutionViewUnknown {
		t.Fatalf("legacy claim = %v, want unknown", got["legacy"])
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

// ---------------------------------------------------------------------------
// R1-2: the history preview column never stores statement content. Post-claim
// records carry the lexical mask; pre-claim evidence pairs carry the fixed
// withheld marker. The owner-only full-statement contract is untouched.
// ---------------------------------------------------------------------------

func TestExecutePGClaimed_PreviewMasksOnFinalize(t *testing.T) {
	t.Parallel()
	stmt := `SELECT a FROM app.t WHERE a = 'SECRET-X1' AND b = E'pre\'SECRET-X2' AND c = $tag$SECRET-X3$tag$ -- SECRET-X4
	/* SECRET-X5 */ AND d = 424242`
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		return &PGDisclosedResult{Rows: [][]*string{}},
			&PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess, BackendPID: 77}, nil
	}
	req := validClaimedRequest()
	req.Statement = stmt
	_, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalizeCalls = %d, want 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	for _, marker := range []string{"SECRET-X1", "SECRET-X2", "SECRET-X3", "SECRET-X4", "SECRET-X5", "424242"} {
		if strings.Contains(rec.StatementPreview, marker) {
			t.Fatalf("history preview leaked %q: %q", marker, rec.StatementPreview)
		}
	}
	if !strings.Contains(rec.StatementPreview, "SELECT a FROM app.t") {
		t.Fatalf("preview lost statement shape: %q", rec.StatementPreview)
	}
	// The user+success owner contract still stores the verbatim statement —
	// preview masking is a separate, always-on boundary.
	if rec.FullStatement != stmt {
		t.Fatal("user success lost its owner-only full statement")
	}
	// Digest still runs on original bytes.
	claim := repo.claimInputs[0]
	want := DigestOrdinaryExecute(OrdinaryExecuteDigestInput{
		Database: "appdb", Schema: "public", Statement: stmt,
		Page: 1, PageSize: pgClaimDefaultPageSize, MaxRows: pgClaimDefaultMaxRows,
	})
	if claim.RequestDigest != want || rec.StatementDigest != want {
		t.Fatal("digest diverged from the original statement bytes")
	}
}

// A machine actor's successful attempt masks the preview AND saves no full
// statement — the owner-only contract never applies to machines.
func TestExecutePGClaimed_MachinePreviewNoFullStatement(t *testing.T) {
	t.Parallel()
	stmt := `SELECT 'SECRET-M1' FROM app.t`
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		return &PGDisclosedResult{Rows: [][]*string{}},
			&PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess}, nil
	}
	req := validClaimedRequest()
	req.Statement = stmt
	machine := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 7}
	_, err := svc.ExecutePGClaimed(context.Background(), machine, 9002, req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	rec := repo.finalizeCalls[0]
	if strings.Contains(rec.StatementPreview, "SECRET-M1") {
		t.Fatalf("machine preview leaked: %q", rec.StatementPreview)
	}
	if rec.FullStatement != "" {
		t.Fatal("machine actor acquired a full statement")
	}
}

// Every pre-claim evidence row carries the fixed withheld marker — no
// scanning of an unadmitted statement is ever needed.
func TestExecutePGClaimed_PreClaimPreviewWithheld(t *testing.T) {
	t.Parallel()
	// Access denial path.
	_, cred, dsn := pgClaimFixture()
	cred.Enabled = false
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9002: cred}}
	svc := newClaimedService(repo, &fakeResolver{dsn: dsn})
	req := validClaimedRequest()
	req.Statement = `SELECT 'SECRET-P1' FROM app.t`
	if _, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req); !errors.Is(err, ErrSchemaConnectionDisabled) {
		t.Fatalf("access denial = %v", err)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.StatementPreview != pgClaimedWithheldPreview {
		t.Fatalf("access-denial preview = %q, want withheld", repo.pairCalls[0].rec.StatementPreview)
	}
	// Claim-store failure path.
	repo2 := claimedPGRepo(t)
	repo2.claimErr = errors.New("store down")
	svc2 := newClaimedService(repo2, &fakeResolver{dsn: mustDSN(t)})
	if _, err := svc2.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req); !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("claim-store failure = %v", err)
	}
	if len(repo2.pairCalls) != 1 || repo2.pairCalls[0].rec.StatementPreview != pgClaimedWithheldPreview {
		t.Fatalf("claim-failure preview = %q, want withheld", repo2.pairCalls[0].rec.StatementPreview)
	}
}

// A guard-rejected statement is still masked in its keyed terminal row.
func TestExecutePGClaimed_RejectedPreviewMasked(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	req := validClaimedRequest()
	req.Statement = `DELETE FROM app.t WHERE a = 'SECRET-R1'`
	if _, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req); err == nil {
		t.Fatal("guard rejection must surface")
	}
	rec := repo.finalizeCalls[0]
	if rec.Status != model.QueryExecutionRejected {
		t.Fatalf("status = %v, want rejected", rec.Status)
	}
	if strings.Contains(rec.StatementPreview, "SECRET-R1") {
		t.Fatalf("rejected preview leaked: %q", rec.StatementPreview)
	}
	if rec.FullStatement != "" {
		t.Fatal("a rejected attempt saved a full statement")
	}
}

// ---------------------------------------------------------------------------
// R1-6: the pre-finalize consistency gate. These inject impossible (result,
// evidence, error) triples an executor could never produce — an internal
// contract fault proves the gate fails closed instead of persisting a
// success that contradicts its own evidence.
// ---------------------------------------------------------------------------

func TestExecutePGClaimed_OutcomeConsistencyGate(t *testing.T) {
	t.Parallel()
	successEvidence := func() *PGAttemptEvidence {
		return &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess, BackendPID: 42}
	}
	nonEmptyResult := func() *PGDisclosedResult {
		return &PGDisclosedResult{Rows: [][]*string{}}
	}
	cases := map[string]struct {
		res        *PGDisclosedResult
		ev         *PGAttemptEvidence
		err        error
		wantErr    error
		wantReject bool
		wantStatus model.QueryExecutionStatus
		wantCode   string
		wantRows   int
	}{
		"result without evidence or error": {
			res: nonEmptyResult(), ev: nil, err: nil,
			wantErr: ErrQueryBackendFailure, wantStatus: model.QueryExecutionFailed, wantCode: "query_internal_error",
		},
		"success evidence without result": {
			res: nil, ev: successEvidence(), err: nil,
			wantErr: ErrQueryBackendFailure, wantStatus: model.QueryExecutionFailed, wantCode: "query_internal_error",
		},
		"failed evidence under non-empty result": {
			res: nonEmptyResult(), ev: &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionFailed}, err: nil,
			wantErr: ErrQueryBackendFailure, wantStatus: model.QueryExecutionFailed, wantCode: "query_internal_error",
		},
		"success evidence under trusted error": {
			res: nonEmptyResult(), ev: successEvidence(), err: ErrQueryTimeout,
			wantErr: ErrQueryBackendFailure, wantStatus: model.QueryExecutionFailed, wantCode: "query_internal_error",
		},
		"error without evidence": {
			res: nil, ev: nil, err: ErrQueryTimeout,
			wantErr: ErrQueryTimeout, wantStatus: model.QueryExecutionFailed, wantCode: "query_backend_error",
		},
		"normal success": {
			res: nonEmptyResult(), ev: successEvidence(), err: nil,
			wantErr: nil, wantStatus: model.QueryExecutionSuccess, wantCode: "", wantRows: 0,
		},
		"normal cancelled": {
			res: nil, ev: &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionCancelled, BackendPID: 9}, err: context.Canceled,
			wantErr: context.Canceled, wantStatus: model.QueryExecutionCancelled, wantCode: "query_cancelled",
		},
		"normal timeout": {
			res: nil, ev: &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionTimeout, BackendPID: 9}, err: ErrQueryTimeout,
			wantErr: ErrQueryTimeout, wantStatus: model.QueryExecutionTimeout, wantCode: "query_timeout",
		},
		"normal rejected": {
			res: nil, ev: &PGAttemptEvidence{Stage: PGStageGuard, Status: model.QueryExecutionRejected}, err: &pgsql.RejectError{Code: "query_forbidden", Message: "no"},
			wantReject: true, wantStatus: model.QueryExecutionRejected, wantCode: "query_forbidden",
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return tc.res, tc.ev, tc.err
			}
			out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
			switch {
			case tc.wantReject:
				var re *pgsql.RejectError
				if !errors.As(err, &re) {
					t.Fatalf("err = %v, want *pgsql.RejectError", err)
				}
			case tc.wantErr == nil:
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
			case !errors.Is(err, tc.wantErr):
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			// Exactly one keyed finalize regardless of the triple.
			if len(repo.finalizeCalls) != 1 {
				t.Fatalf("finalizeCalls = %d, want 1", len(repo.finalizeCalls))
			}
			rec := repo.finalizeCalls[0]
			if rec.Status != tc.wantStatus {
				t.Fatalf("persisted status = %v, want %v", rec.Status, tc.wantStatus)
			}
			if rec.ErrorCode != tc.wantCode {
				t.Fatalf("persisted error_code = %q, want %q", rec.ErrorCode, tc.wantCode)
			}
			if rec.RowCount != tc.wantRows {
				t.Fatalf("persisted row_count = %d, want %d", rec.RowCount, tc.wantRows)
			}
			// The three status surfaces can never contradict: the returned
			// outcome, its evidence, and the persisted row agree on the same
			// terminal state — the consistency gate's verdict is the verdict.
			if out.Evidence == nil || out.Evidence.Status != out.Status {
				t.Fatalf("evidence status %v contradicts outcome %v",
					out.Evidence, out.Status)
			}
			if out.Status != rec.Status {
				t.Fatalf("outcome status %v diverged from persisted %v", out.Status, rec.Status)
			}
			// Rows leave the entry only on a true consistent success.
			if tc.wantStatus != model.QueryExecutionSuccess && out.Result != nil {
				t.Fatal("a non-success outcome delivered result rows")
			}
			if tc.wantStatus == model.QueryExecutionSuccess && out.Result == nil {
				t.Fatal("consistent success dropped its result")
			}
			// Evidence is never fabricated: a nil evidence input surfaces the
			// failed wiring-fault record, never invented scalars.
			if tc.ev == nil {
				if out.Evidence == nil || out.Evidence.BackendPID != 0 || out.Evidence.RemoteState != "" {
					t.Fatalf("fabricated evidence scalars: %+v", out.Evidence)
				}
				if rec.BackendPID != nil || rec.RemoteState != "" {
					t.Fatalf("persisted fabricated evidence: %+v", rec)
				}
			} else if tc.ev.BackendPID == 0 {
				if rec.BackendPID != nil {
					t.Fatalf("persisted fabricated backend pid: %+v", rec)
				}
			} else if rec.BackendPID == nil || *rec.BackendPID != int64(tc.ev.BackendPID) {
				t.Fatalf("persisted pid = %v, want %d", rec.BackendPID, tc.ev.BackendPID)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R1-6 follow-up (internal contract fault-injection — ordinary clients can
// never construct executor return objects): the verdict-aligned evidence is
// an independent copy. The executor's record keeps its original status, the
// measured facts survive verbatim — a real remote_state=completed coexists
// with a failed verdict — and mutating the returned record never reaches
// the input.
// ---------------------------------------------------------------------------

func TestExecutePGClaimed_ContractFaultEvidenceIsIndependentCopy(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	start := time.Date(2026, 6, 22, 8, 59, 0, 0, time.UTC)
	in := &PGAttemptEvidence{
		Stage: PGStageExecute, Status: model.QueryExecutionSuccess,
		BackendPID: 4321, BackendStart: start,
		RemoteState: model.QueryExecutionRemoteCompleted, UserSQLSent: true,
	}
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		return nil, in, nil
	}
	out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("err = %v, want ErrQueryBackendFailure", err)
	}
	// Independent record, aligned status, facts verbatim.
	if out.Evidence == nil || out.Evidence == in {
		t.Fatal("contract-fault evidence aliases the executor's record")
	}
	if out.Evidence.Status != model.QueryExecutionFailed || out.Status != model.QueryExecutionFailed {
		t.Fatalf("outcome evidence = %+v, want failed", out.Evidence)
	}
	if out.Evidence.Stage != PGStageExecute || out.Evidence.BackendPID != 4321 ||
		!out.Evidence.BackendStart.Equal(start) ||
		out.Evidence.RemoteState != model.QueryExecutionRemoteCompleted ||
		!out.Evidence.UserSQLSent {
		t.Fatalf("measured facts rewritten: %+v", out.Evidence)
	}
	// The persisted row keeps the same real remote_state — a completed
	// backend is a fact, not a status contradiction.
	rec := repo.finalizeCalls[0]
	if rec.RemoteState != model.QueryExecutionRemoteCompleted || rec.BackendPID == nil || *rec.BackendPID != 4321 {
		t.Fatalf("persisted evidence facts lost: %+v", rec)
	}
	// Input ownership: the executor's record is untouched, and mutating the
	// returned copy never reaches it.
	out.Evidence.Status = model.QueryExecutionSuccess
	out.Evidence.BackendPID = 1
	if in.Status != model.QueryExecutionSuccess || in.BackendPID != 4321 {
		t.Fatalf("executor record mutated: %+v", in)
	}
}

// ---------------------------------------------------------------------------
// T8-C: the post-finalize delivery gate. The success pair is already
// persisted when capabilities decide delivery: a truncated page without the
// declared capability returns the upgrade error while the recorded outcome
// stays success and no rows escape; unknown/duplicate capabilities never
// satisfy the exact-match rule. Internal fault-injection only — ordinary
// clients cannot construct executor return objects.
// ---------------------------------------------------------------------------

func TestExecutePGClaimed_DeliveryGate(t *testing.T) {
	t.Parallel()
	truncatedResult := func() *PGDisclosedResult {
		short := "8192-byte prefix"
		return &PGDisclosedResult{
			Columns: []PGDisclosedColumn{
				{PGResultColumn: PGResultColumn{Name: "v", DatabaseType: "TEXT"},
					DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
			},
			Rows:          [][]*string{{&short}},
			CellTruncated: [][]bool{{true}},
		}
	}
	cleanResult := func() *PGDisclosedResult {
		v := "ok"
		return &PGDisclosedResult{
			Columns: []PGDisclosedColumn{
				{PGResultColumn: PGResultColumn{Name: "v", DatabaseType: "TEXT"},
					DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
			},
			Rows:          [][]*string{{&v}},
			CellTruncated: [][]bool{{false}},
		}
	}
	cases := map[string]struct {
		res         func() *PGDisclosedResult
		caps        []string
		wantErr     error
		wantResult  bool
		wantMatrixV bool // expected flag at [0][0]
	}{
		"truncated page, no capability":        {truncatedResult, nil, ErrResultContractUpgradeRequired, false, false},
		"truncated page, exact capability":     {truncatedResult, []string{CapabilityCellTruncated}, nil, true, true},
		"truncated page, unknown capabilities": {truncatedResult, []string{"bogus", "cellTruncated-extra", "bogus"}, ErrResultContractUpgradeRequired, false, false},
		"clean page, no capability":            {cleanResult, nil, nil, true, false},
		"clean page, capability":               {cleanResult, []string{CapabilityCellTruncated}, nil, true, false},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
			res := tc.res()
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return res, &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess, BackendPID: 42}, nil
			}
			req := validClaimedRequest()
			req.Capabilities = tc.caps
			out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
			switch {
			case tc.wantErr == nil && err != nil:
				t.Fatalf("err = %v, want nil", err)
			case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			// Delivery refusal is post-finalize only: exactly one keyed
			// finalize, the persisted outcome stays success, and the true
			// execution id survives — never a rejected row or a second pair.
			if len(repo.finalizeCalls) != 1 {
				t.Fatalf("finalizeCalls = %d, want 1", len(repo.finalizeCalls))
			}
			rec := repo.finalizeCalls[0]
			if rec.Status != model.QueryExecutionSuccess || rec.RowCount != 1 {
				t.Fatalf("persisted = %+v, want success row_count 1", rec)
			}
			if out.ExecutionID == 0 || out.Status != model.QueryExecutionSuccess ||
				out.Evidence == nil || out.Evidence.Status != model.QueryExecutionSuccess {
				t.Fatalf("outcome = %+v, want delivered-fact success", out)
			}
			if tc.wantResult {
				if out.Result == nil || len(out.Result.Rows) != 1 {
					t.Fatalf("delivery = %+v, want the disclosed page", out.Result)
				}
				if out.Result.CellTruncated[0][0] != tc.wantMatrixV {
					t.Fatalf("matrix[0][0] = %v, want %v", out.Result.CellTruncated[0][0], tc.wantMatrixV)
				}
			} else if out.Result != nil {
				t.Fatalf("refused delivery leaked rows: %+v", out.Result)
			}
		})
	}
}

// A non-empty disclosed page with a missing or misaligned matrix is producer
// damage: the attempt finalizes as one keyed failed row under the internal
// contract code — never a disclosure refusal, never an all-false repair,
// never a success discovered too late.
func TestExecutePGClaimed_MatrixContractFault(t *testing.T) {
	t.Parallel()
	v := "x"
	base := func() *PGDisclosedResult {
		return &PGDisclosedResult{
			Columns: []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "v"}}},
			Rows:    [][]*string{{&v}},
		}
	}
	cases := map[string]func() *PGDisclosedResult{
		"missing matrix": base,
		"matrix row drift": func() *PGDisclosedResult {
			r := base()
			r.CellTruncated = [][]bool{{false}, {false}}
			return r
		},
		"matrix width drift": func() *PGDisclosedResult {
			r := base()
			r.CellTruncated = [][]bool{{false, false}}
			return r
		},
	}
	for name, build := range cases {
		name, build := name, build
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return build(), &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess, BackendPID: 42}, nil
			}
			out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
			if !errors.Is(err, ErrQueryBackendFailure) {
				t.Fatalf("err = %v, want ErrQueryBackendFailure", err)
			}
			if len(repo.finalizeCalls) != 1 {
				t.Fatalf("finalizeCalls = %d, want 1", len(repo.finalizeCalls))
			}
			rec := repo.finalizeCalls[0]
			if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_internal_error" || rec.RowCount != 0 {
				t.Fatalf("persisted = %+v, want failed/query_internal_error/0", rec)
			}
			if out.Result != nil || out.Status != model.QueryExecutionFailed {
				t.Fatalf("outcome = %+v, want failed with no result", out)
			}
		})
	}
}

// A finalize failure outranks any delivery refusal: the truncated page
// without capability still surfaces the persistence error, never the
// upgrade error, and no compensating pair is written.
func TestExecutePGClaimed_FinalizeFailureOutranksDelivery(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.finalizeErr = errors.New("deadlock")
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	v := "x"
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		return &PGDisclosedResult{
			Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "v"}}},
			Rows:          [][]*string{{&v}},
			CellTruncated: [][]bool{{true}},
		}, &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess}, nil
	}
	out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
	if !errors.Is(err, errPersistAttempt) || errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("err = %v, want the persistence failure, not a capability refusal", err)
	}
	if out.Result != nil || out.ExecutionID != 0 {
		t.Fatalf("outcome = %+v, want empty on persistence failure", out)
	}
	if len(repo.pairCalls) != 0 {
		t.Fatal("finalize failure wrote a compensating pair")
	}
}

// ---------------------------------------------------------------------------
// T8-C-R1: the delivery snapshot/ownership boundary. The coordinator gates
// and returns its own pre-finalize snapshot — never the producer's live
// object. Mutations injected inside the fake finalize (an internal ownership
// fault — the real keyed finalize takes no result pointer) cannot reach the
// validated snapshot, and writes into the returned payload cannot reach
// upstream records. Every assertion checks the final returned rows AND the
// matrix, not just the decision's internals or the last error code.
// ---------------------------------------------------------------------------

func TestExecutePGClaimed_DeliverySnapshotOwnership(t *testing.T) {
	t.Parallel()
	oneCol := func() []PGDisclosedColumn {
		return []PGDisclosedColumn{{
			PGResultColumn: PGResultColumn{Name: "v", DatabaseType: "TEXT"},
			DisplayMode:    model.ResultDisclosureRawCopyAllowed,
			CopyAllowed:    true,
		}}
	}
	successEvidence := func() *PGAttemptEvidence {
		return &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess, BackendPID: 42}
	}

	// After a successful return neither side can write through to the
	// other: upstream cells/matrix and delivered cells/matrix are detached
	// in both directions, and the returned object is never the producer's.
	t.Run("post-return bidirectional isolation", func(t *testing.T) {
		t.Parallel()
		repo := claimedPGRepo(t)
		svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
		safe, empty, lit := "safe", "", "NULL"
		res := &PGDisclosedResult{
			Columns: append(oneCol(), PGDisclosedColumn{
				PGResultColumn: PGResultColumn{Name: "w", DatabaseType: "TEXT"},
				DisplayMode:    model.ResultDisclosureRawCopyAllowed, CopyAllowed: true}),
			Rows:          [][]*string{{&safe, nil}, {&empty, &lit}},
			CellTruncated: [][]bool{{false, false}, {false, false}},
		}
		svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
			return res, successEvidence(), nil
		}
		out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
		got := out.Result
		if got == nil {
			t.Fatal("no result delivered")
		}
		if got == res {
			t.Fatal("returned the producer's own result object")
		}
		// Multi-row / NULL / empty / literal "NULL" survive verbatim.
		if len(got.Rows) != 2 || len(got.Rows[0]) != 2 || len(got.Rows[1]) != 2 {
			t.Fatalf("rows = %v, want the 2x2 page", got.Rows)
		}
		if got.Rows[0][0] == nil || *got.Rows[0][0] != "safe" ||
			got.Rows[0][1] != nil ||
			got.Rows[1][0] == nil || *got.Rows[1][0] != "" ||
			got.Rows[1][1] == nil || *got.Rows[1][1] != "NULL" {
			t.Fatalf("delivered cells = %v, want safe/NULL/empty/literal-NULL", got.Rows)
		}
		if got.Rows[0][0] == res.Rows[0][0] {
			t.Fatal("delivered cell aliases the producer's pointer")
		}
		// Upstream writes cannot reach the delivered payload.
		*res.Rows[0][0] = "tampered-upstream"
		res.CellTruncated[0][0] = true
		if *got.Rows[0][0] != "safe" || got.CellTruncated[0][0] {
			t.Fatal("upstream mutation reached the delivered payload")
		}
		// Delivered writes cannot reach upstream state.
		*got.Rows[0][0] = "tampered-returned"
		got.CellTruncated[1][1] = true
		if *res.Rows[0][0] != "tampered-upstream" || res.CellTruncated[1][1] {
			t.Fatal("delivered mutation reached the producer's records")
		}
	})

	// A producer-side mutation inside the keyed finalize never reaches the
	// snapshot the coordinator validated, gated, and returned.
	t.Run("finalize-boundary mutation keeps validated snapshot", func(t *testing.T) {
		t.Parallel()
		cases := map[string]struct {
			matrix   [][]bool
			hook     func(res *PGDisclosedResult)
			wantErr  error
			wantFlag bool
		}{
			"cell rewritten inside finalize": {
				matrix: [][]bool{{false}},
				hook: func(res *PGDisclosedResult) {
					*res.Rows[0][0] = strings.Repeat("x", 9000)
				},
				wantFlag: false,
			},
			"row widened inside finalize": {
				matrix: [][]bool{{false}},
				hook: func(res *PGDisclosedResult) {
					extra := "extra"
					res.Rows[0] = append(res.Rows[0], &extra)
				},
				wantFlag: false,
			},
			"flag flipped clean inside finalize": {
				matrix: [][]bool{{true}},
				hook: func(res *PGDisclosedResult) {
					res.CellTruncated[0][0] = false
				},
				wantErr: ErrResultContractUpgradeRequired,
			},
		}
		for name, tc := range cases {
			tc := tc
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				repo := claimedPGRepo(t)
				svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
				safe := "safe"
				res := &PGDisclosedResult{
					Columns:       oneCol(),
					Rows:          [][]*string{{&safe}},
					CellTruncated: tc.matrix,
				}
				repo.finalizeHook = func() { tc.hook(res) }
				svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
					return res, successEvidence(), nil
				}
				out, err := svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
				switch {
				case tc.wantErr == nil && err != nil:
					t.Fatalf("err = %v, want nil", err)
				case tc.wantErr != nil && !errors.Is(err, tc.wantErr):
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				// The single keyed finalize still persisted success — a
				// boundary mutation never rewrote the recorded outcome.
				if len(repo.finalizeCalls) != 1 || repo.finalizeCalls[0].Status != model.QueryExecutionSuccess {
					t.Fatalf("finalize = %+v, want exactly one success row", repo.finalizeCalls)
				}
				if tc.wantErr != nil {
					if out.Result != nil {
						t.Fatalf("refusal leaked rows: %+v", out.Result)
					}
					return
				}
				got := out.Result
				if got == nil || len(got.Rows) != 1 || len(got.Rows[0]) != 1 {
					t.Fatalf("rows = %+v, want the validated 1x1 snapshot", got)
				}
				if got.Rows[0][0] == nil || *got.Rows[0][0] != "safe" {
					t.Fatalf("cell = %v, want the validated snapshot value", got.Rows[0][0])
				}
				if len(got.CellTruncated) != 1 || len(got.CellTruncated[0]) != 1 || got.CellTruncated[0][0] != tc.wantFlag {
					t.Fatalf("matrix = %v, want the validated flag %v", got.CellTruncated, tc.wantFlag)
				}
			})
		}
	})
}
