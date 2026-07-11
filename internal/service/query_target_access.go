// Package service provides the shared governed target access resolver for query
// execution and credential introspection. It centralises the target lookup,
// engine check, credential validation, policy enforcement, secret resolution,
// and DSN binding validation so Execute and InspectCredentialRuntime can never
// drift on governance rules.
// input: context, database/sql, errors, internal/model
// output: BoundTargetAccess, TargetAccessError, TargetAccessResolver, NewTargetAccessResolver
// pos: Shared governed target access resolution — the single path from target ID to bound DSN
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fan/controlhub/internal/model"
)

// BoundTargetAccess is the result of a successful target access resolution. It
// contains the resolved target, credential metadata, and the bound DSN. The DSN
// is unexported and never appears in logs, responses, or errors — it is only
// accessible within the service package for passing to the executor.
type BoundTargetAccess struct {
	Target     model.QueryTarget
	Credential model.QueryCredentialMetadata
	dsn        string // private to service package; never exported
}

// TargetAccessError is returned when the target exists but access is denied. It
// carries the QueryCredentialRuntimeStatus so InspectCredentialRuntime can map
// it directly, and a fixed, leak-free message so Execute can surface it as a
// controlled ErrQueryNotAllowed. The message never echoes the DSN, host, port,
// or password.
type TargetAccessError struct {
	Status  model.QueryCredentialRuntimeStatus
	message string
}

// Error returns the fixed, client-safe message. It never contains the DSN.
func (e *TargetAccessError) Error() string { return e.message }

// TargetAccessResolver resolves governed target access by performing the full
// governance chain: target lookup, engine check, connection metadata check,
// credential validation, policy enforcement, secret resolution, and DSN binding
// validation. It is shared by Execute and InspectCredentialRuntime so the two
// paths can never drift on governance rules.
type TargetAccessResolver struct {
	targets     QueryTargetRepository
	credentials QueryCredentialReader
	resolver    QueryCredentialResolver
}

// NewTargetAccessResolver wires the resolver with its dependencies. The
// credentials parameter is a QueryCredentialReader (the narrow interface for
// reading credential metadata); the resolver parameter resolves a credential
// ref to a DSN.
func NewTargetAccessResolver(
	targets QueryTargetRepository,
	credentials QueryCredentialReader,
	resolver QueryCredentialResolver,
) *TargetAccessResolver {
	return &TargetAccessResolver{
		targets:     targets,
		credentials: credentials,
		resolver:    resolver,
	}
}

// Resolve performs the full governed target access resolution. On success it
// returns a BoundTargetAccess containing the target, credential metadata, and
// resolved DSN. On failure it returns one of:
//   - ErrQueryTargetNotFound when the target does not exist (no target to
//     attribute history to)
//   - *TargetAccessError for all credential/DSN governance failures (the
//     .Target field is populated so callers can record a rejected history row)
//
// The DSN never appears in any error, log, or response.
func (r *TargetAccessResolver) Resolve(ctx context.Context, actorID uint64, targetID uint64) (BoundTargetAccess, error) {
	// 1. Target lookup. A missing target is a distinct signal — callers must
	//    not record a history row for an unknown target.
	target, err := r.findTarget(ctx, targetID)
	if err != nil {
		return BoundTargetAccess{}, ErrQueryTargetNotFound
	}

	// 2. Engine check. Only MySQL/TiDB are supported for read-only execution.
	engine := target.ConnectionContext.Engine
	if !isExecutableEngine(engine) {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeUnsupportedTarget,
			message: "engine is not supported for read-only execution",
		}
	}

	// 3. Connection metadata check. A target with incomplete host/port cannot
	//    be bound to any credential.
	if target.ConnectionContext.Host == "" || target.ConnectionContext.Port == 0 {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeIncompleteConnection,
			message: "target connection metadata is incomplete",
		}
	}

	// 4. Credential metadata check. A missing row, invalid ref, disabled
	//    credential, or policy block all fail closed with the same controlled
	//    rejection message.
	cred, err := r.credentials.GetCredentialByResourceID(ctx, targetID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return BoundTargetAccess{Target: target}, &TargetAccessError{
				Status:  model.QueryCredentialRuntimeMissingMetadata,
				message: "target is not enabled for execution",
			}
		}
		// Invalid metadata or unexpected read error — fail closed.
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeInvalidRef,
			message: "target is not enabled for execution",
		}
	}

	// 5. Credential ref validation. An invalid ref must never reach the
	//    resolver (env lookup) — fail closed before any secret resolution.
	if err := model.ValidateCredentialRef(cred.CredentialRef); err != nil {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeInvalidRef,
			message: "target is not enabled for execution",
		}
	}

	// 6. Enabled check.
	if !cred.Enabled {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeDisabled,
			message: "target is not enabled for execution",
		}
	}

	// 7. Policy enforcement. credentialAllowsExecution applies the full
	//    engine-match + environment-policy matrix.
	if !credentialAllowsExecution(cred, engine, target.ConnectionContext.Environment) {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimePolicyBlocked,
			message: "target is not enabled for execution",
		}
	}

	// 8. Secret resolution. The resolver validates the ref (fail closed) and
	//    reads the env key. The DSN is never included in errors.
	dsn, err := r.resolver.Resolve(ctx, cred.CredentialRef)
	if err != nil || dsn == "" {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeSecretMissing,
			message: "credential could not be resolved",
		}
	}

	// 9. DSN binding validation. The resolved DSN must point at the selected
	//    target's host/port — a credential misconfigured to another database
	//    is a fail-closed condition.
	if err := validateDSNBinding(dsn, target); err != nil {
		return BoundTargetAccess{Target: target}, &TargetAccessError{
			Status:  model.QueryCredentialRuntimeBindingMismatch,
			message: "credential is not bound to this target",
		}
	}

	return BoundTargetAccess{
		Target:     target,
		Credential: cred,
		dsn:        dsn,
	}, nil
}

// findTarget locates a single query target by id. A read error or missing
// target maps to ErrQueryTargetNotFound so callers fail closed.
func (r *TargetAccessResolver) findTarget(ctx context.Context, targetID uint64) (model.QueryTarget, error) {
	targets, _, err := r.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetID})
	if err != nil {
		return model.QueryTarget{}, ErrQueryTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetID {
			return t, nil
		}
	}
	return model.QueryTarget{}, ErrQueryTargetNotFound
}
