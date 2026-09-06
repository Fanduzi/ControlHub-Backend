// Package service tests the Execution Evidence Pair recording entry.
// input: context, errors, testing, time, internal/model
// output: TestPersistEvidencePair_*
// pos: Tests at persistEvidencePair — the single write for every Evidence-Bearing Query Attempt: detached window, fixed audit event type, verbatim identity, fail-closed rollback
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

func evidencePairService(repo *fakeExecRepo) *QueryExecutionService {
	return &QueryExecutionService{executions: repo, clock: &fakeClock{t: time.Now()}}
}

func evidencePairRecord() model.QueryExecutionRecord {
	return model.QueryExecutionRecord{
		TargetResourceID: 9001,
		ActorUserID:      7,
		Engine:           "mysql",
		Status:           model.QueryExecutionFailed,
		StatementDigest:  "caller-digest",
		StatementPreview: "caller-preview",
		ErrorCode:        "query_canceled",
		ErrorMessage:     "query canceled",
	}
}

// TestPersistEvidencePair_CanceledRequestStillWritesInDetachedWindow proves
// WHY the pair is the recording entry: a canceled request context must not
// drop the write. The window is detached and two seconds; the caller's digest
// and preview are stored verbatim — the pair does not rewrite identity.
func TestPersistEvidencePair_CanceledRequestStillWritesInDetachedWindow(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{}
	svc := evidencePairService(repo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	id, err := svc.persistEvidencePair(ctx, evidencePairRecord(), executionEvidenceQuery)
	if err != nil {
		t.Fatalf("persistEvidencePair: %v", err)
	}
	if id == 0 {
		t.Fatal("committed id = 0")
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history rows = %d, want 1", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if rec.StatementDigest != "caller-digest" || rec.StatementPreview != "caller-preview" {
		t.Fatalf("identity rewritten: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].event != "query.executed" || repo.pairCalls[0].result != "failed" {
		t.Fatalf("audit = %+v, want query.executed/failed", repo.pairCalls)
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestPersistEvidencePair_NavigationKindSelectsFixedAuditEvent proves WHY
// callers pass a private kind instead of event text: navigation must persist
// related_record_navigation, and the supplied identity still lands unchanged.
func TestPersistEvidencePair_NavigationKindSelectsFixedAuditEvent(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{}
	svc := evidencePairService(repo)
	rec := evidencePairRecord()
	rec.Status = model.QueryExecutionSuccess
	rec.ErrorCode = ""
	rec.ErrorMessage = ""
	rec.StatementDigest = "nav:orders.orders/fk_order_items_order"
	rec.StatementPreview = "related:orders.orders/fk_order_items_order"

	_, err := svc.persistEvidencePair(context.Background(), rec, executionEvidenceNavigation)
	if err != nil {
		t.Fatalf("persistEvidencePair: %v", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pair calls = %d, want 1", len(repo.pairCalls))
	}
	got := repo.pairCalls[0]
	if got.event != "related_record_navigation" || got.result != "success" {
		t.Fatalf("audit = %q/%q, want related_record_navigation/success", got.event, got.result)
	}
	if got.rec.StatementDigest != rec.StatementDigest || got.rec.StatementPreview != rec.StatementPreview {
		t.Fatalf("navigation identity rewritten: %+v", got.rec)
	}
}

// TestPersistEvidencePair_WriteFailureRecordsNothing proves WHY a failed pair
// must not leave a history row or audit event: the repository rolls the pair
// back, and persistEvidencePair returns that failure to the caller.
func TestPersistEvidencePair_WriteFailureRecordsNothing(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{pairErr: errors.New("evidence store down")}
	svc := evidencePairService(repo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	id, err := svc.persistEvidencePair(ctx, evidencePairRecord(), executionEvidenceQuery)
	if err == nil {
		t.Fatal("expected pair write failure")
	}
	if id != 0 {
		t.Fatalf("id = %d, want 0", id)
	}
	if len(repo.insertedAttempts) != 0 || len(repo.auditEvents) != 0 {
		t.Fatalf("failed pair recorded history=%d audit=%d", len(repo.insertedAttempts), len(repo.auditEvents))
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pair attempts = %d, want 1", len(repo.pairCalls))
	}
	assertDetachedEvidenceWindow(t, repo)
}
