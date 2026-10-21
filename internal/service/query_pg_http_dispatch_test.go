// Package service tests the construction-time PG HTTP dispatch gate.
// input: testing, context, errors, encoding/json, internal/model fixtures,
// the shared fake repos/executor
// output: TestPGHTTPDispatch_* — default-closed legacy verdicts, enabled PG
// dispatch into the keyed orchestration, per-engine field arbitration, and
// the minimal-dispatch-read contract (no credentials, no claim, no SQL)
// pos: T12 PG-HTTP unit coverage (issue #120) — proves the capability is
// assembly-bound and request-inert: the same requests under the default
// assembly keep every legacy verdict, and the same service without the
// option never reaches the keyed PG entries.
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// mysqlDispatchTarget is a non-PG target for the engine-arbitration cases:
// its own credential row lets the legacy path run to a real (fake) executor
// so the MySQL regression is a positive proof, not just a non-dispatch.
func mysqlDispatchTarget() (model.QueryTarget, model.QueryCredentialMetadata) {
	target := model.QueryTarget{
		ResourceID: 9003,
		ConnectionContext: model.QueryTargetConnectionContext{
			Engine:      "mysql",
			Environment: "staging",
			Host:        "127.0.0.1",
			Port:        3306,
		},
	}
	cred := model.QueryCredentialMetadata{
		ResourceID:        9003,
		Engine:            "mysql",
		DatabaseName:      "mysql",
		CredentialRef:     "MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	return target, cred
}

// mixedDispatchRepo carries credentials for both the PG fixture target and
// the MySQL arbitration target.
func mixedDispatchRepo(t *testing.T) *fakeExecRepo {
	t.Helper()
	_, pgCred, _ := pgClaimFixture()
	_, mysqlCred := mysqlDispatchTarget()
	return &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{
		9002: pgCred,
		9003: mysqlCred,
	}}
}

func mixedDispatchTargets() fakeTargetRepo {
	pg, _, _ := pgClaimFixture()
	mysql, _ := mysqlDispatchTarget()
	return fakeTargetRepo{targets: []model.QueryTarget{pg, mysql}}
}

// ---------------------------------------------------------------------------
// Ordinary execute dispatch
// ---------------------------------------------------------------------------

// The default assembly keeps every legacy verdict on a PG target: no option
// means the keyed entry is unreachable, scope fields are shape rejections,
// and a field-free request still ends in the NULL-key engine denial pair.
func TestPGHTTPDispatch_Ordinary_DefaultClosed(t *testing.T) {
	t.Parallel()
	repo := mixedDispatchRepo(t)
	executor := &fakeExecutor{}
	resolver := &fakeResolver{dsn: mustDSN(t)}
	svc := NewQueryExecutionService(
		mixedDispatchTargets(), repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: pgDispatchNow}, nil, &fakeDisclosureService{},
	)

	// A request carrying PG scope fields is the legacy shape rejection —
	// no dispatch, no access read beyond the request itself, no evidence.
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9002,
		model.QueryExecuteRequest{Statement: "select 1", Database: "appdb", ClientExecutionID: "k1"})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("scoped request on closed assembly = %v, want validation_failed", err)
	}
	if len(repo.pairCalls) != 0 || len(repo.claimInputs) != 0 {
		t.Fatal("scope-field rejection wrote evidence or claimed")
	}

	// A field-free request still reaches access and fails the engine gate:
	// exactly one NULL-key rejected pair — the preserved pre-T12 boundary —
	// never a claim occupancy.
	_, err = svc.Execute(context.Background(), userExecutionIdentity(7), 9002,
		model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "k2"})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("field-free request on closed assembly = %v, want query_not_allowed", err)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("closed assembly occupied a claim")
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionRejected ||
		repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("closed assembly evidence = %+v, want one NULL-key rejected pair", repo.pairCalls)
	}
	if executor.called {
		t.Fatal("closed assembly reached the legacy executor on a PG target")
	}
}

// The enabled assembly dispatches a PG-target request into the keyed
// orchestration: exactly one claim admission followed by the keyed finalize
// (the dead DSN terminal), never a NULL-key pair or the legacy executor.
func TestPGHTTPDispatch_Ordinary_Enabled(t *testing.T) {
	t.Parallel()
	repo := mixedDispatchRepo(t)
	executor := &fakeExecutor{}
	resolver := &fakeResolver{dsn: mustDSN(t)}
	svc := NewQueryExecutionService(
		mixedDispatchTargets(), repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: pgDispatchNow}, nil, &fakeDisclosureService{},
	).WithPGClaimOrchestration(&fakePGPolicyReader{}).WithPGHTTPEntries()

	// Full PG scope dispatches: the keyed chain claims the key, then the
	// attempt terminates through one keyed finalize — no pair, no fake
	// executor query.
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9002,
		model.QueryExecuteRequest{Statement: "select 1", Database: "appdb", ClientExecutionID: "k3"})
	if err == nil {
		t.Fatal("enabled dispatch with dead DSN must terminate through the keyed chain")
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claimInputs = %d, want 1 — the keyed PG entry ran", len(repo.claimInputs))
	}
	if repo.claimInputs[0].ClientExecutionID != "k3" || repo.claimInputs[0].DatabaseName != "appdb" {
		t.Fatalf("claim input = %+v, want key k3 on database appdb", repo.claimInputs[0])
	}
	if len(repo.pairCalls) != 0 {
		t.Fatal("dispatched request wrote a pre-claim pair")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalizeCalls = %d, want exactly one keyed finalize", len(repo.finalizeCalls))
	}
	if executor.called {
		t.Fatal("PG dispatch reached the legacy MySQL executor")
	}
	if resolver.calls != 1 {
		t.Fatalf("credential resolved %d times, want exactly 1 (dispatch metadata read must not resolve)", resolver.calls)
	}

	// Missing the required database is the PG entry's own shape rejection:
	// validation failure, no second claim, no new evidence.
	_, err = svc.Execute(context.Background(), userExecutionIdentity(7), 9002,
		model.QueryExecuteRequest{Statement: "select 1", ClientExecutionID: "k4"})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("missing database on enabled assembly = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 1 || len(repo.pairCalls) != 0 {
		t.Fatal("shape failure claimed or wrote evidence")
	}
}

// Engine arbitration on the enabled assembly: a non-PG target never sees the
// keyed path — PG scope fields stay shape rejections and the legacy keyed/
// keyless semantics run unchanged.
func TestPGHTTPDispatch_Ordinary_MySQLUnchanged(t *testing.T) {
	t.Parallel()
	repo := mixedDispatchRepo(t)
	executor := &fakeExecutor{}
	resolver := &fakeResolver{dsn: "rouser:p@tcp(127.0.0.1:3306)/mysql"}
	svc := NewQueryExecutionService(
		mixedDispatchTargets(), repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: pgDispatchNow}, nil, &fakeDisclosureService{},
	).WithPGClaimOrchestration(&fakePGPolicyReader{}).WithPGHTTPEntries()

	// PG scope fields on a MySQL target remain the legacy rejection.
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9003,
		model.QueryExecuteRequest{Statement: "select 1", Database: "appdb", ClientExecutionID: "k5"})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("scoped request on MySQL target = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("MySQL scope rejection claimed or wrote evidence")
	}

	// Keyless legacy path still runs the fake executor and writes the
	// keyless evidence pair — untouched by the gate's existence.
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9003,
		model.QueryExecuteRequest{Statement: "select 1"})
	if err != nil {
		t.Fatalf("legacy keyless execute: %v", err)
	}
	if !executor.called || executor.queryCalls != 1 {
		t.Fatal("legacy path did not reach the executor")
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 1 {
		t.Fatalf("legacy evidence = %d claims / %d pairs, want 0/1", len(repo.claimInputs), len(repo.pairCalls))
	}
	_ = resp
}

// ---------------------------------------------------------------------------
// Saved statement dispatch
// ---------------------------------------------------------------------------

// On a PG target the ?database= scope is required, single, and verified
// against the authorized snapshot inside the keyed entry — a mismatch is
// the same invisible not-found as an unknown statement.
func TestPGHTTPDispatch_Saved_ScopeContract(t *testing.T) {
	t.Parallel()
	build := func() (*fakeExecRepo, *fakeSavedStatementRead, *QueryExecutionService) {
		repo := mixedDispatchRepo(t)
		statements := &fakeSavedStatementRead{resp: savedPGFixture()}
		svc := NewQueryExecutionService(
			mixedDispatchTargets(), repo, &fakeResolver{dsn: mustDSN(t)}, &fakeExecutor{},
			NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
			&fakeClock{t: pgDispatchNow}, nil, &fakeDisclosureService{},
		).WithPGClaimOrchestration(&fakePGPolicyReader{}).
			WithTemplateExecution(statements, NewTemplateStatementCompiler()).
			WithPGHTTPEntries()
		return repo, statements, svc
	}

	// Missing/invalid scope is rejected before the statement read.
	for _, scope := range []string{"", "app\x00db"} {
		repo, statements, svc := build()
		req := model.QuerySavedStatementExecuteRequest{ClientExecutionID: "s1", ScopeDatabase: scope}
		_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9002, 41, req)
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Fatalf("scope %q = %v, want validation_failed", scope, err)
		}
		if statements.calls != 0 || len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
			t.Fatalf("scope %q read the statement, claimed, or wrote evidence", scope)
		}
	}

	// A scope that differs from the authorized snapshot's database renders
	// the statement invisible — one NULL-key denial pair, never a claim.
	repo, statements, svc := build()
	req := model.QuerySavedStatementExecuteRequest{ClientExecutionID: "s2", ScopeDatabase: "other"}
	_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9002, 41, req)
	if !errors.Is(err, ErrQuerySavedStatementNotFound) {
		t.Fatalf("scope mismatch = %v, want saved-statement not found", err)
	}
	if statements.calls != 1 {
		t.Fatalf("statement reads = %d, want exactly one authorized snapshot read", statements.calls)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("scope mismatch occupied a claim")
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("scope mismatch evidence = %+v, want one NULL-key pair", repo.pairCalls)
	}

	// A matching scope dispatches into the keyed entry: claim admission and
	// a keyed finalize (dead DSN), never the legacy saved path.
	repo, statements, svc = build()
	req = model.QuerySavedStatementExecuteRequest{
		ClientExecutionID: "s3", ScopeDatabase: "appdb",
		Values: map[string]json.RawMessage{"status": json.RawMessage(`"paid"`), "minimum": json.RawMessage(`20`)},
	}
	_, err = svc.ExecuteSavedStatement(context.Background(), 7, 9002, 41, req)
	if err == nil {
		t.Fatal("matching-scope saved dispatch must terminate through the keyed chain")
	}
	if statements.calls != 1 || len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 {
		t.Fatalf("saved dispatch: reads=%d claims=%d finalizes=%d, want 1/1/1",
			statements.calls, len(repo.claimInputs), len(repo.finalizeCalls))
	}
	if len(repo.pairCalls) != 0 {
		t.Fatal("dispatched saved request wrote a pre-claim pair")
	}
}

// The default assembly rejects a PG-style scope selector outright; on a
// MySQL target the scope field stays forbidden even when enabled.
func TestPGHTTPDispatch_Saved_LegacyUnchanged(t *testing.T) {
	t.Parallel()
	build := func(enable bool) (*fakeExecRepo, *QueryExecutionService) {
		repo := mixedDispatchRepo(t)
		svc := NewQueryExecutionService(
			mixedDispatchTargets(), repo, &fakeResolver{dsn: mustDSN(t)}, &fakeExecutor{},
			NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
			&fakeClock{t: pgDispatchNow}, nil, &fakeDisclosureService{},
		).WithPGClaimOrchestration(&fakePGPolicyReader{}).
			WithTemplateExecution(&fakeSavedStatementRead{resp: savedPGFixture()}, NewTemplateStatementCompiler())
		if enable {
			svc = svc.WithPGHTTPEntries()
		}
		return repo, svc
	}

	// Closed assembly + PG target + ?database: legacy rejection before any
	// statement read or claim.
	repo, svc := build(false)
	req := model.QuerySavedStatementExecuteRequest{ClientExecutionID: "s4", ScopeDatabase: "appdb"}
	_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9002, 41, req)
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("closed saved scope = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("closed saved request claimed or wrote evidence")
	}

	// Enabled assembly + MySQL target + ?database: same rejection — the
	// selector never smuggles a scope switch into a non-PG engine.
	repo, svc = build(true)
	_, err = svc.ExecuteSavedStatement(context.Background(), 7, 9003, 41, req)
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("MySQL saved scope = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("MySQL saved scope claimed or wrote evidence")
	}
}

// ---------------------------------------------------------------------------
// Related-records navigation dispatch
// ---------------------------------------------------------------------------

// On a PG target the enabled assembly accepts source.schema and dispatches
// into the keyed navigation entry (claim admission proves the path); the
// default assembly keeps the MySQL-only schema rejection, and a MySQL
// target keeps it even when the capability is wired.
func TestPGHTTPDispatch_Navigation(t *testing.T) {
	t.Parallel()
	navReq := func(schema string) model.RelatedRecordNavigationRequest {
		return model.RelatedRecordNavigationRequest{
			Source: model.RelatedRecordNavigationSource{
				Database: "appdb", Schema: schema, Object: "order_items",
				Kind: string(model.ObjectKindTable), ForeignKey: "fk_orders",
			},
			LocalValues:       []string{"7"},
			ClientExecutionID: "n1",
		}
	}
	build := func(enable bool, prep pgNavPrepareFunc) (*fakeExecRepo, *fakeExecutor, *QueryExecutionService) {
		repo := mixedDispatchRepo(t)
		executor := &fakeExecutor{}
		svc := NewQueryExecutionService(
			mixedDispatchTargets(), repo, &fakeResolver{dsn: mustDSN(t)}, executor,
			NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
			&fakeClock{t: pgDispatchNow}, nil, &fakeDisclosureService{},
		).WithPGClaimOrchestration(&fakePGPolicyReader{})
		if prep != nil {
			svc.pgNavPrepare = prep
		}
		if enable {
			svc = svc.WithPGHTTPEntries()
		}
		return repo, executor, svc
	}

	// Enabled + PG target + schema: real dispatch — the prepared binding
	// admits the claim before the dead-DSN attempt finalizes.
	repo, executor, svc := build(true, pgNavPrepBinding(pgNavBindingFixture()))
	_, err := svc.NavigateRelatedRecords(context.Background(), 7, 9002, navReq("app"))
	if err == nil {
		t.Fatal("enabled nav dispatch must terminate through the keyed chain")
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 {
		t.Fatalf("nav dispatch: claims=%d finalizes=%d, want 1/1", len(repo.claimInputs), len(repo.finalizeCalls))
	}
	if executor.gotNavInput != nil {
		t.Fatal("PG nav dispatch reached the legacy navigation executor")
	}

	// Enabled + PG target + missing schema: the PG entry's own requiredness
	// — validation failure before any claim.
	repo, _, svc = build(true, nil)
	_, err = svc.NavigateRelatedRecords(context.Background(), 7, 9002, navReq(""))
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("missing schema on enabled PG = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("missing-schema nav occupied a claim")
	}

	// Closed + PG target + schema: the legacy MySQL-only rejection stands.
	repo, _, svc = build(false, nil)
	_, err = svc.NavigateRelatedRecords(context.Background(), 7, 9002, navReq("app"))
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("schema on closed PG = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("closed schema rejection claimed or wrote evidence")
	}

	// Enabled + MySQL target + schema: the same rejection — the schema
	// segment never activates on a schema-less engine.
	repo, _, svc = build(true, nil)
	_, err = svc.NavigateRelatedRecords(context.Background(), 7, 9003, navReq("app"))
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("schema on MySQL target = %v, want validation_failed", err)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("MySQL schema rejection claimed")
	}
}

// pgDispatchNow is the fixed dispatch-test clock.
var pgDispatchNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
