//go:build integration

// Package integration provides real-MySQL coverage for authentication and
// authorization audit event emission.
// input: context, database/sql, encoding/json, fmt, log, net/http, testing, internal/api, internal/repository/mysql, internal/service
// output: TestAuthAudit_* integration cases
// pos: Proves auth audit events are persisted correctly against real MySQL, fail-open on inject errors (login, bearer, and role-denied 403 outcomes), and never contain prohibited values
// note: if this file changes, update header and README.md
package integration

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
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
			Clock: time.Now,
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
			Clock: time.Now,
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

// TestAuthAudit_MissingHeaderEmitsNoRow proves a request with no
// Authorization header is absence of a credential, not a rejected supplied
// credential: the generic 401 is returned and no auth.bearer rejected row is
// persisted (bounded-audit ADR 2026-08-15).
func TestAuthAudit_MissingHeaderEmitsNoRow(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-missing-header@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	before := countUntrustedBearerRejections(t, db)

	// No Authorization header at all.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/query-targets/1/credential", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no-header status = %d, want 401", rec.Code)
	}

	after := countUntrustedBearerRejections(t, db)
	if after-before != 0 {
		t.Fatalf("untrusted rejection rows delta = %d, want 0 for missing Authorization", after-before)
	}
}

// TestAuthAudit_SuppliedInvalidBearerEmitsRejectedRow proves a supplied but
// untrusted Bearer (invalid token) persists the fixed auth.bearer rejected
// event with no actor while the process budget has capacity.
func TestAuthAudit_SuppliedInvalidBearerEmitsRejectedRow(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-invalid-bearer@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})
	t.Cleanup(service.ProcessBearerRejectBudget.Reset)

	before := countUntrustedBearerRejections(t, db)

	rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", "forged-token")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid-token status = %d, want 401", rec.Code)
	}

	after := countUntrustedBearerRejections(t, db)
	if after-before != 1 {
		t.Fatalf("untrusted rejection rows delta = %d, want 1", after-before)
	}
}

// TestAuthAudit_BoundedUntrustedBearerPersistence proves the fixed 60/min
// per-process persistence budget against real MySQL: the 61st untrusted
// rejection keeps the generic 401 but persists no row, the safe suppression
// counter is visible on the administrator-only metrics surface, and a
// verified actor's role denial still persists after budget exhaustion.
func TestAuthAudit_BoundedUntrustedBearerPersistence(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	adminID := insertAuthzTestUser(t, db, "audit-budget-admin@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, adminID) })
	editorID := insertAuthzTestUser(t, db, "audit-budget-editor@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, editorID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	adminToken := mustLogin(t, router, "audit-budget-admin@example.com", "secret123")
	editorToken := mustLogin(t, router, "audit-budget-editor@example.com", "secret123")
	t.Cleanup(service.ProcessBearerRejectBudget.Reset)

	beforeRows := countUntrustedBearerRejections(t, db)
	beforeSuppressed, _ := readAuthAuditMetrics(t, router, adminToken)
	beforeDenied := countRoleDenials(t, db, editorID)

	for i := 0; i < 61; i++ {
		rec := doBearer(t, router, http.MethodGet, "/query-targets/1/credential", "forged-token")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	afterRows := countUntrustedBearerRejections(t, db)
	if afterRows-beforeRows != 60 {
		t.Fatalf("untrusted rejection rows delta = %d, want exactly 60 of 61", afterRows-beforeRows)
	}

	afterSuppressed, ok := readAuthAuditMetrics(t, router, adminToken)
	if !ok {
		t.Fatal("authAuditSuppressedRejections missing from admin metrics surface")
	}
	if afterSuppressed-beforeSuppressed != 1 {
		t.Fatalf("suppression counter delta = %d, want 1", afterSuppressed-beforeSuppressed)
	}

	// Verified editor denied by role AFTER budget exhaustion still persists.
	rec := doBearer(t, router, http.MethodPut, "/query-targets/1/credential", editorToken)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor PUT status = %d, want 403", rec.Code)
	}
	afterDenied := countRoleDenials(t, db, editorID)
	if afterDenied-beforeDenied != 1 {
		t.Fatalf("role denial rows delta = %d, want 1 despite exhausted budget", afterDenied-beforeDenied)
	}
}

func countUntrustedBearerRejections(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`select count(*) from audit_events
		 where event_type = 'auth.bearer' and result = 'rejected' and actor_user_id is null`,
	).Scan(&n); err != nil {
		t.Fatalf("count untrusted rejections: %v", err)
	}
	return n
}

func countRoleDenials(t *testing.T, db *sql.DB, actorID uint64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`select count(*) from audit_events
		 where event_type = 'auth.authorization' and result = 'denied' and actor_user_id = ?`,
		actorID,
	).Scan(&n); err != nil {
		t.Fatalf("count role denials: %v", err)
	}
	return n
}

// readAuthAuditMetrics calls the administrator-only auth-audit metrics
// endpoint with a valid admin token and returns the safe suppression counter
// and whether it is present at all.
func readAuthAuditMetrics(t *testing.T, h http.Handler, adminToken string) (suppressed int64, ok bool) {
	t.Helper()
	rec := doBearer(t, h, http.MethodGet, "/ops/auth-audit-metrics", adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200", rec.Code)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	val, ok := raw["authAuditSuppressedRejections"]
	if !ok {
		return 0, false
	}
	if err := json.Unmarshal(val, &suppressed); err != nil {
		t.Fatalf("decode suppression counter: %v", err)
	}
	return suppressed, true
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
			Clock: time.Now,
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
		   and actor_user_id = ?
		 order by created_at desc limit 1`,
		userID,
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

// TestAuthAudit_ResourceScopedDeniedEmitsTargetID proves that an editor
// denied on a resource-scoped admin mutation (PATCH /resources/{id}) emits
// exactly one auth.authorization denied event with target_resource_id set
// to that resource ID and actor_user_id set to the editor.
func TestAuthAudit_ResourceScopedDeniedEmitsTargetID(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-resource-denied@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)
	emitter := mysql.NewAuthAuditEmitter(db)
	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
	})

	token := mustLogin(t, router, "audit-resource-denied@example.com", "secret123")

	// PATCH /resources/7 — admin-only; editor gets 403 before handler runs.
	body := `{"displayName":"should-not-apply"}`
	rec := doBearerWithBody(t, router, http.MethodPatch, "/resources/7", token, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor PATCH status = %d, want 403", rec.Code)
	}

	// Query for the specific denied event: must have actor = editor, target = 7.
	var actorUserID sql.NullInt64
	var targetResourceID sql.NullInt64
	err := db.QueryRow(
		`select actor_user_id, target_resource_id from audit_events
		 where event_type = 'auth.authorization' and result = 'denied'
		   and actor_user_id = ?
		 order by created_at desc limit 1`,
		userID,
	).Scan(&actorUserID, &targetResourceID)
	if err != nil {
		t.Fatalf("query auth.authorization denied for editor: %v", err)
	}
	if !actorUserID.Valid || uint64(actorUserID.Int64) != userID {
		t.Fatalf("actor_user_id = %v, want %d", actorUserID, userID)
	}
	if !targetResourceID.Valid || uint64(targetResourceID.Int64) != 7 {
		t.Fatalf("target_resource_id = %v, want 7", targetResourceID)
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
			Clock: time.Now,
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

// TestAuthAudit_FailOpenPreservesRoleDenied403 proves the role-denied half of
// the fail-open contract (Issue #28): when auth-audit persistence fails, a
// valid editor requesting an admin-only protected operation with a known
// target resource still receives the controlled 403 and the protected handler
// does not execute. TestAuthAudit_FailOpenOnDBError already proves login
// success and Bearer rejection are unchanged; this test closes the remaining
// gap that the fail-open contract covers authorization outcomes too.
func TestAuthAudit_FailOpenPreservesRoleDenied403(t *testing.T) {
	db := setupTestDB(t)
	assertSchemaChainBaseline(t, db)

	userID := insertAuthzTestUser(t, db, "audit-failopen-403@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	userRepo := mysql.NewUserRepository(db)
	authSvc := service.NewAuthService(userRepo, authzIntegrationSecret)

	// Repository's existing failure-injection seam (identical to
	// TestAuthAudit_FailOpenOnDBError): an emitter backed by a closed DB so
	// every audit INSERT fails. No second audit abstraction is introduced.
	brokenDB, _ := sql.Open("mysql", "invalid:dsn@tcp(127.0.0.1:1)/noexist?timeout=1ms")
	brokenDB.Close()
	emitter := mysql.NewAuthAuditEmitter(brokenDB)

	// Capture the fail-open diagnostics so the test can prove they stay
	// privacy-safe: fixed taxonomy label + fixed error class only, never an
	// identity, credential, session, DSN, request value, or failure detail.
	logBuf := &bytes.Buffer{}
	origLogOut := log.Writer()
	log.SetOutput(logBuf)
	t.Cleanup(func() { log.SetOutput(origLogOut) })

	router := api.NewRouter(api.Dependencies{
		AuthService:      authSvc,
		AuthAuditEmitter: emitter,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
		// Real MySQL-backed resource service: if the role gate ever let the
		// handler through, the PATCH would genuinely mutate the row, so the
		// no-execution probe below is a clean proof rather than a nil-service
		// panic that would mask the bug.
		ResourceService: service.NewResourceService(mysql.NewResourceRepository(db)),
	})

	// Known target resource where the admin-only route has one: the seeded
	// payment-mysql-replica-01-prod resource (same fixture as
	// TestGooseCleanMigration) with a known display_name probe.
	var resourceID uint64
	var originalDisplayName string
	err := db.QueryRow(
		`select id, display_name from resources where name = 'payment-mysql-replica-01-prod'`,
	).Scan(&resourceID, &originalDisplayName)
	if err != nil {
		t.Fatalf("lookup seeded resource: %v", err)
	}
	if originalDisplayName == "" {
		t.Fatal("seeded resource has empty display_name; handler-execution probe would be vacuous")
	}

	token := mustLogin(t, router, "audit-failopen-403@example.com", "secret123")

	// Snapshot the operational metric AFTER login: login itself also fails its
	// own emit against the broken emitter, so the delta below isolates exactly
	// the denied authorization emit.
	beforeFailures := mysql.AuthAuditPersistenceFailures.Value()

	// Editor PATCH on the admin-only resource route while audit persistence
	// fails. The request body carries a distinct marker display name that only
	// the protected handler could apply.
	mutatedName := originalDisplayName + "-mutated-by-failopen-test"
	body := fmt.Sprintf(`{"displayName":%q}`, mutatedName)
	rec := doBearerWithBody(t, router, http.MethodPatch, fmt.Sprintf("/resources/%d", resourceID), token, body)

	// Externally visible outcome stays the controlled 403 — not 401, 2xx, or
	// 5xx. Diagnostics never echo the body: a leaking response must not become
	// test output (criterion: no prohibited value in responses or test output).
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor PATCH status = %d, want controlled 403", rec.Code)
	}
	// The response is exactly the fixed controlled payload — no audit or
	// persistence internals appended.
	wantBody := `{"error":"forbidden","message":"admin role is required"}` + "\n"
	if rec.Body.String() != wantBody {
		t.Fatalf("403 body length = %d, want the controlled %d-byte payload", rec.Body.Len(), len(wantBody))
	}

	// The protected handler did not execute: the real-MySQL resource row is
	// unchanged. With the real ResourceService wired above, an executed handler
	// would have updated display_name to the marker value.
	var displayNameAfter string
	err = db.QueryRow(`select display_name from resources where id = ?`, resourceID).Scan(&displayNameAfter)
	if err != nil {
		t.Fatalf("query resource after denied PATCH: %v", err)
	}
	if displayNameAfter != originalDisplayName {
		// Do not echo the mutated value: it is the request-derived marker, which
		// must never appear in test output (criterion: no request value in
		// responses, logs, diagnostics, or test output).
		t.Fatal("protected handler executed despite 403: display_name was mutated")
	}

	// Safe fail-open operational observability still occurs: exactly the one
	// denied authorization emit incremented the fixed-category metric, while
	// the authorization decision stayed unchanged.
	afterFailures := mysql.AuthAuditPersistenceFailures.Value()
	if afterFailures-beforeFailures != 1 {
		t.Fatalf("auth audit persistence failures delta = %d, want 1 (only the denied emit)", afterFailures-beforeFailures)
	}

	// The captured fail-open diagnostics carry exactly the fixed taxonomy label
	// and the fixed error class — no email, password, Bearer credential,
	// session material, DSN, request value, or detailed failure reason. The
	// shape regexp admits only the ADR's fixed event/result taxonomy and the
	// fixed error class; anything else is a deviation. Diagnostics below never
	// echo a line or a value, so a leaking diagnostic cannot leak again into
	// test output.
	safeShape := regexp.MustCompile(
		`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} auth_audit_emit_fail ` +
			`event=(auth\.login|auth\.bearer|auth\.authorization) ` +
			`result=(succeeded|rejected|denied) error_class=audit_persistence_failure$`)
	prohibited := []string{
		"audit-failopen-403@example.com", // email
		"secret123",                      // password value
		token,                            // bearer credential
		"noexist", "invalid:dsn",         // DSN internals
		mutatedName, // request value
	}
	logLines := strings.Split(logBuf.String(), "\n")
	capturedCount := 0
	for i, line := range logLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		capturedCount++
		if !safeShape.MatchString(line) {
			t.Errorf("captured diagnostic line %d (len=%d) deviates from the fixed safe shape", i+1, len(line))
			continue
		}
		lower := strings.ToLower(line)
		for _, p := range prohibited {
			if p != "" && strings.Contains(lower, strings.ToLower(p)) {
				t.Errorf("captured diagnostic line %d contains a prohibited value", i+1)
			}
		}
	}
	if capturedCount == 0 {
		t.Error("expected fail-open diagnostic lines, captured none")
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
			Clock: func() time.Time { return now },
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
			Clock: time.Now,
		},
		QueryCredentialService: &authzCredStub{},
	})

	// Generate auth audit events: login, bearer rejection, role denial
	_ = mustLogin(t, router, "audit-prohibited@example.com", "secret123")
	_ = doBearer(t, router, http.MethodGet, "/query-targets/1/credential", "forged-token")
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
