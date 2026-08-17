//go:build integration

// Package integration provides real-MySQL coverage for the operator access
// boundary matrix.
// input: context, encoding/json, fmt, net/http, net/http/httptest, strings, testing, time, internal/api, internal/model, internal/repository/mysql, internal/service, internal/testsupport/operatoraccess
// output: TestOperatorAccessBoundary integration case
// pos: Proves the operator access matrix (including 38R conditional saved statements) on real MySQL state
// note: if this file changes, update header and README.md
package integration

import (
	"context"
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
	"github.com/fan/controlhub/internal/testsupport/operatoraccess"
)

// TestOperatorAccessBoundary proves the operator authorization matrix against
// real MySQL state, driven by the shared operatoraccess policy: anonymous
// operators get 401 on every protected operation; editors get 2xx on
// editor-readable and fresh-any-role operations and 403 on admin-gated ones;
// admins get 2xx on admin-gated operations. Conditional saved-statement
// mutations are exercised against the real service and repository so the 38R
// scope/owner policy is proven end to end.
func TestOperatorAccessBoundary(t *testing.T) {
	db := setupTestDB(t)
	resourceRepo := mysql.NewResourceRepository(db)
	profileService := service.NewProfileService(resourceRepo, resourceRepo)
	relationRepo := mysql.NewRelationRepository(db)
	dictRepo := mysql.NewDictionaryRepository(db)
	authService := service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret)
	qtRepo := mysql.NewQueryTargetRepository(db)
	router := api.NewRouter(api.Dependencies{
		ResourceService:        service.NewResourceService(resourceRepo),
		ProfileService:         profileService,
		RelationService:        service.NewRelationService(relationRepo),
		TopologyService:        service.NewTopologyService(relationRepo),
		AuditService:           service.NewAuditService(mysql.NewAuditRepository(db)),
		AuthService:            authService,
		EnvironmentService:     service.NewEnvironmentService(dictRepo),
		OwnerService:           service.NewOwnerService(dictRepo),
		RoleService:            service.NewRoleService(dictRepo),
		ResourceTypeService:    service.NewResourceTypeService(dictRepo),
		RelationTypeService:    service.NewRelationTypeService(dictRepo),
		LifecycleStatusService: service.NewLifecycleStatusService(dictRepo),
		HealthStatusService:    service.NewHealthStatusService(dictRepo),
		ResourceSubtypeService: service.NewResourceSubtypeService(),
		QueryTargetService:     service.NewQueryTargetService(qtRepo),
		QueryExecutionService:  &boundaryExecStub{},
		QueryExplainService:    &boundaryExplainStub{},
		QuerySchemaService:     &boundarySchemaStub{},
		QueryCredentialService: &authzCredStub{},
		QueryDisclosureService: &boundaryDisclosureStub{},
		QuerySavedStatementService: service.NewQuerySavedStatementService(
			mysql.NewQuerySavedStatementRepository(db),
			mysql.NewQuerySavedStatementRepository(db),
			qtRepo,
			service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		),
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			Clock: time.Now,
		},
	})

	// Self-contained fixture target (complete mysql profile) so the boundary
	// does not depend on fuzz-mutable seed profiles.
	targetID := createBoundaryQueryTarget(t, resourceRepo)

	// The boundary provisions its own admin/editor actors: the 0002 seed
	// users are disabled by the seed-credential remediation migration and
	// must not be assumed active by any test.
	adminID := insertAuthzTestUser(t, db, "authz-boundary-admin@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, adminID) })
	adminToken := mustLogin(t, router, "authz-boundary-admin@example.com", "secret123")
	editorID := insertAuthzTestUser(t, db, "authz-boundary-editor@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, editorID) })
	editorToken := mustLogin(t, router, "authz-boundary-editor@example.com", "secret123")
	// A second editor provides the personal non-owner actor for 38R cases.
	editor2ID := insertAuthzTestUser(t, db, "authz-boundary-editor2@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, editor2ID) })
	editor2Token := mustLogin(t, router, "authz-boundary-editor2@example.com", "secret123")

	// Anonymous: every protected operation rejects with 401.
	for _, op := range operatoraccess.All() {
		op := op
		t.Run("anonymous "+op.Method+" "+op.Path, func(t *testing.T) {
			got, body := doBoundaryRequest(t, router, op.Method, boundaryRequestPath(op, targetID), boundaryBody(op), "")
			if got != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body=%s", got, body)
			}
		})
	}

	// Editor: 2xx on editor-readable and fresh-any-role operations, 403 on
	// admin-gated operations. Conditional saved-statement mutations are
	// handled by the dedicated 38R subtest below.
	for _, op := range operatoraccess.All() {
		if op.Class == operatoraccess.ConditionalSavedStatementMutation {
			continue
		}
		op := op
		t.Run("editor "+op.Method+" "+op.Path, func(t *testing.T) {
			got, body := doBoundaryRequest(t, router, op.Method, boundaryRequestPath(op, targetID), boundaryBody(op), editorToken)
			switch op.Class {
			case operatoraccess.RouterAdmin, operatoraccess.HandlerAdmin:
				if got != http.StatusForbidden {
					t.Fatalf("status = %d, want 403; body=%s", got, body)
				}
			default:
				if got < 200 || got >= 300 {
					t.Fatalf("status = %d, want 2xx for %s %s; body=%s", got, op.Method, op.Path, body)
				}
			}
		})
	}

	// Admin: 2xx on admin-gated operations; editor-readable stays 2xx too.
	for _, op := range operatoraccess.All() {
		if op.Class == operatoraccess.ConditionalSavedStatementMutation {
			continue
		}
		op := op
		t.Run("admin "+op.Method+" "+op.Path, func(t *testing.T) {
			got, body := doBoundaryRequest(t, router, op.Method, boundaryRequestPath(op, targetID), boundaryBody(op), adminToken)
			if got < 200 || got >= 300 {
				t.Fatalf("status = %d, want 2xx for %s %s; body=%s", got, op.Method, op.Path, body)
			}
		})
	}

	// 38R conditional saved-statement authorization against the real service
	// and repository: editors create personal statements; only admins create
	// shared templates; personal update/delete are owner-only (any non-owner
	// gets 404, including an admin); shared-template update/delete are
	// admin-only for non-admin actors.
	t.Run("saved-statement 38R conditional authorization", func(t *testing.T) {
		listPath := fmt.Sprintf("/query-targets/%d/saved-statements", targetID)
		personal := fmt.Sprintf(`{"name":"int-personal-%d","statement":"select 1 as v","scope":"personal"}`, time.Now().UnixNano())
		shared := fmt.Sprintf(`{"name":"int-shared-%d","statement":"select 1 as v","scope":"shared_template"}`, time.Now().UnixNano())
		update := `{"name":"int-updated","statement":"select 2 as v"}`

		personalID := boundaryCreateSavedStatement(t, router, listPath, personal, editorToken, http.StatusCreated)
		if got, _ := doBoundaryRequest(t, router, http.MethodPost, listPath, shared, editorToken); got != http.StatusForbidden {
			t.Fatalf("editor create shared_template = %d, want 403", got)
		}
		sharedID := boundaryCreateSavedStatement(t, router, listPath, shared, adminToken, http.StatusCreated)

		if got, _ := doBoundaryRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", listPath, personalID), update, editorToken); got != http.StatusNoContent {
			t.Fatalf("editor update own personal = %d, want 204", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", listPath, sharedID), update, editorToken); got != http.StatusForbidden {
			t.Fatalf("editor update shared_template = %d, want 403", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", listPath, personalID), update, editor2Token); got != http.StatusNotFound {
			t.Fatalf("non-owner update personal = %d, want 404", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", listPath, personalID), update, adminToken); got != http.StatusNotFound {
			t.Fatalf("admin update non-owner personal = %d, want 404 (personal is owner-only)", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", listPath, sharedID), update, adminToken); got != http.StatusNoContent {
			t.Fatalf("admin update shared_template = %d, want 204", got)
		}

		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", listPath, sharedID), "", editorToken); got != http.StatusForbidden {
			t.Fatalf("editor delete shared_template = %d, want 403", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", listPath, sharedID), "", adminToken); got != http.StatusNoContent {
			t.Fatalf("admin delete shared_template = %d, want 204", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", listPath, personalID), "", editorToken); got != http.StatusNoContent {
			t.Fatalf("editor delete own personal = %d, want 204", got)
		}
		// A fresh personal statement so a non-owner delete can be observed
		// (the earlier personal was already removed by its owner).
		personalID = boundaryCreateSavedStatement(t, router, listPath, personal, editorToken, http.StatusCreated)
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", listPath, personalID), "", editor2Token); got != http.StatusNotFound {
			t.Fatalf("non-owner delete personal = %d, want 404", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", listPath, personalID), "", editorToken); got != http.StatusNoContent {
			t.Fatalf("editor delete own personal = %d, want 204", got)
		}
		// Admin is also allowed to create personal statements (only the
		// shared_template scope is admin-gated); as owner, admin may update
		// and delete them like any other owner.
		adminPersonalID := boundaryCreateSavedStatement(t, router, listPath, personal, adminToken, http.StatusCreated)
		if got, _ := doBoundaryRequest(t, router, http.MethodPut, fmt.Sprintf("%s/%d", listPath, adminPersonalID), update, adminToken); got != http.StatusNoContent {
			t.Fatalf("admin update own personal = %d, want 204", got)
		}
		if got, _ := doBoundaryRequest(t, router, http.MethodDelete, fmt.Sprintf("%s/%d", listPath, adminPersonalID), "", adminToken); got != http.StatusNoContent {
			t.Fatalf("admin delete own personal = %d, want 204", got)
		}
	})
}

// createBoundaryQueryTarget creates a self-contained database_instance with a
// complete profile so the boundary fixture never depends on fuzz-mutable seed
// data.
func createBoundaryQueryTarget(t *testing.T, resourceRepo *mysql.ResourceRepository) uint64 {
	t.Helper()
	created, err := resourceRepo.CreateResource(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            fmt.Sprintf("boundary-target-%d", time.Now().UnixNano()),
		DisplayName:     "Operator Boundary Target",
		EnvironmentID:   envProd,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create boundary target: %v", err)
	}
	if err := resourceRepo.UpsertDatabaseInstanceProfile(context.Background(), created.ID, "mysql", "8.0.36", "boundary-mysql.internal", 3306, "primary"); err != nil {
		t.Fatalf("upsert boundary target profile: %v", err)
	}
	return created.ID
}

// boundaryCreateSavedStatement creates a saved statement and asserts the
// expected status, returning the created statement id when it succeeded.
func boundaryCreateSavedStatement(t *testing.T, router http.Handler, path, body, token string, wantStatus int) uint64 {
	t.Helper()
	got, respBody := doBoundaryRequest(t, router, http.MethodPost, path, body, token)
	if got != wantStatus {
		t.Fatalf("create saved statement = %d, want %d; body=%s", got, wantStatus, respBody)
	}
	var created struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(respBody), &created); err != nil {
		t.Fatalf("decode created saved statement: %v body=%s", err, respBody)
	}
	if created.ID == 0 {
		t.Fatalf("created saved statement id = 0; body=%s", respBody)
	}
	return created.ID
}

// boundaryRequestPath binds the policy's example concrete paths to the
// self-contained fixture: query-target paths bind to the fixture target id,
// and resource paths requiring a concrete resource bind to the fixture
// resource id (a database_instance whose profile schema matches the boundary
// bodies), so the matrix never depends on mutable canonical seed IDs.
func boundaryRequestPath(op operatoraccess.Operation, targetID uint64) string {
	path := strings.Replace(op.RequestPath, "/query-targets/22", fmt.Sprintf("/query-targets/%d", targetID), 1)
	return strings.Replace(path, "/resources/1", fmt.Sprintf("/resources/%d", targetID), 1)
}

// boundaryBody returns a valid request body for operations that decode one.
func boundaryBody(op operatoraccess.Operation) string {
	switch op.Method + " " + op.Path {
	case "POST /resources":
		return fmt.Sprintf(`{"resourceType":"host","resourceSubtype":"vm","name":"operator-boundary-%d","displayName":"Operator Boundary","environmentId":1,"ownerId":1,"lifecycleStatus":"running","healthStatus":"healthy","source":"manual","labels":{}}`, time.Now().UnixNano())
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

// doBoundaryRequest issues one request against the boundary router and returns
// the recorded status and body. Token is empty for anonymous requests.
func doBoundaryRequest(t *testing.T, router http.Handler, method, path, body, token string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	router.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}
