// Package service tests the T8-B production wiring of the result contract.
// input: context, errors, testing, time, internal/model
// output: Test*_CellTruncated* / Test*_ResultContract* cases proving capability propagation, post-finalize gate ordering, and corrupt-evidence handling through the three governed service paths
// pos: Service-boundary proof that real Execute/ExecuteSavedStatement/NavigateRelatedRecords flows carry the aligned matrix, apply the capability gate only after the success evidence pair, and treat producer matrix faults as internal failures — never fabricated-clean pages
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// truncatedResult returns a one-cell page whose executor matrix carries a true
// truncation flag, as produced by the real scan path on an oversized value.
func truncatedResult() QueryDatabaseResult {
	return QueryDatabaseResult{
		Columns:       []model.QueryResultColumn{{Name: "value", DatabaseType: "VARCHAR"}},
		Rows:          [][]any{{"prefix"}},
		RowCount:      1,
		CellTruncated: [][]bool{{true}},
	}
}

// lastPairStatus returns the status of the single evidence-pair write.
func lastPairStatus(t *testing.T, repo *fakeExecRepo) model.QueryExecutionStatus {
	t.Helper()
	if len(repo.pairCalls) != 1 {
		t.Fatalf("evidence pair writes = %d, want exactly 1", len(repo.pairCalls))
	}
	return repo.pairCalls[0].rec.Status
}

// --- Ordinary execute path ---

func TestExecute_CellTruncatedGatePostFinalize(t *testing.T) {
	for _, tc := range []struct {
		name         string
		capabilities []string
		wantRefusal  bool
	}{
		{"no capability", nil, true},
		{"exact capability", []string{CapabilityCellTruncated}, false},
		{"unknown capability never satisfies", []string{"csvExport"}, true},
		{"near-miss capability never satisfies", []string{"celltruncated"}, true},
		{"duplicates behave like one", []string{CapabilityCellTruncated, CapabilityCellTruncated}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, executor := executionTestScaffold(t)
			executor.result = truncatedResult()

			resp, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{
				Statement:    "select name from t",
				Capabilities: tc.capabilities,
			})

			// The evidence pair is already committed before the gate runs: the
			// recorded outcome stays success either way — a delivery refusal
			// must never rewrite it and must never write a second pair.
			if status := lastPairStatus(t, repo); status != model.QueryExecutionSuccess {
				t.Fatalf("recorded status = %q, want success", status)
			}
			if len(repo.auditEvents) != 1 || repo.auditEvents[0].etype != "query.executed" {
				t.Fatalf("audit events = %+v, want one query.executed", repo.auditEvents)
			}

			if tc.wantRefusal {
				if !errors.Is(err, ErrResultContractUpgradeRequired) {
					t.Fatalf("err = %v, want ErrResultContractUpgradeRequired", err)
				}
				if len(resp.Rows) != 0 {
					t.Fatalf("refused delivery leaked %d rows", len(resp.Rows))
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if len(resp.Rows) != 1 || len(resp.CellTruncated) != 1 || !resp.CellTruncated[0][0] {
				t.Fatalf("response = %+v, want one row with aligned true matrix", resp.CellTruncated)
			}
		})
	}
}

func TestExecute_CleanPageDeliversAllFalseMatrixWithoutCapability(t *testing.T) {
	svc, _, _, executor := executionTestScaffold(t)
	executor.result = QueryDatabaseResult{
		Columns:       []model.QueryResultColumn{{Name: "id", DatabaseType: "BIGINT"}, {Name: "name", DatabaseType: "VARCHAR"}},
		Rows:          [][]any{{int64(1), "a"}, {int64(2), "b"}},
		RowCount:      2,
		CellTruncated: [][]bool{{false, false}, {false, false}},
	}

	resp, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select id, name from t"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// WHY: G2 requires a complete aligned matrix on every non-empty page —
	// a clean page must ship an explicit all-false matrix, not omit the field.
	if len(resp.CellTruncated) != 2 || resp.CellTruncated[0][0] || resp.CellTruncated[1][1] {
		t.Fatalf("cellTruncated = %+v, want 2x2 all-false", resp.CellTruncated)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(resp.Rows))
	}
}

// --- Saved-statement path shares the chain ---

func TestExecuteSavedStatement_CellTruncatedGatePostFinalize(t *testing.T) {
	svc, repo, executor, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select name from t", nil), nil,
	)
	executor.result = truncatedResult()

	req := templateExecuteRequest(nil)
	_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
	if !errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("err = %v, want ErrResultContractUpgradeRequired", err)
	}
	if status := lastPairStatus(t, repo); status != model.QueryExecutionSuccess {
		t.Fatalf("recorded status = %q, want success", status)
	}

	req.Capabilities = []string{CapabilityCellTruncated}
	resp, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, req)
	if err != nil {
		t.Fatalf("ExecuteSavedStatement with capability: %v", err)
	}
	if len(resp.CellTruncated) != 1 || !resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %+v, want aligned true", resp.CellTruncated)
	}
}

// --- Navigation path applies the same gate ---

func TestNavigateRelatedRecords_CellTruncatedGatePostFinalize(t *testing.T) {
	svc, repo, executor, _ := navTestScaffold(t)
	executor.result = truncatedResult()

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatalf("err = %v, want ErrResultContractUpgradeRequired", err)
	}
	// Same post-finalize ordering: the navigation pair (fixed
	// related_record_navigation event) stays a single committed success.
	if status := lastPairStatus(t, repo); status != model.QueryExecutionSuccess {
		t.Fatalf("recorded status = %q, want success", status)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].etype != "related_record_navigation" {
		t.Fatalf("audit events = %+v, want one related_record_navigation", repo.auditEvents)
	}

	req := validNavRequest()
	req.Capabilities = []string{CapabilityCellTruncated}
	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords with capability: %v", err)
	}
	if len(resp.CellTruncated) != 1 || !resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %+v, want aligned true", resp.CellTruncated)
	}
}

// --- Corrupt producer evidence is an internal failure, not a clean page ---

func TestExecute_CorruptTruncationEvidenceIsInternalFailure(t *testing.T) {
	oneCol := []model.QueryResultColumn{{Name: "value", DatabaseType: "VARCHAR"}}
	for _, tc := range []struct {
		name   string
		result QueryDatabaseResult
	}{
		{"missing matrix on non-empty page", QueryDatabaseResult{
			Columns: oneCol, Rows: [][]any{{"x"}}, RowCount: 1, CellTruncated: nil,
		}},
		{"row wider than public columns", QueryDatabaseResult{
			Columns: oneCol, Rows: [][]any{{"x", "y"}}, RowCount: 1, CellTruncated: [][]bool{{false, false}},
		}},
		{"matrix row width mismatch", QueryDatabaseResult{
			Columns: oneCol, Rows: [][]any{{"x"}}, RowCount: 1, CellTruncated: [][]bool{{false, false}},
		}},
		{"matrix row count mismatch", QueryDatabaseResult{
			Columns: oneCol, Rows: [][]any{{"x"}}, RowCount: 1, CellTruncated: [][]bool{{false}, {false}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, executor := executionTestScaffold(t)
			// rawResult bypasses the fake's contract fill — the corrupt matrix
			// reaches the service exactly as a broken producer would emit it.
			executor.rawResult = true
			executor.result = tc.result

			resp, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{
				Statement:    "select value from t",
				Capabilities: []string{CapabilityCellTruncated},
			})
			// WHY: producer faults are internal failures — never delivered as a
			// known-clean page, never dressed up as a client-capability issue.
			if !errors.Is(err, ErrQueryBackendFailure) {
				t.Fatalf("err = %v, want ErrQueryBackendFailure", err)
			}
			if errors.Is(err, ErrResultContractUpgradeRequired) {
				t.Fatal("producer fault must not surface as the capability refusal")
			}
			if len(resp.Rows) != 0 {
				t.Fatalf("corrupt evidence leaked %d rows", len(resp.Rows))
			}
			// Exactly one evidence pair, recorded failed — the fault happens
			// before finalize, so it cannot leave a success pair behind.
			if status := lastPairStatus(t, repo); status != model.QueryExecutionFailed {
				t.Fatalf("recorded status = %q, want failed", status)
			}
			if repo.pairCalls[0].rec.ErrorCode != "query_backend_error" {
				t.Fatalf("recorded error code = %q, want query_backend_error", repo.pairCalls[0].rec.ErrorCode)
			}
		})
	}
}

func TestNavigateRelatedRecords_CorruptTruncationEvidenceIsInternalFailure(t *testing.T) {
	svc, repo, executor, _ := navTestScaffold(t)
	executor.rawResult = true
	executor.result = QueryDatabaseResult{
		Columns: []model.QueryResultColumn{{Name: "id", DatabaseType: "BIGINT"}},
		Rows:    [][]any{{int64(1)}}, RowCount: 1, CellTruncated: nil,
	}

	req := validNavRequest()
	req.Capabilities = []string{CapabilityCellTruncated}
	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("err = %v, want ErrQueryBackendFailure", err)
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("corrupt evidence leaked %d rows", len(resp.Rows))
	}
	if status := lastPairStatus(t, repo); status != model.QueryExecutionFailed {
		t.Fatalf("recorded status = %q, want failed", status)
	}
	if repo.pairCalls[0].event != "related_record_navigation" {
		t.Fatalf("audit event = %q, want related_record_navigation", repo.pairCalls[0].event)
	}
}

// --- Evidence-persist failure stays a 502-class upstream error ---

func TestExecute_EvidenceFailureWinsOverCapabilityGate(t *testing.T) {
	svc, repo, _, executor := executionTestScaffold(t)
	executor.result = truncatedResult()
	repo.pairErr = errors.New("pair write failed")

	// Even on a truncated page without capability, the evidence persistence
	// failure is the answer — the gate never masks the 502-class outcome.
	resp, err := svc.Execute(context.Background(), userExecutionIdentity(1), 9001, model.QueryExecuteRequest{Statement: "select name from t"})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("err = %v, want ErrQueryBackendFailure", err)
	}
	if errors.Is(err, ErrResultContractUpgradeRequired) {
		t.Fatal("evidence failure must not surface as the capability refusal")
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("failed persistence leaked %d rows", len(resp.Rows))
	}
}

