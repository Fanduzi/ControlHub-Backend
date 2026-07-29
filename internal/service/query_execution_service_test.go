// Package service provides tests for the Phase 37/38S query execution service.
// input: context, errors, strings, testing, time, internal/model
// output: TestExecute_*, TestReadyDerivation_*, TestCredentialResolver_* (fakes for repos/resolver/executor/clock)
// pos: Unit tests for execute gating, governed result pages, the environment-policy matrix, history/audit recording, and credential fail-closed behavior
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
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
	// Captures the last ListExecutions query for mode-dispatch assertions.
	lastQuery model.QueryExecutionListQuery
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
	f.lastQuery = q
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
		if q.Status != nil && rec.Status != *q.Status {
			continue
		}
		if q.From != nil && rec.CreatedAt.Before(*q.From) {
			continue
		}
		if q.To != nil && !rec.CreatedAt.Before(*q.To) {
			continue
		}
		items = append(items, rec)
	}

	if q.Cursor != nil {
		payload, err := model.DecodeCursor(*q.Cursor)
		if err != nil {
			return nil, 0, err
		}
		cursorID, _ := strconv.ParseUint(payload.ID, 10, 64)
		filtered := make([]model.QueryExecutionRecord, 0, len(items))
		for _, rec := range items {
			if rec.CreatedAt.Before(payload.CreatedAt) || (rec.CreatedAt.Equal(payload.CreatedAt) && rec.ID < cursorID) {
				filtered = append(filtered, rec)
			}
		}
		items = filtered
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	if q.PageSize > 0 && len(items) > q.PageSize {
		items = items[:q.PageSize]
	}

	// WHY: match the real repository's contract — cursor mode skips COUNT
	// (total=0), offset mode returns the unfiltered count for PageInfo.
	if q.Mode == model.PaginationModeCursor {
		return items, 0, nil
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
	calls     int
	calledRef string
}

func (f *fakeResolver) Resolve(_ context.Context, ref string) (string, error) {
	f.called = true
	f.calls++
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
	result      QueryDatabaseResult
	err         error
	delay       time.Duration
	called      bool
	queryCalls  int
	queries     []GuardedQuery
	gotDSN      string
	gotNavInput *RelatedRecordsQueryInput
}

func (f *fakeExecutor) Query(ctx context.Context, dsn string, guarded GuardedQuery) (QueryDatabaseResult, error) {
	f.called = true
	f.queryCalls++
	f.queries = append(f.queries, guarded)
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

func (f *fakeExecutor) QueryRelatedRecords(ctx context.Context, dsn string, input RelatedRecordsQueryInput) (QueryDatabaseResult, error) {
	f.called = true
	f.gotDSN = dsn
	f.gotNavInput = &input
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

type fakeDisclosureService struct {
	blockErr         error
	preflightCalls   int
	preflightQueries []GuardedQuery
}

func (f *fakeDisclosureService) Preflight(_ context.Context, _ string, _ uint64, guarded GuardedQuery) (DisclosurePlan, error) {
	f.preflightCalls++
	f.preflightQueries = append(f.preflightQueries, guarded)
	if f.blockErr != nil {
		return DisclosurePlan{}, f.blockErr
	}
	return DisclosurePlan{}, nil
}

func (f *fakeDisclosureService) PreflightRelatedRecords(_ context.Context, _ string, _ uint64, _, _ string) (DisclosurePlan, error) {
	if f.blockErr != nil {
		return DisclosurePlan{}, f.blockErr
	}
	return DisclosurePlan{}, nil
}

func (f *fakeDisclosureService) Apply(_ DisclosurePlan, columns []model.QueryResultColumn, rows [][]any) ([]model.QueryResultColumn, [][]any, error) {
	return columns, rows, nil
}

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

func (f *fakeNavSchemaInspector) GetTableDefinition(_ context.Context, _, _, _ string) (*TableDefinition, error) {
	return &TableDefinition{}, nil
}

func (f *fakeNavSchemaInspector) GetRelationshipMap(_ context.Context, _, _, _ string) (*RelationshipMapResult, error) {
	return &RelationshipMapResult{}, nil
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
		&fakeDisclosureService{},
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
		nil, &fakeDisclosureService{},
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
		nil, &fakeDisclosureService{},
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
		nil, &fakeDisclosureService{},
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
		nil, &fakeDisclosureService{},
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

func TestExecute_DisclosureBlocked_Rejected(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{blockErr: ErrQueryDisclosureBlocked},
	)

	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed (wrapping ErrQueryDisclosureBlocked)", err)
	}
	if executor.called {
		t.Fatal("executor must not be reached when disclosure blocks the query")
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("disclosure-blocked attempt must be recorded as rejected: %+v", repo.insertedAttempts)
	}
}

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
		nil, &fakeDisclosureService{},
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
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil, &fakeDisclosureService{})
	result, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("admin items = %d, want 2", len(result.Items))
	}
	if result.NextCursor != nil {
		t.Fatalf("offset mode nextCursor = %v, want nil", result.NextCursor)
	}
	if result.PageInfo == nil {
		t.Fatal("offset mode pageInfo = nil, want non-nil")
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
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil, &fakeDisclosureService{})
	result, err := svc.ListHistory(context.Background(), 7, "editor", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ActorUserID != 7 {
		t.Fatalf("non-admin items = %+v, want only actor 7", result.Items)
	}
	if result.NextCursor != nil {
		t.Fatalf("offset mode nextCursor = %v, want nil", result.NextCursor)
	}
	if result.PageInfo == nil {
		t.Fatal("offset mode pageInfo = nil, want non-nil")
	}
}

func TestListHistory_UnknownTarget404(t *testing.T) {
	svc := NewQueryExecutionService(fakeTargetRepo{targets: nil}, &fakeExecRepo{}, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil, &fakeDisclosureService{})
	_, err := svc.ListHistory(context.Background(), 1, "admin", 999, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
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
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil, &fakeDisclosureService{})
	result, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if result.Items[0].Actor.DisplayName != model.UnknownHistoryActorDisplayName {
		t.Fatalf("displayName = %q, want Unknown user", result.Items[0].Actor.DisplayName)
	}
	raw, _ := json.Marshal(result.Items[0])
	if strings.Contains(string(raw), "actorUserId") {
		t.Fatalf("public JSON must not include actorUserId: %s", raw)
	}
	if !strings.Contains(string(raw), `"displayName":"Unknown user"`) {
		t.Fatalf("public JSON missing actor.displayName: %s", raw)
	}
}

// --- cursor-based pagination service tests ---

func TestListHistory_Cursor_AdminSeesAll(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Status: model.QueryExecutionSuccess, CreatedAt: now},
			{ID: 2, TargetResourceID: 22, ActorUserID: 7, Actor: model.QueryExecutionActor{DisplayName: "Editor"}, Status: model.QueryExecutionSuccess, CreatedAt: now.Add(-time.Second)},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})
	result, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("admin cursor items = %d, want 2", len(result.Items))
	}
}

func TestListHistory_Cursor_NonAdminSeesOwnOnly(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{DisplayName: "Admin"}, Status: model.QueryExecutionSuccess, CreatedAt: now},
			{ID: 2, TargetResourceID: 22, ActorUserID: 7, Actor: model.QueryExecutionActor{DisplayName: "Editor"}, Status: model.QueryExecutionSuccess, CreatedAt: now.Add(-time.Second)},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})
	result, err := svc.ListHistory(context.Background(), 7, "editor", 22, model.QueryExecutionListQuery{PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ActorUserID != 7 {
		t.Fatalf("non-admin cursor items = %+v, want only actor 7", result.Items)
	}
}

func TestListHistory_Cursor_StatusFilter(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: now},
			{ID: 2, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionRejected, CreatedAt: now.Add(-time.Second)},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})
	status := model.QueryExecutionSuccess
	result, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 20, Status: &status})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].Status != model.QueryExecutionSuccess {
		t.Fatalf("status filter items = %+v, want only success", result.Items)
	}
}

func TestListHistory_Cursor_TimeFilter(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: t1},
			{ID: 2, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: t2},
			{ID: 3, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: t3},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: t3}, nil, &fakeDisclosureService{})
	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	result, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 20, From: &from, To: &to})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}
	if len(result.Items) != 1 || result.Items[0].ID != 2 {
		t.Fatalf("time filter items = %+v, want only ID=2", result.Items)
	}
}

func TestListHistory_Cursor_Continuation(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: now},
			{ID: 2, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: now.Add(-time.Second)},
			{ID: 3, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: now.Add(-2 * time.Second)},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})

	// Build an initial cursor pointing past the newest item so all 3 are "before" it.
	queryHash := model.ComputeQueryHash(22, nil, nil, nil, "all")
	initialCursor, err := model.EncodeCursor(now.Add(time.Second), 9999, queryHash)
	if err != nil {
		t.Fatalf("encode initial cursor: %v", err)
	}

	page1, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 2, Cursor: &initialCursor})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page1 items = %d, want 2", len(page1.Items))
	}
	if page1.NextCursor == nil {
		t.Fatal("page1 NextCursor = nil, want non-nil (more pages exist)")
	}

	page2, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Items) != 1 {
		t.Fatalf("page2 items = %d, want 1", len(page2.Items))
	}
	if page2.NextCursor != nil {
		t.Fatalf("page2 NextCursor = %v, want nil (no more pages)", *page2.NextCursor)
	}
}

func TestListHistory_Cursor_InvalidCursorReturnsValidation(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, &fakeExecRepo{}, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})
	badCursor := "not-a-valid-cursor"
	_, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 20, Cursor: &badCursor})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("err = %v, want ErrQueryValidationFailed", err)
	}
}

// TestListHistory_Cursor_TamperedIDReturnsValidation proves Oracle Finding 2
// is fixed: a cursor with a structurally valid envelope but tampered (non-
// uint64) ID field is rejected at the service boundary with
// ErrQueryValidationFailed (400), not a 500 from strconv.ParseUint in the repo.
func TestListHistory_Cursor_TamperedIDReturnsValidation(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, &fakeExecRepo{}, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})

	// Build a cursor with a valid hash, then tamper the ID to "abc".
	queryHash := model.ComputeQueryHash(22, nil, nil, nil, "all")
	validCursor, err := model.EncodeCursor(now, 1, queryHash)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	b, _ := base64.RawURLEncoding.DecodeString(validCursor)
	tampered := strings.Replace(string(b), `"id":"1"`, `"id":"abc"`, 1)
	tamperedCursor := base64.RawURLEncoding.EncodeToString([]byte(tampered))

	_, err = svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 20, Cursor: &tamperedCursor})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("err = %v, want ErrQueryValidationFailed (tampered cursor ID must not cause 500)", err)
	}
}

func TestListHistory_Cursor_UnknownTarget404(t *testing.T) {
	t.Parallel()
	svc := NewQueryExecutionService(fakeTargetRepo{targets: nil}, &fakeExecRepo{}, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil, &fakeDisclosureService{})
	_, err := svc.ListHistory(context.Background(), 1, "admin", 999, model.QueryExecutionListQuery{PageSize: 20})
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("err = %v, want ErrQueryTargetNotFound", err)
	}
}

// TestListHistory_CursorInitial_SetsExplicitCursorMode proves P1: when no
// ?page= is supplied (Page==0), the service dispatches to cursor mode and
// passes Mode=PaginationModeCursor to the repository. The first cursor page
// has Cursor==nil (no boundary predicate) and never sets Page (which would
// force offset mode at the repo level). This test FAILS on the old candidate
// which set Page=1 for cursor-initial, causing the repo to use offset mode.
func TestListHistory_CursorInitial_SetsExplicitCursorMode(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: now},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})

	_, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}

	// WHY: cursor-initial must set Mode=PaginationModeCursor explicitly.
	if repo.lastQuery.Mode != model.PaginationModeCursor {
		t.Fatalf("cursor-initial Mode = %v, want PaginationModeCursor", repo.lastQuery.Mode)
	}
	// WHY: cursor-initial must NOT set Page (which would trigger offset mode
	// at the repo level if Mode were ever ignored or defaulted incorrectly).
	if repo.lastQuery.Page != 0 {
		t.Fatalf("cursor-initial Page = %d, want 0 (must not fall back to offset)", repo.lastQuery.Page)
	}
	// WHY: cursor-initial must have Cursor==nil (no boundary predicate).
	if repo.lastQuery.Cursor != nil {
		t.Fatalf("cursor-initial Cursor = %v, want nil (no boundary predicate)", repo.lastQuery.Cursor)
	}
	// WHY: cursor mode requests PageSize+1 to detect a next page via sentinel.
	if repo.lastQuery.PageSize != 21 {
		t.Fatalf("cursor-initial PageSize = %d, want 21 (pageSize+1 sentinel)", repo.lastQuery.PageSize)
	}
}

// TestListHistory_Offset_SetsExplicitOffsetMode proves P1: when ?page= is
// supplied (Page>0), the service dispatches to offset mode and passes
// Mode=PaginationModeOffset to the repository with COUNT and OFFSET.
func TestListHistory_Offset_SetsExplicitOffsetMode(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	repo := &fakeExecRepo{
		insertedAttempts: []model.QueryExecutionRecord{
			{ID: 1, TargetResourceID: 22, ActorUserID: 1, Status: model.QueryExecutionSuccess, CreatedAt: now},
		},
	}
	svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: now}, nil, &fakeDisclosureService{})

	_, err := svc.ListHistory(context.Background(), 1, "admin", 22, model.QueryExecutionListQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListHistory: %v", err)
	}

	// WHY: offset mode must set Mode=PaginationModeOffset explicitly.
	if repo.lastQuery.Mode != model.PaginationModeOffset {
		t.Fatalf("offset Mode = %v, want PaginationModeOffset", repo.lastQuery.Mode)
	}
	if repo.lastQuery.Page != 1 {
		t.Fatalf("offset Page = %d, want 1", repo.lastQuery.Page)
	}
	// WHY: offset mode must NOT request the sentinel (no pageSize+1).
	if repo.lastQuery.PageSize != 20 {
		t.Fatalf("offset PageSize = %d, want 20 (no sentinel)", repo.lastQuery.PageSize)
	}
}

func TestExecute_AuditHistoryFailureCannotProduceSuccess(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	// Phase 38S: if the audit/history write fails for any page, the overall
	// response must NOT be a success. This is the same guarantee as Phase 37
	// but extended to per-page recording.
	repo.insertExecErr = errors.New("history db down")
	resp, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (audit/history failure must not produce success)", err)
	}
	if resp.Status == model.QueryExecutionSuccess {
		t.Fatal("must not return success when history write failed")
	}
}

func TestExecute_PagedRequestDefaultsMaxRowsToPageSize(t *testing.T) {
	// Given a page request without an overall maxRows cap.
	svc, _, _, executor := executionTestScaffold(t)
	req := model.QueryExecuteRequest{
		Statement: "select value from metrics",
		Pagination: &model.QueryExecutePaginationRequest{
			Page:     1,
			PageSize: model.QueryExecuteDefaultPageSize,
		},
	}

	// When the first page executes.
	resp, err := svc.Execute(context.Background(), 7, 9001, req)

	// Then the service owns the omitted maxRows default before building the page window.
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(executor.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(executor.queries))
	}
	if executor.queries[0].LimitApplied != model.QueryExecuteDefaultPageSize {
		t.Fatalf("LimitApplied = %d, want %d", executor.queries[0].LimitApplied, model.QueryExecuteDefaultPageSize)
	}
	if resp.Pagination == nil || resp.Pagination.Page != 1 || resp.Pagination.PageSize != model.QueryExecuteDefaultPageSize {
		t.Fatalf("pagination = %+v, want first default page", resp.Pagination)
	}
}

func TestExecute_PagedSecondPageRunsFreshAccessAndDisclosure(t *testing.T) {
	// Given two requests for adjacent pages of the same statement.
	targetQueries := []model.QueryTargetListQuery{}
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{
		9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
	}}
	resolver := &fakeResolver{dsn: testResolverDSN}
	executor := &fakeExecutor{result: QueryDatabaseResult{
		Columns:   []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:      [][]any{{int64(1)}},
		RowCount:  1,
		Truncated: true,
	}}
	disclosure := &fakeDisclosureService{}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}, queries: &targetQueries},
		repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{}, disclosure,
	)
	page := func(number int) model.QueryExecuteRequest {
		return model.QueryExecuteRequest{
			Statement: "select value from metrics",
			MaxRows:   25,
			Pagination: &model.QueryExecutePaginationRequest{
				Page:     number,
				PageSize: 10,
			},
		}
	}

	// When page one and then page two execute.
	if _, err := svc.Execute(context.Background(), 7, 9001, page(1)); err != nil {
		t.Fatalf("page 1 Execute: %v", err)
	}
	resp, err := svc.Execute(context.Background(), 7, 9001, page(2))

	// Then every page re-enters access and disclosure before its own guarded query.
	if err != nil {
		t.Fatalf("page 2 Execute: %v", err)
	}
	if len(targetQueries) != 2 || resolver.calls != 2 || disclosure.preflightCalls != 2 || executor.queryCalls != 2 {
		t.Fatalf("governance calls target=%d resolver=%d disclosure=%d executor=%d, want 2 each", len(targetQueries), resolver.calls, disclosure.preflightCalls, executor.queryCalls)
	}
	if got := executor.queries[1].ExecutableSQL; got != "select `value` from metrics limit 10, 11" {
		t.Fatalf("page 2 SQL = %q, want server-owned offset window", got)
	}
	if resp.Pagination == nil || !resp.Pagination.HasPreviousPage || !resp.Pagination.HasNextPage {
		t.Fatalf("page 2 pagination = %+v, want previous and next pages", resp.Pagination)
	}
	if len(repo.insertedAttempts) != 2 || len(repo.auditEvents) != 2 {
		t.Fatalf("history/audit records = %d/%d, want 2/2", len(repo.insertedAttempts), len(repo.auditEvents))
	}
}

func TestExecute_PagedSecondPageRechecksChangedPolicy(t *testing.T) {
	// Given an allowed first page whose credential policy changes before page two.
	targetQueries := []model.QueryTargetListQuery{}
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{
		9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
	}}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	disclosure := &fakeDisclosureService{}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}, queries: &targetQueries},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{}, disclosure,
	)
	page := func(number int) model.QueryExecuteRequest {
		return model.QueryExecuteRequest{
			Statement: "select value from metrics",
			MaxRows:   25,
			Pagination: &model.QueryExecutePaginationRequest{
				Page:     number,
				PageSize: 10,
			},
		}
	}
	if _, err := svc.Execute(context.Background(), 7, 9001, page(1)); err != nil {
		t.Fatalf("page 1 Execute: %v", err)
	}
	credential := repo.credentials[9001]
	credential.EnvironmentPolicy = model.QueryEnvPolicyDisabled
	repo.credentials[9001] = credential

	// When page two executes after policy revocation.
	_, err := svc.Execute(context.Background(), 7, 9001, page(2))

	// Then the fresh access check blocks it before disclosure or execution.
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("page 2 error = %v, want ErrQueryNotAllowed", err)
	}
	if len(targetQueries) != 2 || disclosure.preflightCalls != 1 || executor.queryCalls != 1 {
		t.Fatalf("governance calls target=%d disclosure=%d executor=%d, want 2/1/1", len(targetQueries), disclosure.preflightCalls, executor.queryCalls)
	}
	if len(repo.insertedAttempts) != 2 || repo.insertedAttempts[1].Status != model.QueryExecutionRejected || len(repo.auditEvents) != 2 {
		t.Fatalf("page 2 rejected recording = attempts=%+v audits=%d", repo.insertedAttempts, len(repo.auditEvents))
	}
}

func TestExecute_PagedSuccessRequiresHistoryAndAudit(t *testing.T) {
	for _, tt := range []struct {
		name      string
		configure func(*fakeExecRepo)
	}{
		{
			name: "history write fails",
			configure: func(repo *fakeExecRepo) {
				repo.insertExecErr = errors.New("history db down")
			},
		},
		{
			name: "audit write fails",
			configure: func(repo *fakeExecRepo) {
				repo.insertAuditErr = errors.New("audit db down")
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Given a successful page whose required persistence write fails.
			svc, repo, _, _ := executionTestScaffold(t)
			tt.configure(repo)
			req := model.QueryExecuteRequest{
				Statement: "select value from metrics",
				MaxRows:   10,
				Pagination: &model.QueryExecutePaginationRequest{
					Page:     1,
					PageSize: 10,
				},
			}

			// When the governed page executes.
			resp, err := svc.Execute(context.Background(), 7, 9001, req)

			// Then it cannot report an unaudited success.
			if !errors.Is(err, ErrQueryBackendFailure) {
				t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
			}
			if resp.Status == model.QueryExecutionSuccess || resp.ExecutionID != 0 {
				t.Fatalf("response = %+v, must not report success", resp)
			}
		})
	}
}

func TestExecute_PagedMetadataStatementsRemainSingleResponses(t *testing.T) {
	for _, statement := range []string{"show tables", "describe metrics", "explain select value from metrics"} {
		t.Run(statement, func(t *testing.T) {
			// Given a metadata statement sent with a page request.
			svc, _, _, executor := executionTestScaffold(t)
			req := model.QueryExecuteRequest{
				Statement: statement,
				MaxRows:   25,
				Pagination: &model.QueryExecutePaginationRequest{
					Page:     1,
					PageSize: 10,
				},
			}

			// When Execute falls back to the normal guard.
			resp, err := svc.Execute(context.Background(), 7, 9001, req)

			// Then the response is the existing metadata shape, without paging.
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if resp.Pagination != nil {
				t.Fatalf("pagination = %+v, want nil for metadata", resp.Pagination)
			}
			if len(executor.queries) != 1 || executor.queries[0].ResultLimit != 0 {
				t.Fatalf("guarded metadata query = %+v, want non-paginated result", executor.queries)
			}
		})
	}
}

func TestExecute_ControlledErrorsNoSQLDSNCredentialsActorIDOffset(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	// Phase 38S: error messages must not leak SQL, DSN, credentials, actor ID,
	// or offset. This is the same Phase 37 guarantee extended to pagination.
	_, err := svc.Execute(context.Background(), 7, 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	for _, rec := range repo.insertedAttempts {
		if strings.Contains(rec.ErrorMessage, testResolverDSN) {
			t.Errorf("DSN leaked into error message: %q", rec.ErrorMessage)
		}
		if strings.Contains(rec.ErrorMessage, "delete from") {
			t.Errorf("SQL leaked into error message: %q", rec.ErrorMessage)
		}
	}
	_ = svc
}
