//go:build integration

// Package integration provides real-MySQL coverage for Authorization Version invalidation.
// input: context, crypto/hmac, database/sql, net/http, internal/api, internal/model, internal/repository/mysql, internal/service
// output: TestAuthorizationVersion_* integration cases
// pos: Proves an already-issued Backend Bearer Credential becomes invalid after role change, disablement, or password reset against real MySQL
// note: if this file changes, update header and README.md
package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const authzIntegrationSecret = "authz-integration-secret"

// Same password hash as migration 0002 seed users.
const seedPasswordHash = "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"

// TestAuthorizationVersion_IssuedCredentialInvalidatedAfterRoleChange proves a
// live Bearer credential is rejected with generic 401 after demotion on real MySQL.
func TestAuthorizationVersion_IssuedCredentialInvalidatedAfterRoleChange(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)
	assertUserAuthzColumns(t, db)

	userID := insertAuthzTestUser(t, db, "authz-role@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	router := newAuthzTestRouter(authSvc)

	token := mustLogin(t, router, "authz-role@example.com", "secret123")
	assertAuthorized(t, router, token)

	if err := authSvc.ChangeUserRole(userID, "editor"); err != nil {
		t.Fatalf("ChangeUserRole: %v", err)
	}

	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after demotion status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	assertGenericUnauthorizedBody(t, rec.Body.String())

	// Fresh login after demotion authenticates; role is editor from server state.
	fresh := mustLogin(t, router, "authz-role@example.com", "secret123")
	assertAuthorized(t, router, fresh)
}

// TestAuthorizationVersion_DisablementInvalidatesCredential covers disablement.
func TestAuthorizationVersion_DisablementInvalidatesCredential(t *testing.T) {
	db := setupTestDB(t)
	userID := insertAuthzTestUser(t, db, "authz-disable@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	router := newAuthzTestRouter(authSvc)

	token := mustLogin(t, router, "authz-disable@example.com", "secret123")
	assertAuthorized(t, router, token)

	if err := authSvc.SetUserActive(userID, false); err != nil {
		t.Fatalf("SetUserActive: %v", err)
	}
	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after disablement status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	assertGenericUnauthorizedBody(t, rec.Body.String())
}

// TestAuthorizationVersion_PasswordResetInvalidatesCredential covers password reset.
func TestAuthorizationVersion_PasswordResetInvalidatesCredential(t *testing.T) {
	db := setupTestDB(t)
	userID := insertAuthzTestUser(t, db, "authz-pw@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	router := newAuthzTestRouter(authSvc)

	token := mustLogin(t, router, "authz-pw@example.com", "secret123")
	assertAuthorized(t, router, token)

	if err := authSvc.ResetUserPassword(userID, "rotated-password-value"); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after password reset status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	assertGenericUnauthorizedBody(t, rec.Body.String())

	if _, err := authSvc.Login("authz-pw@example.com", "secret123"); err == nil {
		t.Fatal("old password still accepted after reset")
	}
	if _, err := authSvc.Login("authz-pw@example.com", "rotated-password-value"); err != nil {
		t.Fatalf("new password login: %v", err)
	}
}

// TestAuthorizationVersion_QueryFreshnessRemainsEightHours proves governed-query
// freshness stays fixed at eight hours alongside Authorization Version checks.
func TestAuthorizationVersion_QueryFreshnessRemainsEightHours(t *testing.T) {
	db := setupTestDB(t)
	userID := insertAuthzTestUser(t, db, "authz-ttl@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	user, err := userRepo.FindByID(userID)
	if err != nil || user == nil {
		t.Fatalf("FindByID: %v %#v", err, user)
	}

	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	router := api.NewRouter(api.Dependencies{
		AuthService: authSvc,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       func() time.Time { return now },
		},
		QueryCredentialService: &authzCredStub{},
	})

	stale := mintIntegrationToken(t, user.ID, user.AuthorizationVersion, now.Add(-9*time.Hour))
	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", stale)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("9h-old token status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	assertGenericUnauthorizedBody(t, rec.Body.String())

	fresh := mintIntegrationToken(t, user.ID, user.AuthorizationVersion, now.Add(-1*time.Hour))
	assertAuthorized(t, router, fresh)
}

// TestAuthorizationVersion_ValidEditorForbiddenOnAdminWrite proves a valid
// current editor identity gets 403 (not 401) on an admin-only credential write.
func TestAuthorizationVersion_ValidEditorForbiddenOnAdminWrite(t *testing.T) {
	db := setupTestDB(t)
	userID := insertAuthzTestUser(t, db, "authz-forbid@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	router := newAuthzTestRouter(authSvc)

	token := mustLogin(t, router, "authz-forbid@example.com", "secret123")
	body := `{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only"}`
	rec := doBearerWithBody(t, router, http.MethodPut, "/query-targets/1/credential", token, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor PUT status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != "forbidden" {
		t.Fatalf("error = %q, want forbidden", env.Error)
	}
}

func newAuthzTestRouter(authSvc *service.AuthService) http.Handler {
	return api.NewRouter(api.Dependencies{
		AuthService: authSvc,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})
}

type authzCredStub struct{}

func (authzCredStub) GetStatus(_ context.Context, _ uint64) (model.QueryCredentialStatusResponse, error) {
	return model.QueryCredentialStatusResponse{
		ResourceID:        1,
		Configured:        false,
		ExecutionEligible: false,
		Message:           "No read-only credential reference is configured.",
	}, nil
}

func (authzCredStub) Upsert(_ context.Context, _ service.AuthenticatedUser, _ uint64, _ model.QueryCredentialUpsertRequest) (model.QueryCredentialStatusResponse, error) {
	return model.QueryCredentialStatusResponse{}, nil
}

func (authzCredStub) Delete(_ context.Context, _ service.AuthenticatedUser, _ uint64) error {
	return nil
}

func insertAuthzTestUser(t *testing.T, db *sql.DB, email, roleName string) uint64 {
	t.Helper()
	res, err := db.Exec(`
		insert into users (email, password_hash, display_name, role_id, is_active, authorization_version)
		select ?, ?, 'Authz Test User', roles.id, 1, 1
		from roles where roles.name = ?`,
		email, seedPasswordHash, roleName,
	)
	if err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil || id <= 0 {
		t.Fatalf("last insert id: %v %d", err, id)
	}
	return uint64(id)
}

func deleteAuthzTestUser(t *testing.T, db *sql.DB, userID uint64) {
	t.Helper()
	if _, err := db.Exec(`delete from users where id = ?`, userID); err != nil {
		t.Fatalf("cleanup user %d: %v", userID, err)
	}
}

func assertUserAuthzColumns(t *testing.T, db *sql.DB) {
	t.Helper()
	assertUnsignedBigintColumn(t, db, "users", "authorization_version")
	var dataType string
	err := db.QueryRow(`
		select data_type from information_schema.columns
		where table_schema = database() and table_name = 'users' and column_name = 'is_active'`).Scan(&dataType)
	if err != nil {
		t.Fatalf("is_active column: %v", err)
	}
	if !strings.Contains(strings.ToLower(dataType), "tinyint") && !strings.Contains(strings.ToLower(dataType), "int") {
		t.Fatalf("is_active type = %q, want tinyint/int", dataType)
	}
}

func mustLogin(t *testing.T, h http.Handler, email, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Token == "" {
		t.Fatalf("login token: err=%v body=%s", err, rec.Body.String())
	}
	return resp.Token
}

func doBearer(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doBearerWithBody(t, h, method, path, token, "")
}

func doBearerWithBody(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == "" {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader([]byte(body))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)
	return rec
}

func assertAuthorized(t *testing.T, h http.Handler, token string) {
	t.Helper()
	rec := doBearer(t, h, http.MethodGet, "/query-targets/1/credential", token)
	// WHY: "authorized" means the credential passed current-state verification and
	// the stub handler ran — only 200 proves that. 403/404/500 would silently
	// greenwash a broken auth path if we only rejected 401.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected authorized 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func assertGenericUnauthorizedBody(t *testing.T, body string) {
	t.Helper()
	var env struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("401 body not JSON: %v body=%s", err, body)
	}
	if env.Error != "unauthorized" || env.Message != "unauthorized" {
		t.Fatalf("generic 401 shape = %+v, want error=unauthorized message=unauthorized", env)
	}
	lower := strings.ToLower(body)
	for _, leak := range []string{"password", "authorization_version", "disabled", "version mismatch", "signature"} {
		if strings.Contains(lower, leak) {
			t.Fatalf("401 body leaks %q: %s", leak, body)
		}
	}
}

func mintIntegrationToken(t *testing.T, userID, authzVersion uint64, issuedAt time.Time) string {
	t.Helper()
	payload := fmt.Sprintf("%d:%d:%d", userID, authzVersion, issuedAt.Unix())
	mac := hmac.New(sha256.New, []byte(authzIntegrationSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
}
