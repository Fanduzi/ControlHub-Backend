//go:build integration

// Package integration provides regression coverage for the forward-only seed
// credential remediation migration.
// input: database/sql, encoding/json, net/http, net/http/httptest, testing, internal/repository/mysql, internal/service
// output: TestSeedUsersDisabledAfterRemediation regression case
// pos: Proves migration 00016 deactivates both published seed users and kills their published credentials end to end
// note: if this file changes, update header and README.md
package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestSeedUsersDisabledAfterRemediation is the regression guard for Issue #13:
// migration 00016 must disable both published seed users (admin@example.com and
// editor@example.com) and increment their authorization_version, and both
// published credentials must stop signing in. The test fails on any migration
// chain where the seeds are still active, so reintroducing the published
// credentials — or a later migration re-enabling them — trips it.
func TestSeedUsersDisabledAfterRemediation(t *testing.T) {
	db := setupTestDB(t)
	assertSeedUsersInactive(t, db)

	authSvc := service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret)
	router := newAuthzTestRouter(authSvc)
	for _, email := range []string{"admin@example.com", "editor@example.com"} {
		assertSeedLoginRejected(t, router, email)
	}
}

func assertSeedUsersInactive(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, email := range []string{"admin@example.com", "editor@example.com"} {
		var isActive int
		var authzVersion uint64
		err := db.QueryRow(
			`SELECT is_active, authorization_version FROM users WHERE email = ?`,
			email,
		).Scan(&isActive, &authzVersion)
		if err != nil {
			t.Fatalf("read seed user %s: %v", email, err)
		}
		if isActive != 0 {
			t.Errorf("seed user %s is_active = %d, want 0 (disabled by remediation)", email, isActive)
		}
		if authzVersion < 2 {
			t.Errorf("seed user %s authorization_version = %d, want >= 2 (remediation must increment it)", email, authzVersion)
		}
	}
}

func assertSeedLoginRejected(t *testing.T, router http.Handler, email string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": "secret123"})
	if err != nil {
		t.Fatalf("marshal login body for %s: %v", email, err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("login %s status = %d, want 401; body=%s", email, rec.Code, rec.Body.String())
		return
	}
	// Login rejects with the same generic invalid_credentials shape as a bad
	// password, so the disabled state is never disclosed by the response.
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("401 body not JSON: %v body=%s", err, rec.Body.String())
	}
	if resp.Error != "invalid_credentials" {
		t.Errorf("login %s error = %q, want invalid_credentials; body=%s", email, resp.Error, rec.Body.String())
	}
	lower := strings.ToLower(rec.Body.String())
	if strings.Contains(lower, "disabled") || strings.Contains(lower, "inactive") {
		t.Errorf("login %s 401 body discloses account state: %s", email, rec.Body.String())
	}
}
