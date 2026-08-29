// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, net/http/httptest, strings, testing, internal/model, internal/service
// output: table-driven machine read/collector route scopes, verified health observer attribution, truthful execute identity, user-only sibling-route, and controlled-error contract tests
// pos: Router-level regression boundary for independent machine authentication, principal-bound collector health writes, and ordinary governed execution
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type scopeMatrixMachineService struct {
	granted model.MachineScope
	err     error
	calls   int
}

func (s *scopeMatrixMachineService) Authenticate(_ context.Context, _ string, required model.MachineScope) (model.MachinePrincipalIdentity, error) {
	s.calls++
	if s.err != nil {
		return model.MachinePrincipalIdentity{}, s.err
	}
	if required != s.granted {
		return model.MachinePrincipalIdentity{}, service.ErrMachineScopeDenied
	}
	return model.MachinePrincipalIdentity{ID: 91, Name: "collector-a", CredentialID: 92, Scopes: []model.MachineScope{s.granted}}, nil
}

func (*scopeMatrixMachineService) List(context.Context, service.AuthenticatedUser) ([]model.MachinePrincipalListItem, error) {
	return nil, nil
}

func (*scopeMatrixMachineService) Create(context.Context, service.AuthenticatedUser, model.MachinePrincipalCreateRequest) (model.MachineCredentialIssue, error) {
	return model.MachineCredentialIssue{}, nil
}

func (*scopeMatrixMachineService) Rotate(context.Context, service.AuthenticatedUser, uint64, model.MachineCredentialRotateRequest) (model.MachineCredentialIssue, error) {
	return model.MachineCredentialIssue{}, nil
}

func (*scopeMatrixMachineService) Revoke(context.Context, service.AuthenticatedUser, uint64) error {
	return nil
}

type scopeMatrixNamedViewService struct{ shared bool }

func (*scopeMatrixNamedViewService) List(context.Context, service.AuthenticatedUser) ([]model.NamedInventoryView, error) {
	return nil, errors.New("machine request fell back to user-scoped List")
}

func (s *scopeMatrixNamedViewService) ListShared(context.Context) ([]model.NamedInventoryView, error) {
	s.shared = true
	return []model.NamedInventoryView{}, nil
}

func (*scopeMatrixNamedViewService) Create(context.Context, service.AuthenticatedUser, model.NamedInventoryViewCreateRequest) (model.NamedInventoryView, error) {
	return model.NamedInventoryView{}, nil
}

func (*scopeMatrixNamedViewService) Update(context.Context, service.AuthenticatedUser, uint64, model.NamedInventoryViewUpdateRequest) error {
	return nil
}

func (*scopeMatrixNamedViewService) Delete(context.Context, service.AuthenticatedUser, uint64) error {
	return nil
}

func TestMachineRouteScopeMatrix(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		granted    model.MachineScope
		wantStatus int
		wantCode   string
		wantShared bool
	}{
		{"inventory read", http.MethodGet, "/resources/not-an-id", model.MachineScopeInventoryRead, http.StatusBadRequest, "validation_failed", false},
		{"relations read", http.MethodGet, "/resources/not-an-id/relations", model.MachineScopeRelationsRead, http.StatusBadRequest, "validation_failed", false},
		{"topology read", http.MethodGet, "/resources/not-an-id/topology", model.MachineScopeRelationsRead, http.StatusBadRequest, "validation_failed", false},
		{"environment topology read", http.MethodGet, "/environments/not-an-id/topology", model.MachineScopeRelationsRead, http.StatusBadRequest, "validation_failed", false},
		{"audit read", http.MethodGet, "/resources/not-an-id/audit-events", model.MachineScopeAuditRead, http.StatusBadRequest, "validation_failed", false},
		{"shared named views", http.MethodGet, "/inventory/views", model.MachineScopeNamedViewsRead, http.StatusOK, "", true},
		{"governed select execute", http.MethodPost, "/query-targets/22/execute", model.MachineScopeGovernedSelect, http.StatusBadRequest, "validation_failed", false},
		{"collector ingestion preview", http.MethodPost, "/admin/ingestions/preview", model.MachineScopeInventoryIngest, http.StatusBadRequest, "validation_failed", false},
		{"collector ingestion confirm", http.MethodPost, "/admin/ingestions/confirm", model.MachineScopeInventoryIngest, http.StatusBadRequest, "validation_failed", false},
		{"collector health observation", http.MethodPost, "/resources/not-an-id/health-observations", model.MachineScopeHealthWrite, http.StatusBadRequest, "validation_failed", false},
		{"ingestion scope cannot write health", http.MethodPost, "/resources/not-an-id/health-observations", model.MachineScopeInventoryIngest, http.StatusForbidden, "machine_scope_denied", false},
		{"health scope cannot ingest", http.MethodPost, "/admin/ingestions/preview", model.MachineScopeHealthWrite, http.StatusForbidden, "machine_scope_denied", false},
		{"execution history stays user only", http.MethodGet, "/query-targets/22/executions", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"related records stay user only", http.MethodPost, "/query-targets/22/related-records", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"explain stays user only", http.MethodPost, "/query-targets/22/explain", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"schema stays user only", http.MethodGet, "/query-targets/22/schema/databases", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"credential stays user only", http.MethodGet, "/query-targets/22/credential", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"saved statements stay user only", http.MethodGet, "/query-targets/22/saved-statements", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"saved execution stays user only", http.MethodPost, "/query-targets/22/saved-statements/7/execute", model.MachineScopeGovernedSelect, http.StatusForbidden, "machine_scope_denied", false},
		{"missing route scope", http.MethodGet, "/resources/not-an-id", model.MachineScopeAuditRead, http.StatusForbidden, "machine_scope_denied", false},
		{"inventory mutation denied", http.MethodPatch, "/resources/1", model.MachineScopeInventoryRead, http.StatusForbidden, "machine_scope_denied", false},
		{"collector patch denied", http.MethodPatch, "/resources/1", model.MachineScopeInventoryIngest, http.StatusForbidden, "machine_scope_denied", false},
		{"collector archive denied", http.MethodPost, "/resources/1/archive", model.MachineScopeInventoryIngest, http.StatusForbidden, "machine_scope_denied", false},
		{"unlisted admin route denied", http.MethodGet, "/admin/machine-principals", model.MachineScopeInventoryRead, http.StatusForbidden, "machine_scope_denied", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			machine := &scopeMatrixMachineService{granted: tc.granted}
			views := &scopeMatrixNamedViewService{}
			server := NewTestServer()
			server.deps.MachinePrincipalService = machine
			server.deps.MachineCredentialService = machine
			server.deps.NamedInventoryViewService = views
			server.deps.QueryExecutionService = &stubQueryExec{}
			server.deps.QueryExplainService = &stubExplainAPI{}
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Authorization", "Bearer chmp_route-test.secret")
			rec := httptest.NewRecorder()

			NewRouter(server.deps).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantCode != "" && !strings.Contains(rec.Body.String(), `"error":"`+tc.wantCode+`"`) {
				t.Fatalf("body = %s, want controlled code %q", rec.Body.String(), tc.wantCode)
			}
			if views.shared != tc.wantShared {
				t.Fatalf("ListShared called = %t, want %t", views.shared, tc.wantShared)
			}
		})
	}
}

func TestMachineHealthObservationUsesVerifiedPrincipalObserver(t *testing.T) {
	machine := &scopeMatrixMachineService{granted: model.MachineScopeHealthWrite}
	server := NewTestServer()
	server.deps.MachineCredentialService = machine
	req := httptest.NewRequest(http.MethodPost, "/resources/1/health-observations", strings.NewReader(`{"status":"warning","observedAt":"2026-08-30T00:00:00Z","observer":"spoofed"}`))
	req.Header.Set("Authorization", "Bearer chmp_route-test.secret")
	rec := httptest.NewRecorder()

	NewRouter(server.deps).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	resource, err := server.deps.ResourceService.Get(1)
	if err != nil {
		t.Fatal(err)
	}
	if resource.HealthObserver != "machine:collector-a" {
		t.Fatalf("observer = %q, want verified machine principal", resource.HealthObserver)
	}
}

func TestMachineExecutePassesPrincipalIdentityNotCredential(t *testing.T) {
	machine := &scopeMatrixMachineService{granted: model.MachineScopeGovernedSelect}
	stub := &stubQueryExec{executeResp: model.QueryExecuteResponse{Status: model.QueryExecutionSuccess}}
	server := NewTestServer()
	server.deps.MachineCredentialService = machine
	server.deps.QueryExecutionService = stub
	req := httptest.NewRequest(http.MethodPost, "/query-targets/22/execute", strings.NewReader(`{"statement":"select 1"}`))
	req.Header.Set("Authorization", "Bearer chmp_route-test.secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	NewRouter(server.deps).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	want := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 91}
	if stub.gotIdentity != want {
		t.Fatalf("identity = %+v, want %+v (credential id 92 must not become evidence identity)", stub.gotIdentity, want)
	}
}

func TestMachineCredentialErrorsAreControlledAndOmitSecret(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code string
	}{
		{"malformed", service.ErrMachineCredentialInvalid, "machine_credential_invalid"},
		{"expired", service.ErrMachineCredentialExpired, "machine_credential_expired"},
		{"revoked", service.ErrMachineCredentialRevoked, "machine_credential_revoked"},
		{"scope", service.ErrMachineScopeDenied, "machine_scope_denied"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			machine := &scopeMatrixMachineService{err: tc.err}
			server := NewTestServer()
			server.deps.MachinePrincipalService = machine
			server.deps.MachineCredentialService = machine
			secret := "chmp_route-test.do-not-echo"
			req := httptest.NewRequest(http.MethodGet, "/resources/not-an-id", nil)
			req.Header.Set("Authorization", "Bearer "+secret)
			rec := httptest.NewRecorder()

			NewRouter(server.deps).ServeHTTP(rec, req)

			if !strings.Contains(rec.Body.String(), `"error":"`+tc.code+`"`) {
				t.Fatalf("body = %s, want controlled code %q", rec.Body.String(), tc.code)
			}
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatal("controlled error echoed the machine credential")
			}
		})
	}
}
