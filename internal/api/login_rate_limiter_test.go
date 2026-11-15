package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func fakeClock(start time.Time) (func() time.Time, func(d time.Duration)) {
	now := start
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestLoginRateLimiterIPBoundaryAndExpiry(t *testing.T) {
	now, advance := fakeClock(time.Unix(1700000000, 0))
	l := newLoginRateLimiter(now)

	for i := 0; i < loginAttemptsPerRemoteIP; i++ {
		if !l.allow("10.0.0.1:1234", fmt.Sprintf("a%d@example.com", i)) {
			t.Fatalf("attempt %d refused before IP quota", i+1)
		}
	}
	if l.allow("10.0.0.1:9999", "b@example.com") {
		t.Fatal("same IP behind a different source port must share the bucket")
	}

	advance(loginRateWindow)
	if !l.allow("10.0.0.1:1", "a@example.com") {
		t.Fatal("IP quota should reset after the window")
	}
}

func TestLoginRateLimiterAccountNormalizedAcrossIPs(t *testing.T) {
	l := newLoginRateLimiter(time.Now)

	for i := 0; i < loginAttemptsPerAccount; i++ {
		if !l.allow("10.0.0.2:1", "  User@Example.COM ") {
			t.Fatalf("attempt %d refused before account quota", i+1)
		}
	}
	if l.allow("10.0.0.3:1", "user@example.com") {
		t.Fatal("normalized account quota should hold across IPs and casing")
	}
	if !l.allow("10.0.0.3:1", "other@example.com") {
		t.Fatal("a different account on the same IP must be independent")
	}
}

func TestLoginRateLimiterKeySaturationAndReclamation(t *testing.T) {
	now, advance := fakeClock(time.Unix(1700000000, 0))
	l := newLoginRateLimiter(now)

	for i := 0; i < loginMaxTrackedKeys; i++ {
		l.entries[fmt.Sprintf("account:u%d@example.com", i)] = &loginWindowCounter{start: now(), count: 1}
	}
	if l.allow("10.0.0.9:1", "brand-new@example.com") {
		t.Fatal("new IP+account keys must be rejected when the map is saturated")
	}
	if len(l.entries) != loginMaxTrackedKeys {
		t.Fatalf("refused attempt grew the map to %d keys", len(l.entries))
	}

	advance(loginRateWindow)
	if !l.allow("10.0.0.9:1", "brand-new@example.com") {
		t.Fatal("expired keys should be pruned so a new key is admitted")
	}
}

func TestLoginRateLimiterBlockedIPDrainsNoAccountQuota(t *testing.T) {
	l := newLoginRateLimiter(time.Now)

	for i := 0; i < loginAttemptsPerRemoteIP; i++ {
		l.allow("10.0.0.7:1", fmt.Sprintf("filler%d@example.com", i))
	}
	if l.allow("10.0.0.7:1", "fresh@example.com") {
		t.Fatal("IP-blocked attempt should be refused")
	}
	if l.entries["account:fresh@example.com"] != nil {
		t.Fatal("refused attempt allocated an account counter")
	}
	if !l.allow("10.0.0.8:1", "fresh@example.com") {
		t.Fatal("the same account from an unblocked IP should start at zero")
	}
}

func TestLoginHandlerRefuses429BeforeAuth(t *testing.T) {
	limiter := newLoginRateLimiter(time.Now)
	for i := 0; i < loginAttemptsPerAccount; i++ {
		limiter.allow("10.0.0.5:1", "victim@example.com")
	}

	handler := handleLogin(nil, nil, limiter)
	req := httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewBufferString(`{"email":"victim@example.com","password":"x"}`))
	req.RemoteAddr = "10.0.0.5:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}
	assertAPIError(t, rec, http.StatusTooManyRequests, "login_rate_limited")
}

func TestLoginHandlerIgnoresForwardedForHeader(t *testing.T) {
	limiter := newLoginRateLimiter(time.Now)
	for i := 0; i < loginAttemptsPerRemoteIP; i++ {
		limiter.allow("10.0.0.6:1", fmt.Sprintf("filler%d@example.com", i))
	}

	handler := handleLogin(nil, nil, limiter)
	req := httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewBufferString(`{"email":"a@example.com","password":"x"}`))
	req.RemoteAddr = "10.0.0.6:1"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("X-Forwarded-For must not reset the IP bucket, got %d", rec.Code)
	}
}

func TestRouterNoLongerAnswersGlobalCORS(t *testing.T) {
	server := NewTestServer()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected Access-Control-Allow-Origin %q", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/health", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec = httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if rec.Code == http.StatusNoContent {
		t.Fatal("OPTIONS must no longer be swallowed as 204")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected Access-Control-Allow-Origin %q", got)
	}
}
