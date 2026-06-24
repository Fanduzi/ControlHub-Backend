// Package api provides tests for query route auth middleware.
// input: crypto/hmac, crypto/sha256, encoding/base64, encoding/hex, net/http, net/http/httptest, time, internal/service
// output: TestAuthenticatedActor*, TestFreshQueryActor*
// pos: Tests bearer extraction, actor context storage, and query token freshness TTL
// note: if this file changes, update header and README.md
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/service"
)

// mintToken mirrors service.AuthService.issueToken so middleware tests can mint
// tokens for arbitrary actor IDs and issuedAt timestamps without going through
// login. It must stay byte-identical to the production issuer; the in-package
// service tests (TestVerifyTokenReturns*) guard that contract.
func mintToken(t *testing.T, secret string, userID uint64, role string, issuedAt time.Time) string {
	t.Helper()
	payload := fmt.Sprintf("%d:%s:%d", userID, role, issuedAt.Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + signature))
}

// newMiddlewareAuthService builds an AuthService for middleware tests. VerifyToken
// never touches the user repository, so a nil repo is safe here.
func newMiddlewareAuthService(secret string) *service.AuthService {
	return service.NewAuthService(nil, secret)
}

func TestAuthenticatedActorRejectsMissingBearerToken(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	h := requireAuthenticatedActor(svc)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run without a bearer token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthenticatedActorRejectsInvalidBearerToken(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	h := requireAuthenticatedActor(svc)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run with an invalid token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAuthenticatedActorStoresActorInContext(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	token := mintToken(t, "test-secret", 42, "admin", now)

	var capturedID uint64
	var capturedOK bool
	h := requireAuthenticatedActor(svc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestFreshQueryActorRejectsMissingBearer(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{TokenMaxAge: 8 * time.Hour, Clock: fixedClock(now)}

	h := requireFreshQueryActor(svc, cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run without a bearer token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestFreshQueryActorRejectsMalformedBearer(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{TokenMaxAge: 8 * time.Hour, Clock: fixedClock(now)}

	h := requireFreshQueryActor(svc, cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run with a malformed token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer not-a-valid-token")
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestFreshQueryActorRejectsBadSignature(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{TokenMaxAge: 8 * time.Hour, Clock: fixedClock(now)}

	// WHY: a token signed with a different secret must never reach the handler,
	// regardless of its age.
	token := mintToken(t, "wrong-secret", 1, "admin", now)

	h := requireFreshQueryActor(svc, cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run with a bad-signature token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestFreshQueryActorAcceptsTokenWithinTTL(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{TokenMaxAge: 8 * time.Hour, Clock: fixedClock(now)}

	// Issued one hour ago — well within the 8h TTL.
	token := mintToken(t, "test-secret", 7, "editor", now.Add(-1*time.Hour))

	called := false
	h := requireFreshQueryActor(svc, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	cfg := QueryExecutionAuthConfig{TokenMaxAge: 8 * time.Hour, Clock: fixedClock(now)}

	// WHY: query execution is a higher-risk surface than read/list routes, so a
	// token that is structurally valid but older than the bounded TTL must be
	// rejected with 401 even though its signature is correct.
	token := mintToken(t, "test-secret", 7, "editor", now.Add(-9*time.Hour))

	h := requireFreshQueryActor(svc, cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not run for an expired token")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/query-targets/1/execute", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// fixedClock returns a clock closure pinned to the given time, matching the
// QueryExecutionAuthConfig.Clock signature.
func fixedClock(now time.Time) func() time.Time {
	return func() time.Time { return now }
}

// TestFreshQueryActorStoresActorRole proves the fresh-bearer middleware stores
// the token's role in context (alongside the id) so credential write handlers can
// enforce the admin-only boundary. WHY: the role is the authorization signal for
// Phase 38A credential metadata writes; it must come from the verified token.
func TestFreshQueryActorStoresActorRole(t *testing.T) {
	svc := newMiddlewareAuthService("test-secret")
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	cfg := QueryExecutionAuthConfig{TokenMaxAge: 8 * time.Hour, Clock: fixedClock(now)}
	token := mintToken(t, "test-secret", 42, "admin", now)

	var capturedRole string
	var capturedOK bool
	h := requireFreshQueryActor(svc, cfg)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
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
