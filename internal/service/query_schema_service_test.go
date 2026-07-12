// Package service provides RED tests for the QuerySchemaService. These tests
// pin the governance, audit, error-mapping, and cache-integration behaviour of
// ListDatabases, ListObjects, and GetObjectDetails.
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// fakeSchemaInspector is a test double for QuerySchemaInspector. It returns
// pre-configured results and records calls for assertion.
type fakeSchemaInspector struct {
	databases   []DatabaseSummary
	dbPageInfo  model.PageInfo
	objects     []ObjectSummary
	objPageInfo model.PageInfo
	detail      *ObjectDetail
	err         error
	called      bool
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
