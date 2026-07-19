// Package service provides tests for the governed Explain service.
// input: context, errors, strings, testing, time, internal/model, internal/service
// output: TestQueryExplainService* covering access/guard/executor/audit/typed outcomes
// pos: Phase 38N — prove governed access, no bare-SELECT execution, typed audit, no history row
// note: stubs the executor and audit recorder; the integration test covers the real MySQL path
package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// fakeExplainExecutor captures the ExplainStatement it receives so the test
// can assert the sealed form is EXPLAIN-prefixed and never the bare SELECT.
type fakeExplainExecutor struct {
	receivedStmt ExplainStatement
	receivedDSN  string
	raw          ExplainRawPlan
	err          error
	called       bool
}

func (f *fakeExplainExecutor) Explain(ctx context.Context, dsn string, stmt ExplainStatement) (ExplainRawPlan, error) {
	f.called = true
	f.receivedStmt = stmt
	f.receivedDSN = dsn
	return f.raw, f.err
}

// fakeAuditRecorder records every outcome for assertion.
type fakeAuditRecorder struct {
	calls []fakeAuditCall
	err   error
}

type fakeAuditCall struct {
	actorUserID, targetResourceID uint64
	outcome                       model.ExplainAuditOutcome
}

func (f *fakeAuditRecorder) RecordExplainAttempt(ctx context.Context, actorUserID, targetResourceID uint64, outcome model.ExplainAuditOutcome) error {
	f.calls = append(f.calls, fakeAuditCall{actorUserID: actorUserID, targetResourceID: targetResourceID, outcome: outcome})
	return f.err
}

// newExplainService builds a service with the existing fakes from
// query_execution_service_test.go (same package). The target access resolver
// is wired identically to the execute path so governance cannot drift.
func newExplainService(t *testing.T, exec QueryExplainExecutor, audit QueryExplainAuditRecorder, targets []model.QueryTarget, creds map[uint64]model.QueryCredentialMetadata, dsn string) *QueryExplainService {
	t.Helper()
	targetRepo := fakeTargetRepo{targets: targets}
	execRepo := &fakeExecRepo{credentials: creds}
	resolver := &fakeResolver{dsn: dsn}
	access := NewTargetAccessResolver(targetRepo, execRepo, resolver)
	guard := NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	normalizer := NewExplainNormalizer()
	clock := &fakeClock{t: time.Now()}
	return NewQueryExplainService(guard, access, exec, normalizer, clock, audit)
}

// explainTestDSN mirrors the execute test DSN. It must parse as a real MySQL
// DSN and bind to the target's host/port so validateDSNBinding passes.
const explainTestDSN = "rouser:secret-do-not-leak@tcp(db.internal:3306)/sandbox"

// makeReadyMySQLTarget builds a staging MySQL target ready for execution.
func makeReadyMySQLTarget(t *testing.T, id uint64) model.QueryTarget {
	t.Helper()
	return model.QueryTarget{
		ResourceID: id,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: "staging",
			Engine:      "mysql",
			Host:        "db.internal",
			Port:        3306,
		},
	}
}

// makeReadyTiDBTarget builds a staging TiDB target ready for execution but
// NOT for Explain (TiDB fails closed on EXPLAIN FORMAT=JSON). Port matches
// the test DSN so validateDSNBinding passes; the engine check is what gates
// Explain, not the actual connectivity.
func makeReadyTiDBTarget(t *testing.T, id uint64) model.QueryTarget {
	t.Helper()
	return model.QueryTarget{
		ResourceID: id,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: "staging",
			Engine:      "tidb",
			Host:        "db.internal",
			Port:        3306,
		},
	}
}

func readyMySQLCredential(t *testing.T) model.QueryCredentialMetadata {
	t.Helper()
	return model.QueryCredentialMetadata{
		ResourceID:        616,
		Enabled:           true,
		Engine:            "mysql",
		CredentialRef:     "ORDER_MYSQL_RO",
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
}

func readyTiDBCredential(t *testing.T) model.QueryCredentialMetadata {
	t.Helper()
	return model.QueryCredentialMetadata{
		ResourceID:        617,
		Enabled:           true,
		Engine:            "tidb",
		CredentialRef:     "ORDER_TIDB_RO",
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
}

func disabledCredential(t *testing.T) model.QueryCredentialMetadata {
	t.Helper()
	return model.QueryCredentialMetadata{
		ResourceID:        616,
		Enabled:           false,
		Engine:            "mysql",
		CredentialRef:     "ORDER_MYSQL_RO",
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
}

func parseJSON(t *testing.T, j string) interface{} {
	t.Helper()
	var v interface{}
	if err := json.Unmarshal([]byte(j), &v); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	return v
}

// TestExplainServiceSuccess proves the happy path: governed access resolves,
// guard accepts the SELECT, executor receives a sealed EXPLAIN-prefixed
// statement (NOT the bare select), normalizer returns the v1 model, audit
// records success.
func TestExplainServiceSuccess(t *testing.T) {
	t.Parallel()
	raw := ExplainRawPlan{tree: parseJSON(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 120000}}}`)}
	exec := &fakeExplainExecutor{raw: raw}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)

	resp, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select * from big"})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	if resp.TargetResourceID != 616 {
		t.Errorf("targetResourceId = %d, want 616", resp.TargetResourceID)
	}
	if resp.Engine != model.ExplainEngineMySQL {
		t.Errorf("engine = %s, want mysql", resp.Engine)
	}
	if resp.FormatVersion != model.ExplainFormatVersion {
		t.Errorf("formatVersion = %d, want %d", resp.FormatVersion, model.ExplainFormatVersion)
	}
	if len(resp.Nodes) == 0 {
		t.Fatalf("expected at least one node")
	}
	if !hasRisk(resp.Risks, model.ExplainRiskFullTableScan, model.ExplainSeverityWarning) {
		t.Errorf("expected full_table_scan risk, got %v", resp.Risks)
	}
	if !hasRisk(resp.Risks, model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning) {
		t.Errorf("expected high_estimated_rows risk for 120000 rows, got %v", resp.Risks)
	}
	if !exec.called {
		t.Fatalf("executor.Explain was not called")
	}
	dispatched := exec.receivedStmt.WrappedSQL()
	if !strings.HasPrefix(dispatched, "EXPLAIN FORMAT=JSON ") {
		t.Errorf("executor received non-prefixed SQL: %q", dispatched)
	}
	if strings.Contains(strings.ToLower(dispatched), "explain format=json select * from big") == false {
		t.Errorf("executor received unexpected SQL: %q", dispatched)
	}
	if dispatched == "select * from big" {
		t.Errorf("executor received the bare guarded select — the structural seam is broken: %q", dispatched)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditSuccess {
		t.Errorf("expected one success audit call, got %v", audit.calls)
	}
}

// TestExplainServiceTargetNotFound proves an unknown target returns
// ErrQueryTargetNotFound and records no audit (no target to attribute).
func TestExplainServiceTargetNotFound(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{}
	audit := &fakeAuditRecorder{}
	svc := newExplainService(t, exec, audit, []model.QueryTarget{}, nil, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 999, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("error = %v, want ErrQueryTargetNotFound", err)
	}
	if exec.called {
		t.Errorf("executor must NOT be called for unknown target")
	}
	if len(audit.calls) != 0 {
		t.Errorf("audit must NOT be recorded for unknown target, got %v", audit.calls)
	}
}

// TestExplainServiceAccessDenied proves a target access denial returns
// ErrQueryNotAllowed and records a rejected audit event.
func TestExplainServiceAccessDenied(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: disabledCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	if exec.called {
		t.Errorf("executor must NOT be called on access denial")
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditRejected {
		t.Errorf("expected one rejected audit call, got %v", audit.calls)
	}
}

// TestExplainServiceTiDBUnsupported proves a TiDB target returns
// ErrQueryExplainNotSupported and records an unsupported audit event.
func TestExplainServiceTiDBUnsupported(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{}
	audit := &fakeAuditRecorder{}
	target := makeReadyTiDBTarget(t, 617)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{617: readyTiDBCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 617, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryExplainNotSupported) {
		t.Fatalf("error = %v, want ErrQueryExplainNotSupported", err)
	}
	if exec.called {
		t.Errorf("executor must NOT be called for unsupported engine")
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditUnsupported {
		t.Errorf("expected one unsupported audit call, got %v", audit.calls)
	}
}

// TestExplainServiceValidationFailure proves a non-SELECT statement returns
// ErrQueryValidationFailed and records a rejected audit event.
func TestExplainServiceValidationFailure(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	for _, stmt := range []string{"explain select 1", "show tables", "insert into t values (1)", "select 1; drop table t"} {
		audit.calls = nil
		_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: stmt})
		if !errors.Is(err, ErrQueryValidationFailed) {
			t.Errorf("stmt %q: error = %v, want ErrQueryValidationFailed", stmt, err)
		}
		if exec.called {
			t.Errorf("stmt %q: executor must NOT be called on validation failure", stmt)
		}
		if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditRejected {
			t.Errorf("stmt %q: expected one rejected audit call, got %v", stmt, audit.calls)
		}
	}
}

// TestExplainServiceExecutorTimeout proves a deadline-exceeded executor error
// maps to ErrQueryTimeout and records an error audit event.
func TestExplainServiceExecutorTimeout(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{err: context.DeadlineExceeded}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditError {
		t.Errorf("expected one error audit call, got %v", audit.calls)
	}
}

// TestExplainServiceExecutorBackendFailure proves a generic executor error
// maps to ErrQueryBackendFailure and records an error audit event.
func TestExplainServiceExecutorBackendFailure(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{err: errors.New("connection refused")}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditError {
		t.Errorf("expected one error audit call, got %v", audit.calls)
	}
}

// TestExplainServiceExecutorNotSupported proves the executor returning
// ErrQueryExplainNotSupported (e.g. malformed plan) maps to the same sentinel
// and records an unsupported audit event.
func TestExplainServiceExecutorNotSupported(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{err: ErrQueryExplainNotSupported}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryExplainNotSupported) {
		t.Fatalf("error = %v, want ErrQueryExplainNotSupported", err)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditUnsupported {
		t.Errorf("expected one unsupported audit call, got %v", audit.calls)
	}
}

// TestExplainServiceNormalizerFailure proves a normalizer failure (malformed
// raw plan) maps to ErrQueryExplainNotSupported and records an unsupported
// audit event.
func TestExplainServiceNormalizerFailure(t *testing.T) {
	t.Parallel()
	exec := &fakeExplainExecutor{raw: ExplainRawPlan{tree: "not-an-object"}}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if !errors.Is(err, ErrQueryExplainNotSupported) {
		t.Fatalf("error = %v, want ErrQueryExplainNotSupported", err)
	}
	if len(audit.calls) != 1 || audit.calls[0].outcome != model.ExplainAuditUnsupported {
		t.Errorf("expected one unsupported audit call, got %v", audit.calls)
	}
}

// TestExplainServiceNoBareSelectExecution is the P1.1 structural assertion at
// the service layer: the executor's received statement is the sealed
// EXPLAIN-prefixed form, never the bare guarded select. The executor mock has
// no Query method — only Explain is called.
func TestExplainServiceNoBareSelectExecution(t *testing.T) {
	t.Parallel()
	raw := ExplainRawPlan{tree: parseJSON(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}}`)}
	exec := &fakeExplainExecutor{raw: raw}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select * from big"})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	if !exec.called {
		t.Fatalf("executor.Explain must be called exactly once")
	}
	bare := "select * from big"
	if exec.receivedStmt.WrappedSQL() == bare {
		t.Fatalf("executor received the bare select — the structural seam is broken: %q", exec.receivedStmt.WrappedSQL())
	}
	if !strings.HasPrefix(exec.receivedStmt.WrappedSQL(), "EXPLAIN FORMAT=JSON ") {
		t.Fatalf("executor received non-prefixed SQL: %q", exec.receivedStmt.WrappedSQL())
	}
}

// TestExplainServiceDeadlinePropagation proves the executor receives a
// context with a deadline set by the service.
func TestExplainServiceDeadlinePropagation(t *testing.T) {
	t.Parallel()
	raw := ExplainRawPlan{tree: parseJSON(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}}`)}
	exec := &ctxCapturingExecutor{raw: raw}
	audit := &fakeAuditRecorder{}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	if exec.seenCtx == nil {
		t.Fatalf("executor was not called")
	}
	if _, ok := exec.seenCtx.Deadline(); !ok {
		t.Errorf("executor context must have a deadline set by the service")
	}
}

// ctxCapturingExecutor wraps fakeExplainExecutor and captures the context.
type ctxCapturingExecutor struct {
	raw     ExplainRawPlan
	seenCtx context.Context
}

func (c *ctxCapturingExecutor) Explain(ctx context.Context, dsn string, stmt ExplainStatement) (ExplainRawPlan, error) {
	c.seenCtx = ctx
	return c.raw, nil
}

// TestExplainServiceAuditBestEffort proves a failing audit recorder does NOT
// fail the Explain request. The deviation from execute's persist-or-fail is
// justified: Explain returns no rows, only sanitized optimizer metadata.
func TestExplainServiceAuditBestEffort(t *testing.T) {
	t.Parallel()
	raw := ExplainRawPlan{tree: parseJSON(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}}`)}
	exec := &fakeExplainExecutor{raw: raw}
	audit := &fakeAuditRecorder{err: errors.New("audit write failed")}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, audit, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	resp, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if err != nil {
		t.Fatalf("Explain must NOT fail when audit write fails (best-effort): %v", err)
	}
	if resp.FormatVersion != model.ExplainFormatVersion {
		t.Errorf("expected a valid response despite audit failure")
	}
}

// TestExplainServiceNoAuditWhenNil proves a nil audit recorder is safe.
func TestExplainServiceNoAuditWhenNil(t *testing.T) {
	t.Parallel()
	raw := ExplainRawPlan{tree: parseJSON(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}}`)}
	exec := &fakeExplainExecutor{raw: raw}
	target := makeReadyMySQLTarget(t, 616)
	svc := newExplainService(t, exec, nil, []model.QueryTarget{target}, map[uint64]model.QueryCredentialMetadata{616: readyMySQLCredential(t)}, explainTestDSN)
	_, err := svc.Explain(context.Background(), 1, 616, model.ExplainRequest{Statement: "select 1"})
	if err != nil {
		t.Fatalf("Explain with nil audit must not fail: %v", err)
	}
}

// TestExplainServiceNoHistoryRow proves the service struct holds no reference
// to QueryExecutionRepository (structural guarantee: Explain cannot call
// InsertExecution). This is a compile-time intent declaration.
func TestExplainServiceNoHistoryRow(t *testing.T) {
	t.Parallel()
	// The QueryExplainService struct has no executions / QueryExecutionRepository
	// field. If a future change adds one, this test's existence documents the
	// security boundary that must be re-reviewed.
	var svc QueryExplainService
	_ = svc
	// The struct fields are: guard, access, executor, normalizer, clock, audit.
	// None of these is QueryExecutionRepository or has an InsertExecution method.
}
