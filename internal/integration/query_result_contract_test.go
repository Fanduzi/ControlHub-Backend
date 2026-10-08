//go:build integration

// Package integration — T8-A result-contract composition tests (issue #116).
// input: context, errors, strings, testing, Testcontainers MySQL, internal/model, internal/repository/mysql, internal/service
// output: TestResultContract* — success-pair-then-gate ordering and upstream-failure precedence against the real MySQL evidence repository; NOT three-entry-point HTTP wiring
// pos: Proves evidence pair → delivery gate ordering on real persistence
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestResultContractDeliveryAfterEvidencePair composes the real evidence path
// with the production delivery gate: a persisted success pair followed by a
// capability-less refusal must leave the history row as success with exactly
// one audit event — the gate is a post-finalize delivery decision, not a
// status rewrite, second history row, or claim release.
func TestResultContractDeliveryAfterEvidencePair(t *testing.T) {
	db := setupTestDB(t)
	targetID := createQueryTargetResource(t, db, "qe-t8-gate")
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()

	// 1. Persist the real Execution Evidence Pair first (finalize boundary).
	execID, err := repo.InsertExecutionWithAudit(ctx, model.QueryExecutionRecord{
		TargetResourceID: targetID,
		ActorUserID:      ownerDBA,
		Engine:           "mysql",
		StatementDigest:  "digest:t8-gate",
		StatementPreview: "preview:t8-gate",
		FullStatement:    "SELECT 'v'",
		Status:           model.QueryExecutionSuccess,
		RowCount:         1,
	}, "query.executed", "success")
	if err != nil {
		t.Fatalf("evidence pair: %v", err)
	}

	// 2. Post-finalize delivery decision on a page with one truncated cell.
	cols := []model.QueryResultColumn{
		{Name: "a", DatabaseType: "VARCHAR", DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
		{Name: "b", DatabaseType: "TEXT", DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
	}
	page := [][]any{{"short", strings.Repeat("x", 8192)}}
	matrix := [][]bool{{false, true}}

	// Capability-less client: controlled refusal, no usable rows.
	decision, err := service.DecideResultDelivery(service.ResultDeliveryInput{
		Columns: cols, Rows: page, CellTruncated: matrix,
	})
	if !errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatalf("capability-less truncated page must refuse, got %v", err)
	}
	if decision.Rows != nil {
		t.Fatalf("refusal must not carry rows: %+v", decision)
	}
	// The same page is CSV-ineligible regardless of capabilities.
	if err := service.DecideCSVExport(cols, page, matrix); !errors.Is(err, service.ErrCSVExportNotPermitted) {
		t.Fatalf("truncated page must deny CSV export: %v", err)
	}
	// Capable client receives rows + full matrix after the same evidence pair.
	decision, err = service.DecideResultDelivery(service.ResultDeliveryInput{
		Columns: cols, Rows: page, CellTruncated: matrix,
		Capabilities: []string{service.CapabilityCellTruncated},
	})
	if err != nil || !decision.CellTruncated[0][1] {
		t.Fatalf("capable delivery must carry rows+matrix: %v %+v", err, decision)
	}

	// 3. The refusal touched no evidence: status stays success, exactly one
	//    audit row, no second history row.
	var status string
	if err := db.QueryRow(`SELECT status FROM query_executions WHERE id = ?`, execID).Scan(&status); err != nil {
		t.Fatalf("read execution status: %v", err)
	}
	if status != string(model.QueryExecutionSuccess) {
		t.Fatalf("delivery refusal must not rewrite status, got %q", status)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID); n != 1 {
		t.Fatalf("history rows = %d, want exactly 1 (no second write)", n)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.executed'`, targetID); n != 1 {
		t.Fatalf("audit rows = %d, want exactly 1", n)
	}
}

// TestResultContractUpstreamFailurePrecedence proves the gate passes a REAL
// evidence-persistence failure through unchanged: a forced audit-insert
// failure produces the safe repository error, which — fed to the gate on a
// truncated page with no capability — returns the upstream failure, never
// the contract-upgrade refusal.
func TestResultContractUpstreamFailurePrecedence(t *testing.T) {
	db := setupTestDB(t)
	targetID := createQueryTargetResource(t, db, "qe-t8-upstream")

	// Real failure injection: audit INSERT is forced to fail, rolling the
	// whole evidence pair back.
	if _, err := db.Exec(`create trigger t8_force_audit_fail
		before insert on audit_events for each row
		signal sqlstate '45000' set message_text = 'forced audit failure'`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists t8_force_audit_fail`) })

	repo := mysql.NewQueryExecutionRepository(db)
	_, persistErr := repo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: targetID,
		ActorUserID:      ownerDBA,
		Engine:           "mysql",
		StatementDigest:  "digest:t8-upstream",
		StatementPreview: "preview:t8-upstream",
		Status:           model.QueryExecutionSuccess,
		RowCount:         1,
	}, "query.executed", "success")
	if persistErr == nil {
		t.Fatal("expected evidence pair to fail under trigger")
	}

	cols := []model.QueryResultColumn{
		{Name: "a", DisplayMode: model.ResultDisclosureRawCopyAllowed, CopyAllowed: true},
	}
	_, err := service.DecideResultDelivery(service.ResultDeliveryInput{
		UpstreamErr:   persistErr,
		Columns:       cols,
		Rows:          [][]any{{"v"}},
		CellTruncated: [][]bool{{true}}, // truncated + no capability — and yet
	})
	if errors.Is(err, service.ErrResultContractUpgradeRequired) {
		t.Fatal("capability refusal must not mask the evidence failure")
	}
	if !errors.Is(err, persistErr) {
		t.Fatalf("upstream error must pass through unchanged, got %v", err)
	}
	if n := countWhere(t, db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID); n != 0 {
		t.Fatalf("rolled-back pair left %d history rows", n)
	}
}
