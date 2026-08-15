// Package service provides business logic for authentication audit event emission.
// input: expvar, sync, time
// output: AuthAuditEmitter, NoopEmitter, BoundedAuthAuditEmitter, NewBoundedAuthAuditEmitter, BoundedBearerRejectedLimit, AuthAuditSuppressedRejections
// pos: Interface for emitting auth/authz audit events; fail-open: callers never block on failure; bounded decorator caps untrusted Bearer rejection persistence per process
// note: if this file changes, update header and README.md
package service

import (
	"expvar"
	"sync"
	"time"
)

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

// BoundedBearerRejectedLimit is the fixed per-process persistence budget for
// untrusted Backend Bearer rejection events: at most this many
// auth.bearer/rejected events with no verified actor may pass through per
// window per process. It is deliberately not configurable (ADR 2026-08-15).
const BoundedBearerRejectedLimit = 60

// AuthAuditSuppressedRejections is the fixed-category operational counter for
// untrusted Bearer rejection events skipped by the bounded-audit budget. It is
// published under a stable key and exposed only through the administrator-only
// auth-audit metrics surface. It carries no identity or request dimensions.
var AuthAuditSuppressedRejections = expvar.NewInt("auth_audit_suppressed_rejections")

// BoundedAuthAuditEmitter is a process-local decorator over AuthAuditEmitter
// that caps persistence of untrusted Bearer rejection events (event type
// auth.bearer, result rejected, no verified actor) at BoundedBearerRejectedLimit
// per fixed window. All other events — logins, verified-actor rejections, and
// role denials — pass through unchanged and are never budgeted. The budget is
// race-safe and anchored to the first event of each window.
type BoundedAuthAuditEmitter struct {
	inner       AuthAuditEmitter
	limit       int
	window      time.Duration
	clock       func() time.Time
	mu          sync.Mutex
	windowStart time.Time
	count       int
}

// NewBoundedAuthAuditEmitter returns a bounded decorator. clock is injected for
// tests and defaults to time.Now when nil; it is a test seam, not a
// configuration knob.
func NewBoundedAuthAuditEmitter(inner AuthAuditEmitter, limit int, window time.Duration, clock func() time.Time) *BoundedAuthAuditEmitter {
	if clock == nil {
		clock = time.Now
	}
	return &BoundedAuthAuditEmitter{
		inner:  inner,
		limit:  limit,
		window: window,
		clock:  clock,
	}
}

// EmitAuthAudit applies the untrusted-Bearer budget and passes every other
// event straight through. Budget exhaustion keeps the caller's security
// decision unchanged, writes no per-attempt row or log, and increments only
// the safe suppression counter.
func (b *BoundedAuthAuditEmitter) EmitAuthAudit(eventType, result string, actorUserID *uint64, targetResourceID *uint64) error {
	if eventType == "auth.bearer" && result == "rejected" && actorUserID == nil {
		if !b.allow() {
			AuthAuditSuppressedRejections.Add(1)
			return nil
		}
	}
	return b.inner.EmitAuthAudit(eventType, result, actorUserID, targetResourceID)
}

// allow reports whether one more untrusted rejection fits in the current
// window, rolling the window forward when it has elapsed.
func (b *BoundedAuthAuditEmitter) allow() bool {
	now := b.clock()
	b.mu.Lock()
	defer b.mu.Unlock()
	if now.Sub(b.windowStart) >= b.window {
		b.windowStart = now
		b.count = 0
	}
	if b.count >= b.limit {
		return false
	}
	b.count++
	return true
}
