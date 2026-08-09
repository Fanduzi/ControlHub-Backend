//go:build integration

// Package integration provides shared authorization test support consumed by
// the Authorization Version and operator access boundary integration tests.
// input: bytes, context, database/sql, encoding/json, net/http, net/http/httptest, testing, internal/model, internal/service
// output: shared authz constants, login/bearer/user helpers, and query handler stubs
// pos: Lets same-package integration tests reuse login, bearer, user-seeding, and query-stub support without duplication
// note: if this file changes, update header and README.md
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

const authzIntegrationSecret = "authz-integration-secret"

// Same password hash as migration 0002 seed users.
const seedPasswordHash = "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"

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

// boundaryExecStub, boundaryExplainStub, boundarySchemaStub, and
// boundaryDisclosureStub satisfy the handler interfaces for the query surfaces.
// The boundary under test is authorization, not SQL execution, so the stubs
// return 2xx whenever the actor passes the freshness/role gates.
type boundaryExecStub struct{}

func (boundaryExecStub) Execute(_ context.Context, _, _ uint64, _ model.QueryExecuteRequest) (model.QueryExecuteResponse, error) {
	return model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}, nil
}

func (boundaryExecStub) ExecuteSavedStatement(_ context.Context, _, _, _ uint64, _ model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error) {
	return model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}, nil
}

func (boundaryExecStub) ListHistory(_ context.Context, _ uint64, _ string, _ uint64, _ model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error) {
	return &model.QueryExecutionCursorPage{}, nil
}

func (boundaryExecStub) NavigateRelatedRecords(_ context.Context, _, _ uint64, _ model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error) {
	return model.RelatedRecordNavigationResponse{}, nil
}

type boundaryExplainStub struct{}

func (boundaryExplainStub) Explain(_ context.Context, _, _ uint64, _ model.ExplainRequest) (model.ExplainResponse, error) {
	return model.ExplainResponse{}, nil
}

type boundarySchemaStub struct{}

func (boundarySchemaStub) ListDatabases(_ context.Context, _, targetID uint64, _ string, _, _ int, _, _ bool) (model.DatabaseListResponse, error) {
	return model.DatabaseListResponse{TargetResourceID: int64(targetID), Items: []model.DatabaseSummary{}, PageInfo: model.NewPageInfo(1, 50, 0)}, nil
}

func (boundarySchemaStub) ListObjects(_ context.Context, _, targetID uint64, database, _, _ string, _, _ int, _ bool) (model.ObjectListResponse, error) {
	return model.ObjectListResponse{TargetResourceID: int64(targetID), Database: database, Items: []model.ObjectSummary{}, PageInfo: model.NewPageInfo(1, 50, 0)}, nil
}

func (boundarySchemaStub) GetObjectDetails(_ context.Context, _, targetID uint64, database, name, _ string, _ bool) (model.ObjectDetailResponse, error) {
	return model.ObjectDetailResponse{TargetResourceID: int64(targetID), Database: database, Name: name}, nil
}

func (boundarySchemaStub) GetTableDefinition(_ context.Context, _, targetID uint64, database, name string) (model.TableDefinitionResponse, error) {
	return model.TableDefinitionResponse{TargetResourceID: int64(targetID), Database: database, Name: name}, nil
}

func (boundarySchemaStub) GetRelationshipMap(_ context.Context, _, targetID uint64, database, name string, _ bool) (model.RelationshipMapResponse, error) {
	return model.RelationshipMapResponse{TargetResourceID: int64(targetID), Root: model.RelationshipMapNode{Database: database, Name: name}}, nil
}

type boundaryDisclosureStub struct{}

func (boundaryDisclosureStub) ListPolicies(_ context.Context, _ uint64) ([]model.ResultDisclosurePolicy, error) {
	return []model.ResultDisclosurePolicy{}, nil
}

func (boundaryDisclosureStub) CreatePolicy(_ context.Context, _ model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	return 1, nil
}

func (boundaryDisclosureStub) UpdatePolicy(_ context.Context, _ model.ResultDisclosurePolicyUpsertRequest) error {
	return nil
}

func (boundaryDisclosureStub) DeletePolicy(_ context.Context, _ uint64, _, _, _ string) error {
	return nil
}
