// Package api tests for the Issue #34 (38X-3A) admin-only query-evidence
// metrics operation.
// input: internal/api router, expvar, encoding/json, net/http/httptest
// output: TestQueryEvidenceMetrics* (endpoint 401/403/200 matrix, exact one-field shape, counter)
// pos: Proves the query-evidence persistence-failure counter is exposed only to administrators
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"expvar"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestQueryEvidenceMetricsOperatorBoundary proves the anonymous/editor/admin
// access matrix for GET /ops/query-evidence-metrics: 401 / 403 / 200.
func TestQueryEvidenceMetricsOperatorBoundary(t *testing.T) {
	router := testOpsRouter(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	adminTok := mintToken(t, "ops-test-secret", 42, "admin", now)
	editorTok := mintToken(t, "ops-test-secret", 7, "editor", now)

	tests := []struct {
		name   string
		token  string
		want   int
		reason string
	}{
		{"anonymous", "", http.StatusUnauthorized, "anonymous must receive 401"},
		{"editor", editorTok, http.StatusForbidden, "editor must receive 403"},
		{"admin", adminTok, http.StatusOK, "admin must receive 200"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/ops/query-evidence-metrics", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d (want %d): %s; body=%s", rec.Code, tc.want, tc.reason, rec.Body.String())
			}
		})
	}
}

// TestQueryEvidenceMetricsExactlyOneField proves the admin response contains
// exactly the one fixed field queryEvidencePersistenceFailures — no identity,
// target, statement, value, credential, DSN, request, or raw error data.
func TestQueryEvidenceMetricsExactlyOneField(t *testing.T) {
	router := testOpsRouter(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	adminTok := mintToken(t, "ops-test-secret", 42, "admin", now)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/query-evidence-metrics", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if len(raw) != 1 {
		t.Fatalf("response fields = %d, want exactly 1; body=%s", len(raw), rec.Body.String())
	}
	if _, ok := raw["queryEvidencePersistenceFailures"]; !ok {
		t.Fatalf("expected exactly queryEvidencePersistenceFailures, got %v", raw)
	}
}

// TestQueryEvidenceMetricsCounterPublished proves the dimensionless counter is
// published under its fixed expvar key so operational tooling can scrape it.
func TestQueryEvidenceMetricsCounterPublished(t *testing.T) {
	counter := expvar.Get("query_evidence_persistence_failures")
	if counter == nil {
		t.Fatal(`expvar key "query_evidence_persistence_failures" is not published`)
	}
	if counter.String() == "" {
		t.Fatal("counter has no value")
	}
}
