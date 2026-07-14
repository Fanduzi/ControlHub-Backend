// Package service provides tests for the Phase 37 query execution service.
// input: context, errors, strings, testing, time, internal/model
// output: TestExecute_*, TestReadyDerivation_*, TestCredentialResolver_* (fakes for repos/resolver/executor/clock)
// pos: Unit tests for execute gating, the environment-policy matrix, history/audit recording, and credential fail-closed behavior
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// testResolverDSN is a valid MySQL DSN that binds to the scaffold target
// (db.internal:3306). Its password doubles as the leak-detection marker.
const testResolverDSN = "rouser:secret-dsn-do-not-leak@tcp(db.internal:3306)/sandbox"

// --- fakes ---

type fakeTargetRepo struct {
	targets []model.QueryTarget
	queries *[]model.QueryTargetListQuery
}

func (f fakeTargetRepo) ListQueryTargets(_ context.Context, q model.QueryTargetListQuery) ([]model.QueryTarget, int, error) {
	if f.queries != nil {
		*f.queries = append(*f.queries, q)
	}
	if q.TargetID == 0 {
		return f.targets, len(f.targets), nil
	}
	filtered := make([]model.QueryTarget, 0, 1)
	for _, target := range f.targets {
		if target.ResourceID == q.TargetID {
			filtered = append(filtered, target)
		}
	}
	return filtered, len(filtered), nil
}

type fakeExecRepo struct {
	credentials      map[uint64]model.QueryCredentialMetadata
	credentialErr    map[uint64]error
	insertedAttempts []model.QueryExecutionRecord
	auditEvents      []struct {
		actor  uint64
		target uint64
		etype  string
		result string
	}
	// Failure injection for the audit/history guarantee tests.
	insertExecErr  error
	insertAuditErr error
}

func (f *fakeExecRepo) GetCredentialByResourceID(_ context.Context, resourceID uint64) (model.QueryCredentialMetadata, error) {
	if err, ok := f.credentialErr[resourceID]; ok {
		return model.QueryCredentialMetadata{}, err
	}
	c, ok := f.credentials[resourceID]
	if !ok {
		return model.QueryCredentialMetadata{}, sql.ErrNoRows
	}
	return c, nil
}

func (f *fakeExecRepo) InsertExecution(_ context.Context, rec model.QueryExecutionRecord) (uint64, error) {
	if f.insertExecErr != nil {
		return 0, f.insertExecErr
	}
	rec.ID = uint64(len(f.insertedAttempts)) + 1
	f.insertedAttempts = append(f.insertedAttempts, rec)
	return rec.ID, nil
}

func (f *fakeExecRepo) ListExecutions(_ context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error) {
	items := make([]model.QueryExecutionRecord, 0, len(f.insertedAttempts))
	for _, rec := range f.insertedAttempts {
		if rec.TargetResourceID != q.TargetResourceID && q.TargetResourceID != 0 {
			// still allow unfiltered fakes when TargetResourceID is zero
		}
		if q.ActorUserID != nil && rec.ActorUserID != *q.ActorUserID {
			continue
		}
		if q.TargetResourceID != 0 && rec.TargetResourceID != q.TargetResourceID {
			continue
		}
		items = append(items, rec)
	}
	return items, len(items), nil
}

func (f *fakeExecRepo) InsertAuditEvent(_ context.Context, actor, target uint64, etype, result string) error {
	if f.insertAuditErr != nil {
		return f.insertAuditErr
	}
	f.auditEvents = append(f.auditEvents, struct {
		actor  uint64
		target uint64
		etype  string
		result string
	}{actor, target, etype, result})
	return nil
}

// fakeResolver mirrors the real resolver contract: validate the ref first (fail
// closed), then return the configured DSN/error. It records whether it was
// called so tests can assert no env lookup happens with an unvalidated ref.
type fakeResolver struct {
	dsn       string
	err       error
	called    bool
	calledRef string
}

func (f *fakeResolver) Resolve(_ context.Context, ref string) (string, error) {
	f.called = true
	f.calledRef = ref
	if err := model.ValidateCredentialRef(ref); err != nil {
		f.err = err
		return "", err
	}
	if f.err != nil {
		return "", f.err
	}
	return f.dsn, nil
}

type fakeExecutor struct {
	result QueryDatabaseResult
	err    error
	delay  time.Duration
	called bool
	gotDSN string
}

func (f *fakeExecutor) Query(ctx context.Context, dsn string, _ GuardedQuery) (QueryDatabaseResult, error) {
	f.called = true
	f.gotDSN = dsn
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return QueryDatabaseResult{}, context.DeadlineExceeded
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return QueryDatabaseResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeExecutor) QueryBound(ctx context.Context, dsn string, _ string, _ []any, _ int) (QueryDatabaseResult, error) {
	f.called = true
	f.gotDSN = dsn
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return QueryDatabaseResult{}, context.DeadlineExceeded
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return QueryDatabaseResult{}, f.err
	}
	return f.result, nil
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

// fakeNavSchemaInspector implements QuerySchemaInspector for navigation tests.
type fakeNavSchemaInspector struct {
	detail *ObjectDetail
	err    error
}

func (f *fakeNavSchemaInspector) ListDatabases(_ context.Context, _ string, _ string, _ bool, _, _ int) ([]DatabaseSummary, model.PageInfo, error) {
	return nil, model.PageInfo{}, nil
}

func (f *fakeNavSchemaInspector) ListObjects(_ context.Context, _ string, _, _, _ string, _, _ int) ([]ObjectSummary, model.PageInfo, error) {
	return nil, model.PageInfo{}, nil
}

func (f *fakeNavSchemaInspector) GetObjectDetails(_ context.Context, _ string, _, _, _ string) (*ObjectDetail, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.detail != nil {
		return f.detail, nil
	}
	return &ObjectDetail{}, nil
}

// --- fixtures ---

func mysqlTarget(environment string) model.QueryTarget {
	return model.QueryTarget{
		ResourceID: 9001,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: environment,
			Engine:      "mysql",
			Host:        "db.internal",
			Port:        3306,
		},
	}
}

func enabledCred(policy model.QueryEnvironmentPolicy) model.QueryCredentialMetadata {
	return model.QueryCredentialMetadata{
		ResourceID:        9001,
		Enabled:           true,
		Engine:            "mysql",
		CredentialRef:     "ORDER_MYSQL_RO",
		EnvironmentPolicy: policy,
	}
}

// executionTestScaffold wires a service with a ready mysql/staging target.
func executionTestScaffold(t *testing.T) (*QueryExecutionService, *fakeExecRepo, *fakeResolver, *fakeExecutor) {
	t.Helper()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	resolver := &fakeResolver{dsn: testResolverDSN}
	executor := &fakeExecutor{result: QueryDatabaseResult{
		Columns:  []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:     [][]any{{int64(1)}},
		RowCount: 1,
	}}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{},
	)
	return svc, repo, resolver, executor
}

// --- Execute behavior tests ---

func TestExecute_RejectsUnsupportedTarget(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ConnectionContext.Engine = "redis"
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{target}},
		&fakeExecRepo{}, &fakeResolver{}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil,
	)

	_, err := svc.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestExecute_RejectsMissingCredential(t *testing.T) {
	t.Parallel()
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{}, // no credential row
		&fakeResolver{}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil,
	)

	_, err := svc.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestExecute_TargetLookupUsesTargetIDFilter(t *testing.T) {
	t.Parallel()
	queries := []model.QueryTargetListQuery{}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}, queries: &queries},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN},
		&fakeExecutor{result: QueryDatabaseResult{Columns: []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}}, Rows: [][]any{{int64(1)}}, RowCount: 1}},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil,
	)

	_, err := svc.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("ListQueryTargets calls = %d, want 1", len(queries))
	}
	if queries[0].TargetID != 9001 {
		t.Fatalf("TargetID filter = %d, want 9001", queries[0].TargetID)
	}
}

func TestExecute_RejectsUnresolvableCredentialRef(t *testing.T) {
	t.Parallel()
	svc, repo, resolver, executor := executionTestScaffold(t)
	// Credential is valid and allowed, but the resolver fails (e.g. env key unset).
	resolver.err = errors.New("env var not set")

	_, err := svc.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.called {
		t.Fatal("executor must not be reached when the credential cannot be resolved")
	}
	_ = repo
	// WHY: the resolved DSN must never leak into recorded history or errors.
	for _, rec := range repo.insertedAttempts {
		if strings.Contains(rec.ErrorMessage, testResolverDSN) || strings.Contains(rec.StatementDigest, testResolverDSN) {
			t.Fatalf("DSN leaked into history record: %+v", rec)
		}
	}
}

func TestExecute_RejectsUnsafeStatement(t *testing.T) {
	t.Parallel()
	svc, _, _, executor := executionTestScaffold(t)

	_, err := svc.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if executor.called {
		t.Fatal("executor must not be reached for an unsafe statement")
	}
}

func TestExecute_ExecutesSelectWithLimit(t *testing.T) {
	t.Parallel()
	svc, _, _, executor := executionTestScaffold(t)

	resp, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1 as value", MaxRows: 50})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !executor.called {
		t.Fatal("executor must be called for an allowed SELECT")
	}
	if executor.gotDSN != testResolverDSN {
		t.Fatalf("executor should receive the resolved DSN, got %q", executor.gotDSN)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount != 1 || len(resp.Rows) != 1 {
		t.Fatalf("rowCount/rows mismatch: %+v", resp)
	}
	if resp.LimitApplied != 50 {
		t.Fatalf("LimitApplied = %d, want 50", resp.LimitApplied)
	}
	if resp.Engine != "mysql" || resp.TargetResourceID != 9001 {
		t.Fatalf("response identity mismatch: %+v", resp)
	}
	// DSN must never appear in the response.
	if strings.Contains(asString(resp), testResolverDSN) {
		t.Fatal("DSN leaked into response")
	}
}

func TestExecute_MapsTimeoutToQueryTimeout(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := executionTestScaffold(t)
	// Force the executor to block past the 5s default timeout. Use a fake clock
	// is not enough; drive the executor to return the context deadline error.
	// Rebuild with an executor that returns DeadlineExceeded.
	executor := &fakeExecutor{err: context.DeadlineExceeded}
	svc2 := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil,
	)

	_, err := svc2.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	_ = svc
}

func TestExecute_RecordsSuccessfulAttempt(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)

	if _, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("expected 1 history row, got %d", len(repo.insertedAttempts))
	}
	if repo.insertedAttempts[0].Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", repo.insertedAttempts[0].Status)
	}
	if repo.insertedAttempts[0].ActorUserID != 7 {
		t.Fatalf("actor = %d, want 7", repo.insertedAttempts[0].ActorUserID)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "success" {
		t.Fatalf("audit events = %+v, want one success", repo.auditEvents)
	}
}

func TestExecute_RecordsRejectedAttempt(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if executor.called {
		t.Fatal("rejected attempt must not reach the executor")
	}
	// WHY: every attempt, including rejections, must be recorded for audit.
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("expected 1 rejected history row, got %d", len(repo.insertedAttempts))
	}
	if repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("status = %q, want rejected", repo.insertedAttempts[0].Status)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit result = %+v, want validation_failed", repo.auditEvents)
	}
}

func TestExecute_HistoryWriteFailureOnSuccess_ReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	repo.insertExecErr = errors.New("history db down")

	// WHY: Phase 37 guarantees every attempt is recorded. If the history row
	// cannot be written, the service must NOT pretend success (no executionId=0
	// success response); it returns a controlled backend failure.
	resp, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if resp.Status == model.QueryExecutionSuccess {
		t.Fatal("must not return a success response when the history write failed")
	}
	if resp.ExecutionID != 0 {
		t.Fatalf("ExecutionID = %d, want 0 (no successful record)", resp.ExecutionID)
	}
}

func TestExecute_AuditWriteFailureOnSuccess_ReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	repo.insertAuditErr = errors.New("audit db down")

	// WHY: the audit event is part of the recording guarantee; if it cannot be
	// written the request fails closed rather than reporting an unaudited run.
	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
}

func TestExecute_RejectedAttempt_PersistFailure_ReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)
	repo.insertExecErr = errors.New("history db down")

	// WHY: even for a rejected attempt, a recording failure must surface as a
	// controlled backend failure (never silently swallow + claim "recorded").
	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (persist failure must not be swallowed)", err)
	}
	if executor.called {
		t.Fatal("rejected attempt must not reach the executor")
	}
}

// --- Finding 2: credential must bind to the selected target ---

func TestExecute_CredentialEngineMismatch_Rejected(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)
	// The credential is for postgresql but the target is mysql.
	cred := repo.credentials[9001]
	cred.Engine = "postgresql"
	repo.credentials[9001] = cred

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("engine mismatch error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.called {
		t.Fatal("an engine-mismatched credential must never reach the executor")
	}
}

func TestExecute_DSNHostMismatch_Rejected(t *testing.T) {
	t.Parallel()
	svc, _, resolver, executor := executionTestScaffold(t)
	// WHY: a credential resolved to a different host would silently query the
	// wrong database. The DSN host must match the selected target's host.
	resolver.dsn = "rouser:secret@tcp(other-db.internal:3306)/sandbox"

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("DSN host mismatch error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.called {
		t.Fatal("a host-mismatched DSN must never reach the executor")
	}
}

func TestExecute_DSNPortMismatch_Rejected(t *testing.T) {
	t.Parallel()
	svc, _, resolver, executor := executionTestScaffold(t)
	resolver.dsn = "rouser:secret@tcp(db.internal:3307)/sandbox"

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("DSN port mismatch error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.called {
		t.Fatal("a port-mismatched DSN must never reach the executor")
	}
}

func TestExecute_DSNMissingPort_Rejected(t *testing.T) {
	t.Parallel()
	svc, _, resolver, executor := executionTestScaffold(t)
	// No port in the DSN address -> fail closed (Phase 37 targets always have a port).
	resolver.dsn = "rouser:secret@tcp(db.internal)/sandbox"

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("DSN missing port error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.called {
		t.Fatal("a DSN without a port must never reach the executor")
	}
}

func TestExecute_DSNNonTCP_Rejected(t *testing.T) {
	t.Parallel()
	svc, _, resolver, executor := executionTestScaffold(t)
	resolver.dsn = "rouser:secret@unix(/tmp/mysql.sock)/sandbox"

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("non-tcp DSN error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.called {
		t.Fatal("a non-tcp DSN must never reach the executor")
	}
}

func TestExecute_DSNMatching_SucceedsAndDSNNeverLeaks(t *testing.T) {
	t.Parallel()
	svc, repo, resolver, executor := executionTestScaffold(t)
	// The scaffold DSN already binds to db.internal:3306; assert the matching
	// path still succeeds and the DSN/password never appears in recorded history.
	resp, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1 as value", MaxRows: 10})
	if err != nil {
		t.Fatalf("matching DSN Execute error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if !executor.called {
		t.Fatal("a matching DSN must reach the executor")
	}
	for _, rec := range repo.insertedAttempts {
		if strings.Contains(rec.ErrorMessage, testResolverDSN) || strings.Contains(rec.StatementDigest, resolver.dsn) {
			t.Fatalf("DSN leaked into history: %+v", rec)
		}
	}
}

func TestReadyDerivation_CredentialEngineMismatch_IsLocked(t *testing.T) {
	t.Parallel()
	cred := enabledCred(model.QueryEnvPolicyAllEnvironments)
	cred.Engine = "postgresql"
	got := completeQueryTarget(mysqlTarget("Staging"), &cred)
	// WHY: a credential whose engine does not match the target must never mark
	// the target ready, even under all_environments.
	if got.Readiness == model.ReadinessReady {
		t.Fatal("engine-mismatched credential must not make the target ready")
	}
	if got.AvailableActions.Run {
		t.Fatal("Run must stay disabled for an engine-mismatched credential")
	}
}

func TestCredentialResolver_NotCalledWhenCredentialRefInvalid(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentialErr: map[uint64]error{9001: errors.New("credential_ref invalid")},
	}
	resolver := &fakeResolver{dsn: testResolverDSN}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, resolver, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil,
	)

	_, err := svc.Execute(context.Background(), 1, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	// WHY: an invalid credential_ref must fail closed at the repository read, so
	// the resolver (env lookup) is never called with an unvalidated key.
	if resolver.called {
		t.Fatal("resolver must not be called when the credential_ref is invalid")
	}
}

// --- readiness derivation (environment-policy matrix) ---

func TestReadyDerivation_NonProdWithNonProdOnlyPolicy_IsReady(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(mysqlTarget("Staging"), ptrCred(enabledCred(model.QueryEnvPolicyNonProdOnly)))
	if got.Readiness != model.ReadinessReady {
		t.Fatalf("readiness = %q, want ready", got.Readiness)
	}
	if got.Governance.SafetyState != model.SafetyStateReadonlySandboxEnabled {
		t.Fatalf("safetyState = %q, want readonly_sandbox_enabled", got.Governance.SafetyState)
	}
	if !got.AvailableActions.Run {
		t.Fatal("Run action must be enabled for a ready target")
	}
	if got.Governance.ExecutionEnabled != true || got.Governance.CredentialState != "configured_readonly_credential" {
		t.Fatalf("governance mismatch: %+v", got.Governance)
	}
}

func TestReadyDerivation_ProdWithNonProdOnlyPolicy_IsLocked(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(mysqlTarget("Production"), ptrCred(enabledCred(model.QueryEnvPolicyNonProdOnly)))
	if got.Readiness == model.ReadinessReady {
		t.Fatal("production + non_prod_only must NOT be ready")
	}
	if got.AvailableActions.Run {
		t.Fatal("Run must stay disabled for production under non_prod_only")
	}
}

func TestReadyDerivation_ProdWithAllEnvironmentsPolicy_IsReady(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(mysqlTarget("Production"), ptrCred(enabledCred(model.QueryEnvPolicyAllEnvironments)))
	if got.Readiness != model.ReadinessReady {
		t.Fatalf("readiness = %q, want ready (prod + all_environments)", got.Readiness)
	}
}

func TestReadyDerivation_DisabledPolicy_IsLocked(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(mysqlTarget("Staging"), ptrCred(enabledCred(model.QueryEnvPolicyDisabled)))
	if got.Readiness == model.ReadinessReady {
		t.Fatal("disabled policy must NOT be ready")
	}
	if got.Governance.SafetyState != model.SafetyStateExecutionDisabled {
		t.Fatalf("safetyState = %q, want execution_disabled", got.Governance.SafetyState)
	}
}

func TestReadyDerivation_UnknownEmptyPolicy_FailsClosed(t *testing.T) {
	t.Parallel()
	for _, policy := range []model.QueryEnvironmentPolicy{"", "production", "bogus"} {
		got := completeQueryTarget(mysqlTarget("Staging"), ptrCred(enabledCred(policy)))
		if got.Readiness == model.ReadinessReady {
			t.Fatalf("policy %q must fail closed (locked), got ready", policy)
		}
	}
}

func TestReadyDerivation_DisabledCredential_IsLocked(t *testing.T) {
	t.Parallel()
	c := enabledCred(model.QueryEnvPolicyAllEnvironments)
	c.Enabled = false
	got := completeQueryTarget(mysqlTarget("Staging"), ptrCred(c))
	if got.Readiness == model.ReadinessReady {
		t.Fatal("disabled (Enabled=false) credential must NOT be ready")
	}
}

func TestReadyDerivation_NonMysqlEngineStaysLockedEvenWithCredential(t *testing.T) {
	t.Parallel()
	pg := mysqlTarget("Staging")
	pg.ConnectionContext.Engine = "postgresql"
	got := completeQueryTarget(pg, ptrCred(enabledCred(model.QueryEnvPolicyAllEnvironments)))
	// WHY: Phase 37 executes mysql/tidb only; a configured postgres target must
	// stay locked even with an all_environments credential.
	if got.Readiness == model.ReadinessReady {
		t.Fatal("postgresql must not be ready in Phase 37")
	}
	if got.AvailableActions.Run {
		t.Fatal("Run must stay disabled for non-mysql/tidb engines")
	}
}

func ptrCred(c model.QueryCredentialMetadata) *model.QueryCredentialMetadata { return &c }

// asString marshals a value to JSON for substring-based leak checks (tests only
// need to detect the secret DSN substring anywhere in the stringified response).
func asString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func TestListHistory_AdminSeesAllActors(t *testing.T) {
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Status: model.QueryExecutionSuccess},
			{ID: 2, TargetResourceID: 22, ActorUserID: 7, Actor: model.QueryExecutionActor{DisplayName: "Editor"}, Status: model.QueryExecutionSuccess},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil)
	items, page, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("admin items = %d, want 2", len(items))
	}
	if page.TotalItems != 2 {
		t.Fatalf("total = %d, want 2", page.TotalItems)
	}
}

func TestListHistory_NonAdminSeesOwnOnly(t *testing.T) {
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Status: model.QueryExecutionSuccess},
			{ID: 2, TargetResourceID: 22, ActorUserID: 7, Actor: model.QueryExecutionActor{DisplayName: "Editor"}, Status: model.QueryExecutionSuccess},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil)
	items, page, err := svc.ListHistory(context.Background(), 7, "editor", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(items) != 1 || items[0].ActorUserID != 7 {
		t.Fatalf("non-admin items = %+v, want only actor 7", items)
	}
	if page.TotalItems != 1 {
		t.Fatalf("total = %d, want 1", page.TotalItems)
	}
}

func TestListHistory_UnknownTarget404(t *testing.T) {
	svc := NewQueryExecutionService(fakeTargetRepo{targets: nil}, &fakeExecRepo{}, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil)
	_, _, err := svc.ListHistory(context.Background(), 1, "admin", 999, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("err = %v, want ErrQueryTargetNotFound", err)
	}
}

func TestListHistory_UnknownActorFallback(t *testing.T) {
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 99, Actor: model.QueryExecutionActor{DisplayName: ""}, Status: model.QueryExecutionSuccess},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil)
	items, _, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if items[0].Actor.DisplayName != model.UnknownHistoryActorDisplayName {
		t.Fatalf("displayName = %q, want Unknown user", items[0].Actor.DisplayName)
	}
	raw, _ := json.Marshal(items[0])
	if strings.Contains(string(raw), "actorUserId") {
		t.Fatalf("public JSON must not include actorUserId: %s", raw)
	}
	if !strings.Contains(string(raw), `"displayName":"Unknown user"`) {
		t.Fatalf("public JSON missing actor.displayName: %s", raw)
	}
}
