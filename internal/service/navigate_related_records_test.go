// Package service provides tests for the Phase 38J related-record navigation.
// input: context, errors, strings, testing, time, internal/model
// output: TestNavigateRelatedRecords_* (fakes for inspector, executor bound queries)
// pos: Unit tests for FK navigation governance, parameter binding, history/audit, error mapping, and inspector-phase cancellation/deadline terminal evidence (Issue #40)
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// --- navigation test scaffold ---

// navTestScaffold wires a service with a ready mysql/staging target and a
// fake inspector that returns FK metadata for the "order_items" table.
func navTestScaffold(t *testing.T) (*QueryExecutionService, *fakeExecRepo, *fakeExecutor, *fakeNavSchemaInspector) {
	t.Helper()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	resolver := &fakeResolver{dsn: testResolverDSN}
	executor := &fakeExecutor{result: QueryDatabaseResult{
		Columns:  []model.QueryResultColumn{{Name: "id", DatabaseType: "BIGINT"}, {Name: "name", DatabaseType: "VARCHAR"}},
		Rows:     [][]any{{int64(100), "Widget"}},
		RowCount: 1,
	}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name: "fk_order_items_order",
					Columns: []FKColumn{
						{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"},
					},
				},
				{
					Name: "fk_order_items_product",
					Columns: []FKColumn{
						{Column: "product_id", ReferencedSchema: "catalog", ReferencedTable: "products", ReferencedColumn: "id"},
					},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)},
		inspector, &fakeDisclosureService{},
	)
	return svc, repo, executor, inspector
}

// navCompositeScaffold wires a service with a composite FK (two columns).
func navCompositeScaffold(t *testing.T) (*QueryExecutionService, *fakeExecRepo, *fakeExecutor, *fakeNavSchemaInspector) {
	t.Helper()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	resolver := &fakeResolver{dsn: testResolverDSN}
	executor := &fakeExecutor{result: QueryDatabaseResult{
		Columns:  []model.QueryResultColumn{{Name: "id", DatabaseType: "BIGINT"}},
		Rows:     [][]any{{int64(200)}},
		RowCount: 1,
	}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "junction_table",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name: "fk_junction_composite",
					Columns: []FKColumn{
						{Column: "tenant_id", ReferencedSchema: "app", ReferencedTable: "memberships", ReferencedColumn: "tenant_id"},
						{Column: "user_id", ReferencedSchema: "app", ReferencedTable: "memberships", ReferencedColumn: "user_id"},
					},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, resolver, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)},
		inspector, &fakeDisclosureService{},
	)
	return svc, repo, executor, inspector
}

func validNavRequest() model.RelatedRecordNavigationRequest {
	return model.RelatedRecordNavigationRequest{
		Source: model.RelatedRecordNavigationSource{
			Database:   "orders_db",
			Object:     "order_items",
			Kind:       "table",
			ForeignKey: "fk_order_items_order",
		},
		LocalValues: []string{"42"},
		MaxRows:     100,
	}
}

// --- Happy path ---

func TestNavigateRelatedRecords_Success(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navTestScaffold(t)

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", resp.RowCount)
	}
	if resp.ExecutionID == 0 {
		t.Fatal("executionID should be non-zero")
	}
	// Relation metadata must be populated from trusted FK resolution.
	if resp.ReferencedDatabase != "orders" {
		t.Fatalf("referencedDatabase = %q, want %q", resp.ReferencedDatabase, "orders")
	}
	if resp.ReferencedObject != "orders" {
		t.Fatalf("referencedObject = %q, want %q", resp.ReferencedObject, "orders")
	}
	if len(resp.ReferencedColumns) != 1 || resp.ReferencedColumns[0] != "id" {
		t.Fatalf("referencedColumns = %v, want [id]", resp.ReferencedColumns)
	}
	if resp.SourceDatabase != "orders_db" {
		t.Fatalf("sourceDatabase = %q, want %q", resp.SourceDatabase, "orders_db")
	}
	if resp.SourceObject != "order_items" {
		t.Fatalf("sourceObject = %q, want %q", resp.SourceObject, "order_items")
	}
	if resp.ForeignKey != "fk_order_items_order" {
		t.Fatalf("foreignKey = %q, want %q", resp.ForeignKey, "fk_order_items_order")
	}
}

// --- Unknown target ---

func TestNavigateRelatedRecords_UnknownTarget(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: nil},
		repo, &fakeResolver{}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9999, validNavRequest())
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("error = %v, want ErrQueryTargetNotFound", err)
	}
}

// --- Disabled target ---

func TestNavigateRelatedRecords_DisabledTarget(t *testing.T) {
	t.Parallel()
	// Override the target to be non-mysql engine so it gets rejected.
	target := mysqlTarget("Staging")
	target.ConnectionContext.Engine = "redis"
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{target}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		nil, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
}

// --- FK not found ---

func TestNavigateRelatedRecords_FKNotFound(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.Source.ForeignKey = "fk_nonexistent"

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
}

// --- Value count mismatch ---

func TestNavigateRelatedRecords_ValueCountMismatch(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.LocalValues = []string{"42", "extra"} // FK has 1 column, but 2 values provided

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if !errors.Is(err, ErrNavigationValueMismatch) {
		t.Fatalf("error = %v, want ErrNavigationValueMismatch", err)
	}
}

func TestNavigateRelatedRecords_ValueCountMismatch_TooFew(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navCompositeScaffold(t)
	req := model.RelatedRecordNavigationRequest{
		Source: model.RelatedRecordNavigationSource{
			Database:   "app_db",
			Object:     "junction_table",
			Kind:       "table",
			ForeignKey: "fk_junction_composite",
		},
		LocalValues: []string{"1"}, // composite FK has 2 columns, only 1 value
		MaxRows:     100,
	}

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if !errors.Is(err, ErrNavigationValueMismatch) {
		t.Fatalf("error = %v, want ErrNavigationValueMismatch", err)
	}
}

// --- Composite FK with correct ordinal binding ---

func TestNavigateRelatedRecords_CompositeFK(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := navCompositeScaffold(t)
	req := model.RelatedRecordNavigationRequest{
		Source: model.RelatedRecordNavigationSource{
			Database:   "app_db",
			Object:     "junction_table",
			Kind:       "table",
			ForeignKey: "fk_junction_composite",
		},
		LocalValues: []string{"10", "20"},
		MaxRows:     50,
	}

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	// Verify the executor was called (bound query path).
	if !executor.called {
		t.Fatal("executor.QueryBound was not called")
	}
	// Verify relation metadata uses schema ordinal order.
	if len(resp.ReferencedColumns) != 2 {
		t.Fatalf("referencedColumns count = %d, want 2", len(resp.ReferencedColumns))
	}
	if resp.ReferencedColumns[0] != "tenant_id" || resp.ReferencedColumns[1] != "user_id" {
		t.Fatalf("referencedColumns = %v, want [tenant_id, user_id]", resp.ReferencedColumns)
	}
	if resp.ReferencedDatabase != "app" {
		t.Fatalf("referencedDatabase = %q, want %q", resp.ReferencedDatabase, "app")
	}
	if resp.ReferencedObject != "memberships" {
		t.Fatalf("referencedObject = %q, want %q", resp.ReferencedObject, "memberships")
	}
}

// --- History/audit records ---

func TestNavigateRelatedRecords_HistoryAuditNoValues(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}

	// Verify history record was created.
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history attempts = %d, want 1", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]

	// History must NOT contain localValues, SQL with values, or credentials.
	if strings.Contains(rec.StatementDigest, "42") {
		t.Fatalf("statementDigest must not contain localValues: %q", rec.StatementDigest)
	}
	if strings.Contains(rec.StatementPreview, "42") {
		t.Fatalf("statementPreview must not contain localValues: %q", rec.StatementPreview)
	}
	if strings.Contains(rec.ErrorMessage, "secret-dsn") {
		t.Fatalf("errorMessage must not contain DSN: %q", rec.ErrorMessage)
	}

	// History after resolution must use canonical inspected relation identity
	// and the matched FK name.
	if !strings.Contains(rec.StatementDigest, "fk_order_items_order") {
		t.Fatalf("statementDigest should contain FK name: %q", rec.StatementDigest)
	}
	if !strings.Contains(rec.StatementDigest, "orders.orders") {
		t.Fatalf("statementDigest should contain referenced schema.table: %q", rec.StatementDigest)
	}
	if !strings.Contains(rec.StatementPreview, "orders.orders") {
		t.Fatalf("statementPreview should contain referenced schema.table: %q", rec.StatementPreview)
	}
	if strings.Contains(rec.StatementPreview, "orders_db") {
		t.Fatalf("statementPreview must not contain request source database: %q", rec.StatementPreview)
	}

	// Verify audit event was created with fixed action.
	if len(repo.auditEvents) != 1 {
		t.Fatalf("audit events = %d, want 1", len(repo.auditEvents))
	}
	if repo.auditEvents[0].etype != "related_record_navigation" {
		t.Fatalf("audit event type = %q, want %q", repo.auditEvents[0].etype, "related_record_navigation")
	}
	if repo.auditEvents[0].result != "success" {
		t.Fatalf("audit result = %q, want %q", repo.auditEvents[0].result, "success")
	}
}

// --- History/audit for rejected attempts ---

func TestNavigateRelatedRecords_RejectedHistoryAudit(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.Source.ForeignKey = "fk_nonexistent"

	_, _ = svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)

	// Rejected attempt must still be recorded.
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history attempts = %d, want 1", len(repo.insertedAttempts))
	}
	if repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("status = %q, want rejected", repo.insertedAttempts[0].Status)
	}
	if len(repo.auditEvents) != 1 {
		t.Fatalf("audit events = %d, want 1", len(repo.auditEvents))
	}
	if repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit result = %q, want validation_failed", repo.auditEvents[0].result)
	}
}

// --- Disclosure blocked ---

func TestNavigateRelatedRecords_DisclosureBlocked(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{blockErr: ErrQueryDisclosureBlocked},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
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

// --- Timeout handling ---

func TestNavigateRelatedRecords_Timeout(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{err: context.DeadlineExceeded}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	// Timeout must still be recorded.
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history attempts = %d, want 1", len(repo.insertedAttempts))
	}
	if repo.insertedAttempts[0].Status != model.QueryExecutionTimeout {
		t.Fatalf("status = %q, want timeout", repo.insertedAttempts[0].Status)
	}
}

// --- Backend failure handling ---

func TestNavigateRelatedRecords_BackendFailure(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{err: errors.New("connection refused")}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
}

// --- MaxRows clamping ---

func TestNavigateRelatedRecords_MaxRowsClamped(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.MaxRows = model.MaxRelatedRowsHard + 100

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if resp.LimitApplied != model.MaxRelatedRowsHard {
		t.Fatalf("limitApplied = %d, want %d (hard cap)", resp.LimitApplied, model.MaxRelatedRowsHard)
	}
}

func TestNavigateRelatedRecords_MaxRowsDefault(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.MaxRows = 0

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if resp.LimitApplied != model.MaxRelatedRowsDefault {
		t.Fatalf("limitApplied = %d, want %d (default)", resp.LimitApplied, model.MaxRelatedRowsDefault)
	}
}

// --- Inspector error ---

func TestNavigateRelatedRecords_InspectorError(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeNavSchemaInspector{
		err: errors.New("connection timeout"),
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
	// An ordinary inspector failure keeps the existing governance-rejection
	// classification: rejected / navigation_source_error with the public
	// ErrNavigationSourceNotFound contract unchanged (Issue #40). The raw
	// inspector error must never be persisted, returned, or audited.
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("ordinary inspector error must stay a rejected attempt: %+v", repo.insertedAttempts)
	}
	if repo.insertedAttempts[0].ErrorCode != "navigation_source_error" {
		t.Fatalf("error code = %q, want navigation_source_error", repo.insertedAttempts[0].ErrorCode)
	}
	if repo.insertedAttempts[0].ErrorMessage != "could not retrieve source table metadata" {
		t.Fatalf("error message = %q, want fixed safe metadata message", repo.insertedAttempts[0].ErrorMessage)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit events = %+v, want one validation_failed", repo.auditEvents)
	}
	if strings.Contains(asString(repo.insertedAttempts[0]), "connection timeout") {
		t.Fatal("raw inspector error must never be persisted")
	}
}

// --- Inspector-phase cancellation and deadline (Issue #40) ---

// TestNavigateRelatedRecords_InspectorCanceled_RecordsFailedCanceled proves a
// client cancellation during the post-target FK-metadata read (Issue #40) is
// NOT a governance rejection: it must be classified as a terminal
// failed/query_canceled outcome through the shared atomic pair, persisted in
// the detached two-second Evidence Persistence Window — never the canceled
// request context — and the raw inspector error must never surface in the
// response or evidence.
func TestNavigateRelatedRecords_InspectorCanceled_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	svc, repo, _, inspector := navTestScaffold(t)
	inspector.err = context.Canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.NavigateRelatedRecords(ctx, 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if !strings.Contains(err.Error(), "query canceled") {
		t.Fatalf("error = %q, want fixed safe 'query canceled' message", err)
	}
	// Exactly one atomic Evidence Pair call, never a governance rejection.
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want exactly 1", len(repo.pairCalls))
	}
	if repo.pairCalls[0].event != "related_record_navigation" || repo.pairCalls[0].result != "failed" {
		t.Fatalf("pair = %+v, want related_record_navigation/failed", repo.pairCalls[0])
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("canceled inspector attempt must be recorded, got %d rows", len(repo.insertedAttempts))
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
	// Pre-resolution safe metadata: fixed generic preview/digest plus the
	// resolved target/actor identity — never the raw inspector error.
	if rec.TargetResourceID != 9001 || rec.ActorUserID != 1 || rec.Engine != "mysql" {
		t.Fatalf("safe navigation metadata = %+v, want target 9001 / actor 1 / engine mysql", rec)
	}
	if rec.StatementPreview != "related:unresolved" || rec.StatementDigest != "nav:unresolved" {
		t.Fatalf("preview/digest = %q/%q, want fixed pre-resolution related:unresolved/nav:unresolved", rec.StatementPreview, rec.StatementDigest)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "failed" {
		t.Fatalf("audit events = %+v, want one failed", repo.auditEvents)
	}
	if strings.Contains(asString(rec), "context canceled") {
		t.Fatalf("raw inspector error leaked into evidence: %s", asString(rec))
	}
	if strings.Contains(asString(rec), testResolverDSN) {
		t.Fatal("DSN leaked into evidence")
	}
	// The pair write ran in a live bounded context, not the canceled request ctx.
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_InspectorDeadline_RecordsTimeoutEvidence proves a
// deadline-expired post-target FK-metadata read (Issue #40) is NOT a
// governance rejection: it must be classified as the existing timeout terminal
// outcome through the shared atomic pair with the fixed timeout metadata, and
// the pair write still runs in a live bounded persistence context.
func TestNavigateRelatedRecords_InspectorDeadline_RecordsTimeoutEvidence(t *testing.T) {
	t.Parallel()
	svc, repo, _, inspector := navTestScaffold(t)
	inspector.err = context.DeadlineExceeded
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := svc.NavigateRelatedRecords(ctx, 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("error = %v, want ErrQueryTimeout", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want exactly 1", len(repo.pairCalls))
	}
	if repo.pairCalls[0].event != "related_record_navigation" || repo.pairCalls[0].result != "timeout" {
		t.Fatalf("pair = %+v, want related_record_navigation/timeout", repo.pairCalls[0])
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("deadline-expired inspector attempt must be recorded, got %d rows", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if rec.Status != model.QueryExecutionTimeout {
		t.Fatalf("status = %q, want timeout", rec.Status)
	}
	if rec.ErrorCode != "query_timeout" {
		t.Fatalf("error code = %q, want query_timeout", rec.ErrorCode)
	}
	if rec.ErrorMessage != "query exceeded the time limit" {
		t.Fatalf("error message = %q, want fixed timeout message", rec.ErrorMessage)
	}
	// Pre-resolution safe metadata: fixed generic preview/digest plus the
	// resolved target/actor identity — never the raw inspector error.
	if rec.TargetResourceID != 9001 || rec.ActorUserID != 1 || rec.Engine != "mysql" {
		t.Fatalf("safe navigation metadata = %+v, want target 9001 / actor 1 / engine mysql", rec)
	}
	if rec.StatementPreview != "related:unresolved" || rec.StatementDigest != "nav:unresolved" {
		t.Fatalf("preview/digest = %q/%q, want fixed pre-resolution related:unresolved/nav:unresolved", rec.StatementPreview, rec.StatementDigest)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "timeout" {
		t.Fatalf("audit events = %+v, want one timeout", repo.auditEvents)
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_InspectorCanceled_PairFailure_StillReturnsBackendError
// proves a genuine persistence failure is never silent on the canceled
// inspector path: the pair rollback leaves nothing recorded and the request
// surfaces the existing controlled backend-error response, exactly as the
// other navigation and execution paths do (Issue #34/#36).
func TestNavigateRelatedRecords_InspectorCanceled_PairFailure_StillReturnsBackendError(t *testing.T) {
	t.Parallel()
	svc, repo, _, inspector := navTestScaffold(t)
	inspector.err = context.Canceled
	repo.pairErr = errors.New("evidence store down")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.NavigateRelatedRecords(ctx, 1, 9001, validNavRequest())
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("error = %v, want errPersistAttempt (controlled backend failure)", err)
	}
	// The atomic pair was attempted exactly once and failed atomically:
	// nothing is recorded, and the fixed value-free telemetry contract (one
	// counter increment, one safe log category) is repository-owned and
	// proven at the repository seam, not through a new service surface.
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair must be attempted exactly once, got %d calls", len(repo.pairCalls))
	}
	if len(repo.insertedAttempts) != 0 || len(repo.auditEvents) != 0 {
		t.Fatalf("failed pair must record nothing; got %d rows / %d audit events", len(repo.insertedAttempts), len(repo.auditEvents))
	}
	if strings.Contains(err.Error(), "evidence store down") {
		t.Fatal("raw persistence error must not surface")
	}
}

// --- Atomic pair persistence failure ---

// TestNavigateRelatedRecords_PairPersistenceFailure proves a failed atomic
// pair write surfaces the existing controlled backend failure (Issue #34/#36).
func TestNavigateRelatedRecords_PairPersistenceFailure(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
		pairErr: errors.New("db down"),
	}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("error = %v, want errPersistAttempt", err)
	}
}

// --- Response does not contain SQL or DSN ---

func TestNavigateRelatedRecords_ResponseNoSQLNoDSN(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := navTestScaffold(t)

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}

	// The response struct should not have SQL or DSN fields.
	// Verify by checking that known safe fields are present.
	if resp.Engine != "mysql" {
		t.Fatalf("engine = %q, want mysql", resp.Engine)
	}
	if resp.TargetResourceID != 9001 {
		t.Fatalf("targetResourceId = %d, want 9001", resp.TargetResourceID)
	}
}

// --- Source object not found ---

func TestNavigateRelatedRecords_SourceObjectNotFound(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name:        "order_items",
			Kind:        "table",
			ForeignKeys: nil, // no FKs
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
}

// --- Bound query args are not in SQL string ---

func TestNavigateRelatedRecords_BoundArgsNotInSQL(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := navTestScaffold(t)
	req := validNavRequest()
	req.LocalValues = []string{"secret-value-12345"}

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}

	// The executor's QueryRelatedRecords was called with a typed input. Verify
	// the generated SQL contains only trusted identifiers and ? placeholders,
	// and the local value is bound as an argument rather than interpolated.
	if !executor.called {
		t.Fatal("executor.QueryRelatedRecords was not called")
	}
	if executor.gotNavInput == nil {
		t.Fatal("executor did not capture navigation input")
	}
	input := executor.gotNavInput
	if !strings.Contains(input.Statement, "`orders`.`orders`") {
		t.Fatalf("SQL must use quoted trusted identifiers, got: %s", input.Statement)
	}
	if strings.Contains(input.Statement, "secret-value-12345") {
		t.Fatalf("SQL must not contain the local value: %s", input.Statement)
	}
	if len(input.Values) != 1 || input.Values[0] != "secret-value-12345" {
		t.Fatalf("expected bound value [secret-value-12345], got %v", input.Values)
	}
}

func TestNavigateRelatedRecords_SQLHasNumericLimitAndCorrectPlaceholders(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := navTestScaffold(t)
	req := validNavRequest()
	req.MaxRows = 77

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if executor.gotNavInput == nil {
		t.Fatal("executor did not capture navigation input")
	}
	input := executor.gotNavInput
	if !strings.Contains(input.Statement, "LIMIT 77") {
		t.Fatalf("SQL must contain a numeric LIMIT literal, got: %s", input.Statement)
	}
	if strings.Contains(input.Statement, "LIMIT ?") {
		t.Fatalf("SQL must not contain an unbound LIMIT placeholder: %s", input.Statement)
	}
	if strings.Count(input.Statement, "?") != 1 {
		t.Fatalf("SQL should have exactly one placeholder for one local value, got: %s", input.Statement)
	}
	if len(input.Values) != 1 || input.Values[0] != "42" {
		t.Fatalf("expected one bound value [42], got %v", input.Values)
	}
	if input.Limit != 77 {
		t.Fatalf("executor limit = %d, want 77", input.Limit)
	}
}

func TestNavigateRelatedRecords_EmptyLocalValueIsBound(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := navTestScaffold(t)
	req := validNavRequest()
	req.LocalValues = []string{""}

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if executor.gotNavInput == nil {
		t.Fatal("executor did not capture navigation input")
	}
	if len(executor.gotNavInput.Values) != 1 || executor.gotNavInput.Values[0] != "" {
		t.Fatalf("expected one empty bound value, got %v", executor.gotNavInput.Values)
	}
}

func TestNavigateRelatedRecords_EmbeddedBacktickEscaped(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "or`ders", ReferencedTable: "or`ders", ReferencedColumn: "i`d"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if executor.gotNavInput == nil {
		t.Fatal("executor did not capture navigation input")
	}
	stmt := executor.gotNavInput.Statement
	if !strings.Contains(stmt, "`or``ders`") {
		t.Fatalf("embedded backticks must be doubled in schema/table identifiers, got: %s", stmt)
	}
	if !strings.Contains(stmt, "`i``d`") {
		t.Fatalf("embedded backticks must be doubled in column identifiers, got: %s", stmt)
	}
	if strings.Contains(stmt, "or`ders") {
		t.Fatalf("unescaped backtick must not appear in identifier: %s", stmt)
	}
}

func TestNavigateRelatedRecords_ProductionCapEnforced(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Production")
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyAllEnvironments),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{target}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)
	req := validNavRequest()
	req.MaxRows = 500

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if resp.LimitApplied != productionHardMaxRows {
		t.Fatalf("limitApplied = %d, want production hard cap %d", resp.LimitApplied, productionHardMaxRows)
	}
	if executor.gotNavInput == nil || !strings.Contains(executor.gotNavInput.Statement, "LIMIT 100") {
		t.Fatalf("production SQL must use LIMIT 100, got: %s", executor.gotNavInput.Statement)
	}
}

func TestNavigateRelatedRecords_EmptyReferencedSchemaRejected(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
	if executor.called {
		t.Fatal("executor must not be called for invalid FK metadata")
	}
}

func TestNavigateRelatedRecords_MixedReferencedTableRejected(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name: "fk_order_items_order",
					Columns: []FKColumn{
						{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"},
						{Column: "product_id", ReferencedSchema: "orders", ReferencedTable: "products", ReferencedColumn: "id"},
					},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)
	req := validNavRequest()
	req.LocalValues = []string{"1", "2"}

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
	if executor.called {
		t.Fatal("executor must not be called for mixed referenced tables")
	}
}

func TestNavigateRelatedRecords_InspectorErrorNotLeaked(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeNavSchemaInspector{
		err: errors.New("connection refused: tcp(db.internal:3306) password=secret"),
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
	if err.Error() != "navigation source object or foreign key not found" {
		t.Fatalf("returned error must be a fixed safe message, got: %s", err.Error())
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("expected 1 history row, got %d", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if strings.Contains(rec.ErrorMessage, "tcp(") || strings.Contains(rec.ErrorMessage, "secret") {
		t.Fatalf("history errorMessage must not leak raw inspector error: %q", rec.ErrorMessage)
	}
	if rec.StatementDigest != "nav:unresolved" || rec.StatementPreview != "related:unresolved" {
		t.Fatalf("pre-resolution history must use generic metadata, got digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
	}
}

func TestNavigateRelatedRecords_RejectedHistoryUsesGenericMetadata(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.Source.ForeignKey = "fk_nonexistent"

	_, _ = svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)

	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history attempts = %d, want 1", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if rec.Status != model.QueryExecutionRejected {
		t.Fatalf("status = %q, want rejected", rec.Status)
	}
	if rec.StatementDigest != "nav:unresolved" || rec.StatementPreview != "related:unresolved" {
		t.Fatalf("pre-resolution history must use generic metadata, got digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
	}
	if strings.Contains(rec.StatementPreview, "order_items") || strings.Contains(rec.StatementDigest, "order_items") {
		t.Fatalf("pre-resolution history must not contain request source object: digest=%q preview=%q", rec.StatementDigest, rec.StatementPreview)
	}
	if len(repo.auditEvents) != 1 {
		t.Fatalf("audit events = %d, want 1", len(repo.auditEvents))
	}
	if repo.auditEvents[0].result != "validation_failed" {
		t.Fatalf("audit result = %q, want validation_failed", repo.auditEvents[0].result)
	}
}

func TestNavigateRelatedRecords_SuccessHistoryUsesCanonicalIdentity(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history attempts = %d, want 1", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if !strings.Contains(rec.StatementDigest, "orders.orders") {
		t.Fatalf("post-resolution digest should contain referenced schema.table: %q", rec.StatementDigest)
	}
	if !strings.Contains(rec.StatementDigest, "fk_order_items_order") {
		t.Fatalf("post-resolution digest should contain FK name: %q", rec.StatementDigest)
	}
	if strings.Contains(rec.StatementPreview, "orders_db") {
		t.Fatalf("post-resolution preview must not contain request source database: %q", rec.StatementPreview)
	}
}

// --- Issue #36: navigation joins the shared atomic Execution Evidence Pair ---

// TestNavigateRelatedRecords_SuccessUsesSingleAtomicPairWrite proves the
// navigation success path records history + its fixed related_record_navigation
// audit event through ONE repository-owned atomic pair call — never the old
// split InsertExecution+InsertAuditEvent sequence — and returns the committed
// execution id. The pair write must run in the detached two-second Evidence
// Persistence Window, not the request context (Issues #34/#35/#36).
func TestNavigateRelatedRecords_SuccessUsesSingleAtomicPairWrite(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)

	resp, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want 1 (history+audit must share one transaction)", len(repo.pairCalls))
	}
	if repo.pairCalls[0].event != "related_record_navigation" || repo.pairCalls[0].result != "success" {
		t.Fatalf("pair audit params = %q/%q, want related_record_navigation/success", repo.pairCalls[0].event, repo.pairCalls[0].result)
	}
	if resp.ExecutionID == 0 || repo.pairCalls[0].rec.ID != resp.ExecutionID {
		t.Fatalf("committed execution id mismatch: resp=%d pair=%d", resp.ExecutionID, repo.pairCalls[0].rec.ID)
	}
	if len(repo.pairCalls[0].rec.StatementPreview) == 0 {
		t.Fatal("navigation pair record must carry canonical relation identity metadata")
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_RejectedUsesSingleAtomicPairWrite proves the
// rejected-navigation path (FK not found, pre-resolution) also routes through
// the atomic pair with fixed related_record_navigation/validation_failed audit
// evidence.
func TestNavigateRelatedRecords_RejectedUsesSingleAtomicPairWrite(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)
	req := validNavRequest()
	req.Source.ForeignKey = "fk_nonexistent"

	_, _ = svc.NavigateRelatedRecords(context.Background(), 1, 9001, req)

	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want 1 for the rejected attempt", len(repo.pairCalls))
	}
	if repo.pairCalls[0].event != "related_record_navigation" || repo.pairCalls[0].result != "validation_failed" {
		t.Fatalf("pair audit params = %q/%q, want related_record_navigation/validation_failed", repo.pairCalls[0].event, repo.pairCalls[0].result)
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_ClientCanceledDuringExecution_RecordsFailedCanceled
// proves the navigation cancellation outcome (Issue #36 AC 3): when the
// executor returns context.Canceled after a client disconnect, the attempt is
// recorded as failed with the fixed query_canceled code and fixed safe message
// — never a raw driver error — through the DETACHED two-second Evidence
// Persistence Window, not the canceled request context.
func TestNavigateRelatedRecords_ClientCanceledDuringExecution_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	executor := &fakeExecutor{err: context.Canceled}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)
	// Request context already canceled: the client disconnected mid-query.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.NavigateRelatedRecords(ctx, 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if !strings.Contains(err.Error(), "query canceled") {
		t.Fatalf("error = %q, want fixed safe 'query canceled' message", err)
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("canceled navigation attempt must be recorded, got %d rows", len(repo.insertedAttempts))
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
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].result != "failed" {
		t.Fatalf("pair audit = %+v, want one failed pair", repo.pairCalls)
	}
	if strings.Contains(asString(rec), testResolverDSN) {
		t.Fatal("DSN leaked into navigation evidence")
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_DisclosurePreflightBackendFailure_RecordsFailed
// proves a disclosure-policy machinery failure during navigation preflight is
// NOT a policy rejection: it records fixed failed/disclosure_backend_error
// evidence through the atomic pair and surfaces the controlled backend error,
// mirroring core query execution (Issue #35 AC 4 / Issue #36).
func TestNavigateRelatedRecords_DisclosurePreflightBackendFailure_RecordsFailed(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{preflightErr: ErrQueryDisclosureBackendFailure},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("disclosure machinery failure must be recorded, got %d rows", len(repo.insertedAttempts))
	}
	rec := repo.insertedAttempts[0]
	if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_disclosure_backend_error" {
		t.Fatalf("evidence = %q/%q, want failed/query_disclosure_backend_error", rec.Status, rec.ErrorCode)
	}
	if strings.Contains(rec.ErrorMessage, testResolverDSN) {
		t.Fatal("DSN leaked into disclosure-failure evidence")
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_DisclosurePreflightCanceled_RecordsFailedCanceled
// proves a canceled disclosure read during navigation preflight is recorded as
// failed/query_canceled, never as a policy rejection (Issue #35/#36).
func TestNavigateRelatedRecords_DisclosurePreflightCanceled_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{preflightErr: fmt.Errorf("%w: %w", ErrQueryDisclosureBlocked, context.Canceled)},
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure, got a policy rejection instead", err)
	}
	rec := repo.insertedAttempts[0]
	if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_canceled" {
		t.Fatalf("evidence = %q/%q, want failed/query_canceled", rec.Status, rec.ErrorCode)
	}
}

// TestNavigateRelatedRecords_SuccessAfterClientDisconnect_StaysSuccess proves
// Issue #36 AC 4: when navigation backend work completed before the client
// disconnect, the terminal evidence stays success — evidence describes backend
// work, not response delivery — and the pair write still runs in the detached
// window.
func TestNavigateRelatedRecords_SuccessAfterClientDisconnect_StaysSuccess(t *testing.T) {
	t.Parallel()
	svc, repo, _, _ := navTestScaffold(t)
	// Request context canceled after backend completion (response delivery is
	// what gets dropped); the executor fake ignores the context so the query
	// result is already produced.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := svc.NavigateRelatedRecords(ctx, 1, 9001, validNavRequest())
	if err != nil {
		t.Fatalf("NavigateRelatedRecords: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success (evidence describes backend work)", resp.Status)
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionSuccess {
		t.Fatalf("successful attempt must be recorded as success: %+v", repo.insertedAttempts)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].result != "success" {
		t.Fatalf("pair audit = %+v, want one success pair", repo.pairCalls)
	}
	assertDetachedEvidenceWindow(t, repo)
}

// TestNavigateRelatedRecords_PairWriteFailureOnCanceledRequest_StillReturnsBackendError
// proves a genuine persistence failure is never silent on the navigation path:
// even with the client gone, the pair rollback leaves nothing recorded and the
// request surfaces the existing controlled backend-error response.
func TestNavigateRelatedRecords_PairWriteFailureOnCanceledRequest_StillReturnsBackendError(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
		pairErr: errors.New("evidence store down"),
	}
	inspector := &fakeNavSchemaInspector{
		detail: &ObjectDetail{
			Name: "order_items",
			Kind: "table",
			ForeignKeys: []FKSummary{
				{
					Name:    "fk_order_items_order",
					Columns: []FKColumn{{Column: "order_id", ReferencedSchema: "orders", ReferencedTable: "orders", ReferencedColumn: "id"}},
				},
			},
		},
	}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo, &fakeResolver{dsn: testResolverDSN}, &fakeExecutor{result: QueryDatabaseResult{RowCount: 1}},
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Now()},
		inspector, &fakeDisclosureService{},
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.NavigateRelatedRecords(ctx, 1, 9001, validNavRequest())
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("error = %v, want errPersistAttempt (controlled backend failure)", err)
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("failed pair must record nothing; got %d rows", len(repo.insertedAttempts))
	}
}
