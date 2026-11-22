package service

import (
	"errors"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

func testClock(t *testing.T) (*time.Time, func() time.Time) {
	t.Helper()
	at := time.Unix(1_700_000_000, 0)
	return &at, func() time.Time { return at }
}

func mustAcquire(t *testing.T, l *redisRunLimiter, target, user uint64, limits redisRunLimits) func() {
	t.Helper()
	release, ok := l.acquire(target, user, limits)
	if !ok {
		t.Fatalf("acquire(%d,%d) refused", target, user)
	}
	return release
}

func TestRedisRunLimiterPerUserConcurrency(t *testing.T) {
	at, now := testClock(t)
	l := newRedisRunLimiter(now)
	limits := defaultRedisRunLimits()

	release := mustAcquire(t, l, 1, 7, limits)
	if _, ok := l.acquire(1, 7, limits); ok {
		t.Fatal("second in-flight Run for the same user was admitted")
	}
	release()
	release() // idempotent
	mustAcquire(t, l, 1, 7, limits)
	_ = at
}

func TestRedisRunLimiterTargetConcurrency(t *testing.T) {
	_, now := testClock(t)
	l := newRedisRunLimiter(now)
	limits := defaultRedisRunLimits()
	limits.maxConcurrentRuns = 2
	limits.maxConcurrentRunsPerUser = 4

	mustAcquire(t, l, 1, 7, limits)
	mustAcquire(t, l, 1, 8, limits)
	if _, ok := l.acquire(1, 9, limits); ok {
		t.Fatal("third in-flight Run on one target was admitted")
	}
	// A different target is independent.
	mustAcquire(t, l, 2, 9, limits)
}

func TestRedisRunLimiterRateBuckets(t *testing.T) {
	at, now := testClock(t)
	l := newRedisRunLimiter(now)
	limits := defaultRedisRunLimits()
	limits.runsPerSecond = 2
	limits.runsPerMinutePerUser = 3

	// Per-second target bucket: two tokens, then a third Run is refused.
	for i := 0; i < 2; i++ {
		release := mustAcquire(t, l, 1, uint64(7+i), limits)
		release()
	}
	if _, ok := l.acquire(1, 9, limits); ok {
		t.Fatal("third Run inside one second exceeded the target bucket")
	}
	// Half a second refills exactly one token at rate 2/s.
	*at = at.Add(500 * time.Millisecond)
	release := mustAcquire(t, l, 1, 9, limits)
	release()
	if _, ok := l.acquire(1, 10, limits); ok {
		t.Fatal("bucket refilled beyond one token after 500ms at 2/s")
	}

	// Per-minute user bucket: capacity 3 across sequential Runs.
	*at = at.Add(10 * time.Second)
	l = newRedisRunLimiter(now)
	limits.runsPerSecond = 50
	limits.maxConcurrentRunsPerUser = 4
	for i := 0; i < 3; i++ {
		release = mustAcquire(t, l, 1, 7, limits)
		release()
	}
	if _, ok := l.acquire(1, 7, limits); ok {
		t.Fatal("fourth Run by one user exceeded the per-minute bucket")
	}
	// Another user has an independent minute bucket.
	release = mustAcquire(t, l, 1, 8, limits)
	release()
	// Twenty seconds refills one token at 3/60 per second.
	*at = at.Add(20 * time.Second)
	release = mustAcquire(t, l, 1, 7, limits)
	release()
	if _, ok := l.acquire(1, 7, limits); ok {
		t.Fatal("per-minute bucket refilled beyond one token after 20s at 3/min")
	}
}

func TestRedisRunLimiterRefusalConsumesNothing(t *testing.T) {
	_, now := testClock(t)
	l := newRedisRunLimiter(now)
	limits := defaultRedisRunLimits()
	limits.maxConcurrentRunsPerUser = 1

	release := mustAcquire(t, l, 1, 7, limits)
	// Refused on the per-user slot: the per-second and per-minute tokens of
	// user 7 and the target must be untouched.
	if _, ok := l.acquire(1, 7, limits); ok {
		t.Fatal("second in-flight Run for the same user was admitted")
	}
	target := l.targets[1]
	user := l.users[redisRunUserKey{target: 1, user: 7}]
	if target.tokens != float64(limits.runsPerSecond)-1 || user.tokens != float64(limits.runsPerMinutePerUser)-1 {
		t.Fatalf("a refused acquire consumed tokens: target=%v user=%v", target.tokens, user.tokens)
	}
	if target.inFlight != 1 || user.inFlight != 1 {
		t.Fatalf("a refused acquire consumed slots: target=%d user=%d", target.inFlight, user.inFlight)
	}
	release()
}

func TestRedisRunLimiterClampsOnLimitDecrease(t *testing.T) {
	at, now := testClock(t)
	l := newRedisRunLimiter(now)
	limits := defaultRedisRunLimits()
	limits.runsPerSecond = 50
	limits.maxConcurrentRunsPerUser = 4

	release := mustAcquire(t, l, 1, 100, limits)
	release()
	// New approval shrinks the per-second bucket to 5: the stored 49 tokens
	// clamp to 5, so exactly five Runs fit before refusal.
	limits.runsPerSecond = 5
	for i := 0; i < 5; i++ {
		release = mustAcquire(t, l, 1, uint64(200+i), limits)
		release()
	}
	if _, ok := l.acquire(1, 300, limits); ok {
		t.Fatal("shrunk bucket still served the unclamped token surplus")
	}
	_ = at
}

func TestRedisRunLimiterSaturation(t *testing.T) {
	_, now := testClock(t)
	limits := defaultRedisRunLimits()

	// A table of idle full buckets is evicted so a fresh user still fits.
	l := newRedisRunLimiter(now)
	for i := 0; i < redisRunMaxTrackedEntries; i++ {
		l.users[redisRunUserKey{target: 1, user: uint64(i)}] = &redisRunWindow{tokens: 30, capacity: 30}
	}
	mustAcquire(t, l, 1, 999_999, limits)

	// A table of in-flight users cannot free entries: fail closed.
	l2 := newRedisRunLimiter(now)
	l2.targets[1] = &redisRunWindow{tokens: 10, capacity: 10}
	for i := 0; i < redisRunMaxTrackedEntries-1; i++ {
		l2.users[redisRunUserKey{target: 1, user: uint64(i)}] = &redisRunWindow{inFlight: 1, tokens: 30, capacity: 30}
	}
	if _, ok := l2.acquire(1, 888_888, limits); ok {
		t.Fatal("saturated limiter admitted a new tracked user")
	}
}

func TestRedisRunLimitsResolve(t *testing.T) {
	defaults, ok := (*redisRunLimitsWire)(nil).resolve()
	if !ok || defaults != defaultRedisRunLimits() {
		t.Fatalf("nil limits did not resolve to defaults: %+v", defaults)
	}
	ints := func(v int) *int { return &v }
	for name, wire := range map[string]redisRunLimitsWire{
		"maxConcurrentRuns zero":        {MaxConcurrentRuns: ints(0)},
		"maxConcurrentRuns over":        {MaxConcurrentRuns: ints(17)},
		"maxConcurrentRunsPerUser zero": {MaxConcurrentRunsPerUser: ints(0)},
		"maxConcurrentRunsPerUser over": {MaxConcurrentRunsPerUser: ints(5)},
		"runsPerSecond zero":            {RunsPerSecond: ints(0)},
		"runsPerSecond over":            {RunsPerSecond: ints(51)},
		"runsPerMinutePerUser zero":     {RunsPerMinutePerUser: ints(0)},
		"runsPerMinutePerUser over":     {RunsPerMinutePerUser: ints(601)},
	} {
		w := wire
		if _, ok := w.resolve(); ok {
			t.Fatalf("out-of-range %s resolved", name)
		}
	}
	w := redisRunLimitsWire{MaxConcurrentRuns: ints(8), RunsPerMinutePerUser: ints(60)}
	resolved, ok := w.resolve()
	if !ok || resolved.maxConcurrentRuns != 8 || resolved.runsPerMinutePerUser != 60 ||
		resolved.maxConcurrentRunsPerUser != 1 || resolved.runsPerSecond != 10 {
		t.Fatalf("partial limits did not merge defaults: %+v", resolved)
	}
}

func TestRedisRateLimitedClassifies(t *testing.T) {
	status, sentinel, code, _ := classifyExecutorError(ErrQueryRateLimited)
	if status != model.QueryExecutionRejected || !errors.Is(sentinel, ErrQueryRateLimited) || code != "query_rate_limited" {
		t.Fatalf("rate limit misclassified: %s %v %s", status, sentinel, code)
	}
}
