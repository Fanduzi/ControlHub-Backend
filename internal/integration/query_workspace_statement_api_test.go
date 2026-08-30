//go:build integration

// Package integration provides real-MySQL HTTP proofs for query workspace and private execution statements.
// input: shared MySQL/auth/query fixtures, api router, workspace/execution repositories and services
// output: owner workspace OCC plus owner-only successful-user statement access and non-leaking list/audit tests
// pos: Real-DB HTTP acceptance boundary for frontend issues 39 and 41
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func TestQueryWorkspaceAndExecutionStatementHTTPAccessMatrix(t *testing.T) {
	executionService, targetID, db := setupQuerySandboxTarget(t)
	workspaceService := service.NewQueryWorkspaceService(mysql.NewQueryWorkspaceRepository(db))
	machineService := service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db))
	authService := service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret)
	router := api.NewRouter(api.Dependencies{
		AuthService:                    authService,
		AuditService:                   service.NewAuditService(mysql.NewAuditRepository(db)),
		QueryExecutionService:          executionService,
		QueryExecutionStatementService: executionService,
		QueryWorkspaceService:          workspaceService,
		MachineCredentialService:       machineService,
		QueryExecutionAuth:             api.QueryExecutionAuthConfig{Clock: time.Now},
	})

	ownerID := insertAuthzTestUser(t, db, "query-workspace-owner@example.com", "editor")
	otherID := insertAuthzTestUser(t, db, "query-workspace-other@example.com", "editor")
	adminID := insertAuthzTestUser(t, db, "query-workspace-admin@example.com", "admin")
	ownerToken := mustLogin(t, router, "query-workspace-owner@example.com", "secret123")
	otherToken := mustLogin(t, router, "query-workspace-other@example.com", "secret123")
	adminToken := mustLogin(t, router, "query-workspace-admin@example.com", "secret123")

	issued, err := machineService.Create(context.Background(), service.AuthenticatedUser{ID: adminID, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "query-workspace-machine", Scopes: []model.MachineScope{model.MachineScopeInventoryRead},
	})
	if err != nil {
		t.Fatalf("create machine credential: %v", err)
	}

	assertWorkspaceHTTPContract(t, router, ownerToken, otherToken, adminToken, issued.Secret, targetID)

	statement := "select id, name from qe_sandbox_fixtures where id = 1"
	executed, err := executionService.Execute(context.Background(), queryUserIdentity(ownerID), targetID, model.QueryExecuteRequest{Statement: statement, MaxRows: 10})
	if err != nil {
		t.Fatalf("execute owner statement: %v", err)
	}

	executionRepo := mysql.NewQueryExecutionRepository(db)
	failedID, err := executionRepo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: targetID, ActorUserID: ownerID, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorUser}, Engine: "mysql",
		FullStatement: "failed private statement", Status: model.QueryExecutionFailed,
	}, "query.executed", "query_backend_error")
	if err != nil {
		t.Fatalf("insert failed execution: %v", err)
	}
	legacyID, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
		TargetResourceID: targetID, ActorUserID: ownerID, Engine: "mysql", Status: model.QueryExecutionSuccess,
	})
	if err != nil {
		t.Fatalf("insert legacy execution: %v", err)
	}
	machineExecutionID, err := executionRepo.InsertExecutionWithAudit(context.Background(), model.QueryExecutionRecord{
		TargetResourceID: targetID, ActorMachinePrincipalID: issued.Principal.ID, Actor: model.QueryExecutionActor{Kind: model.QueryExecutionActorMachine}, Engine: "mysql",
		FullStatement: "machine private statement", Status: model.QueryExecutionSuccess,
	}, "query.executed", "success")
	if err != nil {
		t.Fatalf("insert machine execution: %v", err)
	}

	statementPath := func(executionID uint64) string {
		return fmt.Sprintf("/query-targets/%d/executions/%d/statement", targetID, executionID)
	}
	ownerResponse := doBearer(t, router, http.MethodGet, statementPath(executed.ExecutionID), ownerToken)
	if ownerResponse.Code != http.StatusOK || strings.TrimSpace(ownerResponse.Body.String()) != fmt.Sprintf(`{"statement":%q}`, statement) {
		t.Fatalf("owner statement status/body = %d/%s", ownerResponse.Code, ownerResponse.Body.String())
	}

	for name, tc := range map[string]struct {
		executionID uint64
		token       string
	}{
		"other user":  {executed.ExecutionID, otherToken},
		"admin other": {executed.ExecutionID, adminToken},
		"failed":      {failedID, ownerToken},
		"legacy":      {legacyID, ownerToken},
		"machine row": {machineExecutionID, ownerToken},
	} {
		t.Run(name, func(t *testing.T) {
			response := doBearer(t, router, http.MethodGet, statementPath(tc.executionID), tc.token)
			if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error":"query_execution_not_found"`) {
				t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
			}
		})
	}
	machineResponse := doBearer(t, router, http.MethodGet, statementPath(executed.ExecutionID), issued.Secret)
	if machineResponse.Code != http.StatusForbidden {
		t.Fatalf("machine credential status = %d, want 403; body=%s", machineResponse.Code, machineResponse.Body.String())
	}

	listResponse := doBearer(t, router, http.MethodGet, fmt.Sprintf("/query-targets/%d/executions?page=1&pageSize=20", targetID), adminToken)
	if listResponse.Code != http.StatusOK || strings.Contains(listResponse.Body.String(), `"statement":`) || strings.Contains(listResponse.Body.String(), "fullStatement") {
		t.Fatalf("history leaked statement contract: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	auditResponse := doBearer(t, router, http.MethodGet, fmt.Sprintf("/audit-events?targetResourceId=%d&pageSize=500", targetID), adminToken)
	if auditResponse.Code != http.StatusOK || strings.Contains(auditResponse.Body.String(), `"statement":`) || strings.Contains(auditResponse.Body.String(), "fullStatement") {
		t.Fatalf("audit leaked statement contract: status=%d body=%s", auditResponse.Code, auditResponse.Body.String())
	}

	_ = otherID
}

func assertWorkspaceHTTPContract(t *testing.T, router http.Handler, ownerToken, otherToken, adminToken, machineToken string, targetID uint64) {
	t.Helper()
	for name, token := range map[string]string{"owner": ownerToken, "other": otherToken, "admin": adminToken} {
		response := doBearer(t, router, http.MethodGet, "/query-workspace", token)
		var workspace model.QueryWorkspace
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &workspace) != nil || workspace.Version != 0 || workspace.Worksheets == nil {
			t.Fatalf("%s missing workspace status/body = %d/%s", name, response.Code, response.Body.String())
		}
	}
	body := fmt.Sprintf(`{"expectedVersion":0,"worksheets":[{"id":"ws-1","name":"Orders","targetResourceId":%d,"statement":"not sql","activeDatabase":"orders"}]}`, targetID)
	put := doBearerWithBody(t, router, http.MethodPut, "/query-workspace", ownerToken, body)
	if put.Code != http.StatusOK || !strings.Contains(put.Body.String(), `"version":1`) {
		t.Fatalf("workspace PUT status/body = %d/%s", put.Code, put.Body.String())
	}
	conflict := doBearerWithBody(t, router, http.MethodPut, "/query-workspace", ownerToken, `{"expectedVersion":0,"worksheets":[]}`)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), `"error":"query_workspace_conflict"`) {
		t.Fatalf("workspace conflict status/body = %d/%s", conflict.Code, conflict.Body.String())
	}
	after := doBearer(t, router, http.MethodGet, "/query-workspace", ownerToken)
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), `"statement":"not sql"`) {
		t.Fatalf("conflict overwrote workspace: %d/%s", after.Code, after.Body.String())
	}
	machine := doBearer(t, router, http.MethodGet, "/query-workspace", machineToken)
	if machine.Code != http.StatusForbidden {
		t.Fatalf("machine workspace status = %d, want 403; body=%s", machine.Code, machine.Body.String())
	}
}
