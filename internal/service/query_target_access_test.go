// Package service provides characterization tests for the shared governed
// target access resolver (Task B2). These tests pin the current behaviour of
// Execute and InspectCredentialRuntime so the refactoring to a shared resolver
// cannot silently change governance, error, or DSN-leak semantics.
package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// --- ResolveTargetAccess characterization tests ---
// These tests pin the governance behaviour that Execute and InspectCredentialRuntime
// share. After the refactor, the shared resolver must produce the same outcomes.

func TestResolveTargetAccess_MissingTarget_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: nil}, // no targets
		&fakeExecRepo{},
		&fakeResolver{},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9999)
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("error = %v, want ErrQueryTargetNotFound", err)
	}
}

func TestResolveTargetAccess_UnsupportedEngine_ReturnsDenied(t *testing.T) {
	t.Parallel()
	target := mysqlTarget("Staging")
	target.ConnectionContext.Engine = "redis"
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{target}},
		&fakeExecRepo{},
		&fakeResolver{},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for unsupported engine")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeUnsupportedTarget {
		t.Fatalf("status = %q, want unsupported_target", denied.Status)
	}
}

func TestResolveTargetAccess_MissingCredential_ReturnsDenied(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{}, // no credential row
		&fakeResolver{},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for missing credential")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeMissingMetadata {
		t.Fatalf("status = %q, want missing_metadata", denied.Status)
	}
	// Message must match Execute's rejection message for this case.
	if denied.Error() != "target is not enabled for execution" {
		t.Fatalf("message = %q, want %q", denied.Error(), "target is not enabled for execution")
	}
}

func TestResolveTargetAccess_InvalidStoredCredential_ReturnsDenied(t *testing.T) {
	t.Parallel()
	repo := &fakeExecRepo{
		credentialErr: map[uint64]error{9001: model.ErrInvalidCredentialMetadata},
	}
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo,
		&fakeResolver{},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for invalid stored credential")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeInvalidRef {
		t.Fatalf("status = %q, want invalid_ref", denied.Status)
	}
}

func TestResolveTargetAccess_DisabledCredential_ReturnsDenied(t *testing.T) {
	t.Parallel()
	cred := enabledCred(model.QueryEnvPolicyNonProdOnly)
	cred.Enabled = false
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: cred}},
		&fakeResolver{dsn: testResolverDSN},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for disabled credential")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeDisabled {
		t.Fatalf("status = %q, want disabled", denied.Status)
	}
}

func TestResolveTargetAccess_PolicyBlocked_ReturnsDenied(t *testing.T) {
	t.Parallel()
	// Production + non_prod_only = blocked.
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Production")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for policy-blocked credential")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimePolicyBlocked {
		t.Fatalf("status = %q, want policy_blocked", denied.Status)
	}
}

func TestResolveTargetAccess_CredentialEngineMismatch_ReturnsDenied(t *testing.T) {
	t.Parallel()
	cred := enabledCred(model.QueryEnvPolicyAllEnvironments)
	cred.Engine = "postgresql"
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: cred}},
		&fakeResolver{dsn: testResolverDSN},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for engine-mismatched credential")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimePolicyBlocked {
		t.Fatalf("status = %q, want policy_blocked", denied.Status)
	}
}

func TestResolveTargetAccess_ResolverFailure_ReturnsLeakFreeError(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{err: errors.New("env var not set")},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for resolver failure")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeSecretMissing {
		t.Fatalf("status = %q, want secret_missing", denied.Status)
	}
	// WHY: the error message must never contain the DSN or its password.
	if strings.Contains(denied.Error(), testResolverDSN) {
		t.Fatalf("DSN leaked into error message: %q", denied.Error())
	}
	if denied.Error() != "credential could not be resolved" {
		t.Fatalf("message = %q, want %q", denied.Error(), "credential could not be resolved")
	}
}

func TestResolveTargetAccess_ResolverReturnsEmptyDSN_ReturnsDenied(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: ""}, // resolver returns empty DSN
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for empty DSN")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeSecretMissing {
		t.Fatalf("status = %q, want secret_missing", denied.Status)
	}
}

// --- DSN binding characterization tests ---
// These pin the fail-closed DSN binding behaviour shared by Execute and
// InspectCredentialRuntime.

func TestResolveTargetAccess_DSNHostMismatch_ReturnsBindingMismatch(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: "rouser:secret@tcp(other-db.internal:3306)/sandbox"},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for host mismatch")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeBindingMismatch {
		t.Fatalf("status = %q, want binding_mismatch", denied.Status)
	}
	// WHY: the error must never echo the DSN host.
	if strings.Contains(denied.Error(), "other-db.internal") {
		t.Fatalf("DSN host leaked into error: %q", denied.Error())
	}
}

func TestResolveTargetAccess_DSNPortMismatch_ReturnsBindingMismatch(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: "rouser:secret@tcp(db.internal:3307)/sandbox"},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for port mismatch")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeBindingMismatch {
		t.Fatalf("status = %q, want binding_mismatch", denied.Status)
	}
}

func TestResolveTargetAccess_DSNMissingPort_ReturnsBindingMismatch(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: "rouser:secret@tcp(db.internal)/sandbox"},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for missing port")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeBindingMismatch {
		t.Fatalf("status = %q, want binding_mismatch", denied.Status)
	}
}

func TestResolveTargetAccess_DSNNonTCP_ReturnsBindingMismatch(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: "rouser:secret@unix(/tmp/mysql.sock)/sandbox"},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for non-TCP DSN")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeBindingMismatch {
		t.Fatalf("status = %q, want binding_mismatch", denied.Status)
	}
}

func TestResolveTargetAccess_DSNMalformed_ReturnsBindingMismatch(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: "not-a-valid-dsn"},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for malformed DSN")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimeBindingMismatch {
		t.Fatalf("status = %q, want binding_mismatch", denied.Status)
	}
}

// --- Success path characterization ---

func TestResolveTargetAccess_Success_ReturnsTargetAndDSN(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN},
	)

	access, err := resolver.Resolve(context.Background(), 7, 9001)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if access.Target.ResourceID != 9001 {
		t.Fatalf("target ResourceID = %d, want 9001", access.Target.ResourceID)
	}
	if access.Credential.CredentialRef != "ORDER_MYSQL_RO" {
		t.Fatalf("credential ref = %q, want ORDER_MYSQL_RO", access.Credential.CredentialRef)
	}
	// DSN must be accessible within the package (for executor).
	if access.dsn != testResolverDSN {
		t.Fatalf("dsn = %q, want %q", access.dsn, testResolverDSN)
	}
}

func TestResolveTargetAccess_Success_MatchingDSNPassesBinding(t *testing.T) {
	t.Parallel()
	// The scaffold DSN binds to db.internal:3306 which matches the target.
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN},
	)

	access, err := resolver.Resolve(context.Background(), 7, 9001)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	// WHY: a matching DSN must succeed binding and return the DSN for execution.
	if access.dsn == "" {
		t.Fatal("resolved DSN must not be empty on success")
	}
}

// --- DSN leak characterization ---

func TestResolveTargetAccess_NeverLeaksDSNInErrors(t *testing.T) {
	t.Parallel()
	// Exercise every failure path that processes a DSN and verify none leak it.
	cases := []struct {
		name     string
		dsn      string
		resolver *fakeResolver
	}{
		{
			name:     "resolver failure",
			dsn:      testResolverDSN,
			resolver: &fakeResolver{err: errors.New("env not set")},
		},
		{
			name:     "host mismatch",
			dsn:      "rouser:secret-dsn-do-not-leak@tcp(other-db.internal:3306)/sandbox",
			resolver: &fakeResolver{dsn: "rouser:secret-dsn-do-not-leak@tcp(other-db.internal:3306)/sandbox"},
		},
		{
			name:     "port mismatch",
			dsn:      "rouser:secret-dsn-do-not-leak@tcp(db.internal:3307)/sandbox",
			resolver: &fakeResolver{dsn: "rouser:secret-dsn-do-not-leak@tcp(db.internal:3307)/sandbox"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := NewTargetAccessResolver(
				fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
				&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
				tc.resolver,
			)
			_, err := r.Resolve(context.Background(), 1, 9001)
			if err == nil {
				t.Fatal("expected error")
			}
			// WHY: the error message must never contain the DSN password or full DSN.
			if strings.Contains(err.Error(), "secret-dsn-do-not-leak") {
				t.Fatalf("DSN password leaked into error: %q", err.Error())
			}
			if strings.Contains(err.Error(), "rouser:") {
				t.Fatalf("DSN username leaked into error: %q", err.Error())
			}
		})
	}
}

// --- Production policy characterization ---

func TestResolveTargetAccess_ProdWithNonProdOnlyPolicy_ReturnsPolicyBlocked(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Production")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		&fakeResolver{dsn: testResolverDSN},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for prod + non_prod_only")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimePolicyBlocked {
		t.Fatalf("status = %q, want policy_blocked", denied.Status)
	}
}

func TestResolveTargetAccess_ProdWithAllEnvironmentsPolicy_Succeeds(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Production")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyAllEnvironments)}},
		&fakeResolver{dsn: testResolverDSN},
	)

	access, err := resolver.Resolve(context.Background(), 1, 9001)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if access.Target.ResourceID != 9001 {
		t.Fatalf("target ResourceID = %d, want 9001", access.Target.ResourceID)
	}
}

func TestResolveTargetAccess_DisabledPolicy_ReturnsPolicyBlocked(t *testing.T) {
	t.Parallel()
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyDisabled)}},
		&fakeResolver{dsn: testResolverDSN},
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error for disabled policy")
	}
	var denied *TargetAccessError
	if !errors.As(err, &denied) {
		t.Fatalf("error = %T %v, want *TargetAccessError", err, err)
	}
	if denied.Status != model.QueryCredentialRuntimePolicyBlocked {
		t.Fatalf("status = %q, want policy_blocked", denied.Status)
	}
}

// --- Resolver not called for early-rejection cases ---

func TestResolveTargetAccess_ResolverNotCalledWhenCredentialMissing(t *testing.T) {
	t.Parallel()
	fakeRes := &fakeResolver{dsn: testResolverDSN}
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{}, // no credential row
		fakeRes,
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error")
	}
	if fakeRes.called {
		t.Fatal("credential resolver must not be called when no credential metadata exists")
	}
}

func TestResolveTargetAccess_ResolverNotCalledWhenCredentialDisabled(t *testing.T) {
	t.Parallel()
	fakeRes := &fakeResolver{dsn: testResolverDSN}
	cred := enabledCred(model.QueryEnvPolicyNonProdOnly)
	cred.Enabled = false
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: cred}},
		fakeRes,
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error")
	}
	if fakeRes.called {
		t.Fatal("credential resolver must not be called when credential is disabled")
	}
}

func TestResolveTargetAccess_ResolverNotCalledWhenPolicyBlocked(t *testing.T) {
	t.Parallel()
	fakeRes := &fakeResolver{dsn: testResolverDSN}
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Production")}},
		&fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}},
		fakeRes,
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error")
	}
	if fakeRes.called {
		t.Fatal("credential resolver must not be called when policy blocks execution")
	}
}

// --- Credential ref not called to resolver when invalid ---

func TestResolveTargetAccess_ResolverNotCalledWhenStoredCredentialInvalid(t *testing.T) {
	t.Parallel()
	fakeRes := &fakeResolver{dsn: testResolverDSN}
	repo := &fakeExecRepo{
		credentialErr: map[uint64]error{9001: sql.ErrNoRows},
	}
	resolver := NewTargetAccessResolver(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo,
		fakeRes,
	)

	_, err := resolver.Resolve(context.Background(), 1, 9001)
	if err == nil {
		t.Fatal("expected error")
	}
	if fakeRes.called {
		t.Fatal("credential resolver must not be called when credential read fails")
	}
}
