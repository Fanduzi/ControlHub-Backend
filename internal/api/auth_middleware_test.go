// Package api provides tests for query route auth middleware.
// input: crypto/hmac, crypto/sha256, encoding/base64, encoding/hex, net/http, net/http/httptest, time, internal/service, internal/model
// output: TestAuthenticatedActor*, TestFreshQueryActor*, TestAuthzVersion*, TestGeneric401*
// pos: Tests bearer extraction, current Authorization Version verification, actor context, query TTL, generic 401 equivalence
// note: if this file changes, update header and README.md
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// mintToken issues a versioned Backend Bearer Credential for tests.
// The role argument seeds/updates the shared test user store so VerifyToken
// loads current server-owned role; role is NOT embedded in the token payload.
func mintToken(t *testing.T, secret string, userID uint64, role string, issuedAt time.Time) string {
	t.Helper()
	version := testAuthUsers.SeedActorVersion(userID, role, 1)
	return mintVersionedToken(t, secret, userID, version, issuedAt)
}

// mintVersionedToken encodes id:authorizationVersion:issuedAt with HMAC-SHA256.
func mintVersionedToken(t *testing.T, secret string, userID uint64, authzVersion uint64, issuedAt time.Time) string {
	t.Helper()
	payload := fmt.Sprintf("%d:%d:%d", userID, authzVersion, issuedAt.Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + signature))
}

// testAuthUsers backs all package api handler/middleware tests so VerifyToken
// performs real current-state checks without MySQL.
var testAuthUsers = service.NewMemoryUserStore(
	model.UserCredential{ID: 1, Email: "admin@example.com", RoleName: "admin", IsActive: true, AuthorizationVersion: 1, PasswordHash: "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"},
	model.UserCredential{ID: 7, RoleName: "editor", IsActive: true, AuthorizationVersion: 1},
	model.UserCredential{ID: 42, RoleName: "admin", IsActive: true, AuthorizationVersion: 1},
	model.UserCredential{ID: 43, RoleName: "viewer", IsActive: true, AuthorizationVersion: 1},
)

func newMiddlewareAuthService(secret string) *service.AuthService {
	return service.NewAuthService(testAuthUsers, secret)
}

func TestAuthenticatedActorRejectsMissingBearerToken(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(time.Now())}
	h := requireAuthenticatedActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run without a bearer token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

func TestAuthenticatedActorRejectsInvalidBearerToken(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(time.Now())}
	h := requireAuthenticatedActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run with an invalid token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

func TestAuthenticatedActorStoresActorInContext(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}
	token := mintToken(t, "test-secret", 42, "admin", now)

	var capturedID uint64
	var capturedOK bool
	h := requireAuthenticatedActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID, capturedOK = actorUserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !capturedOK {
		t.Fatal("actor user id not present in context")
	}
	if capturedID != 42 {
		t.Fatalf("actor user id = %d, want 42", capturedID)
	}
}

// TestAuthenticatedActorRejectsTokenOlderThanEightHours proves the Operator
// Access Boundary lifetime is enforced on ordinary protected reads (AC2).
func TestAuthenticatedActorRejectsTokenOlderThanEightHours(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	// Token at 8h01m — beyond the fixed 8h bound, must be rejected.
	token := mintToken(t, "test-secret", 42, "admin", now.Add(-8*time.Hour-1*time.Minute))
	h := requireAuthenticatedActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run for a token beyond the fixed 8h bound")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resources", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

// TestAuthenticatedActorStaleRejectionEmitsAuthBearerRejected proves that a
// valid but stale bearer token (exceeding the fixed 8h freshness gate) on an
// ordinary protected read emits auth.bearer rejected with the verified actor id.
func TestAuthenticatedActorStaleRejectionEmitsAuthBearerRejected(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}
	spy := &spyEmitter{}

	// Token at 8h01m — beyond the fixed 8h bound.
	token := mintToken(t, "test-secret", 42, "admin", now.Add(-8*time.Hour-1*time.Minute))
	h := requireAuthenticatedActor(svc, cfg, spy)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run for a stale token")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resources", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(spy.events) != 1 {
		nEvents := len(spy.events)
		t.Fatalf("emitter called %d times, want 1", nEvents)
	}
	e := spy.events[0]
	if e.eventType != "auth.bearer" || e.result != "rejected" {
		t.Fatalf("event = %s/%s, want auth.bearer/rejected", e.eventType, e.result)
	}
	if e.actorUserID == nil {
		t.Fatal("expected verified actor on stale rejection, got nil")
	}
	if *e.actorUserID != 42 {
		t.Fatalf("actor = %d, want 42", *e.actorUserID)
	}
}

func TestFreshQueryActorRejectsMissingBearer(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run without a bearer token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

func TestFreshQueryActorRejectsMalformedBearer(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run with a malformed token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

func TestFreshQueryActorRejectsBadSignature(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	token := mintToken(t, "wrong-secret", 1, "admin", now)

	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run with a bad-signature token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

func TestFreshQueryActorAcceptsTokenWithinTTL(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	token := mintToken(t, "test-secret", 7, "editor", now.Add(-1*time.Hour))

	called := false
	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		id, ok := actorUserIDFromContext(r.Context())
		if !ok || id != 7 {
			t.Errorf("actor user id = (%d, %v), want (7, true)", id, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("handler must run for a token within TTL")
	}
}

func TestFreshQueryActorRejectsTokenOlderThanTTL(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	// WHY: governed-query freshness stays fixed at eight hours; an older
	// credential is a generic 401, same as other auth failures.
	token := mintToken(t, "test-secret", 7, "editor", now.Add(-9*time.Hour))

	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run for an expired token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

// TestFreshQueryActorStoresCurrentRole proves middleware stores the role from
// current authorization state (not a token-embedded role claim).
func TestFreshQueryActorStoresCurrentRole(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}
	token := mintToken(t, "test-secret", 42, "admin", now)

	var capturedRole string
	var capturedOK bool
	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedRole, capturedOK = actorRoleFromContext(r.Context())
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/query-targets/1/credential", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if !capturedOK {
		t.Fatal("actor role not present in context")
	}
	if capturedRole != "admin" {
		t.Fatalf("actor role = %q, want admin", capturedRole)
	}
}

// TestFreshQueryActorRejectsStaleAuthorizationVersion covers version mismatch
// at the router seam with the same generic 401 as missing/malformed tokens.
func TestFreshQueryActorRejectsStaleAuthorizationVersion(t *testing.T) {
	store := service.NewMemoryUserStore(model.UserCredential{
		ID: 100, RoleName: "admin", IsActive: true, AuthorizationVersion: 1,
	})
	svc := service.NewAuthService(store, "test-secret")
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	token := mintVersionedToken(t, "test-secret", 100, 1, now)
	if err := svc.ChangeUserRole(100, "editor"); err != nil {
		t.Fatalf("ChangeUserRole: %v", err)
	}

	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler must not run after Authorization Version bump")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

// TestFreshQueryActorRejectsDisabledUser covers disablement → generic 401.
func TestFreshQueryActorRejectsDisabledUser(t *testing.T) {
	store := service.NewMemoryUserStore(model.UserCredential{
		ID: 101, RoleName: "admin", IsActive: true, AuthorizationVersion: 1,
	})
	svc := service.NewAuthService(store, "test-secret")
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	token := mintVersionedToken(t, "test-secret", 101, 1, now)
	if err := svc.SetUserActive(101, false); err != nil {
		t.Fatalf("SetUserActive: %v", err)
	}

	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler must not run for disabled user")
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	assertGeneric401Body(t, rec)
}

// TestGeneric401EquivalenceAcrossAuthFailures proves missing/malformed/expired/
// version-mismatched credentials share one controlled 401 body shape.
func TestGeneric401EquivalenceAcrossAuthFailures(t *testing.T) {
	store := service.NewMemoryUserStore(model.UserCredential{
		ID: 102, RoleName: "admin", IsActive: true, AuthorizationVersion: 1,
	})
	svc := service.NewAuthService(store, "test-secret")
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}
	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler must not run")
	}))

	stale := mintVersionedToken(t, "test-secret", 102, 1, now)
	_ = svc.ChangeUserRole(102, "editor")

	requests := []struct {
		name   string
		header string
	}{
		{"missing", ""},
		{"malformed", "Bearer not-valid"},
		{"expired", "Bearer " + mintVersionedToken(t, "test-secret", 102, 2, now.Add(-9*time.Hour))},
		{"version_mismatch", "Bearer " + stale},
	}

	var bodies []string
	for _, tc := range requests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
		if tc.header != "" {
			req.Header.Set("Authorization", tc.header)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", tc.name, rec.Code)
		}
		assertGeneric401Body(t, rec)
		bodies = append(bodies, rec.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		if bodies[i] != bodies[0] {
			t.Fatalf("401 bodies differ: %q vs %q", bodies[0], bodies[i])
		}
	}
}

// TestFreshQueryActorEnforcesFixedEightHourConstant proves the governed-query
// freshness bound is a fixed eight-hour backend contract (Issue #21). A token
// issued 7h59m ago is accepted; a token issued 8h01m ago is rejected — regardless
// of any configuration value.
func TestFreshQueryActorEnforcesFixedEightHourConstant(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)

	// WHY: the middleware must use a hardcoded constant, not a configurable TTL.
	// The Clock injection must not affect the fixed bound.
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	// Token at 7h59m — within the fixed 8h bound, must be accepted.
	within := mintToken(t, "test-secret", 7, "editor", now.Add(-7*time.Hour-59*time.Minute))
	h := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+within)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("7h59m token: status = %d, want 200", rec.Code)
	}

	// Token at 8h01m — beyond the fixed 8h bound, must be rejected.
	beyond := mintToken(t, "test-secret", 7, "editor", now.Add(-8*time.Hour-1*time.Minute))
	h2 := requireFreshQueryActor(svc, cfg, service.NoopEmitter{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run for a token beyond the fixed 8h bound")
	}))
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req2.Header.Set("Authorization", "Bearer "+beyond)
	h2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("8h01m token: status = %d, want 401", rec2.Code)
	}
	assertGeneric401Body(t, rec2)
}

// TestValidIdentityWithoutRequiredRoleRemains403 uses a credential write path:
// a valid current editor identity receives 403, not 401.
func TestValidIdentityWithoutRequiredRoleRemains403(t *testing.T) {
	// Reuse the real router credential PUT admin gate.
	router := newCredentialRouter(&stubQueryCredential{})
	token := mintToken(t, "qc-test-secret", 43, "viewer", qeTestNow)

	rec := httptest.NewRecorder()
	req := qeRequest(http.MethodPut, "/query-targets/22/credential", `{"credentialRef":"X","enabled":true,"environmentPolicy":"non_prod_only"}`, token)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if env.Error != "forbidden" {
		t.Fatalf("error = %q, want forbidden; body=%s", env.Error, rec.Body.String())
	}
}

func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

// spyEmitter captures EmitAuthAudit calls for test assertions.
type spyEmitter struct {
	events []spyEvent
}

type spyEvent struct {
	eventType    string
	result       string
	actorUserID  *uint64
	targetResID  *uint64
}

func (s *spyEmitter) EmitAuthAudit(eventType, result string, actorUserID *uint64, targetResourceID *uint64) error {
	s.events = append(s.events, spyEvent{eventType: eventType, result: result, actorUserID: actorUserID, targetResID: targetResourceID})
	return nil
}

func assertGeneric401Body(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	body := rec.Body.String()
	// writeJSONError shape: {"error":"<code>","message":"<text>"}
	var env struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("401 body not JSON: %v; body=%s", err, body)
	}
	if env.Error != "unauthorized" {
		t.Fatalf("error = %q, want unauthorized", env.Error)
	}
	if env.Message != controlledUnauthorizedMessage {
		t.Fatalf("message = %q, want %q", env.Message, controlledUnauthorizedMessage)
	}
	lower := strings.ToLower(body)
	for _, leak := range []string{"password", "authorization_version", "version mismatch", "disabled", "expired", "signature", "secret"} {
		if strings.Contains(lower, leak) {
			t.Fatalf("401 body leaks %q: %s", leak, body)
		}
	}
}

// TestRequireAdminActorEmitsTargetForResourceRoute proves that a non-admin
// hitting /resources/{id}/profile emits auth.authorization denied WITH the
// parsed resource ID as target_resource_id.
func TestRequireAdminActorEmitsTargetForResourceRoute(t *testing.T) {
	emitter := &spyEmitter{}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	router := chi.NewRouter()
	router.Use(requireAuthenticatedActor(service.NewAuthService(testAuthUsers, "admin-emit-secret"), cfg, emitter))
	router.Use(requireAdminActor(emitter))
	router.Get("/resources/{id}/profile", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Viewer token (ID 43 is seeded as viewer in testAuthUsers).
	token := mintToken(t, "admin-emit-secret", 43, "viewer", now)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resources/99/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	 router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var deniedEvent *spyEvent
	for i := range emitter.events {
		if emitter.events[i].eventType == "auth.authorization" {
			deniedEvent = &emitter.events[i]
			break
		}
	}
	if deniedEvent == nil {
		t.Fatal("no auth.authorization denied event emitted")
	}
	if deniedEvent.targetResID == nil {
		t.Fatal("expected target_resource_id on /resources/{id} denial, got nil")
	}
	if *deniedEvent.targetResID != 99 {
		t.Fatalf("target_resource_id = %d, want 99", *deniedEvent.targetResID)
	}
}

// TestRequireAdminActorNilTargetForQueryTargetRoute proves that a non-admin
// hitting /query-targets/{id}/credential emits auth.authorization denied with
// nil target_resource_id (query-target IDs must not pollute audit_events).
func TestRequireAdminActorNilTargetForQueryTargetRoute(t *testing.T) {
	emitter := &spyEmitter{}
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}

	router := chi.NewRouter()
	router.Use(requireAuthenticatedActor(service.NewAuthService(testAuthUsers, "admin-emit-secret"), cfg, emitter))
	router.Use(requireAdminActor(emitter))
	router.Put("/query-targets/{id}/credential", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	token := mintToken(t, "admin-emit-secret", 43, "viewer", now)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/query-targets/22/credential", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	 router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	var deniedEvent *spyEvent
	for i := range emitter.events {
		if emitter.events[i].eventType == "auth.authorization" {
			deniedEvent = &emitter.events[i]
			break
		}
	}
	if deniedEvent == nil {
		t.Fatal("no auth.authorization denied event emitted")
	}
	if deniedEvent.targetResID != nil {
		t.Fatalf("expected nil target_resource_id on /query-targets denial, got %d", *deniedEvent.targetResID)
	}
}

// testOpsRouter builds a minimal router with AuthAuditEmitter for ops endpoint tests.
func testOpsRouter(t *testing.T) *chi.Mux {
	t.Helper()
	emitter := &spyEmitter{}
	svc := service.NewAuthService(testAuthUsers, "ops-test-secret")
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{Clock: fixedClock(now)}
	deps := Dependencies{
		AuthService:      svc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: cfg,
	}
	return NewRouter(deps)
}

// TestAuthAuditMetricsEndpointAdminOnly proves the ops endpoint requires admin role.
func TestAuthAuditMetricsEndpointAdminOnly(t *testing.T) {
	router := testOpsRouter(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	token := mintToken(t, "ops-test-secret", 43, "viewer", now)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/auth-audit-metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

// TestAuthAuditMetricsEndpointReturnsCounter proves the endpoint returns
// a JSON object with only the authAuditPersistenceFailures number.
func TestAuthAuditMetricsEndpointReturnsCounter(t *testing.T) {
	router := testOpsRouter(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	token := mintToken(t, "ops-test-secret", 42, "admin", now)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/auth-audit-metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		AuthAuditPersistenceFailures int64 `json:"authAuditPersistenceFailures"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, rec.Body.String())
	}
	if resp.AuthAuditPersistenceFailures < 0 {
		t.Fatalf("counter = %d, want >= 0", resp.AuthAuditPersistenceFailures)
	}
}

// TestAuthAuditMetricsNoLeak proves the response contains ONLY the expected
// field — no identity, no request values, no internal details.
func TestAuthAuditMetricsNoLeak(t *testing.T) {
	router := testOpsRouter(t)
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	token := mintToken(t, "ops-test-secret", 42, "admin", now)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ops/auth-audit-metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	expectedKeys := map[string]bool{"authAuditPersistenceFailures": true}
	for key := range raw {
		if !expectedKeys[key] {
			t.Fatalf("unexpected key %q in response", key)
		}
	}
}
