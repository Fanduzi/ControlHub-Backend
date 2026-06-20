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
		22: model.QueryKindSQL,    // clickhouse
		23: model.QueryKindSQL,    // mysql
		24: model.QueryKindRedis,  // redis
		25: model.QueryKindMongo,  // mongodb
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
