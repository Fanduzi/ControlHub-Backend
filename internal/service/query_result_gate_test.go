// Package service provides unit tests for the T8-B post-finalize delivery gate wiring.
// input: context, encoding/json, errors, testing, time, internal/model
// output: TestExecute_* post-finalize delivery-gate cases for execute/saved-execute/related-records
// pos: Proves the three governed result paths run the cellTruncated capability gate AFTER the success evidence pair commits — refusal keeps exactly one success row, persistence failure wins over refusal
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// gateTruncatedResult is a one-column result page whose single delivered cell
// carries the executor's truncation flag — the shape a real >8192-byte text
// cell produces after scanBoundedRows truncates it.
func gateTruncatedResult() QueryDatabaseResult {
	return QueryDatabaseResult{
		Columns:       []model.QueryResultColumn{{Name: "payload", DatabaseType: "TEXT"}},
		Rows:          [][]any{{"8192-byte prefix"}},
		RowCount:      1,
		CellTruncated: [][]bool{{true}},
	}
}

// gateCleanResult is the same page with an honest all-false matrix — every
// non-empty page must carry it; capable-less clients still receive the page.
func gateCleanResult() QueryDatabaseResult {
	return QueryDatabaseResult{
		Columns:       []model.QueryResultColumn{{Name: "payload", DatabaseType: "TEXT"}},
		Rows:          [][]any{{"small value"}},
		RowCount:      1,
		CellTruncated: [][]bool{{false}},
	}
}

// gateExecScaffold wires a service whose fake executor returns the supplied
// result verbatim (including its CellTruncated evidence).
func gateExecScaffold(t *testing.T, result QueryDatabaseResult) (*QueryExecutionService, *fakeExecRepo, *fakeExecutor) {
	t.Helper()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: result}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{},
		&fakeDisclosureService{},
	)
	return svc, repo, executor
}

// gateExecScaffoldWithDisclosure wires the scaffold with a caller-controlled
// disclosure fake — needed for missing/corrupt-matrix negative cases where the
// disclosure layer drops or mangles the executor's evidence.
func gateExecScaffoldWithDisclosure(t *testing.T, result QueryDatabaseResult, disclosure *fakeDisclosureService) (*QueryExecutionService, *fakeExecRepo) {
	t.Helper()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{result: result},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{},
		disclosure,
	)
	return svc, repo
}

// assertSingleSuccessPair proves a delivery refusal happened after evidence
// finalization: exactly one atomic pair was written, it recorded success, and
// no second history row, rejection rewrite, or retry exists.
func assertSingleSuccessPair(t *testing.T, repo *fakeExecRepo, event string) {
	t.Helper()
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair writes = %d, want exactly 1 (the committed success)", len(repo.pairCalls))
	}
	pair := repo.pairCalls[0]
	if pair.rec.Status != model.QueryExecutionSuccess || pair.result != "success" || pair.event != event {
		t.Fatalf("pair = status %q / event %q / result %q, want success / %s / success",
			pair.rec.Status, pair.event, pair.result, event)
	}
}

// --- ordinary execute ---

func TestExecute_TruncatedPageWithoutCapabilityRefusedAfterSuccess(t *testing.T) {
	t.Parallel()
	svc, repo, executor := gateExecScaffold(t, gateTruncatedResult())

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10})

	if !errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
	}
	if len(resp.Rows) != 0 || resp.CellTruncated != nil {
		t.Fatalf("refused response leaked result data: %+v", resp)
	}
	assertSingleSuccessPair(t, repo, "query.executed")
	if executor.queryCalls != 1 {
		t.Fatalf("executor calls = %d, want exactly 1 (no retry)", executor.queryCalls)
	}
}

func TestExecute_TruncatedPageWithCapabilityDeliversMatrix(t *testing.T) {
	t.Parallel()
	svc, repo, _ := gateExecScaffold(t, gateTruncatedResult())

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10,
			Capabilities: []string{CapabilityCellTruncated}})

	if err != nil {
		t.Fatalf("capable execute error = %v, want success", err)
	}
	if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 1 || !resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", resp.CellTruncated)
	}
	if resp.Rows[0][0] != "8192-byte prefix" {
		t.Fatalf("row value = %v, want delivered prefix", resp.Rows[0][0])
	}
	assertSingleSuccessPair(t, repo, "query.executed")
}

func TestExecute_CleanPageWithoutCapabilityStillDelivers(t *testing.T) {
	t.Parallel()
	svc, _, _ := gateExecScaffold(t, gateCleanResult())

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10})

	if err != nil {
		t.Fatalf("clean-page execute error = %v, want success", err)
	}
	if len(resp.CellTruncated) != 1 || resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[false]] — non-empty pages always carry the aligned matrix", resp.CellTruncated)
	}
}

func TestExecute_UnknownCapabilityTokenDoesNotSatisfyGate(t *testing.T) {
	t.Parallel()
	svc, _, _ := gateExecScaffold(t, gateTruncatedResult())

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10,
			Capabilities: []string{"celltruncated", "cellTruncated ", "truncated"}})

	if !errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("error = %v, want ErrResultContractUpgradeRequired (only the exact token satisfies)", err)
	}
}

func TestExecute_PersistFailureBeatsCapabilityRefusal(t *testing.T) {
	t.Parallel()
	svc, repo, _ := gateExecScaffold(t, gateTruncatedResult())
	repo.pairErr = errors.New("forced pair write failure")

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10})

	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (502), not capability refusal", err)
	}
	if errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatal("capability refusal must not mask an evidence-persistence failure")
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pair writes attempted = %d, want exactly 1", len(repo.pairCalls))
	}
}

// A truncated page whose matrix disappears AFTER disclosure succeeded is
// post-finalize corrupt evidence: the success pair is already committed and is
// never rewritten — the response is the fixed safe backend error with no rows,
// and no second history row exists. This is the gate's defensive validation
// firing; production Apply rejects missing matrices earlier.
func TestExecute_MissingMatrixPostFinalizeIsBackendFailureNotRefusal(t *testing.T) {
	t.Parallel()
	svc, repo := gateExecScaffoldWithDisclosure(t, gateTruncatedResult(),
		&fakeDisclosureService{dropMatrix: true})

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10,
			Capabilities: []string{CapabilityCellTruncated}})

	if err == nil {
		t.Fatal("missing matrix on a non-empty page must fail")
	}
	if errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatal("missing matrix is internal evidence corruption — never a capability refusal")
	}
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("corrupt evidence must not deliver partial rows: %+v", resp)
	}
	// The committed success pair stands — post-finalize corruption must not
	// rewrite it into a failure or add a second row.
	assertSingleSuccessPair(t, repo, "query.executed")
}

// A missing/corrupt matrix detected during disclosure Apply is a pre-finalize
// terminal failure: exactly one failed evidence pair, fixed safe backend error.
func TestExecute_MissingMatrixAtApplyIsFailedNotRefusal(t *testing.T) {
	t.Parallel()
	svc, repo := gateExecScaffoldWithDisclosure(t, gateTruncatedResult(),
		&fakeDisclosureService{applyErr: errResultContractInvalid})

	_, err := svc.Execute(context.Background(), userExecutionIdentity(7), 9001,
		model.QueryExecuteRequest{Statement: "select payload from t", MaxRows: 10,
			Capabilities: []string{CapabilityCellTruncated}})

	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatal("pre-finalize evidence corruption is never a capability refusal")
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionFailed {
		t.Fatalf("pair calls = %+v, want exactly one failed terminal pair", repo.pairCalls)
	}
}

// --- saved-statement execute (shares executeGuardedChain) ---

func TestExecuteSavedStatement_TruncatedPageWithoutCapabilityRefusedAfterSuccess(t *testing.T) {
	t.Parallel()
	svc, repo, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select payload from t where id = :id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	executor.result = gateTruncatedResult()

	resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7,
		model.QuerySavedStatementExecuteRequest{
			Values:  map[string]json.RawMessage{"id": json.RawMessage(`1`)},
			MaxRows: 10,
		})

	if !errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
	}
	if len(resp.Rows) != 0 || resp.CellTruncated != nil {
		t.Fatalf("refused response leaked result data: %+v", resp)
	}
	assertSingleSuccessPair(t, repo, "query.executed")
	if executor.templateCalls != 1 {
		t.Fatalf("template executor calls = %d, want exactly 1", executor.templateCalls)
	}
}

func TestExecuteSavedStatement_TruncatedPageWithCapabilityDeliversMatrix(t *testing.T) {
	t.Parallel()
	svc, repo, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select payload from t where id = :id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	executor.result = gateTruncatedResult()

	resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7,
		model.QuerySavedStatementExecuteRequest{
			Values:       map[string]json.RawMessage{"id": json.RawMessage(`1`)},
			MaxRows:      10,
			Capabilities: []string{CapabilityCellTruncated},
		})

	if err != nil {
		t.Fatalf("capable saved execute error = %v, want success", err)
	}
	if len(resp.CellTruncated) != 1 || !resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", resp.CellTruncated)
	}
	assertSingleSuccessPair(t, repo, "query.executed")
}

func TestExecuteSavedStatement_CleanPageWithoutCapabilityStillDelivers(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select payload from t where id = :id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	executor.result = gateCleanResult()

	resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7,
		model.QuerySavedStatementExecuteRequest{
			Values:  map[string]json.RawMessage{"id": json.RawMessage(`1`)},
			MaxRows: 10,
		})

	if err != nil {
		t.Fatalf("clean-page saved execute error = %v, want success", err)
	}
	if len(resp.CellTruncated) != 1 || resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[false]]", resp.CellTruncated)
	}
}

func TestExecuteSavedStatement_PersistFailureBeatsCapabilityRefusal(t *testing.T) {
	t.Parallel()
	svc, repo, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select payload from t where id = :id",
			[]model.QuerySavedStatementParameterDefinition{{Name: "id", Type: model.QuerySavedStatementParameterInteger}}), nil)
	executor.result = gateTruncatedResult()
	repo.pairErr = errors.New("forced pair write failure")

	_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7,
		model.QuerySavedStatementExecuteRequest{
			Values:  map[string]json.RawMessage{"id": json.RawMessage(`1`)},
			MaxRows: 10,
		})

	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (502)", err)
	}
	if errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatal("capability refusal must not mask an evidence-persistence failure")
	}
}

// --- related-record navigation (separate post-finalize chain) ---

func TestNavigateRelatedRecords_TruncatedPageWithoutCapabilityRefusedAfterSuccess(t *testing.T) {
	t.Parallel()
	svc, repo, executor, _ := navTestScaffold(t)
	executor.result = gateTruncatedResult()

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())

	if !errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
	}
	if len(resp.Rows) != 0 || resp.CellTruncated != nil {
		t.Fatalf("refused response leaked result data: %+v", resp)
	}
	assertSingleSuccessPair(t, repo, "related_record_navigation")
}

func TestNavigateRelatedRecords_TruncatedPageWithCapabilityDeliversMatrix(t *testing.T) {
	t.Parallel()
	svc, repo, executor, _ := navTestScaffold(t)
	executor.result = gateTruncatedResult()

	req := validNavRequest()
	req.Capabilities = []string{CapabilityCellTruncated}
	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)

	if err != nil {
		t.Fatalf("capable navigate error = %v, want success", err)
	}
	if len(resp.CellTruncated) != 1 || !resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", resp.CellTruncated)
	}
	assertSingleSuccessPair(t, repo, "related_record_navigation")
}

func TestNavigateRelatedRecords_CleanPageWithoutCapabilityStillDelivers(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := navTestScaffold(t)
	executor.result = gateCleanResult()

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())

	if err != nil {
		t.Fatalf("clean-page navigate error = %v, want success", err)
	}
	if len(resp.CellTruncated) != 1 || resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[false]]", resp.CellTruncated)
	}
}

func TestNavigateRelatedRecords_PersistFailureBeatsCapabilityRefusal(t *testing.T) {
	t.Parallel()
	svc, repo, executor, _ := navTestScaffold(t)
	executor.result = gateTruncatedResult()
	repo.pairErr = errors.New("forced pair write failure")

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())

	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure (502)", err)
	}
	if errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatal("capability refusal must not mask an evidence-persistence failure")
	}
}
