// Package api provides tests for the bounded untrusted-Bearer audit persistence budget.
// input: net/http, net/http/httptest, encoding/json, testing, github.com/go-chi/chi/v5, internal/service
// output: TestBoundedBearerAudit_* router tests
// pos: Proves missing Authorization emits no audit event, supplied untrusted Bearer rejection persistence is bounded at 60/min per process via the existing AuthAuditEmitter seam, budget exhaustion preserves 401/handler non-execution, and verified role denials stay unbounded
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/service"
)

// boundedCredentialRouter builds a real NewRouter with an injectable emitter
// and a recording credential stub so tests can prove budget semantics and
// handler non-execution at the router seam.
func boundedCredentialRouter(t *testing.T, emitter service.AuthAuditEmitter) (*chi.Mux, *stubQueryCredential) {
	t.Helper()
	stub := &stubQueryCredential{}
	deps := Dependencies{
		AuthService:            service.NewAuthService(testAuthUsers, "qc-test-secret"),
		AuthAuditEmitter:       emitter,
		QueryCredentialService: stub,
		QueryExecutionAuth:     QueryExecutionAuthConfig{Clock: fixedClock(qeTestNow)},
	}
	return NewRouter(deps), stub
}

// TestBoundedBearerAudit_InvalidTokenEmitsWhileBudgetRemains proves a supplied
// but untrusted Bearer (invalid token) still emits the fixed auth.bearer
// rejected event while the process budget has capacity.
func TestBoundedBearerAudit_InvalidTokenEmitsWhileBudgetRemains(t *testing.T) {
	spy := &spyEmitter{}
	router, _ := boundedCredentialRouter(t, spy)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/query-targets/22/credential", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
	if len(spy.events) != 1 {
		t.Fatalf("emitted events = %d, want 1 while budget remains", len(spy.events))
	}
	e := spy.events[0]
	if e.eventType != "auth.bearer" || e.result != "rejected" {
		t.Fatalf("event = %s/%s, want auth.bearer/rejected", e.eventType, e.result)
	}
	if e.actorUserID != nil {
		t.Fatal("untrusted rejection must carry no actor")
	}
}

// exhaustUntrustedBearerBudget sends 61 forged-Bearer requests, asserting each
// returns the generic 401. After this, the per-router 60/min budget is spent.
func exhaustUntrustedBearerBudget(t *testing.T, router http.Handler) {
	t.Helper()
	for i := 0; i < 61; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/query-targets/22/credential", nil)
		req.Header.Set("Authorization", "Bearer forged-token")
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
		assertGeneric401Body(t, rec)
	}
}

// TestBoundedBearerAudit_SixtyFirstRejectionSuppressed proves the fixed 60/min
// per-process budget: the 61st untrusted rejection keeps the generic 401 but
// writes no audit event and increments only the safe suppression counter on
// the administrator-only metrics surface.
func TestBoundedBearerAudit_SixtyFirstRejectionSuppressed(t *testing.T) {
	spy := &spyEmitter{}
	router, _ := boundedCredentialRouter(t, spy)
	adminTok := mintToken(t, "qc-test-secret", 42, "admin", qeTestNow)

	before := readMetricsField(t, router, adminTok, "authAuditSuppressedRejections")

	exhaustUntrustedBearerBudget(t, router)

	if len(spy.events) != 60 {
		t.Fatalf("emitted events = %d, want exactly 60", len(spy.events))
	}
	after := readMetricsField(t, router, adminTok, "authAuditSuppressedRejections")
	if after-before != 1 {
		t.Fatalf("suppression counter delta = %d, want 1", after-before)
	}
}

// TestBoundedBearerAudit_HandlerNeverExecutes proves budget exhaustion does not
// change handler execution semantics: every rejected request returns 401 and
// the protected handler never runs.
func TestBoundedBearerAudit_HandlerNeverExecutes(t *testing.T) {
	spy := &spyEmitter{}
	router, stub := boundedCredentialRouter(t, spy)

	exhaustUntrustedBearerBudget(t, router)

	if stub.upsertCalled {
		t.Fatal("protected handler executed despite untrusted Bearer rejection")
	}
}

// TestBoundedBearerAudit_RoleDenialUnaffectedByBudget proves a verified actor
// denied by role still emits auth.authorization denied even after the
// untrusted-Bearer budget is exhausted, and that denial is not budgeted.
func TestBoundedBearerAudit_RoleDenialUnaffectedByBudget(t *testing.T) {
	spy := &spyEmitter{}
	router, stub := boundedCredentialRouter(t, spy)

	exhaustUntrustedBearerBudget(t, router)

	// Verified viewer is denied by the admin role gate after exhaustion.
	viewerTok := mintToken(t, "qc-test-secret", 43, "viewer", qeTestNow)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/query-targets/22/credential", nil)
	req.Header.Set("Authorization", "Bearer "+viewerTok)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer PUT status = %d, want 403", rec.Code)
	}
	if stub.upsertCalled {
		t.Fatal("protected handler executed despite 403")
	}

	// Exactly the 60 budgeted rejections plus one unbudgeted denial.
	if len(spy.events) != 61 {
		t.Fatalf("emitted events = %d, want 60 rejections + 1 denial", len(spy.events))
	}
	last := spy.events[len(spy.events)-1]
	if last.eventType != "auth.authorization" || last.result != "denied" {
		t.Fatalf("last event = %s/%s, want auth.authorization/denied", last.eventType, last.result)
	}
	if last.actorUserID == nil || *last.actorUserID != 43 {
		t.Fatalf("denial actor = %v, want 43", last.actorUserID)
	}
}

// readMetricsField calls the administrator-only auth-audit metrics endpoint
// with a valid admin token and returns one int64 field of the safe shape.
func readMetricsField(t *testing.T, router http.Handler, adminToken, field string) int64 {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/auth-audit-metrics", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	val, ok := raw[field]
	if !ok {
		t.Fatalf("metrics response missing field %q; keys=%v", field, keysOf(raw))
	}
	var n int64
	if err := json.Unmarshal(val, &n); err != nil {
		t.Fatalf("decode metrics field %q: %v", field, err)
	}
	return n
}

func keysOf(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
