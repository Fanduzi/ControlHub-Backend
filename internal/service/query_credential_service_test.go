// Package service provides tests for the Phase 38A query credential metadata
// service (Task B3): the runtime-status matrix, the readiness correction
// (metadata alone never makes a target ready), and admin-gated upsert/delete
// with audit recording.
package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// --- test fakes ---

// fakeCredentialStore implements QueryCredentialMetadataStore in memory. It
// records upsert/delete/audit calls so tests can assert side effects.
type fakeCredentialStore struct {
	metadata  map[uint64]model.QueryCredentialMetadata
	upserts   []model.QueryCredentialMetadata
	deletes   []uint64
	audits    []credentialAuditCall
	upsertErr error
	deleteErr error
	auditErr  error
	getErr    error
}

type credentialAuditCall struct {
	actor  uint64
	target uint64
	etype  string
	result string
}

func newFakeCredentialStore() *fakeCredentialStore {
	return &fakeCredentialStore{metadata: map[uint64]model.QueryCredentialMetadata{}}
}

func (f *fakeCredentialStore) GetCredentialByResourceID(_ context.Context, rid uint64) (model.QueryCredentialMetadata, error) {
	m, ok := f.metadata[rid]
	if f.getErr != nil {
		// Mirror the real repository: return the scanned row alongside the error
		// (e.g. an invalid-ref sentinel) so callers that classify by errors.Is can
		// still report configured=true. Callers must check the error first.
		return m, f.getErr
	}
	if !ok {
		return model.QueryCredentialMetadata{}, sql.ErrNoRows
	}
	return m, nil
}

func (f *fakeCredentialStore) UpsertCredentialMetadataWithAudit(_ context.Context, m model.QueryCredentialMetadata, actor uint64, etype, result string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	if f.auditErr != nil {
		// Atomic: a failed audit write rolls back the metadata change — no row
		// committed, no audit recorded (mirrors the repository transaction).
		return f.auditErr
	}
	f.upserts = append(f.upserts, m)
	f.metadata[m.ResourceID] = m
	f.audits = append(f.audits, credentialAuditCall{actor, m.ResourceID, etype, result})
	return nil
}

func (f *fakeCredentialStore) DeleteCredentialMetadataWithAudit(_ context.Context, rid, actor uint64, etype, result string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if f.auditErr != nil {
		// Atomic: a failed audit write rolls back the delete — original metadata
		// stays, no audit recorded (mirrors the repository transaction).
		return f.auditErr
	}
	f.deletes = append(f.deletes, rid)
	delete(f.metadata, rid)
	f.audits = append(f.audits, credentialAuditCall{actor, rid, etype, result})
	return nil
}

// --- test builders ---

const credentialTargetID uint64 = 9001

// credentialTarget builds a complete mysql query target with a configurable
// connection so individual runtime cases can mutate host/port/environment/engine.
func credentialTarget(engine, host string, port int, env string) model.QueryTarget {
	return model.QueryTarget{
		ResourceID: credentialTargetID,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: env,
			Engine:      engine,
			Host:        host,
			Port:        port,
		},
	}
}

func credentialMeta(ref string, enabled bool, policy model.QueryEnvironmentPolicy) model.QueryCredentialMetadata {
	return model.QueryCredentialMetadata{
		ResourceID:        credentialTargetID,
		Engine:            "mysql",
		CredentialRef:     ref,
		Enabled:           enabled,
		EnvironmentPolicy: policy,
	}
}

// metaPtr returns a pointer to freshly-built metadata so table cases can hand
// the inspector a *model.QueryCredentialMetadata without taking the address of
// a function-call result (which Go disallows).
func metaPtr(ref string, enabled bool, policy model.QueryEnvironmentPolicy) *model.QueryCredentialMetadata {
	m := credentialMeta(ref, enabled, policy)
	return &m
}

func adminActor() AuthenticatedUser  { return AuthenticatedUser{ID: 7, Role: "admin"} }
func viewerActor() AuthenticatedUser { return AuthenticatedUser{ID: 8, Role: "viewer"} }

// TestInspectCredentialRuntime_StatusMatrix proves the runtime status covers the
// full policy/secret/binding matrix and that the resolver is never called for an
// invalid ref or for any status decided before resolution. WHY: the resolver
// must never perform an env lookup with an unvalidated key, and only
// secret_resolved is eligible.
func TestInspectCredentialRuntime_StatusMatrix(t *testing.T) {
	ctx := context.Background()
	mysqlOK := credentialTarget("mysql", "db.internal", 3306, "staging")

	cases := []struct {
		name             string
		target           model.QueryTarget
		cred             *model.QueryCredentialMetadata
		resolver         *fakeResolver
		want             model.QueryCredentialRuntimeStatus
		wantResolverCall bool
	}{
		{
			name:             "missing metadata",
			target:           mysqlOK,
			cred:             nil,
			resolver:         &fakeResolver{},
			want:             model.QueryCredentialRuntimeMissingMetadata,
			wantResolverCall: false,
		},
		{
			name:             "invalid ref fails closed before resolver",
			target:           mysqlOK,
			cred:             metaPtr("bad-ref!", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{},
			want:             model.QueryCredentialRuntimeInvalidRef,
			wantResolverCall: false,
		},
		{
			name:             "disabled",
			target:           mysqlOK,
			cred:             metaPtr("ORDER_MYSQL_RO", false, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{},
			want:             model.QueryCredentialRuntimeDisabled,
			wantResolverCall: false,
		},
		{
			name:             "policy blocked production with non_prod_only",
			target:           credentialTarget("mysql", "db.internal", 3306, "production"),
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{},
			want:             model.QueryCredentialRuntimePolicyBlocked,
			wantResolverCall: false,
		},
		{
			name:             "secret missing",
			target:           mysqlOK,
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{err: errors.New("unset env")},
			want:             model.QueryCredentialRuntimeSecretMissing,
			wantResolverCall: true,
		},
		{
			name:             "host mismatch",
			target:           credentialTarget("mysql", "other.internal", 3306, "staging"),
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{dsn: testResolverDSN},
			want:             model.QueryCredentialRuntimeBindingMismatch,
			wantResolverCall: true,
		},
		{
			name:             "port mismatch",
			target:           credentialTarget("mysql", "db.internal", 3307, "staging"),
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{dsn: testResolverDSN},
			want:             model.QueryCredentialRuntimeBindingMismatch,
			wantResolverCall: true,
		},
		{
			name:             "resolved and eligible",
			target:           mysqlOK,
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{dsn: testResolverDSN},
			want:             model.QueryCredentialRuntimeSecretResolved,
			wantResolverCall: true,
		},
		{
			name:             "unsupported engine",
			target:           credentialTarget("postgresql", "db.internal", 5432, "staging"),
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyAllEnvironments),
			resolver:         &fakeResolver{},
			want:             model.QueryCredentialRuntimeUnsupportedTarget,
			wantResolverCall: false,
		},
		{
			name:             "incomplete connection",
			target:           credentialTarget("mysql", "", 0, "staging"),
			cred:             metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly),
			resolver:         &fakeResolver{},
			want:             model.QueryCredentialRuntimeIncompleteConnection,
			wantResolverCall: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.resolver.called = false
			got := InspectCredentialRuntime(ctx, tc.resolver, tc.target, tc.cred)
			if got != tc.want {
				t.Fatalf("runtime = %q, want %q", got, tc.want)
			}
			if tc.resolver.called != tc.wantResolverCall {
				t.Fatalf("resolver called = %v, want %v (invalid ref must not reach the resolver)", tc.resolver.called, tc.wantResolverCall)
			}
			if got.IsResolved() != tc.want.IsResolved() {
				t.Fatalf("IsResolved = %v, want %v", got.IsResolved(), tc.want.IsResolved())
			}
		})
	}
}

// TestInspectCredentialRuntime_NeverReturnsDSN proves the returned status carries
// no DSN fragment even when the resolver resolved a DSN with a password. WHY:
// the runtime status is the only credential signal surfaced to clients; it must
// never leak the resolved DSN.
func TestInspectCredentialRuntime_NeverReturnsDSN(t *testing.T) {
	ctx := context.Background()
	status := InspectCredentialRuntime(ctx, &fakeResolver{dsn: testResolverDSN},
		credentialTarget("mysql", "db.internal", 3306, "staging"),
		metaPtr("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly))
	if status != model.QueryCredentialRuntimeSecretResolved {
		t.Fatalf("runtime = %q, want secret_resolved", status)
	}
}

// TestQueryCredentialService_GetStatus_MissingMetadata proves GET returns a
// stable not-configured object with missing_metadata and not eligible when no
// metadata row exists.
func TestQueryCredentialService_GetStatus_MissingMetadata(t *testing.T) {
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		newFakeCredentialStore(),
		&fakeResolver{},
	)
	resp, err := svc.GetStatus(context.Background(), credentialTargetID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if resp.Configured || resp.ExecutionEligible {
		t.Fatalf("missing metadata must not be configured/eligible: %+v", resp)
	}
	if resp.RuntimeStatus != model.QueryCredentialRuntimeMissingMetadata {
		t.Fatalf("runtime = %q, want missing_metadata", resp.RuntimeStatus)
	}
	if resp.EnvironmentPolicy != model.QueryEnvPolicyDisabled {
		t.Fatalf("default policy = %q, want disabled", resp.EnvironmentPolicy)
	}
}

// TestQueryCredentialService_Upsert_WritesMetadataAndAudit proves an admin upsert
// persists metadata, records a query.credential.updated audit event, derives the
// engine from the target (never the request), and returns the post-save status.
func TestQueryCredentialService_Upsert_WritesMetadataAndAudit(t *testing.T) {
	store := newFakeCredentialStore()
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{err: errors.New("not provisioned yet")}, // secret unresolved on purpose
	)
	resp, err := svc.Upsert(context.Background(), adminActor(), credentialTargetID, model.QueryCredentialUpsertRequest{
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// Save succeeds even when the secret is not resolvable yet, but the target
	// stays locked with secret_missing.
	if !resp.Configured {
		t.Fatalf("configured = false, want true after upsert")
	}
	if resp.RuntimeStatus != model.QueryCredentialRuntimeSecretMissing {
		t.Fatalf("runtime = %q, want secret_missing", resp.RuntimeStatus)
	}
	if resp.ExecutionEligible {
		t.Fatal("an unresolved credential must not be execution eligible")
	}
	if resp.Engine != "mysql" {
		t.Fatalf("engine = %q, want mysql derived from target", resp.Engine)
	}
	if len(store.upserts) != 1 || store.upserts[0].CredentialRef != "ORDER_MYSQL_RO" {
		t.Fatalf("upserts = %+v, want one ORDER_MYSQL_RO row", store.upserts)
	}
	wantAudit := credentialAuditCall{actor: 7, target: credentialTargetID, etype: "query.credential.updated", result: "success"}
	if len(store.audits) != 1 || store.audits[0] != wantAudit {
		t.Fatalf("audits = %+v, want %+v", store.audits, wantAudit)
	}
}

// TestQueryCredentialService_Upsert_AllEnvironmentsRequiresConfirmation proves the
// service enforces the model's all-environments confirmation rule.
func TestQueryCredentialService_Upsert_AllEnvironmentsRequiresConfirmation(t *testing.T) {
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		newFakeCredentialStore(),
		&fakeResolver{},
	)
	if _, err := svc.Upsert(context.Background(), adminActor(), credentialTargetID, model.QueryCredentialUpsertRequest{
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: model.QueryEnvPolicyAllEnvironments,
	}); err == nil {
		t.Fatal("all_environments without confirmation must be rejected")
	}
}

// TestQueryCredentialService_Upsert_NonAdminForbidden proves a non-admin actor
// cannot write credential metadata and that no metadata or audit row is written.
func TestQueryCredentialService_Upsert_NonAdminForbidden(t *testing.T) {
	store := newFakeCredentialStore()
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	if _, err := svc.Upsert(context.Background(), viewerActor(), credentialTargetID, model.QueryCredentialUpsertRequest{
		CredentialRef: "ORDER_MYSQL_RO", Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); !errors.Is(err, ErrQueryCredentialForbidden) {
		t.Fatalf("non-admin upsert err = %v, want ErrQueryCredentialForbidden", err)
	}
	if len(store.upserts) != 0 || len(store.audits) != 0 {
		t.Fatalf("non-admin upsert must not write metadata/audit: %+v", store)
	}
}

// TestQueryCredentialService_Delete_WritesAuditAndRemovesMetadata proves an admin
// delete removes metadata and records a query.credential.deleted audit event.
func TestQueryCredentialService_Delete_WritesAuditAndRemovesMetadata(t *testing.T) {
	store := newFakeCredentialStore()
	store.metadata[credentialTargetID] = credentialMeta("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly)
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	if err := svc.Delete(context.Background(), adminActor(), credentialTargetID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(store.deletes) != 1 || store.deletes[0] != credentialTargetID {
		t.Fatalf("deletes = %+v, want one delete of the target", store.deletes)
	}
	if _, ok := store.metadata[credentialTargetID]; ok {
		t.Fatal("metadata must be removed after delete")
	}
	wantAudit := credentialAuditCall{actor: 7, target: credentialTargetID, etype: "query.credential.deleted", result: "success"}
	if len(store.audits) != 1 || store.audits[0] != wantAudit {
		t.Fatalf("audits = %+v, want %+v", store.audits, wantAudit)
	}
}

// TestQueryCredentialService_Delete_NonAdminForbidden proves a non-admin cannot
// delete credential metadata and no audit row is written.
func TestQueryCredentialService_Delete_NonAdminForbidden(t *testing.T) {
	store := newFakeCredentialStore()
	store.metadata[credentialTargetID] = credentialMeta("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly)
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	if err := svc.Delete(context.Background(), viewerActor(), credentialTargetID); !errors.Is(err, ErrQueryCredentialForbidden) {
		t.Fatalf("non-admin delete err = %v, want ErrQueryCredentialForbidden", err)
	}
	if len(store.deletes) != 0 || len(store.audits) != 0 {
		t.Fatalf("non-admin delete must not write delete/audit: %+v", store)
	}
}

// TestQueryCredentialService_TargetNotFound proves a missing target maps to the
// shared not-found error for both read and write paths.
func TestQueryCredentialService_TargetNotFound(t *testing.T) {
	svc := NewQueryCredentialService(fakeTargetRepo{}, newFakeCredentialStore(), &fakeResolver{})
	if _, err := svc.GetStatus(context.Background(), credentialTargetID); !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("get status err = %v, want ErrQueryTargetNotFound", err)
	}
	if _, err := svc.Upsert(context.Background(), adminActor(), credentialTargetID, model.QueryCredentialUpsertRequest{
		CredentialRef: "ORDER_MYSQL_RO", Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("upsert err = %v, want ErrQueryTargetNotFound", err)
	}
}

// TestQueryCredentialService_GetStatus_InvalidStoredRef_ReturnsInvalidRefNotMissingMetadata
// proves a stored row whose credential_ref is invalid surfaces as runtime
// invalid_ref — NOT missing_metadata. WHY: swallowing the repository's
// fail-closed read error (as the old readCredential did) masqueraded corrupt
// metadata as "not configured", hiding a configured-but-broken target. A row
// exists, so configured=true; the raw invalid ref is suppressed (it failed
// validation and could be DSN-shaped if it bypassed the write path). The
// resolver is never consulted for an invalid row.
func TestQueryCredentialService_GetStatus_InvalidStoredRef_ReturnsInvalidRefNotMissingMetadata(t *testing.T) {
	store := newFakeCredentialStore()
	store.metadata[credentialTargetID] = credentialMeta("bad-ref!", true, model.QueryEnvPolicyNonProdOnly)
	store.getErr = model.ErrInvalidCredentialMetadata
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	resp, err := svc.GetStatus(context.Background(), credentialTargetID)
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if resp.RuntimeStatus != model.QueryCredentialRuntimeInvalidRef {
		t.Fatalf("runtime = %q, want invalid_ref (must not degrade to missing_metadata)", resp.RuntimeStatus)
	}
	if resp.RuntimeStatus == model.QueryCredentialRuntimeMissingMetadata {
		t.Fatal("invalid stored metadata must NOT be reported as missing_metadata")
	}
	if !resp.Configured {
		t.Fatal("a row exists, so configured must be true")
	}
	if resp.ExecutionEligible {
		t.Fatal("an invalid credential must never be execution eligible")
	}
	if resp.CredentialRef != "" {
		t.Fatalf("the raw invalid ref must be suppressed (could be DSN-shaped), got %q", resp.CredentialRef)
	}
}

// TestQueryCredentialService_GetStatus_UnexpectedCredentialReadError_ReturnsBackendError
// proves an unexpected DB/read error is propagated as a backend error and is
// NEVER masked as a missing_metadata success. WHY: silently degrading a
// transient/corrupt read to "not configured" hides a real backend failure behind
// a misleading status; the API must fail loud (the handler maps it to 500).
func TestQueryCredentialService_GetStatus_UnexpectedCredentialReadError_ReturnsBackendError(t *testing.T) {
	store := newFakeCredentialStore()
	store.getErr = errors.New("db connection lost")
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	resp, err := svc.GetStatus(context.Background(), credentialTargetID)
	if err == nil {
		t.Fatalf("unexpected read error must propagate as a backend error, got response %+v", resp)
	}
	if resp.RuntimeStatus == model.QueryCredentialRuntimeMissingMetadata {
		t.Fatal("an unexpected read error must not be represented as missing_metadata")
	}
}

// TestQueryCredentialService_Upsert_AuditFailureLeavesNoMetadata proves a failed
// audit write rolls back the metadata upsert: "configured but no audit" is
// forbidden. WHY: metadata changing without an audit row breaks the
// "every successful change is audited" guarantee and leaves an unattributed
// configuration change; the store must treat metadata+audit as one atomic op.
func TestQueryCredentialService_Upsert_AuditFailureLeavesNoMetadata(t *testing.T) {
	store := newFakeCredentialStore()
	store.auditErr = errors.New("audit insert failed")
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	if _, err := svc.Upsert(context.Background(), adminActor(), credentialTargetID, model.QueryCredentialUpsertRequest{
		CredentialRef: "ORDER_MYSQL_RO", Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err == nil {
		t.Fatal("audit failure must surface as an error from Upsert")
	}
	if _, ok := store.metadata[credentialTargetID]; ok {
		t.Fatal("audit failure must not leave committed credential metadata (upsert+audit must be atomic)")
	}
}

// TestQueryCredentialService_Delete_AuditFailureKeepsMetadata proves a failed
// audit write rolls back the metadata delete: the original metadata must remain.
// WHY: deleting metadata without an audit row would silently remove an
// attributed configuration change; delete+audit must be one atomic op.
func TestQueryCredentialService_Delete_AuditFailureKeepsMetadata(t *testing.T) {
	store := newFakeCredentialStore()
	store.metadata[credentialTargetID] = credentialMeta("ORDER_MYSQL_RO", true, model.QueryEnvPolicyNonProdOnly)
	store.auditErr = errors.New("audit insert failed")
	svc := NewQueryCredentialService(
		fakeTargetRepo{targets: []model.QueryTarget{credentialTarget("mysql", "db.internal", 3306, "staging")}},
		store,
		&fakeResolver{},
	)
	if err := svc.Delete(context.Background(), adminActor(), credentialTargetID); err == nil {
		t.Fatal("audit failure must surface as an error from Delete")
	}
	if _, ok := store.metadata[credentialTargetID]; !ok {
		t.Fatal("audit failure must not remove credential metadata (delete+audit must be atomic)")
	}
}
