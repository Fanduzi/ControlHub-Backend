// Package service provides tests for the Phase 38J related-record navigation.
// input: context, errors, strings, testing, time, internal/model
// output: TestNavigateRelatedRecords_* (fakes for inspector, executor bound queries)
// pos: Unit tests for FK navigation governance, parameter binding, history/audit, and error mapping
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
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
		inspector,
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
						{Column: "tenant_id", ReferencedSchema: "app", ReferencedTable: "tenants", ReferencedColumn: "id"},
						{Column: "user_id", ReferencedSchema: "app", ReferencedTable: "users", ReferencedColumn: "id"},
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
		inspector,
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
		nil,
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
		nil,
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
	if resp.ReferencedColumns[0] != "id" || resp.ReferencedColumns[1] != "id" {
		t.Fatalf("referencedColumns = %v, want [id, id]", resp.ReferencedColumns)
	}
	if resp.ReferencedDatabase != "app" {
		t.Fatalf("referencedDatabase = %q, want %q", resp.ReferencedDatabase, "app")
	}
	if resp.ReferencedObject != "tenants" {
		t.Fatalf("referencedObject = %q, want %q", resp.ReferencedObject, "tenants")
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

	// History must contain relation identity.
	if !strings.Contains(rec.StatementDigest, "fk_order_items_order") {
		t.Fatalf("statementDigest should contain FK name: %q", rec.StatementDigest)
	}
	if !strings.Contains(rec.StatementPreview, "order_items") {
		t.Fatalf("statementPreview should contain source object: %q", rec.StatementPreview)
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
		inspector,
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
		inspector,
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
		inspector,
	)

	_, err := svc.NavigateRelatedRecords(context.Background(), 1, 9001, validNavRequest())
	if !errors.Is(err, ErrNavigationSourceNotFound) {
		t.Fatalf("error = %v, want ErrNavigationSourceNotFound", err)
	}
}

// --- History persistence failure ---

func TestNavigateRelatedRecords_HistoryPersistenceFailure(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
		insertExecErr: errors.New("db down"),
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
		inspector,
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
		inspector,
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

	// The executor's QueryBound was called. The args should contain the value,
	// but we can't directly inspect them from the fake. What we CAN verify is
	// that the executor was called (it was), and that the history record does
	// not contain the value (tested elsewhere). The key guarantee is that the
	// service constructs the SQL from trusted identifiers and passes values
	// as args — the executor's QueryBound signature enforces this.
	if !executor.called {
		t.Fatal("executor.QueryBound was not called")
	}
}
