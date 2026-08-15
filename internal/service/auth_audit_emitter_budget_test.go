// Package service provides tests for the bounded auth-audit emitter budget.
// input: sync, testing, time
// output: TestBoundedAuthAuditEmitter_* unit tests
// pos: Proves the 60/min window reset, race-safe concurrent consumption, and the passthrough of non-untrusted events at the AuthAuditEmitter seam
// note: if this file changes, update header and README.md
package service

import (
	"sync"
	"testing"
	"time"
)

// recordingEmitter captures every event the bounded decorator passes through.
type recordingEmitter struct {
	mu     sync.Mutex
	events []string // "eventType/result/actor-present?"
}

func (r *recordingEmitter) EmitAuthAudit(eventType, result string, actorUserID *uint64, _ *uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	actor := "nil"
	if actorUserID != nil {
		actor = "set"
	}
	r.events = append(r.events, eventType+"/"+result+"/"+actor)
	return nil
}

func (r *recordingEmitter) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

// TestBoundedAuthAuditEmitter_CapsUntrustedRejectionsPerWindow proves the
// fixed limit applies per fixed window anchored at the first event: 60 pass,
// the 61st is suppressed, and the suppression counter increments once.
func TestBoundedAuthAuditEmitter_CapsUntrustedRejectionsPerWindow(t *testing.T) {
	inner := &recordingEmitter{}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	b := NewBoundedAuthAuditEmitter(inner, BoundedBearerRejectedLimit, time.Minute, func() time.Time { return now })

	before := AuthAuditSuppressedRejections.Value()
	for i := 0; i < 61; i++ {
		if err := b.EmitAuthAudit("auth.bearer", "rejected", nil, nil); err != nil {
			t.Fatalf("attempt %d: EmitAuthAudit: %v", i+1, err)
		}
	}
	if got := inner.len(); got != 60 {
		t.Fatalf("events passed through = %d, want 60", got)
	}
	if got := AuthAuditSuppressedRejections.Value() - before; got != 1 {
		t.Fatalf("suppression counter delta = %d, want 1", got)
	}
}

// TestBoundedAuthAuditEmitter_WindowResetProvesTheNextMinuteAllowsEventsAgain
// proves the window rolls forward: after the fixed window elapses, the budget
// resets and untrusted rejections pass through again.
func TestBoundedAuthAuditEmitter_WindowResetProvesTheNextMinuteAllowsEventsAgain(t *testing.T) {
	inner := &recordingEmitter{}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	current := now
	b := NewBoundedAuthAuditEmitter(inner, BoundedBearerRejectedLimit, time.Minute, func() time.Time { return current })

	for i := 0; i < 60; i++ {
		_ = b.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
	}
	// 61st within the same window is suppressed.
	_ = b.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
	if got := inner.len(); got != 60 {
		t.Fatalf("events before rollover = %d, want 60", got)
	}

	// Advance past the window boundary: budget resets.
	current = now.Add(time.Minute + time.Second)
	_ = b.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
	if got := inner.len(); got != 61 {
		t.Fatalf("events after rollover = %d, want 61 (budget reset)", got)
	}
}

// TestBoundedAuthAuditEmitter_ConcurrentConsumptionNeverExceedsLimit proves
// the budget is race-safe: concurrent callers together never pass through more
// than the limit per window (verified under -race).
func TestBoundedAuthAuditEmitter_ConcurrentConsumptionNeverExceedsLimit(t *testing.T) {
	inner := &recordingEmitter{}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	b := NewBoundedAuthAuditEmitter(inner, BoundedBearerRejectedLimit, time.Minute, func() time.Time { return now })

	const goroutines = 8
	const perGoroutine = 20 // 160 attempts total, only 60 may pass
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				_ = b.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
			}
		}()
	}
	wg.Wait()

	if got := inner.len(); got != 60 {
		t.Fatalf("events passed through = %d, want exactly 60 under concurrency", got)
	}
}

// TestBoundedAuthAuditEmitter_PassesNonUntrustedEvents proves logins,
// verified-actor bearer rejections, and role denials are never budgeted.
func TestBoundedAuthAuditEmitter_PassesNonUntrustedEvents(t *testing.T) {
	inner := &recordingEmitter{}
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	b := NewBoundedAuthAuditEmitter(inner, BoundedBearerRejectedLimit, time.Minute, func() time.Time { return now })

	actor := uint64(42)
	target := uint64(7)
	// Exhaust the untrusted budget first so any accidental budgeting of these
	// events would suppress them.
	for i := 0; i < 61; i++ {
		_ = b.EmitAuthAudit("auth.bearer", "rejected", nil, nil)
	}

	calls := [][4]any{
		{"auth.login", "succeeded", (*uint64)(nil), (*uint64)(nil)},
		{"auth.login", "rejected", (*uint64)(nil), (*uint64)(nil)},
		{"auth.bearer", "rejected", &actor, (*uint64)(nil)},
		{"auth.authorization", "denied", &actor, &target},
	}
	for _, c := range calls {
		actorArg, _ := c[2].(*uint64)
		targetArg, _ := c[3].(*uint64)
		if err := b.EmitAuthAudit(c[0].(string), c[1].(string), actorArg, targetArg); err != nil {
			t.Fatalf("EmitAuthAudit(%v): %v", c[0], err)
		}
	}

	inner.mu.Lock()
	defer inner.mu.Unlock()
	if got := len(inner.events); got != 64 {
		t.Fatalf("total passed through = %d, want 60 budgeted + 4 unbudgeted", got)
	}
	want := []string{
		"auth.login/succeeded/nil",
		"auth.login/rejected/nil",
		"auth.bearer/rejected/set",
		"auth.authorization/denied/set",
	}
	for i, w := range want {
		if got := inner.events[len(inner.events)-4+i]; got != w {
			t.Fatalf("event %d = %q, want %q", i+1, got, w)
		}
	}
}
