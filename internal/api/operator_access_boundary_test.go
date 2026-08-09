// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api router, auth test token helpers, net/http/httptest
// output: TestOperatorAccessBoundary
// pos: Router-level authorization matrix for anonymous, editor, and admin operators
// note: if this file changes, update header and README.md
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOperatorAccessBoundary(t *testing.T) {
	server := NewTestServer()
	deps := server.deps
	deps.AuthService = newMiddlewareAuthService("test-secret")
	router := NewRouter(deps)
	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	editorToken := mintToken(t, "test-secret", 7, "editor", now)
	adminToken := mintToken(t, "test-secret", 1, "admin", now)

	resourceBody := `{"resourceType":"database_instance","resourceSubtype":"mysql","name":"operator-boundary-resource","displayName":"Operator Boundary Resource","environmentId":1,"ownerId":2,"lifecycleStatus":"running","healthStatus":"healthy","source":"manual","labels":{}}`
	cases := []struct {
		name       string
		token      string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{"anonymous inventory read", "", http.MethodGet, "/resources", "", http.StatusUnauthorized},
		{"anonymous inventory mutation", "", http.MethodPost, "/resources", resourceBody, http.StatusUnauthorized},
		{"anonymous audit read", "", http.MethodGet, "/audit-events", "", http.StatusUnauthorized},
		{"anonymous resource audit read", "", http.MethodGet, "/resources/1/audit-events", "", http.StatusUnauthorized},
		{"anonymous query surface", "", http.MethodGet, "/query-targets", "", http.StatusUnauthorized},
		{"editor inventory read", editorToken, http.MethodGet, "/resources", "", http.StatusOK},
		{"editor resource detail read", editorToken, http.MethodGet, "/resources/1", "", http.StatusOK},
		{"editor inventory mutation", editorToken, http.MethodPost, "/resources", resourceBody, http.StatusForbidden},
		{"editor audit read", editorToken, http.MethodGet, "/audit-events", "", http.StatusForbidden},
		{"editor resource audit read", editorToken, http.MethodGet, "/resources/1/audit-events", "", http.StatusForbidden},
		{"editor query surface", editorToken, http.MethodGet, "/query-targets", "", http.StatusOK},
		{"admin inventory mutation", adminToken, http.MethodPost, "/resources", resourceBody, http.StatusCreated},
		{"admin audit read", adminToken, http.MethodGet, "/audit-events", "", http.StatusOK},
		{"admin resource audit read", adminToken, http.MethodGet, "/resources/1/audit-events", "", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
