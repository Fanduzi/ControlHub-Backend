// Package service provides business logic for authentication audit event emission.
// input: none
// output: AuthAuditEmitter, NoopEmitter
// pos: Interface for emitting auth/authz audit events; fail-open: callers never block on failure
// note: if this file changes, update header and README.md
package service

// AuthAuditEmitter records authentication and authorization outcomes.
// Implementations MUST be fail-open: a persistence failure never changes
// the security decision. Events with no verified actor pass nil for actorUserID.
type AuthAuditEmitter interface {
	// EmitAuthAudit records one authentication/authorization event.
	// actorUserID is nil for unauthenticated outcomes (failed login, rejected
	// Bearer). targetResourceID is nil when the denied boundary has no known
	// target at decision time.
	EmitAuthAudit(eventType, result string, actorUserID *uint64, targetResourceID *uint64) error
}

// NoopEmitter discards every event. Used when no audit store is configured
// or as a safety fallback.
type NoopEmitter struct{}

func (NoopEmitter) EmitAuthAudit(_, _ string, _ *uint64, _ *uint64) error { return nil }
