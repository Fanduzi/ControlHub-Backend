// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api router, internal/service, internal/testsupport/operatoraccess, net/http/httptest
// output: TestOperatorAccessBoundary
// pos: Router-level authorization matrix for anonymous, editor, and admin operators, driven by the shared operatoraccess policy
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
	"github.com/fan/controlhub/internal/testsupport/operatoraccess"
)

// TestOperatorAccessBoundary proves the router-level operator authorization
// matrix against the shared operatoraccess policy: anonymous operators get 401
// on every protected operation; editors get 2xx on editor-readable and
// fresh-any-role operations and 403 on admin-gated ones; admins get 2xx on
// admin-gated operations. Conditional saved-statement mutations (38R) are
// exercised in a dedicated subtest because their outcome depends on statement
// scope and ownership — they must not be flattened into a uniform editor 2xx.
func TestOperatorAccessBoundary(t *testing.T) {
	server := NewTestServer()
	deps := server.deps
	deps.AuthService = newMiddlewareAuthService("test-secret")
	// Wire the real 38R saved-statement service over an in-memory store so the
	// service-level scope/owner authorization is exercised through the router
	// instead of a permissive fake. The query target fixture pins a complete
	// mysql target at resource 22 (host+port) so the credential surface is
	// exercisable; the other seeded targets are irrelevant to the boundary.
	targetRepo := fakeQueryTargetRepo{targets: []fakeQueryTargetRow{{
		environmentID: 1,
		target: model.QueryTarget{
			ResourceID: 22, ResourceName: "boundary-mysql-prod", DisplayName: "Boundary MySQL Prod", ResourceType: model.ResourceTypeDatabaseInstance,
			ConnectionContext: model.QueryTargetConnectionContext{Environment: "Production", Owner: "DBA Team", Engine: "mysql", Host: "boundary-mysql.internal", Port: 3306},
		},
	}}}
	savedStore := &memorySavedStatementStore{statements: map[uint64]model.QuerySavedStatement{}, nextID: 1}
	deps.QuerySavedStatementService = service.NewQuerySavedStatementService(savedStore, savedStore, targetRepo, boundaryGuard{})
	deps.QueryCredentialService = service.NewQueryCredentialService(targetRepo, &fakeCredentialMetadataStore{}, service.NewEnvCredentialResolver())
	// Fresh-actor query surfaces use handler-level stubs: the boundary under
	// test is authorization, not SQL execution. The stubs return 2xx whenever
	// the actor passes the freshness gate.
	deps.QueryExecutionService = &stubQueryExec{}
	deps.QueryExplainService = &stubExplainAPI{}
	router := NewRouter(deps)

	now := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	editorToken := mintToken(t, "test-secret", 7, "editor", now)
	adminToken := mintToken(t, "test-secret", 1, "admin", now)
	viewerToken := mintToken(t, "test-secret", 43, "viewer", now)

	// Anonymous: every protected operation rejects with 401 before any role
	// consideration. Bodies are supplied where the route decodes one so the
	// request shape is realistic; the middleware rejects before body handling.
	for _, op := range operatoraccess.All() {
		op := op
		t.Run("anonymous "+op.Method+" "+op.Path, func(t *testing.T) {
			got, body := doBoundaryRequest(t, router, op.Method, op.RequestPath, boundaryBody(op), "")
			if got != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", got, body)
			}
		})
	}

	// Editor: 2xx on editor-readable and fresh-any-role operations, 403 on
	// admin-gated operations. Conditional saved-statement mutations are handled
	// by the dedicated 38R subtest below.
	for _, op := range operatoraccess.All() {
		if op.Class == operatoraccess.ConditionalSavedStatementMutation {
			continue
		}
		op := op
		t.Run("editor "+op.Method+" "+op.Path, func(t *testing.T) {
			got, body := doBoundaryRequest(t, router, op.Method, op.RequestPath, boundaryBody(op), editorToken)
			switch op.Class {
			case operatoraccess.RouterAdmin, operatoraccess.HandlerAdmin:
				if got != http.StatusForbidden {
					t.Fatalf("status = %d, want 403; body=%s", got, body)
				}
			default:
				assertBoundary2xx(t, op, got, body)
			}
		})
	}

	// Admin: 2xx on admin-gated operations; the editor-readable surface stays
	// 2xx as well (admins retain editor capabilities).
	for _, op := range operatoraccess.All() {
		if op.Class == operatoraccess.ConditionalSavedStatementMutation {
			continue
		}
		op := op
		t.Run("admin "+op.Method+" "+op.Path, func(t *testing.T) {
			got, body := doBoundaryRequest(t, router, op.Method, op.RequestPath, boundaryBody(op), adminToken)
			assertBoundary2xx(t, op, got, body)
		})
	}

	// 38R conditional saved-statement authorization (service-level, not a
	// router admin gate): editors create personal statements; only admins
	// create shared templates; personal update/delete are owner-only (a
	// non-owner — including an admin — gets 404); shared-template
	// update/delete are admin-only for non-admin actors.
	t.Run("saved-statement 38R conditional authorization", func(t *testing.T) {
		const listPath = "/query-targets/22/saved-statements"
		personal := `{"name":"personal-saved","statement":"select 1 as v","scope":"personal"}`
		shared := `{"name":"shared-saved","statement":"select 1 as v","scope":"shared_template"}`
		update := `{"name":"personal-saved","statement":"select 2 as v"}`

		if got, _ := doBoundaryRequest(t, router, http.MethodPost, listPath, personal, editorToken); got != http.StatusCreated {
			t.Fatalf("editor create personal = %d, want 201", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPost, listPath, shared, editorToken); got != http.StatusForbidden {
			t.Fatalf("editor create shared_template = %d, want 403", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPost, listPath, shared, adminToken); got != http.StatusCreated {
			t.Fatalf("admin create shared_template = %d, want 201", got)
		}

		if got, _ := doBoundaryRequest(t, router, http.MethodPut, listPath+"/1", update, editorToken); got != http.StatusNoContent {
			t.Fatalf("editor update own personal = %d, want 204", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, listPath+"/2", update, editorToken); got != http.StatusForbidden {
			t.Fatalf("editor update shared_template = %d, want 403", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, listPath+"/1", update, viewerToken); got != http.StatusNotFound {
			t.Fatalf("non-owner update personal = %d, want 404", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, listPath+"/1", update, adminToken); got != http.StatusNotFound {
			t.Fatalf("admin update non-owner personal = %d, want 404 (personal is owner-only)", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, listPath+"/2", update, adminToken); got != http.StatusNoContent {
			t.Fatalf("admin update shared_template = %d, want 204", got)
		}

		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, listPath+"/2", "", editorToken); got != http.StatusForbidden {
			t.Fatalf("editor delete shared_template = %d, want 403", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, listPath+"/2", "", adminToken); got != http.StatusNoContent {
			t.Fatalf("admin delete shared_template = %d, want 204", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, listPath+"/1", "", editorToken); got != http.StatusNoContent {
			t.Fatalf("editor delete own personal = %d, want 204", got)
		}
		// A fresh personal statement so a non-owner delete can be observed (the
		// earlier personal was already removed by its owner).
		if got, _ := doBoundaryRequest(t, router, http.MethodPost, listPath, personal, editorToken); got != http.StatusCreated {
			t.Fatalf("editor create second personal = %d, want 201", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, listPath+"/3", "", viewerToken); got != http.StatusNotFound {
			t.Fatalf("non-owner delete personal = %d, want 404", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, listPath+"/3", "", editorToken); got != http.StatusNoContent {
			t.Fatalf("editor delete own personal = %d, want 204", got)
		}
	})
}

// doBoundaryRequest issues one request against the boundary router and returns
// the recorded status and body. Token is empty for anonymous requests.
func doBoundaryRequest(t *testing.T, router http.Handler, method, path, body, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// assertBoundary2xx asserts the operation succeeded as an authenticated actor.
// Success codes vary by endpoint (200/201/204), so the boundary assertion is
// the 2xx range; exact per-endpoint codes are pinned by handler tests.
func assertBoundary2xx(t *testing.T, op operatoraccess.Operation, got int, body string) {
	t.Helper()
	if got < 200 || got >= 300 {
		t.Fatalf("status = %d, want 2xx for %s %s; body=%s", got, op.Method, op.Path, body)
	}
}

// boundaryBody returns a valid request body for operations that decode one.
// Anonymous requests are rejected by the middleware before body handling, so
// the same body is used there for realistic request shapes.
func boundaryBody(op operatoraccess.Operation) string {
	switch op.Method + " " + op.Path {
	case "POST /resources":
		return `{"resourceType":"database_instance","resourceSubtype":"mysql","name":"operator-boundary-resource","displayName":"Operator Boundary Resource","environmentId":1,"ownerId":2,"lifecycleStatus":"running","healthStatus":"healthy","source":"manual","labels":{}}`
	case "PATCH /resources/{id}":
		return `{"displayName":"Operator Boundary Updated"}`
	case "POST /resources/{id}/archive":
		return `{"reason":"boundary test"}`
	case "PUT /resources/{id}/profile", "PATCH /resources/{id}/profile":
		return `{"engine":"mysql","version":"8.0.36","host":"boundary.internal","port":3306,"role":"primary"}`
	case "POST /resources/{id}/relations":
		return `{"toResourceId":2,"relationType":"depends_on"}`
	case "POST /query-targets/{id}/execute":
		return `{"statement":"select 1 as value","maxRows":100}`
	case "POST /query-targets/{id}/explain":
		return `{"statement":"select 1 as value"}`
	case "POST /query-targets/{id}/related-records":
		return `{"source":{"database":"orders","object":"users","kind":"table","foreignKey":"order_id"},"localValues":["1"]}`
	case "POST /query-targets/{id}/saved-statements/{statementId}/execute":
		return `{"values":{"status":"paid"}}`
	case "PUT /query-targets/{id}/credential":
		return `{"credentialRef":"ORDER_MYSQL_RO","enabled":true,"environmentPolicy":"non_prod_only"}`
	case "POST /query-disclosure-policies", "PUT /query-disclosure-policies":
		return `{"targetResourceId":22,"databaseName":"orders","objectName":"users","columnName":"email","mode":"raw_copy_allowed"}`
	case "POST /query-targets/{id}/saved-statements":
		return `{"name":"boundary-saved","statement":"select 1 as v","scope":"personal"}`
	case "PUT /query-targets/{id}/saved-statements/{statementId}":
		return `{"name":"boundary-saved","statement":"select 2 as v"}`
	default:
		return ""
	}
}

// memorySavedStatementStore is an in-memory QuerySavedStatementReader/Writer
// used to exercise the real 38R service through the boundary router. It models
// the repository contract: personal statements are visible only to the owner,
// shared templates to everyone, and reads/writes are target-scoped.
type memorySavedStatementStore struct {
	statements map[uint64]model.QuerySavedStatement
	nextID     uint64
}

func (s *memorySavedStatementStore) ListVisible(_ context.Context, q model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error) {
	items := make([]model.QuerySavedStatement, 0)
	for _, st := range s.statements {
		if st.TargetResourceID != q.TargetResourceID {
			continue
		}
		if st.Scope == model.QuerySavedStatementPersonal && st.OwnerUserID != q.OwnerUserID {
			continue
		}
		if q.Search != "" && !strings.Contains(st.Name, q.Search) {
			continue
		}
		items = append(items, st)
	}
	return model.QuerySavedStatementListResponse{Items: items, PageInfo: model.NewPageInfo(q.Page, q.PageSize, len(items))}, nil
}

func (s *memorySavedStatementStore) GetByID(_ context.Context, targetResourceID, id uint64) (model.QuerySavedStatement, error) {
	st, ok := s.statements[id]
	if !ok || st.TargetResourceID != targetResourceID {
		return model.QuerySavedStatement{}, sql.ErrNoRows
	}
	return st, nil
}

func (s *memorySavedStatementStore) CreateWithAudit(_ context.Context, ownerUserID, targetResourceID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	st := model.QuerySavedStatement{
		ID:               s.nextID,
		TargetResourceID: targetResourceID,
		OwnerUserID:      ownerUserID,
		Name:             req.Name,
		Statement:        req.Statement,
		Parameters:       req.Parameters,
		Scope:            req.Scope,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}
	s.nextID++
	s.statements[st.ID] = st
	return st, nil
}

func (s *memorySavedStatementStore) UpdateWithAudit(_ context.Context, _, _, statementID uint64, req model.QuerySavedStatementUpdateRequest, _ bool) error {
	st, ok := s.statements[statementID]
	if !ok {
		return sql.ErrNoRows
	}
	st.Name = req.Name
	st.Statement = req.Statement
	st.Parameters = req.Parameters
	st.UpdatedAt = time.Now().UTC()
	s.statements[statementID] = st
	return nil
}

func (s *memorySavedStatementStore) DeleteWithAudit(_ context.Context, _, _, statementID uint64, _ bool) error {
	if _, ok := s.statements[statementID]; !ok {
		return sql.ErrNoRows
	}
	delete(s.statements, statementID)
	return nil
}

// boundaryGuard accepts any statement so the boundary test exercises
// authorization, not SQL guard validation (guard behavior has its own tests).
type boundaryGuard struct{}

func (boundaryGuard) GuardSavedStatement(statement string) (string, error) {
	return statement, nil
}
