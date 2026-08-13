// Package api provides tests for the legacy hash count endpoint.
// input: crypto/hmac, crypto/sha256, encoding/base64, encoding/hex, encoding/json, fmt, net/http, net/http/httptest, testing, time
// output: TestGetLegacyHashCount_* tests
// pos: Validates admin-only access, non-identity-bearing response shape, and viewer/editor rejection
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
	"testing"
	"time"
)

const legacyHashTestSecret = "test-secret"

// TestGetLegacyHashCount_AdminReturnsCount proves an admin actor receives
// a 200 with a JSON object containing a "count" integer field.
func TestGetLegacyHashCount_AdminReturnsCount(t *testing.T) {
	server := NewTestServer()
	token := mintLegacyHashTestToken(t, 1, "admin", 1)
	req := httptest.NewRequest(http.MethodGet, "/admin/legacy-hash-count", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Count int64 `json:"count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Count < 0 {
		t.Fatalf("count must be non-negative, got %d", resp.Count)
	}
}

// TestGetLegacyHashCount_ViewerRejected proves a non-admin actor receives 403.
func TestGetLegacyHashCount_ViewerRejected(t *testing.T) {
	server := NewTestServer()
	token := mintLegacyHashTestToken(t, 43, "viewer", 1)
	req := httptest.NewRequest(http.MethodGet, "/admin/legacy-hash-count", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetLegacyHashCount_UnauthenticatedRejected proves an unauthenticated
// request receives 401.
func TestGetLegacyHashCount_UnauthenticatedRejected(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/admin/legacy-hash-count", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

// TestGetLegacyHashCount_ResponseNeverLeaksHashes proves the response body
// contains no password_hash, hash, or identity-bearing fields.
func TestGetLegacyHashCount_ResponseNeverLeaksHashes(t *testing.T) {
	server := NewTestServer()
	token := mintLegacyHashTestToken(t, 1, "admin", 1)
	req := httptest.NewRequest(http.MethodGet, "/admin/legacy-hash-count", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	for _, leak := range []string{"password_hash", "hash", "email", "admin@example", "secret123"} {
		if contains(body, leak) {
			t.Fatalf("response body leaks %q: %s", leak, body)
		}
	}

	// Must contain only "count" field.
	var resp map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["count"]; !ok {
		t.Fatalf("response must contain 'count' field, got: %s", body)
	}
	if len(resp) != 1 {
		t.Fatalf("response must contain exactly 1 field (count), got %d fields: %s", len(resp), body)
	}
}

// TestGetLegacyHashCount_AuthMatrix proves the full chi middleware chain
// through the router: anonymous → 401, valid editor → 403, admin → 200.
// This is a single table-driven router-level test that exercises
// requireAuthenticatedActor → requireAdminActor → handler.
func TestGetLegacyHashCount_AuthMatrix(t *testing.T) {
	server := NewTestServer()

	cases := []struct {
		desc       string
		token      string // empty = no Authorization header
		wantStatus int
	}{
		{"anonymous", "", http.StatusUnauthorized},
		{"editor", mintLegacyHashTestToken(t, 7, "editor", 1), http.StatusForbidden},
		{"admin", mintLegacyHashTestToken(t, 1, "admin", 1), http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/admin/legacy-hash-count", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			server.Router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: status = %d, want %d; body=%s", tc.desc, rec.Code, tc.wantStatus, rec.Body.String())
			}

			// Admin path must return a valid count JSON.
			if tc.wantStatus == http.StatusOK {
				var resp struct{ Count int64 `json:"count"` }
				if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if resp.Count < 0 {
					t.Fatalf("count must be non-negative, got %d", resp.Count)
				}
			}

			// Error paths must not leak identity.
			if tc.wantStatus != http.StatusOK {
				body := rec.Body.String()
				for _, leak := range []string{"password_hash", "hash", "email", "admin@example"} {
					if contains(body, leak) {
						t.Fatalf("%s: body leaks %q: %s", tc.desc, leak, body)
					}
				}
			}
		})
	}
}

// mintLegacyHashTestToken creates a valid bearer token for tests.
func mintLegacyHashTestToken(t *testing.T, userID uint64, role string, version uint64) string {
	t.Helper()
	_ = role // role is embedded in the test store, not the token
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	payload := fmt.Sprintf("%d:%d:%d", userID, version, now.Unix())
	mac := hmac.New(sha256.New, []byte(legacyHashTestSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
