// Package api provides tests for the Phase 38I query schema metadata handlers.
// input: bytes, context, fmt, net/http, net/http/httptest, strings, testing, time, chi, internal/model, internal/service
// output: TestQuerySchema_* (auth, parsing, sentinel mapping, special chars, table definitions, no-secret assertions)
// pos: Handler + auth middleware + error-mapping coverage for the Phase 38I schema metadata endpoints
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

var schemaTestNow = time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)

// stubQuerySchema is a configurable querySchemaAPI stub for handler tests.
type stubQuerySchema struct {
	databasesResp       model.DatabaseListResponse
	databasesErr        error
	objectsResp         model.ObjectListResponse
	objectsErr          error
	detailResp          model.ObjectDetailResponse
	detailErr           error
	tableDefResp        model.TableDefinitionResponse
	tableDefErr         error
	gotActor            uint64
	gotTargetID         uint64
	gotQ                string
	gotDatabase         string
	gotName             string
	gotKind             string
	tableDefCalled      bool
	gotTableDefDatabase string
	gotTableDefName     string
	gotPage             int
	gotPageSize         int
	gotIncSystem        bool
	gotRefresh          bool
	databasesCalled     bool
	objectsCalled       bool
	detailCalled        bool
}

func (s *stubQuerySchema) ListDatabases(_ context.Context, actorID, targetID uint64, q string, page, pageSize int, includeSystem, refresh bool) (model.DatabaseListResponse, error) {
	s.databasesCalled = true
	s.gotActor = actorID
	s.gotTargetID = targetID
	s.gotQ = q
	s.gotPage = page
	s.gotPageSize = pageSize
	s.gotIncSystem = includeSystem
	s.gotRefresh = refresh
	return s.databasesResp, s.databasesErr
}

func (s *stubQuerySchema) ListObjects(_ context.Context, actorID, targetID uint64, database, kind, q string, page, pageSize int, refresh bool) (model.ObjectListResponse, error) {
	s.objectsCalled = true
	s.gotActor = actorID
	s.gotTargetID = targetID
	s.gotDatabase = database
	s.gotKind = kind
	s.gotQ = q
	s.gotPage = page
	s.gotPageSize = pageSize
	s.gotRefresh = refresh
	return s.objectsResp, s.objectsErr
}

func (s *stubQuerySchema) GetObjectDetails(_ context.Context, actorID, targetID uint64, database, name, kind string, refresh bool) (model.ObjectDetailResponse, error) {
	s.detailCalled = true
	s.gotActor = actorID
	s.gotTargetID = targetID
	s.gotDatabase = database
	s.gotName = name
	s.gotKind = kind
	s.gotRefresh = refresh
	return s.detailResp, s.detailErr
}

func (s *stubQuerySchema) GetTableDefinition(_ context.Context, actorID, targetID uint64, database, name string) (model.TableDefinitionResponse, error) {
	s.tableDefCalled = true
	s.gotActor = actorID
	s.gotTargetID = targetID
	s.gotTableDefDatabase = database
	s.gotTableDefName = name
	return s.tableDefResp, s.tableDefErr
}

func newSchemaRouter(stub querySchemaAPI) *chi.Mux {
	deps := Dependencies{
		AuthService:        service.NewAuthService(nil, "qs-test-secret"),
		QuerySchemaService: stub,
		QueryExecutionAuth: QueryExecutionAuthConfig{
			TokenMaxAge: 8 * time.Hour,
			Clock:       fixedClock(schemaTestNow),
		},
	}
	return NewRouter(deps)
}

func schemaRequest(method, path, bearer string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return req
}

func schemaToken(t *testing.T) string {
	return mintToken(t, "qs-test-secret", 42, "admin", schemaTestNow)
}

// --- Auth tests (all three routes reject missing/stale bearer tokens) ---

func TestQuerySchema_DatabasesRequiresBearer(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET databases without bearer = %d, want 401", rec.Code)
	}
}

func TestQuerySchema_ObjectsRequiresBearer(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects?database=testdb", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET objects without bearer = %d, want 401", rec.Code)
	}
}

func TestQuerySchema_ObjectDetailsRequiresBearer(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/object-details?database=testdb&name=users&kind=table", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET object-details without bearer = %d, want 401", rec.Code)
	}
}

func TestQuerySchema_DatabasesRejectsInvalidBearer(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", "not-a-valid-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET databases with invalid bearer = %d, want 401", rec.Code)
	}
}

// --- Actor comes from token, never from query params ---

func TestQuerySchema_DatabasesActorFromToken(t *testing.T) {
	stub := &stubQuerySchema{
		databasesResp: model.DatabaseListResponse{
			TargetResourceID: 22,
			Items:            []model.DatabaseSummary{{Name: "testdb"}},
		},
	}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", schemaToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.databasesCalled {
		t.Fatal("ListDatabases was not called")
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor = %d, want 42 (from token)", stub.gotActor)
	}
	if stub.gotTargetID != 22 {
		t.Fatalf("target = %d, want 22", stub.gotTargetID)
	}
}

// --- Parser validation tests ---

func TestQuerySchema_InvalidTargetID(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	for _, path := range []string{
		"/query-targets/abc/schema/databases",
		"/query-targets/abc/schema/objects?database=testdb",
		"/query-targets/abc/schema/object-details?database=testdb&name=t&kind=table",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, schemaRequest(http.MethodGet, path, token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %s: status = %d, want 400", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "validation_failed") {
			t.Fatalf("path %s: body = %s, want validation_failed", path, rec.Body.String())
		}
	}
}

func TestQuerySchema_PageDefaultsAndCaps(t *testing.T) {
	stub := &stubQuerySchema{databasesResp: model.DatabaseListResponse{TargetResourceID: 22}}
	router := newSchemaRouter(stub)
	token := schemaToken(t)

	// Default page=1, pageSize=50.
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if stub.gotPage != 1 || stub.gotPageSize != 50 {
		t.Fatalf("defaults: page=%d pageSize=%d, want 1/50", stub.gotPage, stub.gotPageSize)
	}

	// Valid custom values.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases?page=3&pageSize=10", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("custom pagination: status = %d, want 200", rec.Code)
	}
	if stub.gotPage != 3 || stub.gotPageSize != 10 {
		t.Fatalf("custom: page=%d pageSize=%d, want 3/10", stub.gotPage, stub.gotPageSize)
	}
}

func TestQuerySchema_PageRejectsInvalid(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	cases := []string{
		"/query-targets/22/schema/databases?page=0",
		"/query-targets/22/schema/databases?page=-1",
		"/query-targets/22/schema/databases?page=abc",
		"/query-targets/22/schema/databases?pageSize=0",
		"/query-targets/22/schema/databases?pageSize=-1",
		"/query-targets/22/schema/databases?pageSize=abc",
		"/query-targets/22/schema/databases?pageSize=101",
	}
	for _, path := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, schemaRequest(http.MethodGet, path, token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestQuerySchema_QMaxLength(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	longQ := strings.Repeat("a", 201)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases?q="+url.QueryEscape(longQ), token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long q: status = %d, want 400", rec.Code)
	}
}

func TestQuerySchema_DatabaseMaxLength(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	longDB := strings.Repeat("a", 129)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects?database="+url.QueryEscape(longDB), token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long database: status = %d, want 400", rec.Code)
	}
}

func TestQuerySchema_NameMaxLength(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	longName := strings.Repeat("a", 129)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/object-details?database=db&name="+url.QueryEscape(longName)+"&kind=table", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long name: status = %d, want 400", rec.Code)
	}
}

func TestQuerySchema_KindRejectsInvalid(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects?database=testdb&kind=invalid", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid kind: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "kind must be table or view") {
		t.Fatalf("body = %s, want kind error", rec.Body.String())
	}
}

func TestQuerySchema_KindAcceptsTableAndView(t *testing.T) {
	stub := &stubQuerySchema{objectsResp: model.ObjectListResponse{TargetResourceID: 22, Database: "testdb"}}
	router := newSchemaRouter(stub)
	token := schemaToken(t)
	for _, kind := range []string{"table", "view"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects?database=testdb&kind="+kind, token))
		if rec.Code != http.StatusOK {
			t.Fatalf("kind=%s: status = %d, want 200", kind, rec.Code)
		}
		if stub.gotKind != kind {
			t.Fatalf("kind=%s: got %q", kind, stub.gotKind)
		}
	}
}

func TestQuerySchema_ObjectDetailsKindRequired(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/object-details?database=testdb&name=users", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing kind: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "kind is required") {
		t.Fatalf("body = %s, want kind required", rec.Body.String())
	}
}

func TestQuerySchema_ObjectDetailsNameRequired(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/object-details?database=testdb&kind=table", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing name: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Fatalf("body = %s, want name required", rec.Body.String())
	}
}

func TestQuerySchema_ObjectsDatabaseRequired(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing database: status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "database is required") {
		t.Fatalf("body = %s, want database required", rec.Body.String())
	}
}

func TestQuerySchema_BooleanRejectsInvalid(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	cases := []string{
		"/query-targets/22/schema/databases?includeSystem=maybe",
		"/query-targets/22/schema/databases?refresh=maybe",
	}
	for _, path := range cases {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, schemaRequest(http.MethodGet, path, token))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("path %s: status = %d, want 400", path, rec.Code)
		}
	}
}

func TestQuerySchema_BooleanAcceptsValid(t *testing.T) {
	stub := &stubQuerySchema{databasesResp: model.DatabaseListResponse{TargetResourceID: 22}}
	router := newSchemaRouter(stub)
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases?includeSystem=true&refresh=true", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !stub.gotIncSystem || !stub.gotRefresh {
		t.Fatalf("includeSystem=%v refresh=%v, want true/true", stub.gotIncSystem, stub.gotRefresh)
	}
}

// --- Sentinel error mapping tests ---

func TestQuerySchema_SentinelMapping(t *testing.T) {
	cases := []struct {
		name       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{"ErrSchemaValidationFailed", service.ErrSchemaValidationFailed, http.StatusBadRequest, "schema_validation_failed"},
		{"ErrSchemaNotAllowed", service.ErrSchemaNotAllowed, http.StatusForbidden, "schema_not_allowed"},
		{"ErrSchemaTargetNotFound", service.ErrSchemaTargetNotFound, http.StatusNotFound, "schema_target_not_found"},
		{"ErrSchemaObjectNotFound", service.ErrSchemaObjectNotFound, http.StatusNotFound, "schema_object_not_found"},
		{"ErrSchemaTimeout", service.ErrSchemaTimeout, http.StatusRequestTimeout, "schema_timeout"},
		{"ErrSchemaBackendError", service.ErrSchemaBackendError, http.StatusBadGateway, "schema_backend_error"},
		{"unknown error", fmt.Errorf("boom"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Test via ListDatabases handler path.
			stub := &stubQuerySchema{databasesErr: tc.svcErr}
			router := newSchemaRouter(stub)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", schemaToken(t)))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), tc.wantCode)
			}
		})
	}
}

func TestQuerySchema_SentinelMapping_Objects(t *testing.T) {
	stub := &stubQuerySchema{objectsErr: service.ErrSchemaNotAllowed}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects?database=testdb", schemaToken(t)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "schema_not_allowed") {
		t.Fatalf("body = %s, want schema_not_allowed", rec.Body.String())
	}
}

func TestQuerySchema_SentinelMapping_ObjectDetails(t *testing.T) {
	stub := &stubQuerySchema{detailErr: service.ErrSchemaObjectNotFound}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/object-details?database=testdb&name=users&kind=table", schemaToken(t)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "schema_object_not_found") {
		t.Fatalf("body = %s, want schema_object_not_found", rec.Body.String())
	}
}

// --- Special character parsing tests ---

func TestQuerySchema_SpecialCharsInNames(t *testing.T) {
	stub := &stubQuerySchema{detailResp: model.ObjectDetailResponse{TargetResourceID: 22, Database: "testdb", Name: "users"}}
	router := newSchemaRouter(stub)
	token := schemaToken(t)

	cases := []struct {
		name  string
		param string
		want  string
	}{
		{"spaces", "my+table", "my table"},
		{"unicode", "%E4%B8%AD%E6%96%87", "中文"},
		{"percent", "100%25done", "100%done"},
		{"underscore", "my_table", "my_table"},
		{"quotes", "%27test%27", "'test'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			path := "/query-targets/22/schema/object-details?database=testdb&name=" + tc.param + "&kind=table"
			router.ServeHTTP(rec, schemaRequest(http.MethodGet, path, token))
			if rec.Code != http.StatusOK {
				t.Fatalf("name=%q: status = %d, want 200; body=%s", tc.param, rec.Code, rec.Body.String())
			}
			if stub.gotName != tc.want {
				t.Fatalf("name=%q: got %q, want %q", tc.param, stub.gotName, tc.want)
			}
		})
	}
}

// --- No-secret assertions ---

func TestQuerySchema_ResponseNoSecrets(t *testing.T) {
	stub := &stubQuerySchema{
		databasesResp: model.DatabaseListResponse{
			TargetResourceID: 22,
			Items:            []model.DatabaseSummary{{Name: "testdb"}},
		},
	}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", schemaToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"dsn", "password", "secret", "host", "port", "credentialRef"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("response contains forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestQuerySchema_ErrorNoSecrets(t *testing.T) {
	stub := &stubQuerySchema{databasesErr: service.ErrSchemaBackendError}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/databases", schemaToken(t)))
	body := rec.Body.String()
	for _, forbidden := range []string{"dsn", "password", "secret", "host", "port"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("error response contains forbidden field %q: %s", forbidden, body)
		}
	}
}

// --- Objects endpoint passes params correctly ---

func TestQuerySchema_ObjectsPassesParams(t *testing.T) {
	stub := &stubQuerySchema{
		objectsResp: model.ObjectListResponse{
			TargetResourceID: 22,
			Database:         "mydb",
			Items:            []model.ObjectSummary{{Database: "mydb", Name: "users", Kind: model.ObjectKindTable}},
		},
	}
	router := newSchemaRouter(stub)
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/objects?database=mydb&kind=table&q=user&page=2&pageSize=25&refresh=true", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.objectsCalled {
		t.Fatal("ListObjects was not called")
	}
	if stub.gotDatabase != "mydb" {
		t.Fatalf("database = %q, want mydb", stub.gotDatabase)
	}
	if stub.gotKind != "table" {
		t.Fatalf("kind = %q, want table", stub.gotKind)
	}
	if stub.gotQ != "user" {
		t.Fatalf("q = %q, want user", stub.gotQ)
	}
	if stub.gotPage != 2 || stub.gotPageSize != 25 {
		t.Fatalf("page=%d pageSize=%d, want 2/25", stub.gotPage, stub.gotPageSize)
	}
	if !stub.gotRefresh {
		t.Fatal("refresh = false, want true")
	}
}

// --- ObjectDetails endpoint passes params correctly ---

func TestQuerySchema_ObjectDetailsPassesParams(t *testing.T) {
	stub := &stubQuerySchema{
		detailResp: model.ObjectDetailResponse{
			TargetResourceID: 22,
			Database:         "mydb",
			Name:             "users",
			Kind:             model.ObjectKindTable,
		},
	}
	router := newSchemaRouter(stub)
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/object-details?database=mydb&name=users&kind=table&refresh=true", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.detailCalled {
		t.Fatal("GetObjectDetails was not called")
	}
	if stub.gotDatabase != "mydb" {
		t.Fatalf("database = %q, want mydb", stub.gotDatabase)
	}
	if stub.gotName != "users" {
		t.Fatalf("name = %q, want users", stub.gotName)
	}
	if stub.gotKind != "table" {
		t.Fatalf("kind = %q, want table", stub.gotKind)
	}
	if !stub.gotRefresh {
		t.Fatal("refresh = false, want true")
	}
}

func TestQuerySchema_TableDefinitionRequiresBearer(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name=users", ""))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET table-definition without bearer = %d, want 401", rec.Code)
	}
}

func TestQuerySchema_TableDefinitionRejectsInvalidBearer(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name=users", "not-a-valid-token"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET table-definition with invalid bearer = %d, want 401", rec.Code)
	}
}

func TestQuerySchema_TableDefinitionInvalidTargetID(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/abc/schema/table-definition?database=testdb&name=users", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation_failed") {
		t.Fatalf("body = %s, want validation_failed", rec.Body.String())
	}
}

func TestQuerySchema_TableDefinitionDatabaseRequired(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?name=users", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "database is required") {
		t.Fatalf("body = %s, want database required", rec.Body.String())
	}
}

func TestQuerySchema_TableDefinitionNameRequired(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "name is required") {
		t.Fatalf("body = %s, want name required", rec.Body.String())
	}
}

func TestQuerySchema_TableDefinitionDatabaseMaxLength(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	longDB := strings.Repeat("a", 129)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database="+url.QueryEscape(longDB)+"&name=users", token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long database: status = %d, want 400", rec.Code)
	}
}

func TestQuerySchema_TableDefinitionNameMaxLength(t *testing.T) {
	router := newSchemaRouter(&stubQuerySchema{})
	token := schemaToken(t)
	longName := strings.Repeat("a", 129)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name="+url.QueryEscape(longName), token))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("long name: status = %d, want 400", rec.Code)
	}
}

func TestQuerySchema_TableDefinitionActorFromToken(t *testing.T) {
	stub := &stubQuerySchema{
		tableDefResp: model.TableDefinitionResponse{
			TargetResourceID: 22,
			Database:         "testdb",
			Name:             "users",
			Kind:             model.ObjectKindTable,
			Dialect:          "mysql",
			Definition:       "CREATE TABLE users (id INT)",
		},
	}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name=users", schemaToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !stub.tableDefCalled {
		t.Fatal("GetTableDefinition was not called")
	}
	if stub.gotActor != 42 {
		t.Fatalf("actor = %d, want 42 (from token)", stub.gotActor)
	}
	if stub.gotTargetID != 22 {
		t.Fatalf("target = %d, want 22", stub.gotTargetID)
	}
	if stub.gotTableDefDatabase != "testdb" {
		t.Fatalf("database = %q, want testdb", stub.gotTableDefDatabase)
	}
	if stub.gotTableDefName != "users" {
		t.Fatalf("name = %q, want users", stub.gotTableDefName)
	}
}

func TestQuerySchema_TableDefinitionSuccessResponse(t *testing.T) {
	stub := &stubQuerySchema{
		tableDefResp: model.TableDefinitionResponse{
			TargetResourceID: 22,
			Database:         "testdb",
			Name:             "users",
			Kind:             model.ObjectKindTable,
			Dialect:          "mysql",
			Definition:       "CREATE TABLE `users` (`id` BIGINT PRIMARY KEY)",
			Truncated:        false,
		},
	}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name=users", schemaToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"targetResourceId":22`, `"database":"testdb"`, `"name":"users"`, `"kind":"table"`, `"dialect":"mysql"`, `"truncated":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
	for _, forbidden := range []string{"dsn", "password", "secret", "host", "port", "credentialRef"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("response contains forbidden field %q: %s", forbidden, body)
		}
	}
}

func TestQuerySchema_TableDefinitionSentinelMapping(t *testing.T) {
	cases := []struct {
		name       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{"ErrSchemaValidationFailed", service.ErrSchemaValidationFailed, http.StatusBadRequest, "schema_validation_failed"},
		{"ErrSchemaNotAllowed", service.ErrSchemaNotAllowed, http.StatusForbidden, "schema_not_allowed"},
		{"ErrSchemaTargetNotFound", service.ErrSchemaTargetNotFound, http.StatusNotFound, "schema_target_not_found"},
		{"ErrSchemaObjectNotFound", service.ErrSchemaObjectNotFound, http.StatusNotFound, "schema_object_not_found"},
		{"ErrSchemaDefinitionNotSupported", service.ErrSchemaDefinitionNotSupported, http.StatusBadRequest, "schema_definition_not_supported"},
		{"ErrSchemaTimeout", service.ErrSchemaTimeout, http.StatusRequestTimeout, "schema_timeout"},
		{"ErrSchemaBackendError", service.ErrSchemaBackendError, http.StatusBadGateway, "schema_backend_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubQuerySchema{tableDefErr: tc.svcErr}
			router := newSchemaRouter(stub)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name=users", schemaToken(t)))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Fatalf("body = %s, want code %s", rec.Body.String(), tc.wantCode)
			}
		})
	}
}

func TestQuerySchema_TableDefinitionErrorNoSecrets(t *testing.T) {
	stub := &stubQuerySchema{tableDefErr: service.ErrSchemaBackendError}
	router := newSchemaRouter(stub)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, schemaRequest(http.MethodGet, "/query-targets/22/schema/table-definition?database=testdb&name=users", schemaToken(t)))
	body := rec.Body.String()
	for _, forbidden := range []string{"dsn", "password", "secret", "host", "port"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("error response contains forbidden field %q: %s", forbidden, body)
		}
	}
}
