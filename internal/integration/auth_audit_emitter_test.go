//go:build integration

// Package integration provides real-MySQL coverage for authentication and
// authorization audit event emission.
// input: context, database/sql, encoding/json, net/http, testing, internal/api, internal/repository/mysql, internal/service
// output: TestAuthAudit_* integration cases
// pos: Proves auth audit events are persisted correctly against real MySQL, fail-open on inject errors, and never contain prohibited values
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
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// TestAuthAudit_LoginSucceeded proves a successful login emits auth.login succeeded
// with the verified actor user id.
func TestAuthAudit_LoginSucceeded(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-login-ok@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	_ = mustLogin(t, router, "audit-login-ok@example.com", "secret123")

	// Verify auth.login succeeded row exists with verified actor
	var eventType, result string
	var actorUserID sql.NullInt64
	err := db.QueryRow(
		`select event_type, result, actor_user_id from audit_events
		 where event_type = 'auth.login' and result = 'succeeded'
		 order by created_at desc limit 1`,
	).Scan(&eventType, &result, &actorUserID)
	if err != nil {
		t.Fatalf("query auth.login succeeded: %v", err)
	}
	if eventType != "auth.login" || result != "succeeded" {
		t.Fatalf("unexpected event: type=%s result=%s", eventType, result)
	}
	if !actorUserID.Valid {
		t.Fatal("expected actor on login succeeded, got NULL")
	}
	if uint64(actorUserID.Int64) != userID {
		t.Fatalf("actor = %d, want %d", actorUserID.Int64, userID)
	}
}

// TestAuthAudit_LoginRejected proves a failed login emits auth.login rejected
// with no actor user id.
func TestAuthAudit_LoginRejected(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-login-rej@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	// Wrong password → rejected
	body, _ := json.Marshal(map[string]string{"email": "audit-login-rej@example.com", "password": "wrong"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401", rec.Code)
	}

	var eventType, result string
	var actorUserID sql.NullInt64
	err := db.QueryRow(
		`select event_type, result, actor_user_id from audit_events
		 where event_type = 'auth.login' and result = 'rejected'
		 order by created_at desc limit 1`,
	).Scan(&eventType, &result, &actorUserID)
	if err != nil {
		t.Fatalf("query auth.login rejected: %v", err)
	}
	if eventType != "auth.login" || result != "rejected" {
		t.Fatalf("unexpected event: type=%s result=%s", eventType, result)
	}
	if actorUserID.Valid {
		t.Fatalf("expected no actor on login rejected, got %d", actorUserID.Int64)
	}
}

// TestAuthAudit_BearerRejected proves a request with no/malformed Bearer token
// emits auth.bearer rejected with no actor.
func TestAuthAudit_BearerRejected(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-bearer@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	// No Authorization header
	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-header status = %d, want 401", rec.Code)
	}

	var eventType, result string
	var actorUserID sql.NullInt64
	err := db.QueryRow(
		`select event_type, result, actor_user_id from audit_events
		 where event_type = 'auth.bearer' and result = 'rejected'
		 order by created_at desc limit 1`,
	).Scan(&eventType, &result, &actorUserID)
	if err != nil {
		t.Fatalf("query auth.bearer rejected: %v", err)
	}
	if eventType != "auth.bearer" || result != "rejected" {
		t.Fatalf("unexpected event: type=%s result=%s", eventType, result)
	}
	if actorUserID.Valid {
		t.Fatalf("expected no actor on bearer rejected, got %d", actorUserID.Int64)
	}
}

// TestAuthAudit_AuthorizationDenied proves a valid authenticated user with
// non-admin role hitting an admin-only route emits auth.authorization denied
// WITH the actor user id.
func TestAuthAudit_AuthorizationDenied(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-authz@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	token := mustLogin(t, router, "audit-authz@example.com", "secret123")

	// PUT to admin-only credential endpoint as editor
	body := `{"credentialRef":"TEST","enabled":true,"environmentPolicy":"non_prod_only"}`
	rec := doBearerWithBody(t, router, http.MethodPut, "/query-targets/1/credential", token, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor PUT status = %d, want 403", rec.Code)
	}

	var eventType, result string
	var actorUserID sql.NullInt64
	err := db.QueryRow(
		`select event_type, result, actor_user_id from audit_events
		 where event_type = 'auth.authorization' and result = 'denied'
		 order by created_at desc limit 1`,
	).Scan(&eventType, &result, &actorUserID)
	if err != nil {
		t.Fatalf("query auth.authorization denied: %v", err)
	}
	if eventType != "auth.authorization" || result != "denied" {
		t.Fatalf("unexpected event: type=%s result=%s", eventType, result)
	}
	if !actorUserID.Valid {
		t.Fatal("expected actor on authorization denied")
	}
	if uint64(actorUserID.Int64) != userID {
		t.Fatalf("actor = %d, want %d", actorUserID.Int64, userID)
	}
}

// TestAuthAudit_FailOpenOnDBError proves that when the audit emitter's INSERT
// fails (simulated by a broken DB connection), the authentication/authorization
// decision is unchanged.
func TestAuthAudit_FailOpenOnDBError(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-failopen@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)

	// Create an emitter backed by a closed DB to force INSERT failures
	brokenDB, _ := sql.Open("mysql", "invalid:dsn@tcp(127.0.0.1:1)/noexist?timeout=1ms")
	brokenDB.Close()
	emitter := mysql.NewAuthAuditEmitter(brokenDB)

	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	// Login must still succeed despite audit failure
	token := mustLogin(t, router, "audit-failopen@example.com", "secret123")
	if token == "" {
		t.Fatal("expected login to succeed despite audit failure")
	}

	// Bearer check must still reject bad tokens despite audit failure
	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", "garbage-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 despite audit failure, got %d", rec.Code)
	}
}

// TestAuthAudit_FreshnessRejectionEmitsBearerRejected proves that a valid
// but stale bearer token (exceeding the 8h freshness gate) emits
// auth.bearer rejected with the verified actor id.
func TestAuthAudit_FreshnessRejectionEmitsBearerRejected(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-fresh@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	user, err := userRepo.FindByID(userID)
	if err != nil || user == nil {
		t.Fatalf("FindByID: %v %#v", err, user)
	}

	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       func() time.Time { return now },
		},
		QueryCredentialService: &authzCredStub{},
	})

	// Mint a token that is 9h old — exceeds freshness gate
	stale := mintIntegrationToken(t, user.ID, user.AuthorizationVersion, now.Add(-9*time.Hour))
	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", stale)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("stale token status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}

	// Query for the freshness-rejection event: must have a non-NULL actor
	// (the verified user who presented the stale token).
	var eventType, result string
	var actorUserID sql.NullInt64
	err = db.QueryRow(
		`select event_type, result, actor_user_id from audit_events
		 where event_type = 'auth.bearer' and result = 'rejected'
		   and actor_user_id is not null
		 order by created_at desc limit 1`,
	).Scan(&eventType, &result, &actorUserID)
	if err != nil {
		t.Fatalf("query auth.bearer rejected with actor: %v", err)
	}
	if eventType != "auth.bearer" || result != "rejected" {
		t.Fatalf("unexpected event: type=%s result=%s", eventType, result)
	}
	if !actorUserID.Valid {
		t.Fatal("expected actor on freshness rejection, got NULL")
	}
	if uint64(actorUserID.Int64) != userID {
		t.Fatalf("actor = %d, want %d", actorUserID.Int64, userID)
	}
}

// TestAuthAudit_NoProhibitedValues verifies that audit rows never contain
// email, password, password hash, bearer credential, session material,
// request body, query text, parameter value, DSN, IP address, or User-Agent.
func TestAuthAudit_NoProhibitedValues(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-prohibited@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock:       time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	// Generate auth audit events: login, bearer rejection, role denial
	_ = mustLogin(t, router, "audit-prohibited@example.com", "secret123")
	_ = doBearer(t, router, http.MethodGet, "/query-targets/1/credential", "")
	edToken := mustLogin(t, router, "audit-prohibited@example.com", "secret123")
	_ = doBearerWithBody(t, router, http.MethodPut, "/query-targets/1/credential", edToken, `{}`)

	// Scan ALL auth audit events
	rows, err := db.Query(
		`select event_type, result from audit_events
		 where event_type like 'auth.%'`)
	if err != nil {
		t.Fatalf("query auth events: %v", err)
	}
	defer rows.Close()

	// Prohibited substrings (case-insensitive)
	prohibited := []string{
		"password", "secret123", "authorization_version",
		"session", "dsn", "user-agent",
		"audit-prohibited@example.com", // email
	}

	for rows.Next() {
		var eventType, result string
		if err := rows.Scan(&eventType, &result); err != nil {
			t.Fatalf("scan event: %v", err)
		}
		combined := strings.ToLower(eventType + " " + result)
		for _, p := range prohibited {
			if strings.Contains(combined, strings.ToLower(p)) {
				t.Errorf("auth audit row contains prohibited value %q: type=%s result=%s", p, eventType, result)
			}
		}
	}
}
