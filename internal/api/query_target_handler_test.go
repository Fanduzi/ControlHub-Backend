// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestListQueryTargets*, TestListQueryTargets_*Filter*
// pos: Handler tests for GET /query-targets across engines, readiness, and filters
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func listQueryTargets(t *testing.T, server *TestServer, query string) model.QueryTargetListResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/query-targets"+query, nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}
	var resp model.QueryTargetListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func findTarget(t *testing.T, resp model.QueryTargetListResponse, id uint64) model.QueryTarget {
	t.Helper()
	for _, item := range resp.Items {
		if item.ResourceID == id {
			return item
		}
	}
	t.Fatalf("expected query target id %d in response, items: %v", id, resp.Items)
	return model.QueryTarget{}
}

func TestListQueryTargets_ReturnsItemsEnvelope(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "")

	// WHY: the frontend relies on a stable { items: [...] } envelope and a
	// non-null array so empty states render correctly.
	if resp.Items == nil {
		t.Fatal("expected non-nil items slice")
	}
	if len(resp.Items) == 0 {
		t.Fatal("expected at least one query target")
	}
}

func TestListQueryTargets_ClickHouseTargetIsCredentialRequiredSQL(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "")

	ch := findTarget(t, resp, 22)
	if ch.Capability.QueryKind != model.QueryKindSQL {
		t.Fatalf("clickhouse queryKind = %q, want sql", ch.Capability.QueryKind)
	}
	if ch.Capability.EditorMode != "sql" || ch.Capability.LanguageLabel != "SQL" {
		t.Fatalf("clickhouse capability = %+v, want sql/SQL", ch.Capability)
	}
	// Complete connection but no read-only credential metadata in Phase 36.
	if ch.Readiness != model.ReadinessCredentialRequired {
		t.Fatalf("clickhouse readiness = %q, want credential_required", ch.Readiness)
	}
	if ch.Governance.ExecutionEnabled {
		t.Fatal("executionEnabled must be false — Phase 36 never enables execution")
	}
	if !ch.Governance.AuditRequired {
		t.Fatal("auditRequired must be true")
	}
	aa := ch.AvailableActions
	if aa.Run || aa.Explain || aa.Export || aa.SaveSheet || aa.RequestAccess {
		t.Fatalf("availableActions must all be false, got %+v", aa)
	}
	if len(ch.SchemaPreview) != 0 {
		t.Fatalf("schemaPreview must be empty, got %v", ch.SchemaPreview)
	}
	if ch.ConnectionContext.ClusterID != 14 || ch.ConnectionContext.ClusterName == "" {
		t.Fatalf("cluster context not resolved: %+v", ch.ConnectionContext)
	}
	if ch.ConnectionContext.Engine != "clickhouse" || ch.ConnectionContext.Port != 8123 {
		t.Fatalf("connection context not preserved: %+v", ch.ConnectionContext)
	}
}

func TestListQueryTargets_CoversSupportedEngines(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "")

	want := map[uint64]model.QueryKind{
		22: model.QueryKindSQL,   // clickhouse
		23: model.QueryKindSQL,   // mysql
		24: model.QueryKindRedis, // redis
		25: model.QueryKindMongo, // mongodb
	}
	for id, wantKind := range want {
		target := findTarget(t, resp, id)
		if target.Capability.QueryKind != wantKind {
			t.Errorf("target %d queryKind = %q, want %q", id, target.Capability.QueryKind, wantKind)
		}
		// Every supported, fully-connected target must be credential_required.
		if target.Readiness != model.ReadinessCredentialRequired {
			t.Errorf("target %d readiness = %q, want credential_required", id, target.Readiness)
		}
	}
}

func TestListQueryTargets_UnknownEngineStaysVisibleAsUnsupported(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "")

	oracle := findTarget(t, resp, 26)
	if oracle.Capability.QueryKind != model.QueryKindUnsupported {
		t.Fatalf("oracle queryKind = %q, want unsupported", oracle.Capability.QueryKind)
	}
	if oracle.Readiness != model.ReadinessUnsupportedEngine {
		t.Fatalf("oracle readiness = %q, want unsupported_engine", oracle.Readiness)
	}
}

func TestListQueryTargets_MissingHostIsMissingConnection(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "")

	incomplete := findTarget(t, resp, 27)
	if incomplete.Readiness != model.ReadinessMissingConnection {
		t.Fatalf("missing-host target readiness = %q, want missing_connection", incomplete.Readiness)
	}
	if incomplete.Governance.SafetyState != model.SafetyStateConnectionIncomplete {
		t.Fatalf("missing-host safetyState = %q, want connection_incomplete", incomplete.Governance.SafetyState)
	}
}

func TestListQueryTargets_EngineFilter(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "?engine=redis")

	if len(resp.Items) != 1 {
		t.Fatalf("engine=redis: expected 1 target, got %d", len(resp.Items))
	}
	if resp.Items[0].Capability.QueryKind != model.QueryKindRedis {
		t.Fatalf("expected redis query kind, got %q", resp.Items[0].Capability.QueryKind)
	}
}

func TestListQueryTargets_EngineFilterCaseInsensitive(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "?engine=MySQL")

	if len(resp.Items) != 2 {
		t.Fatalf("engine=MySQL: expected 2 mysql targets, got %d", len(resp.Items))
	}
}

func TestListQueryTargets_EnvironmentIDFilter(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "?environmentId=2")

	if len(resp.Items) == 0 {
		t.Fatal("environmentId=2: expected staging targets")
	}
	for _, item := range resp.Items {
		if item.ConnectionContext.Environment != "Staging" {
			t.Errorf("environmentId=2 returned non-staging target %d (%q)", item.ResourceID, item.ConnectionContext.Environment)
		}
	}
}

func TestListQueryTargets_RejectsInvalidEnvironmentID(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/query-targets?environmentId=abc", nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "validation_failed")
}

func TestListQueryTargets_NoResultsReturnsEmptyArray(t *testing.T) {
	server := NewTestServer()
	// mongodb seed target exists, but cockroach does not.
	resp := listQueryTargets(t, server, "?engine=cockroach")

	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 targets for unknown engine filter, got %d", len(resp.Items))
	}
	// Envelope must still serialize an array, not null.
	if resp.Items == nil {
		t.Fatal("expected non-nil empty items array")
	}
}

// ---------------------------------------------------------------------------
// Phase 38H pagination contract RED tests
// ---------------------------------------------------------------------------

// TestListQueryTargets_DefaultResponseIncludesPageInfo proves the default
// response carries a pageInfo object with page=1, pageSize=50 (the
// query-targets specific defaults), and bounded totalItems/totalPages.
// WHY: the frontend needs pagination metadata to render pager controls even on
// the first page load; omitting pageInfo would force a client-side workaround.
func TestListQueryTargets_DefaultResponseIncludesPageInfo(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "")

	if resp.PageInfo == nil {
		t.Fatal("expected pageInfo in default response, got nil")
	}
	if resp.PageInfo.Page != 1 {
		t.Errorf("default page = %d, want 1", resp.PageInfo.Page)
	}
	if resp.PageInfo.PageSize != 50 {
		t.Errorf("default pageSize = %d, want 50", resp.PageInfo.PageSize)
	}
	if resp.PageInfo.TotalItems <= 0 {
		t.Errorf("totalItems = %d, want > 0", resp.PageInfo.TotalItems)
	}
	if resp.PageInfo.TotalPages <= 0 {
		t.Errorf("totalPages = %d, want > 0", resp.PageInfo.TotalPages)
	}
}

// TestListQueryTargets_CustomPageSizeReturnsCorrectPageInfo proves explicit
// page/pageSize parameters are reflected in pageInfo and the returned items
// count is bounded by pageSize.
// WHY: the frontend must be able to request a specific page and get accurate
// metadata to drive its pager component.
func TestListQueryTargets_CustomPageSizeReturnsCorrectPageInfo(t *testing.T) {
	server := NewTestServer()
	resp := listQueryTargets(t, server, "?page=1&pageSize=2")

	if resp.PageInfo == nil {
		t.Fatal("expected pageInfo, got nil")
	}
	if resp.PageInfo.Page != 1 {
		t.Errorf("page = %d, want 1", resp.PageInfo.Page)
	}
	if resp.PageInfo.PageSize != 2 {
		t.Errorf("pageSize = %d, want 2", resp.PageInfo.PageSize)
	}
	if len(resp.Items) > 2 {
		t.Errorf("items count = %d, want <= 2", len(resp.Items))
	}
}

// TestListQueryTargets_InvalidPagePageSizeReturns400 proves the handler
// rejects invalid pagination parameters with 400 rather than silently
// normalizing. Covers page=0, page=-1, page=abc, page too large, pageSize=0,
// pageSize=101, pageSize=abc.
// WHY: silent normalization hides client bugs and makes debugging harder; a 400
// with a clear error message tells the caller exactly what is wrong.
func TestListQueryTargets_InvalidPagePageSizeReturns400(t *testing.T) {
	server := NewTestServer()
	cases := []struct {
		name  string
		query string
	}{
		{"page=0", "?page=0"},
		{"page=-1", "?page=-1"},
		{"page=abc", "?page=abc"},
		{"page too large", "?page=3716778790613992773&pageSize=43"},
		{"pageSize=0", "?pageSize=0"},
		{"pageSize=101", "?pageSize=101"},
		{"pageSize=abc", "?pageSize=abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/query-targets"+tc.query, nil)
			rec := httptest.NewRecorder()
			server.Router.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("got %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestListQueryTargets_QSearchMatchesDisplayResourceNameHostEngineEnvironmentCluster
// proves the q parameter searches across display_name, resource_name, host,
// engine, environment, owner, and cluster_name — but never secret fields (DSN,
// password, credential_ref).
// WHY: the frontend needs a single search box to filter targets by any visible
// identifier; restricting it to safe fields prevents credential leakage.
func TestListQueryTargets_QSearchMatchesDisplayResourceNameHostEngineEnvironmentCluster(t *testing.T) {
	server := NewTestServer()

	// Search by display name substring
	resp := listQueryTargets(t, server, "?q=ClickHouse")
	if len(resp.Items) == 0 {
		t.Fatal("q=ClickHouse: expected at least one match")
	}
	found := false
	for _, item := range resp.Items {
		if item.ResourceID == 22 {
			found = true
		}
	}
	if !found {
		t.Error("q=ClickHouse: expected to find target 22 (Analytics ClickHouse)")
	}

	// Search by engine
	resp = listQueryTargets(t, server, "?q=redis")
	if len(resp.Items) == 0 {
		t.Fatal("q=redis: expected at least one match")
	}

	// Search by environment
	resp = listQueryTargets(t, server, "?q=Staging")
	if len(resp.Items) == 0 {
		t.Fatal("q=Staging: expected at least one match")
	}

	// Search by host
	resp = listQueryTargets(t, server, "?q=prod-mysql-host-01")
	if len(resp.Items) == 0 {
		t.Fatal("q=prod-mysql-host-01: expected at least one match")
	}

	// Search by owner
	resp = listQueryTargets(t, server, "?q=Platform+Team")
	if len(resp.Items) == 0 {
		t.Fatal("q=Platform Team: expected at least one match")
	}

	// Search by cluster name
	resp = listQueryTargets(t, server, "?q=Order+MySQL+Cluster")
	if len(resp.Items) == 0 {
		t.Fatal("q=Order MySQL Cluster: expected at least one match")
	}

	// Search by resource name
	resp = listQueryTargets(t, server, "?q=session-redis")
	if len(resp.Items) == 0 {
		t.Fatal("q=session-redis: expected at least one match")
	}
}

// TestListQueryTargets_TargetIdExactLookupReturnsTargetEvenOffPage proves
// targetId bypasses pagination and returns the requested target even when it
// would not appear on page 1.
// WHY: the frontend's deep-link /query?targetId=<id> must resolve the target
// regardless of its sort position or current page; without this, the user would
// see an empty workbench when linking to a target on page 2+.
func TestListQueryTargets_TargetIdExactLookupReturnsTargetEvenOffPage(t *testing.T) {
	server := NewTestServer()

	// Target 26 (oracle, last in seed order) is not on page 1 with pageSize=2.
	resp := listQueryTargets(t, server, "?targetId=26&page=1&pageSize=2")
	if resp.PageInfo == nil {
		t.Fatal("expected pageInfo, got nil")
	}
	if len(resp.Items) != 1 {
		t.Fatalf("targetId=26: expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].ResourceID != 26 {
		t.Errorf("targetId=26: got resourceId %d, want 26", resp.Items[0].ResourceID)
	}
	if resp.Items[0].Capability.QueryKind != model.QueryKindUnsupported {
		t.Errorf("targetId=26: queryKind = %q, want unsupported", resp.Items[0].Capability.QueryKind)
	}
}

// TestListQueryTargets_EngineEnvironmentFiltersComposeWithQPageSize proves
// existing engine/environmentId filters compose correctly with the new q,
// page, and pageSize parameters.
// WHY: the frontend must be able to combine search + filter + pagination in a
// single request; if they don't compose, the user has to choose between
// filtering and searching.
func TestListQueryTargets_EngineEnvironmentFiltersComposeWithQPageSize(t *testing.T) {
	server := NewTestServer()

	// engine=mysql + pageSize=1 should return 1 mysql target on page 1.
	resp := listQueryTargets(t, server, "?engine=mysql&pageSize=1&page=1")
	if resp.PageInfo == nil {
		t.Fatal("expected pageInfo, got nil")
	}
	if resp.PageInfo.PageSize != 1 {
		t.Errorf("pageSize = %d, want 1", resp.PageInfo.PageSize)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].ConnectionContext.Engine != "mysql" {
		t.Errorf("expected mysql engine, got %q", resp.Items[0].ConnectionContext.Engine)
	}

	// environmentId=2 + q=redis should find the staging redis target.
	resp = listQueryTargets(t, server, "?environmentId=2&q=redis")
	if len(resp.Items) != 1 {
		t.Fatalf("environmentId=2+q=redis: expected 1 item, got %d", len(resp.Items))
	}
	if resp.Items[0].ResourceID != 24 {
		t.Errorf("expected resourceId 24, got %d", resp.Items[0].ResourceID)
	}
}

// TestListQueryTargets_LargeFixtureReturnsOnlyPageSizeItems proves that when
// the seed has more targets than pageSize, only pageSize items are returned and
// totalPages reflects the full count.
// WHY: this is the fundamental pagination contract — DB-side LIMIT must bound
// the result set, not client-side slicing. If all items are returned regardless
// of pageSize, pagination is broken.
func TestListQueryTargets_LargeFixtureReturnsOnlyPageSizeItems(t *testing.T) {
	server := NewTestServer()

	// The seed has 6 targets. With pageSize=2 we should get exactly 2 on page 1
	// and totalPages should be 3.
	resp := listQueryTargets(t, server, "?pageSize=2&page=1")
	if resp.PageInfo == nil {
		t.Fatal("expected pageInfo, got nil")
	}
	if len(resp.Items) != 2 {
		t.Fatalf("page 1 with pageSize=2: expected 2 items, got %d", len(resp.Items))
	}
	if resp.PageInfo.TotalItems != 6 {
		t.Errorf("totalItems = %d, want 6", resp.PageInfo.TotalItems)
	}
	if resp.PageInfo.TotalPages != 3 {
		t.Errorf("totalPages = %d, want 3", resp.PageInfo.TotalPages)
	}

	// Page 2 should return the next 2 items.
	resp2 := listQueryTargets(t, server, "?pageSize=2&page=2")
	if len(resp2.Items) != 2 {
		t.Fatalf("page 2 with pageSize=2: expected 2 items, got %d", len(resp2.Items))
	}
	// Page 3 should return the last 2 items.
	resp3 := listQueryTargets(t, server, "?pageSize=2&page=3")
	if len(resp3.Items) != 2 {
		t.Fatalf("page 3 with pageSize=2: expected 2 items, got %d", len(resp3.Items))
	}
	// Page 4 should return empty.
	resp4 := listQueryTargets(t, server, "?pageSize=2&page=4")
	if len(resp4.Items) != 0 {
		t.Fatalf("page 4 with pageSize=2: expected 0 items, got %d", len(resp4.Items))
	}
}
