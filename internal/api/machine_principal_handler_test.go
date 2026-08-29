// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http/httptest, internal/model, internal/service
// output: machine-principal HTTP boundary tests
// pos: Admin lifecycle and machine-credential error contract
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type fakeMachinePrincipalAPI struct {
	items       []model.MachinePrincipalListItem
	create      model.MachineCredentialIssue
	rotate      model.MachineCredentialIssue
	createErr   error
	rotateErr   error
	revokeErr   error
	createdWith model.MachinePrincipalCreateRequest
	rotatedID   uint64
	revokedID   uint64
}

func (f *fakeMachinePrincipalAPI) List(context.Context, service.AuthenticatedUser) ([]model.MachinePrincipalListItem, error) {
	return f.items, nil
}

func (f *fakeMachinePrincipalAPI) Create(_ context.Context, _ service.AuthenticatedUser, req model.MachinePrincipalCreateRequest) (model.MachineCredentialIssue, error) {
	f.createdWith = req
	return f.create, f.createErr
}

func (f *fakeMachinePrincipalAPI) Rotate(_ context.Context, _ service.AuthenticatedUser, credentialID uint64, _ model.MachineCredentialRotateRequest) (model.MachineCredentialIssue, error) {
	f.rotatedID = credentialID
	return f.rotate, f.rotateErr
}

func (f *fakeMachinePrincipalAPI) Revoke(_ context.Context, _ service.AuthenticatedUser, credentialID uint64) error {
	f.revokedID = credentialID
	return f.revokeErr
}

func machinePrincipalRouter(svc machinePrincipalAPI) http.Handler {
	srv := NewTestServer()
	srv.deps.MachinePrincipalService = svc
	return NewRouter(srv.deps)
}

func TestMachinePrincipalAdminRoutes(t *testing.T) {
	secret := "chmp_plaintext-must-only-appear-here"
	svc := &fakeMachinePrincipalAPI{
		items:  []model.MachinePrincipalListItem{{ID: 1, Name: "inventory-agent"}},
		create: model.MachineCredentialIssue{Principal: model.MachinePrincipal{ID: 2, Name: "created"}, Credential: model.MachineCredential{ID: 3}, Secret: secret},
		rotate: model.MachineCredentialIssue{Principal: model.MachinePrincipal{ID: 2, Name: "created"}, Credential: model.MachineCredential{ID: 4}, Secret: secret},
	}
	router := machinePrincipalRouter(svc)

	for _, tc := range []struct {
		name, method, path, body string
		want                     int
		secret                   bool
	}{
		{"list", http.MethodGet, "/admin/machine-principals", "", http.StatusOK, false},
		{"create", http.MethodPost, "/admin/machine-principals", `{"name":"created","scopes":["inventory:read"]}`, http.StatusCreated, true},
		{"rotate", http.MethodPost, "/admin/machine-credentials/3/rotate", `{"scopes":["inventory:read"]}`, http.StatusCreated, true},
		{"revoke", http.MethodPost, "/admin/machine-credentials/3/revoke", "", http.StatusNoContent, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer "+ssAdminToken(t))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			router.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("%s = %d, want %d: %s", tc.path, rec.Code, tc.want, rec.Body.String())
			}
			if got := strings.Contains(rec.Body.String(), secret); got != tc.secret {
				t.Fatalf("secret present = %v, want %v: %s", got, tc.secret, rec.Body.String())
			}
		})
	}

	if svc.createdWith.Name != "created" {
		t.Fatalf("create request = %#v", svc.createdWith)
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(method, "/admin/machine-principals", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous %s = %d, want 401", method, rec.Code)
		}
	}
	for _, path := range []string{"/admin/machine-principals", "/admin/machine-credentials/3/rotate", "/admin/machine-credentials/3/revoke"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+ssViewerToken(t))
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("editor %s = %d, want 403", path, rec.Code)
		}
	}
}

func TestMachinePrincipalListReturnsSafeCredentialLifecycleMetadata(t *testing.T) {
	createdAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	lastUsedAt := createdAt.Add(time.Hour)
	revokedAt := createdAt.Add(2 * time.Hour)
	router := machinePrincipalRouter(&fakeMachinePrincipalAPI{items: []model.MachinePrincipalListItem{{
		ID: 1, Name: "inventory-agent", CreatedByUserID: 7, CreatedAt: createdAt,
		Credentials: []model.MachineCredentialLifecycle{
			{ID: 20, CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * 24 * time.Hour), LastUsedAt: &lastUsedAt},
			{ID: 21, CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * 24 * time.Hour), RevokedAt: &revokedAt},
		},
	}}})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/machine-principals", nil)
	req.Header.Set("Authorization", "Bearer "+ssAdminToken(t))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	for _, required := range []string{"\"credentials\"", "\"id\":20", "\"createdAt\"", "\"expiresAt\"", "\"lastUsedAt\"", "\"revokedAt\""} {
		if !strings.Contains(rec.Body.String(), required) {
			t.Fatalf("list missing %s: %s", required, rec.Body.String())
		}
	}
	for _, forbidden := range []string{"secret", "hash", "lookup", "scope", "machinePrincipalId", "rotatedFromCredentialId"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(forbidden)) {
			t.Fatalf("list exposed %q: %s", forbidden, rec.Body.String())
		}
	}
}

func TestMachinePrincipalListCredentialIDDrivesLifecycleRoutes(t *testing.T) {
	const credentialID = 20
	svc := &fakeMachinePrincipalAPI{
		items:  []model.MachinePrincipalListItem{{ID: 1, Credentials: []model.MachineCredentialLifecycle{{ID: credentialID}}}},
		rotate: model.MachineCredentialIssue{Credential: model.MachineCredential{ID: 21}, Secret: "one-time"},
	}
	router := machinePrincipalRouter(svc)
	list := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/admin/machine-principals", nil)
	request.Header.Set("Authorization", "Bearer "+ssAdminToken(t))
	router.ServeHTTP(list, request)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"id":20`) {
		t.Fatalf("list = %d: %s", list.Code, list.Body.String())
	}
	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{"/admin/machine-credentials/20/rotate", `{"scopes":["inventory:read"]}`, http.StatusCreated},
		{"/admin/machine-credentials/20/revoke", "", http.StatusNoContent},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+ssAdminToken(t))
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		router.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Fatalf("%s = %d, want %d: %s", tc.path, rec.Code, tc.want, rec.Body.String())
		}
	}
	if svc.rotatedID != credentialID || svc.revokedID != credentialID {
		t.Fatalf("lifecycle IDs = rotate %d revoke %d, want listed %d", svc.rotatedID, svc.revokedID, credentialID)
	}
}

type fakeMachineCredentialRepo struct {
	auth service.MachineCredentialAuthentication
	err  error
	used bool
}

func (f *fakeMachineCredentialRepo) List(context.Context) ([]model.MachinePrincipalListItem, error) {
	return nil, nil
}
func (f *fakeMachineCredentialRepo) Create(context.Context, uint64, string, service.MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	return model.MachinePrincipal{}, model.MachineCredential{}, errors.New("unused")
}
func (f *fakeMachineCredentialRepo) Rotate(context.Context, uint64, uint64, service.MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	return model.MachinePrincipal{}, model.MachineCredential{}, errors.New("unused")
}
func (f *fakeMachineCredentialRepo) Revoke(context.Context, uint64, uint64, time.Time) error {
	return errors.New("unused")
}
func (f *fakeMachineCredentialRepo) FindCredential(context.Context, string) (service.MachineCredentialAuthentication, error) {
	return f.auth, f.err
}
func (f *fakeMachineCredentialRepo) MarkUsed(context.Context, uint64, time.Time) error {
	f.used = true
	return nil
}

func TestMachineCredentialMiddlewareDoesNotFallBackToUserAuth(t *testing.T) {
	valid := issueMachineCredential(t)
	repo := &fakeMachineCredentialRepo{auth: valid.auth}
	svc := service.NewMachinePrincipalService(repo).WithClock(func() time.Time { return valid.now })
	h := requireMachineCredential(svc, model.MachineScopeInventoryRead)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := machinePrincipalFromContext(r.Context())
		if !ok || identity.ID != valid.auth.Principal.ID {
			t.Fatal("machine identity missing")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	for name, token := range map[string]string{
		"missing": "", "malformed": "not-a-machine-token", "browser user": ssAdminToken(t),
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/resources", nil)
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body.String())
			}
		})
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resources", nil)
	req.Header.Set("Authorization", "Bearer "+valid.token)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !repo.used {
		t.Fatalf("valid credential = %d, used=%v", rec.Code, repo.used)
	}
}

type issuedMachineCredential struct {
	token string
	auth  service.MachineCredentialAuthentication
	now   time.Time
}

func issueMachineCredential(t *testing.T) issuedMachineCredential {
	t.Helper()
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	creator := &recordingMachineCredentialRepo{}
	issue, err := service.NewMachinePrincipalService(creator).WithClock(func() time.Time { return now }).Create(context.Background(), service.AuthenticatedUser{ID: 1, Role: "admin"}, model.MachinePrincipalCreateRequest{Name: "api-test", Scopes: []model.MachineScope{model.MachineScopeInventoryRead}})
	if err != nil {
		t.Fatalf("issue credential: %v", err)
	}
	return issuedMachineCredential{token: issue.Secret, auth: creator.auth, now: now}
}

type recordingMachineCredentialRepo struct {
	auth service.MachineCredentialAuthentication
}

func (r *recordingMachineCredentialRepo) List(context.Context) ([]model.MachinePrincipalListItem, error) {
	return nil, nil
}
func (r *recordingMachineCredentialRepo) Create(_ context.Context, _ uint64, name string, c service.MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	r.auth = service.MachineCredentialAuthentication{Principal: model.MachinePrincipal{ID: 9, Name: name}, Credential: model.MachineCredential{ID: 10, Scopes: c.Scopes, ExpiresAt: c.ExpiresAt}, SecretHash: c.SecretHash}
	return r.auth.Principal, r.auth.Credential, nil
}
func (r *recordingMachineCredentialRepo) Rotate(context.Context, uint64, uint64, service.MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	return model.MachinePrincipal{}, model.MachineCredential{}, errors.New("unused")
}
func (r *recordingMachineCredentialRepo) Revoke(context.Context, uint64, uint64, time.Time) error {
	return errors.New("unused")
}
func (r *recordingMachineCredentialRepo) FindCredential(context.Context, string) (service.MachineCredentialAuthentication, error) {
	return r.auth, nil
}
func (r *recordingMachineCredentialRepo) MarkUsed(context.Context, uint64, time.Time) error {
	return nil
}

func TestMachinePrincipalIssueJSONSecretOnly(t *testing.T) {
	raw, err := json.Marshal(model.MachineCredentialIssue{Principal: model.MachinePrincipal{ID: 1}, Credential: model.MachineCredential{ID: 2}, Secret: "one-time"})
	if err != nil || !strings.Contains(string(raw), "one-time") {
		t.Fatalf("issue JSON = %q, err=%v", raw, err)
	}
}
