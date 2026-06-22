// Package service provides the local/dev query credential metadata seeder.
// input: context, errors, fmt, internal/model
// output: QueryDevCredentialSeedConfig, DevCredentialWriter, QueryDevCredentialSeeder, NewQueryDevCredentialSeeder, Seed, seed sentinel errors
// pos: Local/dev-only credential METADATA seed path — validates config, binds the env DSN to the selected target, and upserts metadata only (the DSN is never stored, logged, or returned)
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors for the dev seed path. They are fixed strings and never carry
// the DSN, the credential password, or any env value.
var (
	errSeedMissingTargetResourceID         = errors.New("dev seed requires a target resource id")
	errSeedInvalidCredentialRef            = errors.New("dev seed credential_ref is invalid")
	errSeedInvalidEnvironmentPolicy        = errors.New("dev seed environment policy is invalid")
	errSeedAllEnvironmentsRequiresOverride = errors.New("dev seed all_environments requires an explicit override")
	errSeedTargetNotFound                  = errors.New("dev seed target not found")
	errSeedUnsupportedEngine               = errors.New("dev seed target engine is not supported for execution")
	errSeedIncompleteConnection            = errors.New("dev seed target connection is incomplete")
	errSeedMissingResolvedDSN              = errors.New("dev seed credential resolved to no DSN")
	errSeedCredentialNotBound              = errors.New("dev seed credential is not bound to the target")
)

// QueryDevCredentialSeedConfig is the validated input to the local/dev seed
// path. It carries identity and policy only — no DSN. The DSN is read from the
// environment by the credential resolver at seed time.
type QueryDevCredentialSeedConfig struct {
	TargetResourceID     uint64
	CredentialRef        string
	EnvironmentPolicy    model.QueryEnvironmentPolicy
	AllowAllEnvironments bool
}

// DevCredentialWriter persists credential metadata for the local/dev seed path.
// It stores metadata only — never a DSN. The concrete MySQL repository
// (mysql.QueryExecutionRepository) satisfies it; the seed command wires that
// repository directly. Keeping this separate from the read-oriented
// QueryExecutionRepository interface means the dev write path does not widen the
// execute/read contract.
type DevCredentialWriter interface {
	UpsertCredentialMetadata(ctx context.Context, meta model.QueryCredentialMetadata) error
}

// QueryDevCredentialSeeder validates and seeds credential METADATA for one
// local/dev query target. It reuses the Phase 37 target lookup,
// executable-engine gate, env credential resolver, and DSN-binding check so the
// dev seed agrees with the execute path: a target the seed marks ready is one
// the execute path will actually run. The DSN is resolved from the environment
// and validated to bind to the selected target, but it is never stored, logged,
// or returned — only the opaque credential_ref and policy are persisted.
type QueryDevCredentialSeeder struct {
	targets     QueryTargetRepository
	credentials QueryCredentialResolver
	writer      DevCredentialWriter
}

// NewQueryDevCredentialSeeder wires the seeder with the query target read model,
// the env credential resolver, and the metadata writer. All three are existing
// Phase 37 contracts; the writer is the only new surface and it accepts
// metadata only.
func NewQueryDevCredentialSeeder(targets QueryTargetRepository, credentials QueryCredentialResolver, writer DevCredentialWriter) *QueryDevCredentialSeeder {
	return &QueryDevCredentialSeeder{targets: targets, credentials: credentials, writer: writer}
}

// Seed validates the config, resolves and binds the credential to the target,
// and upserts metadata only. It returns the persisted metadata (which never
// contains a DSN) or a sentinel error. Every rejection happens before the
// writer is called, so a failed seed leaves the table untouched.
func (s *QueryDevCredentialSeeder) Seed(ctx context.Context, cfg QueryDevCredentialSeedConfig) (model.QueryCredentialMetadata, error) {
	if err := cfg.validate(); err != nil {
		return model.QueryCredentialMetadata{}, err
	}

	target, err := s.findTarget(ctx, cfg.TargetResourceID)
	if err != nil {
		return model.QueryCredentialMetadata{}, err
	}
	if !isExecutableEngine(target.ConnectionContext.Engine) {
		return model.QueryCredentialMetadata{}, errSeedUnsupportedEngine
	}
	if target.ConnectionContext.Host == "" || target.ConnectionContext.Port == 0 {
		return model.QueryCredentialMetadata{}, errSeedIncompleteConnection
	}

	// Resolve the DSN from the environment. The resolver validates the ref first
	// (fail closed). A missing/unset credential is fail-closed; the DSN never
	// appears in the error.
	dsn, err := s.credentials.Resolve(ctx, cfg.CredentialRef)
	if err != nil || dsn == "" {
		return model.QueryCredentialMetadata{}, errSeedMissingResolvedDSN
	}
	// Defense in depth: the resolved DSN must point at the selected target's
	// host/port. Reuses the Phase 37 binding check verbatim so the seed and
	// execute paths agree on what "bound" means. The binding error never echoes
	// the DSN, and it is discarded here in favor of the fixed seed sentinel.
	if err := validateDSNBinding(dsn, target); err != nil {
		return model.QueryCredentialMetadata{}, errSeedCredentialNotBound
	}

	meta := model.QueryCredentialMetadata{
		ResourceID:        target.ResourceID,
		Engine:            target.ConnectionContext.Engine,
		CredentialRef:     cfg.CredentialRef,
		Enabled:           true,
		EnvironmentPolicy: cfg.EnvironmentPolicy,
	}
	if err := s.writer.UpsertCredentialMetadata(ctx, meta); err != nil {
		return model.QueryCredentialMetadata{}, fmt.Errorf("upsert dev credential metadata: %w", err)
	}
	return meta, nil
}

// validate checks the seed config in isolation. It touches neither the target
// read model nor the environment, so failures here are pure and can never leak
// a DSN. The all_environments policy requires an explicit override flag because
// it opens production execution.
func (c QueryDevCredentialSeedConfig) validate() error {
	if c.TargetResourceID == 0 {
		return errSeedMissingTargetResourceID
	}
	if err := model.ValidateCredentialRef(c.CredentialRef); err != nil {
		return fmt.Errorf("%w: %v", errSeedInvalidCredentialRef, err)
	}
	if err := c.EnvironmentPolicy.Validate(); err != nil {
		return fmt.Errorf("%w: %v", errSeedInvalidEnvironmentPolicy, err)
	}
	if c.EnvironmentPolicy == model.QueryEnvPolicyAllEnvironments && !c.AllowAllEnvironments {
		return errSeedAllEnvironmentsRequiresOverride
	}
	return nil
}

// findTarget locates a single query target by id, mirroring the execute path.
// The read model exposes no single-target getter, so this scans the (small)
// target list. A lookup or read error is treated as not-found so the seed fails
// closed rather than writing metadata against an unresolved target.
func (s *QueryDevCredentialSeeder) findTarget(ctx context.Context, targetID uint64) (model.QueryTarget, error) {
	targets, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{})
	if err != nil {
		return model.QueryTarget{}, errSeedTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetID {
			return t, nil
		}
	}
	return model.QueryTarget{}, errSeedTargetNotFound
}
