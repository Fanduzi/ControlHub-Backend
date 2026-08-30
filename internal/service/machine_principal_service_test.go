// Package service implements business logic for resource management.
// input: context, crypto/sha256, database/sql, errors, reflect, testing, time, internal/model
// output: machine-principal lifecycle, not-found mapping, and authentication service contract tests
// pos: Service security-boundary regression coverage with a repository fake
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

func TestMachinePrincipalServiceListReturnsReloadableLifecycleMetadata(t *testing.T) {
	createdAt := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	lastUsedAt := createdAt.Add(time.Hour)
	revokedAt := createdAt.Add(2 * time.Hour)
	repo := &fakeMachinePrincipalRepository{list: []model.MachinePrincipalListItem{{
		ID: 10, Name: "inventory agent", CreatedByUserID: 7, CreatedAt: createdAt,
		Credentials: []model.MachineCredentialLifecycle{
			{ID: 20, CreatedAt: createdAt, ExpiresAt: createdAt.Add(30 * 24 * time.Hour), LastUsedAt: &lastUsedAt},
			{ID: 21, CreatedAt: createdAt.Add(time.Minute), ExpiresAt: createdAt.Add(30 * 24 * time.Hour), RevokedAt: &revokedAt},
		},
	}}}

	items, err := NewMachinePrincipalService(repo).List(t.Context(), AuthenticatedUser{ID: 7, Role: "admin"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || len(items[0].Credentials) != 2 || items[0].Credentials[0].ID != 20 || items[0].Credentials[1].RevokedAt == nil {
		t.Fatalf("reloadable lifecycle list = %#v", items)
	}
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal lifecycle list: %v", err)
	}
	for _, forbidden := range []string{"secret", "hash", "lookup", "scope", "machinePrincipalId", "rotatedFromCredentialId"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(forbidden)) {
			t.Fatalf("lifecycle list exposed %q: %s", forbidden, raw)
		}
	}
}

func TestMachinePrincipalServiceListedCredentialDrivesOverlapAndRevoke(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	repo := &fakeMachinePrincipalRepository{}
	svc := NewMachinePrincipalService(repo).WithClock(func() time.Time { return now })
	admin := AuthenticatedUser{ID: 7, Role: "admin"}
	oldIssue, err := svc.Create(t.Context(), admin, model.MachinePrincipalCreateRequest{Name: "inventory agent", Scopes: []model.MachineScope{model.MachineScopeInventoryRead}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	repo.list = []model.MachinePrincipalListItem{{ID: oldIssue.Principal.ID, Credentials: []model.MachineCredentialLifecycle{{ID: oldIssue.Credential.ID, CreatedAt: oldIssue.Credential.CreatedAt, ExpiresAt: oldIssue.Credential.ExpiresAt}}}}
	items, err := svc.List(t.Context(), admin)
	if err != nil || len(items) != 1 || len(items[0].Credentials) != 1 {
		t.Fatalf("List = %#v, %v", items, err)
	}
	listedID := items[0].Credentials[0].ID
	newIssue, err := svc.Rotate(t.Context(), admin, listedID, model.MachineCredentialRotateRequest{Scopes: []model.MachineScope{model.MachineScopeInventoryRead}})
	if err != nil {
		t.Fatalf("Rotate listed credential: %v", err)
	}
	for name, secret := range map[string]string{"old": oldIssue.Secret, "new": newIssue.Secret} {
		if _, err := svc.Authenticate(t.Context(), secret, model.MachineScopeInventoryRead); err != nil {
			t.Fatalf("%s credential during overlap: %v", name, err)
		}
	}
	if err := svc.Revoke(t.Context(), admin, listedID); err != nil {
		t.Fatalf("Revoke listed credential: %v", err)
	}
	if _, err := svc.Authenticate(t.Context(), oldIssue.Secret, model.MachineScopeInventoryRead); !errors.Is(err, ErrMachineCredentialRevoked) {
		t.Fatalf("listed credential after revoke = %v, want revoked", err)
	}
	if _, err := svc.Authenticate(t.Context(), newIssue.Secret, model.MachineScopeInventoryRead); err != nil {
		t.Fatalf("replacement credential after listed revoke: %v", err)
	}
}

func TestMachinePrincipalServiceCreateReturnsSecretOnceAndPersistsHashOnly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	repo := &fakeMachinePrincipalRepository{}
	service := NewMachinePrincipalService(repo).WithClock(func() time.Time { return now })

	issued, err := service.Create(t.Context(), AuthenticatedUser{ID: 7, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name:   "  inventory agent  ",
		Scopes: []model.MachineScope{model.MachineScopeAuditRead, model.MachineScopeInventoryRead},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issued.Secret == "" {
		t.Fatal("create must return the plaintext credential")
	}
	if issued.Principal.Name != "inventory agent" || issued.Principal.ID == 0 || issued.Credential.ID == 0 {
		t.Fatalf("issued metadata = %+v", issued)
	}
	if repo.created.SecretHash != sha256.Sum256([]byte(issued.Secret)) {
		t.Fatal("repository received anything other than the irreversible credential hash")
	}
	if repo.created.LookupID == "" || repo.created.ExpiresAt != now.Add(model.DefaultMachineCredentialLifetime) {
		t.Fatalf("persisted credential = %+v", repo.created)
	}
	wantScopes := []model.MachineScope{model.MachineScopeInventoryRead, model.MachineScopeAuditRead}
	if !reflect.DeepEqual(repo.created.Scopes, wantScopes) {
		t.Fatalf("persisted scopes = %v, want %v", repo.created.Scopes, wantScopes)
	}
}

func TestMachinePrincipalServiceCreateRequiresAdminAndValidInput(t *testing.T) {
	repo := &fakeMachinePrincipalRepository{}
	service := NewMachinePrincipalService(repo)

	_, err := service.Create(t.Context(), AuthenticatedUser{ID: 8, Role: "editor"}, model.MachinePrincipalCreateRequest{
		Name: "agent", Scopes: []model.MachineScope{model.MachineScopeInventoryRead},
	})
	if !errors.Is(err, ErrMachinePrincipalForbidden) {
		t.Fatalf("non-admin error = %v, want forbidden", err)
	}
	_, err = service.Create(t.Context(), AuthenticatedUser{ID: 7, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "agent", Scopes: []model.MachineScope{"inventory:write"},
	})
	if !errors.Is(err, ErrMachinePrincipalValidation) {
		t.Fatalf("invalid scope error = %v, want validation", err)
	}
	if repo.createCalls != 0 {
		t.Fatalf("repository create calls = %d, want 0", repo.createCalls)
	}
}

func TestMachinePrincipalServiceRotateIssuesOverlappingCredential(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repo := &fakeMachinePrincipalRepository{}
	service := NewMachinePrincipalService(repo).WithClock(func() time.Time { return now })

	issued, err := service.Rotate(t.Context(), AuthenticatedUser{ID: 7, Role: "admin"}, 20, model.MachineCredentialRotateRequest{
		Scopes: []model.MachineScope{model.MachineScopeRelationsRead},
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if issued.Secret == "" || issued.Credential.ID != 21 || issued.Credential.MachinePrincipalID != 10 {
		t.Fatalf("rotated credential = %+v", issued)
	}
	if repo.rotated.SecretHash != sha256.Sum256([]byte(issued.Secret)) {
		t.Fatal("rotation must persist only the new credential hash")
	}
	if repo.rotated.RotatedFromCredentialID == nil || *repo.rotated.RotatedFromCredentialID != 20 {
		t.Fatalf("rotated-from id = %v, want 20", repo.rotated.RotatedFromCredentialID)
	}
	if repo.revokeCalls != 0 {
		t.Fatal("rotation must leave the old credential valid for overlap")
	}
}

func TestMachinePrincipalServiceRotateMapsMissingCredential(t *testing.T) {
	repo := &fakeMachinePrincipalRepository{rotateErr: sql.ErrNoRows}
	svc := NewMachinePrincipalService(repo)

	_, err := svc.Rotate(t.Context(), AuthenticatedUser{ID: 7, Role: "admin"}, 23, model.MachineCredentialRotateRequest{
		Scopes: []model.MachineScope{model.MachineScopeAuditRead},
	})
	if !errors.Is(err, ErrMachineCredentialNotFound) {
		t.Fatalf("Rotate missing credential error = %v, want not found", err)
	}
}

func TestMachinePrincipalServiceRotationOverlapsUntilOldCredentialRevoked(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repo := &fakeMachinePrincipalRepository{}
	service := NewMachinePrincipalService(repo).WithClock(func() time.Time { return now })
	admin := AuthenticatedUser{ID: 7, Role: "admin"}

	oldIssue, err := service.Create(t.Context(), admin, model.MachinePrincipalCreateRequest{
		Name: "inventory agent", Scopes: []model.MachineScope{model.MachineScopeInventoryRead},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	newIssue, err := service.Rotate(t.Context(), admin, oldIssue.Credential.ID, model.MachineCredentialRotateRequest{
		Scopes: []model.MachineScope{model.MachineScopeInventoryRead},
	})
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	for name, token := range map[string]string{"old": oldIssue.Secret, "new": newIssue.Secret} {
		t.Run(name+" overlaps", func(t *testing.T) {
			identity, err := service.Authenticate(t.Context(), token, model.MachineScopeInventoryRead)
			if err != nil || identity.ID != oldIssue.Principal.ID {
				t.Fatalf("Authenticate = %+v, %v", identity, err)
			}
		})
	}
	if err := service.Revoke(t.Context(), admin, oldIssue.Credential.ID); err != nil {
		t.Fatalf("Revoke old: %v", err)
	}
	if _, err := service.Authenticate(t.Context(), oldIssue.Secret, model.MachineScopeInventoryRead); !errors.Is(err, ErrMachineCredentialRevoked) {
		t.Fatalf("old credential after revoke = %v, want revoked", err)
	}
	if _, err := service.Authenticate(t.Context(), newIssue.Secret, model.MachineScopeInventoryRead); err != nil {
		t.Fatalf("new credential after old revoke: %v", err)
	}
}

func TestMachinePrincipalServiceAuthenticateRejectsExpiryAndMissingScope(t *testing.T) {
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	repo := &fakeMachinePrincipalRepository{}
	service := NewMachinePrincipalService(repo).WithClock(func() time.Time { return now })
	expiresAt := now.Add(time.Hour)
	issued, err := service.Create(t.Context(), AuthenticatedUser{ID: 7, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "audit agent", Scopes: []model.MachineScope{model.MachineScopeAuditRead}, ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Authenticate(t.Context(), issued.Secret, model.MachineScopeInventoryRead); !errors.Is(err, ErrMachineScopeDenied) {
		t.Fatalf("missing scope error = %v, want scope denied", err)
	}
	now = expiresAt
	if _, err := service.Authenticate(t.Context(), issued.Secret, model.MachineScopeAuditRead); !errors.Is(err, ErrMachineCredentialExpired) {
		t.Fatalf("expiry boundary error = %v, want expired", err)
	}
	if _, err := service.Authenticate(t.Context(), "not-a-credential", model.MachineScopeAuditRead); !errors.Is(err, ErrMachineCredentialInvalid) {
		t.Fatalf("malformed error = %v, want invalid", err)
	}
}

type fakeMachinePrincipalRepository struct {
	list        []model.MachinePrincipalListItem
	createCalls int
	created     MachineCredentialInsert
	rotateCalls int
	rotated     MachineCredentialInsert
	rotateErr   error
	revokeCalls int
	credentials map[string]MachineCredentialAuthentication
	used        []uint64
}

func (f *fakeMachinePrincipalRepository) List(context.Context) ([]model.MachinePrincipalListItem, error) {
	return f.list, nil
}

func (f *fakeMachinePrincipalRepository) Create(_ context.Context, actorID uint64, name string, credential MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	f.createCalls++
	f.created = credential
	principal := model.MachinePrincipal{ID: 10, Name: name, CreatedByUserID: actorID, CreatedAt: credential.CreatedAt}
	stored := model.MachineCredential{
		ID: 20, MachinePrincipalID: 10, LookupID: credential.LookupID, Scopes: credential.Scopes,
		ExpiresAt: credential.ExpiresAt, CreatedAt: credential.CreatedAt,
	}
	f.store(principal, stored, credential.SecretHash)
	return principal, stored, nil
}

func (f *fakeMachinePrincipalRepository) Rotate(_ context.Context, _ uint64, oldCredentialID uint64, credential MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error) {
	f.rotateCalls++
	f.rotated = credential
	if f.rotateErr != nil {
		return model.MachinePrincipal{}, model.MachineCredential{}, f.rotateErr
	}
	principal := model.MachinePrincipal{ID: 10, Name: "inventory agent"}
	stored := model.MachineCredential{
		ID: 21, MachinePrincipalID: 10, LookupID: credential.LookupID, Scopes: credential.Scopes,
		ExpiresAt: credential.ExpiresAt, RotatedFromCredentialID: &oldCredentialID, CreatedAt: credential.CreatedAt,
	}
	f.store(principal, stored, credential.SecretHash)
	return principal, stored, nil
}

func (f *fakeMachinePrincipalRepository) Revoke(_ context.Context, _ uint64, credentialID uint64, revokedAt time.Time) error {
	f.revokeCalls++
	for lookupID, auth := range f.credentials {
		if auth.Credential.ID == credentialID {
			auth.Credential.RevokedAt = &revokedAt
			f.credentials[lookupID] = auth
			return nil
		}
	}
	return nil
}

func (f *fakeMachinePrincipalRepository) FindCredential(_ context.Context, lookupID string) (MachineCredentialAuthentication, error) {
	auth, ok := f.credentials[lookupID]
	if !ok {
		return MachineCredentialAuthentication{}, errors.New("not found")
	}
	return auth, nil
}

func (f *fakeMachinePrincipalRepository) MarkUsed(_ context.Context, credentialID uint64, _ time.Time) error {
	f.used = append(f.used, credentialID)
	return nil
}

func (f *fakeMachinePrincipalRepository) store(principal model.MachinePrincipal, credential model.MachineCredential, hash [sha256.Size]byte) {
	if f.credentials == nil {
		f.credentials = make(map[string]MachineCredentialAuthentication)
	}
	f.credentials[credential.LookupID] = MachineCredentialAuthentication{Principal: principal, Credential: credential, SecretHash: hash}
}
