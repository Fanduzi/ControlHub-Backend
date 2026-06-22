// Package service provides tests for the local/dev query credential seeder.
// input: context, errors, strings, testing, internal/model
// output: TestQueryDevSeed_* (fake writer + reused fakeTargetRepo/fakeResolver fixtures)
// pos: Unit tests for the dev seed config validation, target/DSN binding, and metadata shape
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// fakeCredentialWriter records the metadata handed to UpsertCredentialMetadata
// and optionally injects a failure. It stores ONLY metadata — there is no DSN
// field to populate, which is exactly the invariant the seed path must keep.
type fakeCredentialWriter struct {
	received model.QueryCredentialMetadata
	called   bool
	err      error
}

func (f *fakeCredentialWriter) UpsertCredentialMetadata(_ context.Context, meta model.QueryCredentialMetadata) error {
	f.called = true
	f.received = meta
	if f.err != nil {
		return f.err
	}
	return nil
}

func newDevSeeder(targets fakeTargetRepo, resolver *fakeResolver, writer *fakeCredentialWriter) *QueryDevCredentialSeeder {
	return NewQueryDevCredentialSeeder(targets, resolver, writer)
}

// validDevSeedConfig is the happy-path config used as the base for mutation in
// each test. It targets a mysql instance at db.internal:3306 (matches
// testResolverDSN) under the default non_prod_only policy.
func validDevSeedConfig() QueryDevCredentialSeedConfig {
	return QueryDevCredentialSeedConfig{
		TargetResourceID:  9001,
		CredentialRef:     "LOCAL_QUERY_RO",
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
}

// TestQueryDevSeed_RejectsMissingTargetResourceID fails closed when no target is
// named. WHY: a seed without a target would upsert metadata against resource 0,
// which can never become a real query target.
func TestQueryDevSeed_RejectsMissingTargetResourceID(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("staging")}}, &fakeResolver{dsn: testResolverDSN}, writer)

	cfg := validDevSeedConfig()
	cfg.TargetResourceID = 0
	_, err := s.Seed(context.Background(), cfg)
	if !errors.Is(err, errSeedMissingTargetResourceID) {
		t.Fatalf("error = %v, want errSeedMissingTargetResourceID", err)
	}
	if writer.called {
		t.Fatal("writer must not be called when target id is missing")
	}
}

// TestQueryDevSeed_RejectsInvalidCredentialRef fails closed on a malformed ref
// and never reaches the resolver (no env lookup with an unvalidated key).
func TestQueryDevSeed_RejectsInvalidCredentialRef(t *testing.T) {
	resolver := &fakeResolver{dsn: testResolverDSN}
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("staging")}}, resolver, writer)

	cfg := validDevSeedConfig()
	cfg.CredentialRef = "bad-ref"
	_, err := s.Seed(context.Background(), cfg)
	if !errors.Is(err, errSeedInvalidCredentialRef) {
		t.Fatalf("error = %v, want errSeedInvalidCredentialRef", err)
	}
	if resolver.called {
		t.Fatal("resolver must not be called for an invalid credential_ref (fail closed before env lookup)")
	}
	if writer.called {
		t.Fatal("writer must not be called for an invalid credential_ref")
	}
}

// TestQueryDevSeed_RejectsUnsupportedEngine fails closed when the target engine
// is known but not mysql/tidb (e.g. postgres). WHY: Phase 37 executes only
// mysql/tidb; seeding a credential for another engine would imply a readiness
// the backend cannot honor.
func TestQueryDevSeed_RejectsUnsupportedEngine(t *testing.T) {
	writer := &fakeCredentialWriter{}
	postgresTarget := mysqlTarget("staging")
	postgresTarget.ConnectionContext.Engine = "postgresql"
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{postgresTarget}}, &fakeResolver{dsn: testResolverDSN}, writer)

	_, err := s.Seed(context.Background(), validDevSeedConfig())
	if !errors.Is(err, errSeedUnsupportedEngine) {
		t.Fatalf("error = %v, want errSeedUnsupportedEngine", err)
	}
	if writer.called {
		t.Fatal("writer must not be called for an unsupported engine")
	}
}

// TestQueryDevSeed_RejectsIncompleteConnection fails closed when host or port is
// missing. WHY: the DSN binding check requires an explicit host:port to compare
// against; an incomplete connection cannot be safely bound.
func TestQueryDevSeed_RejectsIncompleteConnection(t *testing.T) {
	writer := &fakeCredentialWriter{}
	incomplete := mysqlTarget("staging")
	incomplete.ConnectionContext.Host = ""
	incomplete.ConnectionContext.Port = 0
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{incomplete}}, &fakeResolver{dsn: testResolverDSN}, writer)

	_, err := s.Seed(context.Background(), validDevSeedConfig())
	if !errors.Is(err, errSeedIncompleteConnection) {
		t.Fatalf("error = %v, want errSeedIncompleteConnection", err)
	}
	if writer.called {
		t.Fatal("writer must not be called for an incomplete connection")
	}
}

// TestQueryDevSeed_RejectsMissingResolvedDSN fails closed when the credential
// ref resolves to no DSN (unset env). WHY: a metadata row with no resolvable
// credential must never be written — it would mark a target ready that cannot
// execute.
func TestQueryDevSeed_RejectsMissingResolvedDSN(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("staging")}}, &fakeResolver{dsn: ""}, writer)

	_, err := s.Seed(context.Background(), validDevSeedConfig())
	if !errors.Is(err, errSeedMissingResolvedDSN) {
		t.Fatalf("error = %v, want errSeedMissingResolvedDSN", err)
	}
	if writer.called {
		t.Fatal("writer must not be called when the resolved DSN is missing")
	}
}

// TestQueryDevSeed_RejectsAllEnvironmentsUnlessExplicitlyAllowed enforces the
// override gate: all_environments is dangerous (opens production) so it must be
// rejected unless the operator passed an explicit AllowAllEnvironments flag.
func TestQueryDevSeed_RejectsAllEnvironmentsUnlessExplicitlyAllowed(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("production")}}, &fakeResolver{dsn: testResolverDSN}, writer)

	cfg := validDevSeedConfig()
	cfg.EnvironmentPolicy = model.QueryEnvPolicyAllEnvironments
	cfg.AllowAllEnvironments = false
	if _, err := s.Seed(context.Background(), cfg); !errors.Is(err, errSeedAllEnvironmentsRequiresOverride) {
		t.Fatalf("error = %v, want errSeedAllEnvironmentsRequiresOverride", err)
	}
	if writer.called {
		t.Fatal("writer must not be called when all_environments lacks the explicit override")
	}
}

// TestQueryDevSeed_AllEnvironmentsAllowedWithOverride confirms the override flag
// opens the all_environments policy and writes the metadata.
func TestQueryDevSeed_AllEnvironmentsAllowedWithOverride(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("production")}}, &fakeResolver{dsn: testResolverDSN}, writer)

	cfg := validDevSeedConfig()
	cfg.EnvironmentPolicy = model.QueryEnvPolicyAllEnvironments
	cfg.AllowAllEnvironments = true
	meta, err := s.Seed(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Seed error = %v, want nil", err)
	}
	if meta.EnvironmentPolicy != model.QueryEnvPolicyAllEnvironments {
		t.Fatalf("policy = %q, want all_environments", meta.EnvironmentPolicy)
	}
	if !writer.called {
		t.Fatal("writer must be called when the override is present")
	}
}

// TestQueryDevSeed_BuildsCredentialMetadataForNonProdOnly is the happy path: a
// non-production mysql target whose resolved DSN binds to its host/port gets a
// ready credential metadata row with enabled=true and the default policy.
func TestQueryDevSeed_BuildsCredentialMetadataForNonProdOnly(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("staging")}}, &fakeResolver{dsn: testResolverDSN}, writer)

	meta, err := s.Seed(context.Background(), validDevSeedConfig())
	if err != nil {
		t.Fatalf("Seed error = %v, want nil", err)
	}
	if !writer.called {
		t.Fatal("writer must be called on the happy path")
	}
	// WHY: the persisted metadata carries only safe identity/policy fields —
	// never a DSN, password, or env value.
	if got := writer.received; got.ResourceID != 9001 || got.Engine != "mysql" ||
		got.CredentialRef != "LOCAL_QUERY_RO" || !got.Enabled ||
		got.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly {
		t.Fatalf("upserted metadata = %+v, want {9001 mysql LOCAL_QUERY_RO enabled non_prod_only}", got)
	}
	if meta != writer.received {
		t.Fatalf("returned meta %+v != upserted meta %+v", meta, writer.received)
	}
}

// TestQueryDevSeed_RejectsWhenTargetNotFound fails closed when the named target
// id does not exist in the read model.
func TestQueryDevSeed_RejectsWhenTargetNotFound(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("staging")}}, &fakeResolver{dsn: testResolverDSN}, writer)

	cfg := validDevSeedConfig()
	cfg.TargetResourceID = 999999
	if _, err := s.Seed(context.Background(), cfg); !errors.Is(err, errSeedTargetNotFound) {
		t.Fatalf("error = %v, want errSeedTargetNotFound", err)
	}
	if writer.called {
		t.Fatal("writer must not be called when the target is not found")
	}
}

// TestQueryDevSeed_RejectsUnboundDSNWithoutLeakingIt fails closed when the
// resolved DSN points at a different host/port than the target, AND asserts the
// returned error never echoes the DSN (which contains the credential password).
// WHY: the binding check is defense-in-depth; its failure message must stay
// DSN-free so a misconfiguration cannot leak the secret through the seed path.
func TestQueryDevSeed_RejectsUnboundDSNWithoutLeakingIt(t *testing.T) {
	writer := &fakeCredentialWriter{}
	// Target host differs from the DSN host (db.internal), so binding fails.
	mismatched := mysqlTarget("staging")
	mismatched.ConnectionContext.Host = "other.internal"
	mismatched.ConnectionContext.Port = 3307
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mismatched}}, &fakeResolver{dsn: testResolverDSN}, writer)

	_, err := s.Seed(context.Background(), validDevSeedConfig())
	if !errors.Is(err, errSeedCredentialNotBound) {
		t.Fatalf("error = %v, want errSeedCredentialNotBound", err)
	}
	if writer.called {
		t.Fatal("writer must not be called when the DSN is not bound to the target")
	}
	if msg := err.Error(); strings.Contains(msg, "secret-dsn-do-not-leak") || strings.Contains(msg, "tcp(") {
		t.Fatalf("error leaks the DSN: %q", msg)
	}
}

// TestQueryDevSeed_RejectsUnknownEnvironmentPolicy fails closed on a policy that
// is not part of the typed enum, so a target can never be silently seeded as
// all_environments via a typo.
func TestQueryDevSeed_RejectsUnknownEnvironmentPolicy(t *testing.T) {
	writer := &fakeCredentialWriter{}
	s := newDevSeeder(fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("staging")}}, &fakeResolver{dsn: testResolverDSN}, writer)

	cfg := validDevSeedConfig()
	cfg.EnvironmentPolicy = model.QueryEnvironmentPolicy("prod_please")
	if _, err := s.Seed(context.Background(), cfg); !errors.Is(err, errSeedInvalidEnvironmentPolicy) {
		t.Fatalf("error = %v, want errSeedInvalidEnvironmentPolicy", err)
	}
	if writer.called {
		t.Fatal("writer must not be called for an unknown environment policy")
	}
}
