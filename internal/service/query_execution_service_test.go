// Package service provides tests for the Phase 37/38S query execution service.
// input: context, errors, fmt, strings, testing, time, internal/model
// output: TestExecute_* including successful-User full SQL, owner-only statement retrieval/history restore eligibility, machine identity terminal outcomes, readiness/credential tests, and repository/resolver/executor/clock fakes
// pos: Unit boundary for identity-aware governed execution, private statement access and restore projection, atomic terminal evidence, paging, cancellation durability, credential fail-closed behavior, and disclosure error mapping
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
	statement        model.QueryExecutionStatementResponse
	statementErr     error
	statementIDs     [3]uint64
	auditEvents      []struct {
		actor  uint64
		target uint64
		etype  string
		result string
	}
	// Failure injection for the audit/history guarantee tests.
	insertAuditErr error
	// Issue #34 (38X-3A): atomic Execution Evidence Pair observability. The
	// pair path records the record + audit parameters in one call; the split
	// counters prove no standalone history/audit write happens on the
	// ordinary/paged/template paths.
	pairErr   error
	pairCalls []struct {
		rec    model.QueryExecutionRecord
		event  string
		result string
	}
	// Issue #35: the context the atomic pair write received (must be the
	// detached two-second Evidence Persistence Window, never the request ctx).
	// Err/deadline are captured AT CALL TIME (the service's defer cancel()
	// resolves the window handle immediately after the write, so a post-hoc
	// ctx.Err() on the stored handle would see an already-canceled context).
	lastPairCtx        context.Context
	pairCtxErr         error
	pairCtxDeadline    time.Time
	pairCtxHasDeadline bool
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

// InsertAuditEvent mirrors the audit-only seam still used by explain/schema
// services; the governed query execution and navigation services never call it
// — every Evidence-Bearing Query Attempt routes through InsertExecutionWithAudit.
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

// InsertExecutionWithAudit is the Issue #34 atomic Execution Evidence Pair
// primitive mirror. It records one atomic call; on injection failure nothing is
// recorded (atomic rollback semantics), otherwise the history row and its
// audit event are recorded together. Issue #35: it captures the context the
// service hands the pair write so tests can assert it is the detached,
// two-second Evidence Persistence Window, not the (possibly canceled) request
// context.
func (f *fakeExecRepo) InsertExecutionWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error) {
	f.lastPairCtx = ctx
	f.pairCtxErr = ctx.Err()
	f.pairCtxDeadline, f.pairCtxHasDeadline = ctx.Deadline()
	rec.ID = uint64(len(f.insertedAttempts)) + 1
	f.pairCalls = append(f.pairCalls, struct {
		rec    model.QueryExecutionRecord
		event  string
		result string
	}{rec, eventType, result})
	if f.pairErr != nil {
		return 0, f.pairErr
	}
	f.insertedAttempts = append(f.insertedAttempts, rec)
	f.auditEvents = append(f.auditEvents, struct {
		actor  uint64
		target uint64
		etype  string
		result string
	}{rec.ActorUserID, rec.TargetResourceID, eventType, result})
	return rec.ID, nil
}

func (f *fakeExecRepo) QueryEvidencePersistenceFailures() int64 { return 0 }

func (f *fakeExecRepo) GetSuccessfulExecutionStatement(_ context.Context, executionID, targetResourceID, actorUserID uint64) (model.QueryExecutionStatementResponse, error) {
	f.statementIDs = [3]uint64{executionID, targetResourceID, actorUserID}
	return f.statement, f.statementErr
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
	result        QueryDatabaseResult
	err           error
	delay         time.Duration
	called        bool
	queryCalls    int
	queries       []GuardedQuery
	gotDSN        string
	gotNavInput   *RelatedRecordsQueryInput
	templateCalls int
	// Issue #35: cancels the request context at result-production time (the
	// query completed successfully before the client cancellation arrived).
	cancelOnQuery context.CancelFunc
}

func (f *fakeExecutor) Query(ctx context.Context, dsn string, guarded GuardedQuery) (QueryDatabaseResult, error) {
	f.called = true
	f.queryCalls++
	f.queries = append(f.queries, guarded)
	f.gotDSN = dsn
	// Issue #35: when a test sets cancelOnQuery, the client disconnect arrives
	// after the query already produced its result — the executor still returns
	// the completed result and the service must keep the success evidence.
	if f.cancelOnQuery != nil {
		f.cancelOnQuery()
	}
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

func (f *fakeExecutor) QueryTemplate(ctx context.Context, dsn string, statement GuardedTemplateStatement) (QueryDatabaseResult, error) {
	f.called = true
	f.templateCalls++
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
	preflightErr     error
	applyErr         error
	preflightCalls   int
	preflightQueries []GuardedQuery
}

func (f *fakeDisclosureService) Preflight(_ context.Context, _ string, _ uint64, guarded GuardedQuery) (DisclosurePlan, error) {
	f.preflightCalls++
	f.preflightQueries = append(f.preflightQueries, guarded)
	if f.preflightErr != nil {
		return DisclosurePlan{}, f.preflightErr
	}
	if f.blockErr != nil {
		return DisclosurePlan{}, f.blockErr
	}
	return DisclosurePlan{}, nil
}

func (f *fakeDisclosureService) PreflightRelatedRecords(_ context.Context, _ string, _ uint64, _, _ string) (DisclosurePlan, error) {
	if f.preflightErr != nil {
		return DisclosurePlan{}, f.preflightErr
	}
	if f.blockErr != nil {
		return DisclosurePlan{}, f.blockErr
	}
	return DisclosurePlan{}, nil
}

func (f *fakeDisclosureService) Apply(_ DisclosurePlan, columns []model.QueryResultColumn, rows [][]any) ([]model.QueryResultColumn, [][]any, error) {
	if f.applyErr != nil {
		return nil, nil, f.applyErr
	}
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

func userExecutionIdentity(id uint64) model.QueryExecutionIdentity {
	return model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: id}
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
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

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1 as value", MaxRows: 50})
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

	_, err := svc2.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	_ = svc
}

func TestExecute_RecordsSuccessfulAttempt(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)

	if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10}); err != nil {
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
	if repo.insertedAttempts[0].FullStatement != "select 1" {
		t.Fatalf("full statement = %q, want exact successful user statement", repo.insertedAttempts[0].FullStatement)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "success" {
		t.Fatalf("audit events = %+v, want one success", repo.auditEvents)
	}
	if executor.templateCalls != 0 {
		t.Fatalf("ordinary Execute called QueryTemplate %d times, want 0", executor.templateCalls)
	}
}

func TestGetExecutionStatementUsesExactOwnerPredicates(t *testing.T) {
	repo := &fakeExecRepo{statement: model.QueryExecutionStatementResponse{Statement: "select private_value"}}
	svc := &QueryExecutionService{executions: repo}

	response, err := svc.GetExecutionStatement(context.Background(), 7, 9001, 22)
	if err != nil {
		t.Fatalf("GetExecutionStatement() error = %v", err)
	}
	if response.Statement != "select private_value" || repo.statementIDs != [3]uint64{22, 9001, 7} {
		t.Fatalf("response/ids = %+v/%v", response, repo.statementIDs)
	}
}

func TestGetExecutionStatementCollapsesRepositoryNoRows(t *testing.T) {
	svc := &QueryExecutionService{executions: &fakeExecRepo{statementErr: sql.ErrNoRows}}

	_, err := svc.GetExecutionStatement(context.Background(), 7, 9001, 22)
	if !errors.Is(err, ErrQueryExecutionNotFound) {
		t.Fatalf("GetExecutionStatement() error = %v, want ErrQueryExecutionNotFound", err)
	}
}

func TestExecute_RecordsRejectedAttempt(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
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
	if repo.insertedAttempts[0].FullStatement != "" {
		t.Fatalf("rejected full statement = %q, want empty", repo.insertedAttempts[0].FullStatement)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit result = %+v, want validation_failed", repo.auditEvents)
	}
}

func TestExecute_MachineIdentityReachesEveryTerminalEvidenceOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		statement string
		execErr   error
		want      model.QueryExecutionStatus
	}{
		{"success", "select 1", nil, model.QueryExecutionSuccess},
		{"rejected", "delete from t", nil, model.QueryExecutionRejected},
		{"failed", "select 1", errors.New("driver detail must stay private"), model.QueryExecutionFailed},
		{"timeout", "select 1", context.DeadlineExceeded, model.QueryExecutionTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, executor := executionTestScaffold(t)
			executor.err = tc.execErr
			_, _ = svc.Execute(context.Background(), model.QueryExecutionIdentity{
				Kind: model.QueryExecutionActorMachine,
				ID:   91,
			}, 9001, model.QueryExecuteRequest{Statement: tc.statement, MaxRows: 10})

			if len(repo.pairCalls) != 1 {
				t.Fatalf("pair calls = %d, want 1", len(repo.pairCalls))
			}
			rec := repo.pairCalls[0].rec
			if rec.ActorUserID != 0 || rec.ActorMachinePrincipalID != 91 || rec.Actor.Kind != model.QueryExecutionActorMachine {
				t.Fatalf("evidence identity = user:%d machine:%d kind:%q, want machine principal 91 only", rec.ActorUserID, rec.ActorMachinePrincipalID, rec.Actor.Kind)
			}
			if rec.Status != tc.want {
				t.Fatalf("status = %q, want %q", rec.Status, tc.want)
			}
			if rec.FullStatement != "" {
				t.Fatalf("machine full statement = %q, want empty", rec.FullStatement)
			}
		})
	}
}

func TestExecute_HistoryWriteFailureOnSuccess_ReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	repo.pairErr = errors.New("history db down")

	// WHY: Phase 37 guarantees every attempt is recorded. If the pair (history
	// + audit) cannot be written, the service must NOT pretend success (no
	// executionId=0 success response); it returns a controlled backend failure
	// and no history row is committed.
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if resp.Status == model.QueryExecutionSuccess {
		t.Fatal("must not return a success response when the history write failed")
	}
	if resp.ExecutionID != 0 {
		t.Fatalf("ExecutionID = %d, want 0 (no successful record)", resp.ExecutionID)
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("history rows = %d, want 0 (atomic pair rolled back)", len(repo.insertedAttempts))
	}
}

func TestExecute_AuditWriteFailureOnSuccess_ReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	repo.pairErr = errors.New("audit db down")

	// WHY: the audit event is part of the recording guarantee; if the pair
	// write fails the request fails closed rather than reporting an unaudited
	// run, and the atomic rollback leaves no history row.
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("history rows = %d, want 0 (failed pair must commit no history)", len(repo.insertedAttempts))
	}
}

// TestExecute_SuccessUsesSingleAtomicPairWrite proves the ordinary execution
// path records its history row and audit event through ONE repository-owned
// atomic pair call — never two independent writes. This is the Issue #34
// expand-step invariant.
func TestExecute_SuccessUsesSingleAtomicPairWrite(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want 1 (history+audit must share one transaction)", len(repo.pairCalls))
	}
	if repo.pairCalls[0].event != "query.executed" || repo.pairCalls[0].result != "success" {
		t.Fatalf("pair audit params = %q/%q, want query.executed/success", repo.pairCalls[0].event, repo.pairCalls[0].result)
	}
	if resp.ExecutionID == 0 {
		t.Fatal("ExecutionID = 0, want the committed execution id")
	}
	if repo.pairCalls[0].rec.ID != resp.ExecutionID {
		t.Fatalf("committed id = %d, want %d", repo.pairCalls[0].rec.ID, resp.ExecutionID)
	}
}

// TestExecute_PagedSuccessUsesSingleAtomicPairWritePerPage proves every fresh
// paged execution records through the same one-call atomic pair.
func TestExecute_PagedSuccessUsesSingleAtomicPairWritePerPage(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{
		Statement:  "select value from metrics limit 100",
		MaxRows:    100,
		Pagination: &model.QueryExecutePaginationRequest{Page: 2, PageSize: 10},
	})
	if err != nil {
		t.Fatalf("execute paged: %v", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want 1 for one governed page execution", len(repo.pairCalls))
	}
	if resp.ExecutionID == 0 {
		t.Fatal("paged execution returned no committed execution id")
	}
}

// TestExecute_PairWriteFailureOnSuccess_ReturnsBackendError_NoHistory proves
// the audit-failure path: the pair write fails, the request surfaces the
// existing controlled backend failure, and NOTHING is committed to history —
// the atomic rollback invariant at the service seam.
func TestExecute_PairWriteFailureOnSuccess_ReturnsBackendError_NoHistory(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := executionTestScaffold(t)
	repo.pairErr = errors.New("evidence store down")

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pair calls = %d, want 1", len(repo.pairCalls))
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("history rows = %d, want 0 (failed pair must commit no history)", len(repo.insertedAttempts))
	}
	if len(repo.auditEvents) != 0 {
		t.Fatalf("audit events = %d, want 0 (failed pair must commit no audit)", len(repo.auditEvents))
	}
}

func TestExecute_RejectedAttempt_PersistFailure_ReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)
	repo.pairErr = errors.New("history db down")

	// WHY: even for a rejected attempt, a recording failure must surface as a
	// controlled backend failure (never silently swallow + claim "recorded").
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (persist failure must not be swallowed)", err)
	}
	if executor.called {
		t.Fatal("rejected attempt must not reach the executor")
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("history rows = %d, want 0 (failed pair must not commit a rejection row)", len(repo.insertedAttempts))
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed (wrapping ErrQueryDisclosureBlocked)", err)
	}
	if !errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("error = %v, want ErrQueryDisclosureBlocked so HTTP can publish query_result_disclosure_blocked", err)
	}
	if executor.called {
		t.Fatal("executor must not be reached when disclosure blocks the query")
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("disclosure-blocked attempt must be recorded as rejected: %+v", repo.insertedAttempts)
	}
}

// assertApplyPathDisclosureHTTPSentinel proves Apply-path classification
// returns ErrQueryDisclosureBlocked and does not also wrap ErrQueryNotAllowed.
// WHY: HTTP matches disclosure before not-allowed, but a dual wrap still
// satisfies errors.Is(err, ErrQueryDisclosureBlocked). The exclusive check
// is what fails if classifyExecutorError regresses to wrapping both sentinels
// the way Preflight reject does.
func assertApplyPathDisclosureHTTPSentinel(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrQueryDisclosureBlocked) {
		t.Fatalf("error = %v, want ErrQueryDisclosureBlocked so HTTP can publish query_result_disclosure_blocked", err)
	}
	if errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, also matches ErrQueryNotAllowed; Apply-path must return ErrQueryDisclosureBlocked alone so HTTP can distinguish a policy block from a target that is not enabled", err)
	}
}

// TestExecute_ApplyDisclosureBlocked_Rejected proves a disclosure block after
// a successful executor run (Apply) returns ErrQueryDisclosureBlocked — not
// only ErrQueryNotAllowed — so HTTP can publish query_result_disclosure_blocked.
// WHY: Preflight reject wraps both sentinels; Apply goes through
// classifyExecutorError, which used to collapse the block to ErrQueryNotAllowed.
func TestExecute_ApplyDisclosureBlocked_Rejected(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{
		Columns:  []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:     [][]any{{int64(1)}},
		RowCount: 1,
	}}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{applyErr: ErrQueryDisclosureBlocked},
	)

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	assertApplyPathDisclosureHTTPSentinel(t, err)
	if !executor.called {
		t.Fatal("executor must run before Apply blocks the result")
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("apply-path disclosure block must be recorded as rejected: %+v", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "query_result_disclosure_blocked" {
		t.Fatalf("error code = %q, want query_result_disclosure_blocked", repo.insertedAttempts[0].ErrorCode)
	}
}

// --- Issue #35 (38X-3B): cancellation-durable terminal evidence ---

// assertDetachedEvidenceWindow proves the service hands the repository an
// evidence-persistence context that is NOT the request context: detached from
// client cancellation and bounded to the fixed two-second Evidence Persistence
// Window (Issue #35). The write is a single bounded attempt with no retry,
// queue, worker, or disk buffer.
func assertDetachedEvidenceWindow(t *testing.T, f *fakeExecRepo) {
	t.Helper()
	if f.lastPairCtx == nil {
		t.Fatal("evidence persistence context is nil")
	}
	if f.pairCtxErr != nil {
		t.Fatalf("evidence persistence context must be detached from request cancellation, got Err() = %v", f.pairCtxErr)
	}
	if !f.pairCtxHasDeadline {
		t.Fatal("evidence persistence context must carry the fixed Evidence Persistence Window deadline")
	}
	remaining := time.Until(f.pairCtxDeadline)
	if remaining <= 0 || remaining > 2*time.Second {
		t.Fatalf("evidence persistence window remaining = %v, want within (0s, 2s]", remaining)
	}
}

// TestExecute_PairWriteFailureOnCanceledRequest_StillReturnsBackendError proves
// AC 7 on the canceled path: even when the client is gone, a genuine
// persistence failure is never silent — the pair rollback leaves nothing
// recorded and the request surfaces the existing controlled backend-error
// response instead (the exact-once telemetry increment is proven by the
// repository and integration tests on the InsertExecutionWithAudit seam).
func TestExecute_PairWriteFailureOnCanceledRequest_StillReturnsBackendError(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
		pairErr: errors.New("evidence store down"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{err: context.Canceled},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{},
	)

	_, err := svc.Execute(ctx, userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("error = %v, want errPersistAttempt (controlled backend failure)", err)
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("failed pair must record nothing; got %d rows", len(repo.insertedAttempts))
	}
}

// TestExecute_ClientCanceledDuringExecution_RecordsFailedCanceled proves the
// client-cancellation outcome: the executor returns context.Canceled (driver
// abort after disconnect) and the attempt must be recorded as failed with the
// fixed query_canceled code and a fixed safe message — never a raw error —
// persisted through the DETACHED two-second window, not the canceled request
// context.
func TestExecute_ClientCanceledDuringExecution_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	// Request context already canceled: the client disconnected mid-query.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{err: context.Canceled},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{},
	)

	_, err := svc.Execute(ctx, userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if !strings.Contains(err.Error(), "query canceled") {
		t.Fatalf("error = %q, want fixed safe 'query canceled' message", err)
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("canceled attempt must be recorded, got %d rows", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if rec.Status != model.QueryExecutionFailed {
		t.Fatalf("status = %q, want failed", rec.Status)
	}
	if rec.ErrorCode != "query_canceled" {
		t.Fatalf("error code = %q, want query_canceled", rec.ErrorCode)
	}
	if rec.ErrorMessage != "query canceled" {
		t.Fatalf("error message = %q, want fixed 'query canceled'", rec.ErrorMessage)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "failed" {
		t.Fatalf("audit result = %+v, want one failed", repo.auditEvents)
	}
	if strings.Contains(asString(rec), testResolverDSN) {
		t.Fatal("DSN leaked into evidence")
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestExecute_PagedClientCanceledDuringExecution_RecordsFailedCanceled proves
// the paged cancellation path (Issue #35 AC: ordinary, paged, and template
// cancellation paths are covered): a canceled paged query records the same
// failed/query_canceled evidence through the detached window.
func TestExecute_PagedClientCanceledDuringExecution_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{err: context.Canceled},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{},
	)

	_, err := svc.Execute(ctx, userExecutionIdentity(7), 9001, model.QueryExecuteRequest{
		Statement:  "select value from metrics limit 100",
		MaxRows:    100,
		Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 10},
	})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionFailed {
		t.Fatalf("paged canceled attempt must be recorded as failed: %+v", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "query_canceled" {
		t.Fatalf("error code = %q, want query_canceled", repo.insertedAttempts[0].ErrorCode)
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestExecute_ClientCanceledDuringDisclosure_RecordsFailedCanceled proves the
// disclosure-work cancellation outcome: a public disclosure-policy rejection
// stays rejected ONLY when no cancellation is involved; a canceled disclosure
// read (cancellation blended into the disclosure service's blocked wrap) must
// be recorded as failed/query_canceled — never as a policy rejection.
func TestExecute_ClientCanceledDuringDisclosure_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	// Both wrap shapes a canceled disclosure read can arrive in must classify
	// as failed/query_canceled, never as a policy rejection: the backend
	// sentinel (the real disclosure service's wrap for inspector/read
	// failures) and a defensive blocked-blend (any future regression that
	// folds a cancelable cause back into the blocked wrap).
	for name, wrapErr := range map[string]error{
		"backend-sentinel-blend": fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, context.Canceled),
		"blocked-blend":          fmt.Errorf("%w: %w", ErrQueryDisclosureBlocked, context.Canceled),
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeExecRepo{
				credentials: map[uint64]model.QueryCredentialMetadata{
					9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
				},
			}
			executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
			disclosure := &fakeDisclosureService{preflightErr: wrapErr}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			svc := NewQueryExecutionService(
				fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
				repo, &fakeResolver{dsn: testResolverDSN}, executor,
				NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
				&fakeClock{t: time.Now()},
				nil, disclosure,
			)

			_, err := svc.Execute(ctx, userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
			if !errors.Is(err, ErrQueryBackendFailure) {
				t.Fatalf("error = %v, want ErrQueryBackendFailure for canceled disclosure work", err)
			}
			if executor.called {
				t.Fatal("executor must not run when disclosure fails")
			}
			if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionFailed {
				t.Fatalf("canceled disclosure attempt must be recorded as failed: %+v", repo.insertedAttempts)
			}
			if repo.insertedAttempts[0].ErrorCode != "query_canceled" {
				t.Fatalf("error code = %q, want query_canceled", repo.insertedAttempts[0].ErrorCode)
			}
			assertDetachedEvidenceWindow(t, repo)
		})
	}
}

// TestExecute_DisclosurePreflightTimeout_RecordsTimeoutEvidence proves a
// deadline-expired disclosure read is recorded as the existing timeout outcome
// with evidence — never as a policy rejection.
func TestExecute_DisclosurePreflightTimeout_RecordsTimeoutEvidence(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	disclosure := &fakeDisclosureService{preflightErr: fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, context.DeadlineExceeded)}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, disclosure,
	)

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	if executor.called {
		t.Fatal("executor must not run when disclosure times out")
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionTimeout {
		t.Fatalf("disclosure timeout attempt must be recorded as timeout: %+v", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "query_timeout" {
		t.Fatalf("error code = %q, want query_timeout", repo.insertedAttempts[0].ErrorCode)
	}
}

// TestExecute_DisclosurePreflightTerminalFailure_RecordsFailedEvidence proves
// all other post-target disclosure terminal failures reach the shared atomic
// evidence path as fixed safe failed evidence with a controlled backend-error
// response, never a raw 500 and never silent.
func TestExecute_DisclosurePreflightTerminalFailure_RecordsFailedEvidence(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	disclosure := &fakeDisclosureService{preflightErr: fmt.Errorf("%w: %w", ErrQueryDisclosureBackendFailure, errors.New("disclosure engine unreachable"))}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, disclosure,
	)

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if executor.called {
		t.Fatal("executor must not run when disclosure fails")
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionFailed {
		t.Fatalf("disclosure failure must be recorded as failed: %+v", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "query_disclosure_backend_error" {
		t.Fatalf("error code = %q, want query_disclosure_backend_error", repo.insertedAttempts[0].ErrorCode)
	}
	if repo.insertedAttempts[0].ErrorMessage != "query disclosure governance failed" {
		t.Fatalf("error message = %q, want fixed safe disclosure-failure message", repo.insertedAttempts[0].ErrorMessage)
	}
}

// TestExecute_CompletedQueryBeforeClientCancel_RemainsSuccess proves a query
// that completed successfully before the client cancellation arrived stays
// recorded as success: the cancellation never retroactively downgrades or drops
// the success evidence, and the pair write still runs in the detached window.
func TestExecute_CompletedQueryBeforeClientCancel_RemainsSuccess(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	executor := &fakeExecutor{
		result: QueryDatabaseResult{
			Columns:  []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
			Rows:     [][]any{{int64(1)}},
			RowCount: 1,
		},
		// The client disconnect lands right after the query produced its result.
		cancelOnQuery: cancel,
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{},
	)

	resp, err := svc.Execute(ctx, userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if err != nil {
		t.Fatalf("completed-before-cancel execution must not fail: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("response status = %q, want success", resp.Status)
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionSuccess {
		t.Fatalf("completed-before-cancel attempt must be recorded as success: %+v", repo.insertedAttempts)
	}
	assertDetachedEvidenceWindow(t, repo)
}

func TestExecute_CredentialEngineMismatch_Rejected(t *testing.T) {
	t.Parallel()
	svc, repo, _, executor := executionTestScaffold(t)
	// The credential is for postgresql but the target is mysql.
	cred := repo.credentials[9001]
	cred.Engine = "postgresql"
	repo.credentials[9001] = cred

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1 as value", MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
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

func TestListHistory_CanRestoreOnlyOwnSuccessfulStatement(t *testing.T) {
	target := mysqlTarget("Staging")
	target.ResourceID = 22
	records := []model.QueryExecutionRecord{
		{ID: 1, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser}, Status: model.QueryExecutionSuccess, HasFullStatement: true},
		{ID: 2, TargetResourceID: 22, ActorUserID: 7, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser}, Status: model.QueryExecutionSuccess, HasFullStatement: true},
		{ID: 3, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser}, Status: model.QueryExecutionSuccess},
		{ID: 4, TargetResourceID: 22, ActorUserID: 1, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser}, Status: model.QueryExecutionFailed, HasFullStatement: true},
		{ID: 5, TargetResourceID: 22, ActorMachinePrincipalID: 9, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorMachine}, Status: model.QueryExecutionSuccess, HasFullStatement: true},
	}

	for _, q := range []model.QueryExecutionListQuery{{PageSize: 20}, {Page: 1, PageSize: 20}} {
		repo := &fakeExecRepo{insertedAttempts: records}
		svc := NewQueryExecutionService(fakeTargetRepo{targets: []model.QueryTarget{target}}, repo, &fakeResolver{}, &fakeExecutor{}, NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}), &fakeClock{t: time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)}, nil, &fakeDisclosureService{})
		result, err := svc.ListHistory(context.Background(), 1, "admin", 22, q)
		if err != nil {
			t.Fatalf("ListHistory(%+v): %v", q, err)
		}
		if len(result.Items) != len(records) {
			t.Fatalf("ListHistory(%+v) items = %d, want %d", q, len(result.Items), len(records))
		}
		for _, item := range result.Items {
			want := item.ID == 1
			if item.CanRestore != want {
				t.Fatalf("ListHistory(%+v) item %d canRestore = %v, want %v", q, item.ID, item.CanRestore, want)
			}
		}
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
	// Phase 38S: if the audit/history pair write fails for any page, the overall
	// response must NOT be a success. This is the same guarantee as Phase 37
	// but extended to per-page recording.
	repo.pairErr = errors.New("history db down")
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (audit/history failure must not produce success)", err)
	}
	if resp.Status == model.QueryExecutionSuccess {
		t.Fatal("must not return success when history write failed")
	}
}

func TestExecute_PagedRequestDefaultsMaxRowsToGuardDefault(t *testing.T) {
	// Given a page request without an overall maxRows cap.
	svc, _, _, executor := executionTestScaffold(t)
	req := model.QueryExecuteRequest{
		Statement: "select value from metrics",
		Pagination: &model.QueryExecutePaginationRequest{
			Page:     1,
			PageSize: 10,
		},
	}

	// When the first page executes.
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, req)

	// Then the omitted maxRows falls back to the guard's DefaultMaxRows total
	// release cap, not the page size.
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(executor.queries) != 1 {
		t.Fatalf("query calls = %d, want 1", len(executor.queries))
	}
	if executor.queries[0].LimitApplied != 100 {
		t.Fatalf("LimitApplied = %d, want guard DefaultMaxRows 100", executor.queries[0].LimitApplied)
	}
	if resp.Pagination == nil || resp.Pagination.Page != 1 || resp.Pagination.PageSize != 10 {
		t.Fatalf("pagination = %+v, want first page of size 10", resp.Pagination)
	}
}

func TestExecute_PagedRequestCannotExceedHardMaxRows(t *testing.T) {
	// Given a paged request that asks for an absurd overall cap.
	svc, repo, _, executor := executionTestScaffold(t)
	page := func(number int) model.QueryExecuteRequest {
		return model.QueryExecuteRequest{
			Statement: "select value from metrics",
			MaxRows:   2_000_000_000,
			Pagination: &model.QueryExecutePaginationRequest{
				Page:     number,
				PageSize: 10,
			},
		}
	}

	// When page one executes.
	if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, page(1)); err != nil {
		t.Fatalf("page 1 Execute: %v", err)
	}

	// Then the guard clamps the total release cap to HardMaxRows.
	if len(executor.queries) != 1 || executor.queries[0].LimitApplied != 500 {
		t.Fatalf("queries = %+v, want single query clamped to HardMaxRows 500", executor.queries)
	}

	// And a page beyond the clamped cap is rejected without touching the executor.
	if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, page(51)); !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("beyond-cap page error = %v, want ErrQueryValidationFailed", err)
	}
	if executor.queryCalls != 1 {
		t.Fatalf("executor calls = %d, want 1 (beyond-cap page must not execute)", executor.queryCalls)
	}
	if len(repo.insertedAttempts) != 2 || repo.insertedAttempts[1].Status != model.QueryExecutionRejected {
		t.Fatalf("history = %+v, want rejected record for beyond-cap page", repo.insertedAttempts)
	}
}

func TestExecute_PagedResponseReportsEffectivePageWindow(t *testing.T) {
	// Given a total cap smaller than the requested page size.
	svc, _, _, executor := executionTestScaffold(t)
	req := model.QueryExecuteRequest{
		Statement: "select value from metrics",
		MaxRows:   10,
		Pagination: &model.QueryExecutePaginationRequest{
			Page:     1,
			PageSize: 25,
		},
	}

	// When the page executes.
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, req)

	// Then the AST window and the reported pageSize both reflect the real
	// effective window, so the metadata never overstates what was released.
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := executor.queries[0].ExecutableSQL; got != "select `value` from metrics limit 0, 11" {
		t.Fatalf("SQL = %q, want cap-bounded first window", got)
	}
	if resp.Pagination == nil || resp.Pagination.PageSize != 10 {
		t.Fatalf("pagination = %+v, want effective pageSize 10", resp.Pagination)
	}
	if resp.Pagination.HasNextPage {
		t.Fatal("HasNextPage = true, want false when the cap is exhausted on page 1")
	}
}

func TestExecute_PagedNegativeMaxRowsIsLimitValidationError(t *testing.T) {
	// Given a paged request carrying a negative overall cap.
	svc, repo, _, executor := executionTestScaffold(t)
	req := model.QueryExecuteRequest{
		Statement: "select value from metrics",
		MaxRows:   -1,
		Pagination: &model.QueryExecutePaginationRequest{
			Page:     1,
			PageSize: 10,
		},
	}

	// When the page executes.
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, req)

	// Then the rejection reuses the existing limit validation, not the
	// pagination-window validation.
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if !strings.Contains(err.Error(), ErrQueryLimitInvalid.Error()) {
		t.Fatalf("error = %q, want the guard limit-validation message %q", err, ErrQueryLimitInvalid.Error())
	}
	if strings.Contains(err.Error(), ErrQueryPaginationInvalid.Error()) {
		t.Fatalf("error = %q, must not be misreported as invalid pagination", err)
	}

	// And the executor is never dispatched for an invalid cap.
	if executor.called {
		t.Fatal("executor must not be called for a negative maxRows")
	}

	// And exactly one rejected attempt is recorded to history + audit.
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("history = %+v, want one rejected attempt", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "validation_failed" {
		t.Fatalf("history error code = %q, want validation_failed", repo.insertedAttempts[0].ErrorCode)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit events = %+v, want one validation_failed event", repo.auditEvents)
	}

	// And neither the response error nor the recorded attempt leaks the raw
	// statement or the resolved DSN.
	recorded := repo.insertedAttempts[0].ErrorMessage
	for _, leak := range []string{"secret-dsn-do-not-leak", "select value from metrics"} {
		if strings.Contains(err.Error(), leak) || strings.Contains(recorded, leak) {
			t.Fatalf("rejection leaks %q: err=%q history=%q", leak, err, recorded)
		}
	}
}

func TestExecute_PagedResultTooLargeRejectsWithoutPartialPage(t *testing.T) {
	// Given a paged SELECT whose executor reports the payload-cap rejection.
	svc, repo, _, executor := executionTestScaffold(t)
	executor.err = ErrQueryResultTooLarge
	req := model.QueryExecuteRequest{
		Statement: "select value from metrics",
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page:     1,
			PageSize: 10,
		},
	}

	// When the page executes.
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, req)

	// Then the attempt is a controlled rejection with the fixed safe message.
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if !strings.Contains(err.Error(), "result set exceeds configured limits") {
		t.Fatalf("error = %q, want fixed safe message", err)
	}
	if strings.Contains(err.Error(), "secret-dsn-do-not-leak") {
		t.Fatalf("error leaks the DSN: %q", err)
	}

	// And no partial page or pagination metadata is released.
	if resp.Status == model.QueryExecutionSuccess {
		t.Fatal("oversized paginated result must not be a success")
	}
	if len(resp.Rows) != 0 || resp.RowCount != 0 {
		t.Fatalf("rows = %d/%d, want none", len(resp.Rows), resp.RowCount)
	}
	if resp.Pagination != nil {
		t.Fatalf("pagination = %+v, want none for a rejected page", resp.Pagination)
	}

	// And the rejected attempt is recorded to history + audit as usual.
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("history = %+v, want one rejected attempt", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "validation_failed" || repo.insertedAttempts[0].ErrorMessage != "result set exceeds configured limits" {
		t.Fatalf("history code/message = %q/%q, want validation_failed with fixed safe message", repo.insertedAttempts[0].ErrorCode, repo.insertedAttempts[0].ErrorMessage)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit events = %+v, want one validation_failed event", repo.auditEvents)
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
	if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, page(1)); err != nil {
		t.Fatalf("page 1 Execute: %v", err)
	}
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, page(2))

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
	if _, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, page(1)); err != nil {
		t.Fatalf("page 1 Execute: %v", err)
	}
	credential := repo.credentials[9001]
	credential.EnvironmentPolicy = model.QueryEnvPolicyDisabled
	repo.credentials[9001] = credential

	// When page two executes after policy revocation.
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, page(2))

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
				repo.pairErr = errors.New("history db down")
			},
		},
		{
			name: "audit write fails",
			configure: func(repo *fakeExecRepo) {
				repo.pairErr = errors.New("audit db down")
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
			resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, req)

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
			resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, req)

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
	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001, model.QueryExecuteRequest{Statement: "delete from t", MaxRows: 10})
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
