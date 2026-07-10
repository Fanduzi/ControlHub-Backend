// Package service provides the Phase 38A query credential metadata service.
// input: context, database/sql, errors, fmt, internal/model
// output: QueryCredentialMetadataStore interface, InspectCredentialRuntime, QueryCredentialService, NewQueryCredentialService, GetStatus/Upsert/Delete, ErrQueryCredential* sentinels
// pos: Phase 38A authenticated credential METADATA management — runtime status inspection (resolver + binding, never returning the DSN), admin-gated upsert/delete, and audit recording
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors for the credential metadata service. They map to controlled
// HTTP responses (403 / 400 / 404) and never carry a DSN, host, port, or secret.
var (
	ErrQueryCredentialForbidden  = errors.New("query credential operation requires admin role")
	ErrQueryCredentialValidation = errors.New("query credential validation failed")
)

// adminRole is the only role permitted to write or delete credential metadata.
const adminRole = "admin"

// isAdmin reports whether an authenticated actor may manage credential metadata.
func isAdmin(actor AuthenticatedUser) bool {
	return actor.Role == adminRole
}

// QueryCredentialMetadataStore is the data-access interface for credential
// metadata and audit. The concrete implementation is the query execution
// repository (mysql.QueryExecutionRepository), which stores metadata only —
// never a DSN. Defining it where it is used follows the service-layer interface
// convention used across ControlHub.
//
// The write/delete methods are ATOMIC "metadata change + audit" operations: the
// store (not the service) owns the transaction, so a failed audit write rolls
// back the metadata change. This keeps the "every successful change is audited"
// guarantee — there is never a configured row without its audit trail.
type QueryCredentialMetadataStore interface {
	GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error)
	UpsertCredentialMetadataWithAudit(ctx context.Context, meta model.QueryCredentialMetadata, actorUserID uint64, eventType, result string) error
	DeleteCredentialMetadataWithAudit(ctx context.Context, resourceID, actorUserID uint64, eventType, result string) error
}

// query credential audit event types and the success result vocabulary.
const (
	queryCredentialUpdatedEvent = "query.credential.updated"
	queryCredentialDeletedEvent = "query.credential.deleted"
	auditResultSuccess          = "success"
)

// InspectCredentialRuntime evaluates the runtime status of a target's credential
// binding WITHOUT ever storing or returning the DSN. It mirrors the execute
// path's checks (engine, connection, ref validity, enabled, policy, secret
// resolution, host/port binding) so GET /query-targets readiness agrees with
// execution. cred may be nil (no metadata row). The resolver is never called for
// a status decided before resolution (unsupported, incomplete, missing,
// invalid, disabled, policy_blocked).
func InspectCredentialRuntime(ctx context.Context, resolver QueryCredentialResolver, target model.QueryTarget, cred *model.QueryCredentialMetadata) model.QueryCredentialRuntimeStatus {
	engine := target.ConnectionContext.Engine
	if !isExecutableEngine(engine) {
		return model.QueryCredentialRuntimeUnsupportedTarget
	}
	if target.ConnectionContext.Host == "" || target.ConnectionContext.Port == 0 {
		return model.QueryCredentialRuntimeIncompleteConnection
	}
	if cred == nil {
		return model.QueryCredentialRuntimeMissingMetadata
	}
	if err := model.ValidateCredentialRef(cred.CredentialRef); err != nil {
		return model.QueryCredentialRuntimeInvalidRef
	}
	if !cred.Enabled {
		return model.QueryCredentialRuntimeDisabled
	}
	if !credentialAllowsExecution(*cred, engine, target.ConnectionContext.Environment) {
		return model.QueryCredentialRuntimePolicyBlocked
	}
	dsn, err := resolver.Resolve(ctx, cred.CredentialRef)
	if err != nil || dsn == "" {
		return model.QueryCredentialRuntimeSecretMissing
	}
	if err := validateDSNBinding(dsn, target); err != nil {
		return model.QueryCredentialRuntimeBindingMismatch
	}
	return model.QueryCredentialRuntimeSecretResolved
}

// QueryCredentialService manages credential metadata for MySQL/TiDB query
// targets. It inspects runtime status (never returning the DSN), enforces the
// admin-only write/delete boundary, persists metadata only, and records an audit
// event for every successful write/delete. The engine is always derived from the
// selected target, never accepted from a request.
type QueryCredentialService struct {
	targets  QueryTargetRepository
	store    QueryCredentialMetadataStore
	resolver QueryCredentialResolver
}

// NewQueryCredentialService wires the service with the query target read model,
// the metadata/audit store, and the credential resolver.
func NewQueryCredentialService(targets QueryTargetRepository, store QueryCredentialMetadataStore, resolver QueryCredentialResolver) *QueryCredentialService {
	return &QueryCredentialService{targets: targets, store: store, resolver: resolver}
}

// GetStatus returns the credential status for a target, stable even when no
// metadata row exists. It never returns a DSN, host, or port. A missing target
// maps to ErrQueryTargetNotFound.
//
// The credential read is classified so a failure is never masked as "no row":
//   - no row (sql.ErrNoRows) -> missing_metadata, target locked;
//   - a row whose stored ref/policy is invalid (ErrInvalidCredentialMetadata) ->
//     invalid_ref, target locked, resolver NOT consulted, configured=true with
//     the raw ref suppressed (it failed validation and could be DSN-shaped);
//   - any other read error -> propagated as a controlled backend error (the
//     handler maps it to 500), never missing_metadata.
func (s *QueryCredentialService) GetStatus(ctx context.Context, targetID uint64) (model.QueryCredentialStatusResponse, error) {
	target, err := s.findTarget(ctx, targetID)
	if err != nil {
		return model.QueryCredentialStatusResponse{}, err
	}
	c, readErr := s.store.GetCredentialByResourceID(ctx, targetID)
	switch {
	case readErr == nil:
		runtime := InspectCredentialRuntime(ctx, s.resolver, target, &c)
		return buildCredentialStatusResponse(target, &c, runtime), nil
	case errors.Is(readErr, sql.ErrNoRows):
		runtime := InspectCredentialRuntime(ctx, s.resolver, target, nil)
		return buildCredentialStatusResponse(target, nil, runtime), nil
	case errors.Is(readErr, model.ErrInvalidCredentialMetadata):
		// A row exists but is corrupt. Fail closed as invalid_ref (no resolver
		// lookup) and report configured=true. Suppress the raw ref — it failed
		// validation and, if it bypassed the write path, could be DSN-shaped.
		// Echo enabled (a safe boolean) and policy only when it still validates.
		invalid := c
		invalid.CredentialRef = ""
		if pErr := invalid.EnvironmentPolicy.Validate(); pErr != nil {
			invalid.EnvironmentPolicy = model.QueryEnvPolicyDisabled
		}
		return buildCredentialStatusResponse(target, &invalid, model.QueryCredentialRuntimeInvalidRef), nil
	default:
		// Unexpected DB/read error — propagate; the handler maps it to 500.
		return model.QueryCredentialStatusResponse{}, readErr
	}
}

// Upsert validates the request, derives the engine from the target, then
// atomically persists metadata AND records a query.credential.updated audit
// event (the store owns the transaction, so a failed audit rolls back the
// metadata). It returns the post-save status. Save succeeds even when the secret
// is not resolvable yet; the returned runtime status explains it and the target
// stays locked. Only an admin may upsert; a non-admin receives
// ErrQueryCredentialForbidden and nothing is written.
func (s *QueryCredentialService) Upsert(ctx context.Context, actor AuthenticatedUser, targetID uint64, req model.QueryCredentialUpsertRequest) (model.QueryCredentialStatusResponse, error) {
	if !isAdmin(actor) {
		return model.QueryCredentialStatusResponse{}, ErrQueryCredentialForbidden
	}
	if err := req.Validate(); err != nil {
		return model.QueryCredentialStatusResponse{}, fmt.Errorf("%w: %v", ErrQueryCredentialValidation, err)
	}
	target, err := s.findTarget(ctx, targetID)
	if err != nil {
		return model.QueryCredentialStatusResponse{}, err
	}
	if !isExecutableEngine(target.ConnectionContext.Engine) || target.ConnectionContext.Host == "" || target.ConnectionContext.Port == 0 {
		return model.QueryCredentialStatusResponse{}, fmt.Errorf("%w: target is not a complete mysql/tidb query target", ErrQueryCredentialValidation)
	}
	meta := model.QueryCredentialMetadata{
		ResourceID:        target.ResourceID,
		Engine:            target.ConnectionContext.Engine,
		CredentialRef:     req.CredentialRef,
		Enabled:           req.Enabled,
		EnvironmentPolicy: req.EnvironmentPolicy,
	}
	if err := s.store.UpsertCredentialMetadataWithAudit(ctx, meta, actor.ID, queryCredentialUpdatedEvent, auditResultSuccess); err != nil {
		return model.QueryCredentialStatusResponse{}, err
	}
	runtime := InspectCredentialRuntime(ctx, s.resolver, target, &meta)
	return buildCredentialStatusResponse(target, &meta, runtime), nil
}

// Delete atomically removes a target's credential metadata AND records a
// query.credential.deleted audit event (the store owns the transaction, so a
// failed audit leaves the original metadata in place). Only an admin may delete;
// a non-admin receives ErrQueryCredentialForbidden and nothing is removed.
func (s *QueryCredentialService) Delete(ctx context.Context, actor AuthenticatedUser, targetID uint64) error {
	if !isAdmin(actor) {
		return ErrQueryCredentialForbidden
	}
	target, err := s.findTarget(ctx, targetID)
	if err != nil {
		return err
	}
	return s.store.DeleteCredentialMetadataWithAudit(ctx, target.ResourceID, actor.ID, queryCredentialDeletedEvent, auditResultSuccess)
}

// findTarget locates a single query target by id, mirroring the execute path.
// A read error or missing target maps to ErrQueryTargetNotFound so writes fail
// closed rather than against an unresolved target.
func (s *QueryCredentialService) findTarget(ctx context.Context, targetID uint64) (model.QueryTarget, error) {
	targets, _, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetID})
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

// buildCredentialStatusResponse assembles the metadata-only status response. It
// surfaces the opaque ref, enabled flag, and policy when configured, and the
// runtime status + eligibility otherwise. It never carries a DSN, host, or port.
func buildCredentialStatusResponse(target model.QueryTarget, cred *model.QueryCredentialMetadata, runtime model.QueryCredentialRuntimeStatus) model.QueryCredentialStatusResponse {
	resp := model.QueryCredentialStatusResponse{
		ResourceID:        target.ResourceID,
		Engine:            target.ConnectionContext.Engine,
		RuntimeStatus:     runtime,
		ExecutionEligible: runtime.IsResolved(),
		Message:           credentialRuntimeMessage(runtime),
	}
	if cred != nil {
		resp.Configured = true
		resp.CredentialRef = cred.CredentialRef
		resp.Enabled = cred.Enabled
		resp.EnvironmentPolicy = cred.EnvironmentPolicy
	} else {
		resp.EnvironmentPolicy = model.QueryEnvPolicyDisabled
	}
	return resp
}

// credentialRuntimeMessage maps a runtime status to a fixed, client-safe
// message. None of these strings echoes a DSN, host, port, or password.
func credentialRuntimeMessage(status model.QueryCredentialRuntimeStatus) string {
	switch status {
	case model.QueryCredentialRuntimeMissingMetadata:
		return "No read-only credential reference is configured."
	case model.QueryCredentialRuntimeInvalidRef:
		return "The configured credential reference is invalid."
	case model.QueryCredentialRuntimeDisabled:
		return "The read-only credential is disabled."
	case model.QueryCredentialRuntimePolicyBlocked:
		return "Environment policy blocks execution for this target."
	case model.QueryCredentialRuntimeSecretMissing:
		return "The server could not resolve the configured credential secret."
	case model.QueryCredentialRuntimeBindingMismatch:
		return "The resolved credential does not bind to this target."
	case model.QueryCredentialRuntimeSecretResolved:
		return "Read-only credential is configured and bound to this target."
	case model.QueryCredentialRuntimeUnsupportedTarget:
		return "This target engine is not supported for query execution."
	case model.QueryCredentialRuntimeIncompleteConnection:
		return "Target connection metadata is incomplete."
	default:
		return "Credential status is unavailable."
	}
}
