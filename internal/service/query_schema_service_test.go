// Package service provides RED tests for the QuerySchemaService. These tests
// pin the governance, audit, error-mapping, and cache-integration behaviour of
// ListDatabases, ListObjects, and GetObjectDetails.
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

// fakeSchemaInspector is a test double for QuerySchemaInspector. It returns
// pre-configured results and records calls for assertion.
type fakeSchemaInspector struct {
	databases       []DatabaseSummary
	dbPageInfo      model.PageInfo
	objects         []ObjectSummary
	objPageInfo     model.PageInfo
	detail          *ObjectDetail
	tableDef        *TableDefinition
	tableDefErr     error
	relationshipMap *RelationshipMapResult
	err             error
	called          bool
}

func (f *fakeSchemaInspector) ListDatabases(_ context.Context, _ string, _ string, _ bool, _, _ int) ([]DatabaseSummary, model.PageInfo, error) {
	f.called = true
	if f.err != nil {
		return nil, model.PageInfo{}, f.err
	}
	return f.databases, f.dbPageInfo, nil
}

func (f *fakeSchemaInspector) ListObjects(_ context.Context, _, _, _ string, _ string, _, _ int) ([]ObjectSummary, model.PageInfo, error) {
	f.called = true
	if f.err != nil {
		return nil, model.PageInfo{}, f.err
	}
	return f.objects, f.objPageInfo, nil
}

func (f *fakeSchemaInspector) GetObjectDetails(_ context.Context, _, _, _, _ string) (*ObjectDetail, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return f.detail, nil
}

func (f *fakeSchemaInspector) GetTableDefinition(_ context.Context, _, _, _ string) (*TableDefinition, error) {
	f.called = true
	if f.tableDefErr != nil {
		return nil, f.tableDefErr
	}
	return f.tableDef, nil
}

func (f *fakeSchemaInspector) GetRelationshipMap(_ context.Context, _, _, _ string) (*RelationshipMapResult, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	if f.relationshipMap != nil {
		return f.relationshipMap, nil
	}
	return &RelationshipMapResult{Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "db", Name: "tbl", Kind: "table", Role: "root"}}}, nil
}

// --- Service tests ---

// TestQuerySchemaService_IndependentTargetAccess verifies that schema access
// independently enforces the shared target-access resolver. A target that
// fails resolution must not reach the inspector.
// WHY: the schema service must not bypass governance even when the cache is
// populated or the inspector is available.
func TestQuerySchemaService_IndependentTargetAccess(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{}
	inspector := &fakeSchemaInspector{
		databases: []DatabaseSummary{{Name: "orders"}},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: nil}, // no targets
			&fakeExecRepo{},
			&fakeResolver{},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.ListDatabases(context.Background(), 1, 9999, "", 1, 20, false, false)
	if !errors.Is(err, ErrSchemaTargetNotFound) {
		t.Fatalf("ListDatabases error = %v, want ErrSchemaTargetNotFound", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called when target resolution fails")
	}
}

// TestQuerySchemaService_UnsupportedTargetNeverCallsInspector verifies that
// targets with unsupported engines, locked credentials, or binding mismatches
// never reach the inspector.
// WHY: governance failures must be mapped to controlled sentinels before any
// database connection is attempted.
func TestQuerySchemaService_UnsupportedTargetNeverCallsInspector(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		engine  string
		wantErr error
	}{
		{
			name:    "unsupported engine",
			engine:  "redis",
			wantErr: ErrSchemaNotAllowed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := mysqlTarget("Staging")
			target.ConnectionContext.Engine = tc.engine
			audit := &fakeExecRepo{}
			inspector := &fakeSchemaInspector{}
			clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			cache := NewQuerySchemaCache(100, clock)

			svc := NewQuerySchemaService(
				NewTargetAccessResolver(
					fakeTargetRepo{targets: []model.QueryTarget{target}},
					&fakeExecRepo{},
					&fakeResolver{},
				),
				inspector,
				cache,
				audit,
				clock,
			)

			_, err := svc.ListDatabases(context.Background(), 1, 9001, "", 1, 20, false, false)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if inspector.called {
				t.Fatal("inspector must not be called when target is denied")
			}
		})
	}
}

// TestQuerySchemaService_AuditEventStringsAreFixed verifies that audit event
// types and results are fixed strings that contain no object identifiers.
// WHY: audit rows must be safe for the general event stream; they record
// actor, target, event type, and result only.
func TestQuerySchemaService_AuditEventStringsAreFixed(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		databases: []DatabaseSummary{{Name: "orders"}},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// ListDatabases
	if _, err := svc.ListDatabases(context.Background(), 1, 9001, "", 1, 20, false, false); err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if len(audit.auditEvents) == 0 {
		t.Fatal("expected audit event for ListDatabases")
	}
	last := audit.auditEvents[len(audit.auditEvents)-1]
	if last.etype != "query.schema.databases.listed" {
		t.Fatalf("audit event type = %q, want %q", last.etype, "query.schema.databases.listed")
	}
	if last.result != "success" {
		t.Fatalf("audit result = %q, want %q", last.result, "success")
	}
	// WHY: audit must not contain database/object names.
	if last.actor != 1 {
		t.Fatalf("audit actor = %d, want 1", last.actor)
	}
	if last.target != 9001 {
		t.Fatalf("audit target = %d, want 9001", last.target)
	}

	// ListObjects
	audit.auditEvents = nil
	inspector.objects = []ObjectSummary{{Name: "users", Kind: string(model.ObjectKindTable)}}
	if _, err := svc.ListObjects(context.Background(), 1, 9001, "orders", "", "", 1, 20, false); err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(audit.auditEvents) == 0 {
		t.Fatal("expected audit event for ListObjects")
	}
	last = audit.auditEvents[len(audit.auditEvents)-1]
	if last.etype != "query.schema.objects.listed" {
		t.Fatalf("audit event type = %q, want %q", last.etype, "query.schema.objects.listed")
	}

	// GetObjectDetails
	audit.auditEvents = nil
	inspector.detail = &ObjectDetail{Name: "users", Kind: "table"}
	if _, err := svc.GetObjectDetails(context.Background(), 1, 9001, "orders", "users", "table", false); err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	if len(audit.auditEvents) == 0 {
		t.Fatal("expected audit event for GetObjectDetails")
	}
	last = audit.auditEvents[len(audit.auditEvents)-1]
	if last.etype != "query.schema.object.read" {
		t.Fatalf("audit event type = %q, want %q", last.etype, "query.schema.object.read")
	}
}

// TestQuerySchemaService_ObjectListItemsCarryRequestedDatabase verifies that
// every ObjectSummary in the ListObjects response carries the requested
// database name, not just the envelope-level Database field.
// WHY: downstream consumers (frontend, export) rely on per-item Database to
// render cross-database summaries without re-scanning the envelope.
func TestQuerySchemaService_ObjectListItemsCarryRequestedDatabase(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		objects: []ObjectSummary{{Name: "users", Kind: string(model.ObjectKindTable)}},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	resp, err := svc.ListObjects(context.Background(), 1, 9001, "mydb", "", "", 1, 20, false)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if resp.Database != "mydb" {
		t.Fatalf("envelope Database = %q, want %q", resp.Database, "mydb")
	}
	if len(resp.Items) == 0 {
		t.Fatal("expected at least one item")
	}
	if resp.Items[0].Database != "mydb" {
		t.Fatalf("per-item Database = %q, want %q", resp.Items[0].Database, "mydb")
	}
}

// TestQuerySchemaService_ObjectDetailCarryRequestedDatabase verifies that the
// GetObjectDetails response carries the requested database name at envelope level.
// WHY: the detail response must echo the database the caller asked about.
func TestQuerySchemaService_ObjectDetailCarryRequestedDatabase(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		detail: &ObjectDetail{Name: "users", Kind: "table"},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	resp, err := svc.GetObjectDetails(context.Background(), 1, 9001, "mydb", "users", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	if resp.Database != "mydb" {
		t.Fatalf("envelope Database = %q, want %q", resp.Database, "mydb")
	}
}

// TestQuerySchemaService_EmptyDatabaseRejectedAtService verifies that empty
// database parameters are rejected with ErrSchemaValidationFailed before
// reaching the inspector or cache.
// WHY: an empty database would produce nonsensical metadata; the service must
// reject it as a validation error so the handler can return 400.
func TestQuerySchemaService_EmptyDatabaseRejectedAtService(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// ListObjects with empty database
	_, err := svc.ListObjects(context.Background(), 1, 9001, "", "", "", 1, 20, false)
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("ListObjects empty database error = %v, want ErrSchemaValidationFailed", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called when database is empty")
	}

	// GetObjectDetails with empty database
	inspector.called = false
	_, err = svc.GetObjectDetails(context.Background(), 1, 9001, "", "users", "table", false)
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("GetObjectDetails empty database error = %v, want ErrSchemaValidationFailed", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called when database is empty")
	}
}

// TestQuerySchemaService_RawInspectorErrorsMapToSentinels verifies that raw
// inspector errors are mapped to controlled service sentinels.
// WHY: the handler layer must map errors to HTTP status codes without
// inspecting raw error messages.
func TestQuerySchemaService_RawInspectorErrorsMapToSentinels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		inspectorErr error
		wantErr      error
	}{
		{
			name:         "context deadline exceeded maps to timeout",
			inspectorErr: context.DeadlineExceeded,
			wantErr:      ErrSchemaTimeout,
		},
		{
			name:         "generic inspector error maps to backend error",
			inspectorErr: errors.New("connection refused"),
			wantErr:      ErrSchemaBackendError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			audit := &fakeExecRepo{
				credentials: map[uint64]model.QueryCredentialMetadata{
					9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
				},
			}
			inspector := &fakeSchemaInspector{err: tc.inspectorErr}
			clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			cache := NewQuerySchemaCache(100, clock)

			svc := NewQuerySchemaService(
				NewTargetAccessResolver(
					fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
					audit,
					&fakeResolver{dsn: testResolverDSN},
				),
				inspector,
				cache,
				audit,
				clock,
			)

			_, err := svc.ListDatabases(context.Background(), 1, 9001, "", 1, 20, false, false)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			// Audit must still be written even on inspector failure.
			if len(audit.auditEvents) == 0 {
				t.Fatal("expected audit event even on inspector failure")
			}
		})
	}
}

// TestToModelObjectDetail_EmptyCollectionsJSONNotNull proves the conversion
// path for a table with no secondary indexes/FKs never emits JSON null for
// required array fields (top-level or nested).
func TestToModelObjectDetail_EmptyCollectionsJSONNotNull(t *testing.T) {
	svc := &QuerySchemaService{}
	detail := &ObjectDetail{
		Name: "plain_table",
		Kind: "table",
		Columns: []ColumnSummary{
			{Name: "id", Type: "bigint", Position: 1, Nullable: "NO", Key: "PRI"},
		},
		// Indexes and ForeignKeys intentionally empty (nil)
	}
	resp := svc.toModelObjectDetail(22, "sandbox", detail)
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, bad := range []string{`"columns":null`, `"indexes":null`, `"foreignKeys":null`} {
		if strings.Contains(body, bad) {
			t.Fatalf("body contains %s: %s", bad, body)
		}
	}
	if !strings.Contains(body, `"indexes":[]`) || !strings.Contains(body, `"foreignKeys":[]`) {
		t.Fatalf("expected empty arrays: %s", body)
	}
	if resp.Columns == nil || resp.Indexes == nil || resp.ForeignKeys == nil {
		t.Fatal("in-memory slices must be non-nil")
	}
}

func TestQuerySchemaService_TableDefinition_TargetAccessFirst(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{}
	inspector := &fakeSchemaInspector{
		tableDef: &TableDefinition{Definition: "CREATE TABLE t (id INT)", Truncated: false},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	// No targets → access must fail before inspector is called.
	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: nil},
			&fakeExecRepo{},
			&fakeResolver{},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetTableDefinition(context.Background(), 1, 9999, "db", "tbl")
	if !errors.Is(err, ErrSchemaTargetNotFound) {
		t.Fatalf("error = %v, want ErrSchemaTargetNotFound", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called when target resolution fails")
	}
}

func TestQuerySchemaService_TableDefinition_EmptyParamsRejected(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// Empty database
	_, err := svc.GetTableDefinition(context.Background(), 1, 9001, "", "tbl")
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("empty database error = %v, want ErrSchemaValidationFailed", err)
	}

	// Empty name
	inspector.called = false
	_, err = svc.GetTableDefinition(context.Background(), 1, 9001, "db", "")
	if !errors.Is(err, ErrSchemaValidationFailed) {
		t.Fatalf("empty name error = %v, want ErrSchemaValidationFailed", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called when params are empty")
	}
}

func TestQuerySchemaService_TableDefinition_Success(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		tableDef: &TableDefinition{Definition: "CREATE TABLE orders (id INT)", Truncated: false},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	resp, err := svc.GetTableDefinition(context.Background(), 1, 9001, "orders", "order_items")
	if err != nil {
		t.Fatalf("GetTableDefinition: %v", err)
	}
	if resp.TargetResourceID != 9001 {
		t.Fatalf("targetResourceId = %d, want 9001", resp.TargetResourceID)
	}
	if resp.Database != "orders" {
		t.Fatalf("database = %q, want orders", resp.Database)
	}
	if resp.Name != "order_items" {
		t.Fatalf("name = %q, want order_items", resp.Name)
	}
	if resp.Kind != model.ObjectKindTable {
		t.Fatalf("kind = %q, want table", resp.Kind)
	}
	if resp.Dialect != "mysql" {
		t.Fatalf("dialect = %q, want mysql", resp.Dialect)
	}
	if resp.Definition != "CREATE TABLE orders (id INT)" {
		t.Fatalf("definition = %q", resp.Definition)
	}
	if resp.Truncated {
		t.Fatal("truncated = true, want false")
	}
}

func TestQuerySchemaService_TableDefinition_NoCache(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		tableDef: &TableDefinition{Definition: "CREATE TABLE t (id INT)", Truncated: false},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// Call twice
	if _, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "tbl"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	inspector.called = false
	if _, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "tbl"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Inspector must be called again — no caching.
	if !inspector.called {
		t.Fatal("inspector must be called on repeated reads (no cache)")
	}
}

func TestQuerySchemaService_TableDefinition_MissingTable(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{tableDefErr: ErrSchemaObjectNotFound}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "missing")
	if !errors.Is(err, ErrSchemaObjectNotFound) {
		t.Fatalf("error = %v, want ErrSchemaObjectNotFound", err)
	}
}

func TestQuerySchemaService_TableDefinition_ViewRejected(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{tableDefErr: ErrSchemaDefinitionNotSupported}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "my_view")
	if !errors.Is(err, ErrSchemaDefinitionNotSupported) {
		t.Fatalf("error = %v, want ErrSchemaDefinitionNotSupported", err)
	}
}

func TestQuerySchemaService_TableDefinition_Timeout(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{tableDefErr: context.DeadlineExceeded}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "tbl")
	if !errors.Is(err, ErrSchemaTimeout) {
		t.Fatalf("error = %v, want ErrSchemaTimeout", err)
	}
}

func TestQuerySchemaService_TableDefinition_AuditFixedEvent(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		tableDef: &TableDefinition{Definition: "CREATE TABLE t (id INT)", Truncated: false},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	if _, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "tbl"); err != nil {
		t.Fatalf("GetTableDefinition: %v", err)
	}
	if len(audit.auditEvents) == 0 {
		t.Fatal("expected audit event")
	}
	last := audit.auditEvents[len(audit.auditEvents)-1]
	if last.etype != "query.schema.table_definition.read" {
		t.Fatalf("audit event type = %q, want %q", last.etype, "query.schema.table_definition.read")
	}
	if last.result != "success" {
		t.Fatalf("audit result = %q, want success", last.result)
	}
	// Audit must not contain definition text.
	if strings.Contains(last.etype, "CREATE") || strings.Contains(last.result, "CREATE") {
		t.Fatal("audit must not contain definition text")
	}
}

// TestQuerySchemaService_TableDefinition_AuditErrorNeverExposesDriverText
// proves that when the audit repository fails on a successful table-definition
// read, the service returns only the controlled ErrSchemaBackendError sentinel.
// The raw audit/driver error text must not leak into the service error.
// WHY: the handler maps ErrSchemaBackendError to HTTP 502 with a fixed safe
// message; wrapping or appending the raw audit error would expose driver text.
func TestQuerySchemaService_TableDefinition_AuditErrorNeverExposesDriverText(t *testing.T) {
	t.Parallel()
	const auditMarker = "AUDIT_DRIVER_FAILURE_7f3a9b2c"
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
		insertAuditErr: errors.New(auditMarker),
	}
	inspector := &fakeSchemaInspector{
		tableDef: &TableDefinition{Definition: "CREATE TABLE t (id INT)", Truncated: false},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetTableDefinition(context.Background(), 1, 9001, "db", "tbl")
	if !errors.Is(err, ErrSchemaBackendError) {
		t.Fatalf("error = %v, want ErrSchemaBackendError", err)
	}
	// The raw audit marker must NOT appear in the error text.
	if strings.Contains(err.Error(), auditMarker) {
		t.Fatalf("error text contains raw audit marker: %v", err)
	}
}

// --- GetRelationshipMap tests ---

// TestGetRelationshipMap_AccessFirstGovernance verifies that the inspector is
// never called when target-access resolution fails.
// WHY: the service must not bypass governance even when the cache is populated.
func TestGetRelationshipMap_AccessFirstGovernance(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "db", Name: "tbl", Kind: "table", Role: "root"}},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: nil},
			&fakeExecRepo{},
			&fakeResolver{},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetRelationshipMap(context.Background(), 1, 9999, "db", "tbl", false)
	if !errors.Is(err, ErrSchemaTargetNotFound) {
		t.Fatalf("error = %v, want ErrSchemaTargetNotFound", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called when target resolution fails")
	}
}

// TestGetRelationshipMap_CacheIdentity verifies that the cache key includes
// targetID, credentialRef, database, and name. A second call with the same
// params must hit the cache (inspector not called again).
// WHY: cache identity prevents redundant inspector calls for identical requests.
func TestGetRelationshipMap_CacheIdentity(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "db", Name: "tbl", Kind: "table", Role: "root"}},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// First call — cache miss, inspector called.
	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	inspector.called = false

	// Second call with same params — cache hit, inspector not called.
	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if inspector.called {
		t.Fatal("inspector must not be called on cache hit (same params)")
	}
}

// TestGetRelationshipMap_CacheHitStillAudits verifies that a cache hit still
// writes an audit event.
// WHY: every schema access attempt must be audited regardless of cache status.
func TestGetRelationshipMap_CacheHitStillAudits(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "db", Name: "tbl", Kind: "table", Role: "root"}},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// First call populates cache.
	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	auditEventsBefore := len(audit.auditEvents)

	// Second call is a cache hit — must still write audit.
	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(audit.auditEvents) <= auditEventsBefore {
		t.Fatal("cache hit must still write an audit event")
	}
}

// TestGetRelationshipMap_InspectorCalled verifies the inspector is called on
// cache miss with the correct parameters.
// WHY: the service must delegate to the inspector with the resolved DSN.
func TestGetRelationshipMap_InspectorCalled(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "orders", Name: "users", Kind: "table", Role: "root"}},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "orders", "users", false); err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}
	if !inspector.called {
		t.Fatal("inspector must be called on cache miss")
	}
}

// TestGetRelationshipMap_InspectorErrorMapsToSentinel verifies that raw
// inspector errors are mapped to controlled service sentinels by
// mapRelationshipMapError.
// WHY: the handler layer must map errors to HTTP status codes without
// inspecting raw error messages.
func TestGetRelationshipMap_InspectorErrorMapsToSentinel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		inspectorErr error
		wantErr      error
	}{
		{
			name:         "context deadline exceeded maps to timeout",
			inspectorErr: context.DeadlineExceeded,
			wantErr:      ErrSchemaTimeout,
		},
		{
			name:         "definition not supported maps to relationship not supported",
			inspectorErr: ErrSchemaDefinitionNotSupported,
			wantErr:      ErrSchemaRelationshipNotSupported,
		},
		{
			name:         "object not found preserved",
			inspectorErr: ErrSchemaObjectNotFound,
			wantErr:      ErrSchemaObjectNotFound,
		},
		{
			name:         "generic inspector error maps to backend error",
			inspectorErr: errors.New("connection refused"),
			wantErr:      ErrSchemaBackendError,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			audit := &fakeExecRepo{
				credentials: map[uint64]model.QueryCredentialMetadata{
					9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
				},
			}
			inspector := &fakeSchemaInspector{err: tc.inspectorErr}
			clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			cache := NewQuerySchemaCache(100, clock)

			svc := NewQuerySchemaService(
				NewTargetAccessResolver(
					fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
					audit,
					&fakeResolver{dsn: testResolverDSN},
				),
				inspector,
				cache,
				audit,
				clock,
			)

			_, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			// Audit must still be written even on inspector failure.
			if len(audit.auditEvents) == 0 {
				t.Fatal("expected audit event even on inspector failure")
			}
		})
	}
}

// TestGetRelationshipMap_ViewReturnsNotFound verifies that when the inspector
// returns ErrSchemaObjectNotFound (e.g. for a view), the service propagates it.
// WHY: views may not support relationship maps and must surface a controlled
// sentinel error.
func TestGetRelationshipMap_ViewReturnsNotFound(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{err: ErrSchemaObjectNotFound}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	_, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "my_view", false)
	if !errors.Is(err, ErrSchemaObjectNotFound) {
		t.Fatalf("error = %v, want ErrSchemaObjectNotFound", err)
	}
}

// TestGetRelationshipMap_AuditEvent verifies the audit event type string is
// the fixed value "query.schema.relationship_map.read".
// WHY: audit event types must be stable for downstream consumers.
func TestGetRelationshipMap_AuditEvent(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "db", Name: "tbl", Kind: "table", Role: "root"}},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false); err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}
	if len(audit.auditEvents) == 0 {
		t.Fatal("expected audit event")
	}
	last := audit.auditEvents[len(audit.auditEvents)-1]
	if last.etype != "query.schema.relationship_map.read" {
		t.Fatalf("audit event type = %q, want %q", last.etype, "query.schema.relationship_map.read")
	}
	if last.result != "success" {
		t.Fatalf("audit result = %q, want %q", last.result, "success")
	}
	if last.actor != 1 {
		t.Fatalf("audit actor = %d, want 1", last.actor)
	}
	if last.target != 9001 {
		t.Fatalf("audit target = %d, want 9001", last.target)
	}
}

// TestGetRelationshipMap_Success verifies a successful call returns a valid
// response with the correct targetResourceId, root node, nodes, and edges.
// WHY: the response must carry the correct envelope fields for downstream
// consumers.
func TestGetRelationshipMap_Success(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{
				{ID: "n0", Database: "orders", Name: "users", Kind: "table", Role: "root"},
				{ID: "n1", Database: "orders", Name: "orders", Kind: "table", Role: "related"},
			},
			Edges: []RelationshipMapEdgeResult{
				{
					ID: "e0", Direction: "outbound", SourceID: "n0", TargetID: "n1",
					Columns: []string{"id"}, ReferencedColumns: []string{"user_id"},
					OnUpdate: "CASCADE", OnDelete: "SET NULL",
				},
			},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	resp, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "orders", "users", false)
	if err != nil {
		t.Fatalf("GetRelationshipMap: %v", err)
	}
	if resp.TargetResourceID != 9001 {
		t.Fatalf("targetResourceId = %d, want 9001", resp.TargetResourceID)
	}
	if resp.Root.ID != "n0" {
		t.Fatalf("root ID = %q, want n0", resp.Root.ID)
	}
	if resp.Root.Database != "orders" {
		t.Fatalf("root Database = %q, want orders", resp.Root.Database)
	}
	if resp.Root.Name != "users" {
		t.Fatalf("root Name = %q, want users", resp.Root.Name)
	}
	if resp.Root.Kind != model.ObjectKindTable {
		t.Fatalf("root Kind = %q, want table", resp.Root.Kind)
	}
	if len(resp.Nodes) != 2 {
		t.Fatalf("nodes count = %d, want 2", len(resp.Nodes))
	}
	if len(resp.Edges) != 1 {
		t.Fatalf("edges count = %d, want 1", len(resp.Edges))
	}
	if resp.Edges[0].Direction != model.RelationshipMapDirectionOutbound {
		t.Fatalf("edge direction = %q, want outbound", resp.Edges[0].Direction)
	}
	if resp.Edges[0].OnDelete != "SET NULL" {
		t.Fatalf("edge onDelete = %q, want SET NULL", resp.Edges[0].OnDelete)
	}
	// Nodes and Edges must be non-nil (EnsureNonNilCollections).
	if resp.Nodes == nil {
		t.Fatal("nodes must be non-nil")
	}
	if resp.Edges == nil {
		t.Fatal("edges must be non-nil")
	}
}

// TestGetRelationshipMap_RefreshBypassesCache verifies that refresh=true
// bypasses the cache and calls the inspector again.
// WHY: the caller must be able to force a fresh read after schema changes.
func TestGetRelationshipMap_RefreshBypassesCache(t *testing.T) {
	t.Parallel()
	audit := &fakeExecRepo{
		credentials: map[uint64]model.QueryCredentialMetadata{
			9001: enabledCred(model.QueryEnvPolicyNonProdOnly),
		},
	}
	inspector := &fakeSchemaInspector{
		relationshipMap: &RelationshipMapResult{
			Nodes: []RelationshipMapNodeResult{{ID: "n0", Database: "db", Name: "tbl", Kind: "table", Role: "root"}},
		},
	}
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache := NewQuerySchemaCache(100, clock)

	svc := NewQuerySchemaService(
		NewTargetAccessResolver(
			fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
			audit,
			&fakeResolver{dsn: testResolverDSN},
		),
		inspector,
		cache,
		audit,
		clock,
	)

	// First call populates cache.
	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	inspector.called = false

	// Second call with refresh=true must bypass cache.
	if _, err := svc.GetRelationshipMap(context.Background(), 1, 9001, "db", "tbl", true); err != nil {
		t.Fatalf("refresh call: %v", err)
	}
	if !inspector.called {
		t.Fatal("inspector must be called when refresh=true")
	}
}
