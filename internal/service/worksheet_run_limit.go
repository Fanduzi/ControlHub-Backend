package service

import (
	"sync"
	"time"
)

// worksheetRunMaxTrackedEntries bounds the limiter's per-target and per-user entry
// tables. Entries accrue only for approved users of configured targets, so
// growth is naturally bounded; past the cap the limiter drops idle full
// buckets and then fails closed rather than tracking unbounded flood keys.
const worksheetRunMaxTrackedEntries = 10000

// worksheetRunLimits carries the four resolved Run protections for one approval
// snapshot: in-flight Runs per target and per (target, user), plus token
// buckets for Runs per second per target and Runs per minute per user.
type worksheetRunLimits struct {
	maxConcurrentRuns        int
	maxConcurrentRunsPerUser int
	runsPerSecond            int
	runsPerMinutePerUser     int
}

func defaultWorksheetRunLimits() worksheetRunLimits {
	return worksheetRunLimits{maxConcurrentRuns: 4, maxConcurrentRunsPerUser: 1, runsPerSecond: 10, runsPerMinutePerUser: 30}
}

// worksheetRunLimitsWire is the optional "limits" object in the approval file.
// Omitted fields take defaults; a present field outside its range invalidates
// the whole approval.
type worksheetRunLimitsWire struct {
	MaxConcurrentRuns        *int `json:"maxConcurrentRuns"`
	MaxConcurrentRunsPerUser *int `json:"maxConcurrentRunsPerUser"`
	RunsPerSecond            *int `json:"runsPerSecond"`
	RunsPerMinutePerUser     *int `json:"runsPerMinutePerUser"`
}

func (w *worksheetRunLimitsWire) resolve() (worksheetRunLimits, bool) {
	l := defaultWorksheetRunLimits()
	if w == nil {
		return l, true
	}
	if w.MaxConcurrentRuns != nil {
		if *w.MaxConcurrentRuns < 1 || *w.MaxConcurrentRuns > 16 {
			return l, false
		}
		l.maxConcurrentRuns = *w.MaxConcurrentRuns
	}
	if w.MaxConcurrentRunsPerUser != nil {
		if *w.MaxConcurrentRunsPerUser < 1 || *w.MaxConcurrentRunsPerUser > 4 {
			return l, false
		}
		l.maxConcurrentRunsPerUser = *w.MaxConcurrentRunsPerUser
	}
	if w.RunsPerSecond != nil {
		if *w.RunsPerSecond < 1 || *w.RunsPerSecond > 50 {
			return l, false
		}
		l.runsPerSecond = *w.RunsPerSecond
	}
	if w.RunsPerMinutePerUser != nil {
		if *w.RunsPerMinutePerUser < 1 || *w.RunsPerMinutePerUser > 600 {
			return l, false
		}
		l.runsPerMinutePerUser = *w.RunsPerMinutePerUser
	}
	return l, true
}

// worksheetRunWindow tracks one in-flight count and one token bucket for a target
// or a (target, user) pair. capacity records the last observed bucket size so
// idle-full entries can be evicted under saturation without losing tokens a
// later request should still own.
type worksheetRunWindow struct {
	inFlight int
	tokens   float64
	capacity float64
	at       time.Time
}

// refill credits elapsed time at rate tokens/second and clamps the bucket to
// the caller's current capacity — a smaller approval shrinks stored tokens.
func (w *worksheetRunWindow) refill(now time.Time, capacity int, rate float64) {
	cap := float64(capacity)
	if now.After(w.at) {
		w.tokens += now.Sub(w.at).Seconds() * rate
		w.at = now
	}
	if w.tokens > cap {
		w.tokens = cap
	}
	w.capacity = cap
}

type worksheetRunUserKey struct {
	target uint64
	user   uint64
}

// worksheetRunLimiter bounds governed worksheet Runs inside one ControlHub process.
// Every admitted Run holds one in-flight slot per target and per (target,
// user) plus one token from each bucket; a refusal consumes nothing.
type worksheetRunLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	targets map[uint64]*worksheetRunWindow
	users   map[worksheetRunUserKey]*worksheetRunWindow
}

func newWorksheetRunLimiter(now func() time.Time) *worksheetRunLimiter {
	return &worksheetRunLimiter{
		now:     now,
		targets: make(map[uint64]*worksheetRunWindow),
		users:   make(map[worksheetRunUserKey]*worksheetRunWindow),
	}
}

// acquire takes both concurrency slots and one token from both buckets
// atomically, or nothing at all. The returned release is idempotent.
func (l *worksheetRunLimiter) acquire(targetID, userID uint64, limits worksheetRunLimits) (func(), bool) {
	key := worksheetRunUserKey{target: targetID, user: userID}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	target := l.targets[targetID]
	user := l.users[key]
	if target == nil || user == nil {
		missing := 0
		if target == nil {
			missing++
		}
		if user == nil {
			missing++
		}
		if len(l.targets)+len(l.users)+missing > worksheetRunMaxTrackedEntries {
			l.dropIdle()
			missing = 0
			if l.targets[targetID] == nil {
				missing++
			}
			if l.users[key] == nil {
				missing++
			}
			if len(l.targets)+len(l.users)+missing > worksheetRunMaxTrackedEntries {
				return nil, false
			}
		}
	}
	if target == nil {
		target = &worksheetRunWindow{tokens: float64(limits.runsPerSecond), at: now}
		l.targets[targetID] = target
	}
	if user == nil {
		user = &worksheetRunWindow{tokens: float64(limits.runsPerMinutePerUser), at: now}
		l.users[key] = user
	}
	target.refill(now, limits.runsPerSecond, float64(limits.runsPerSecond))
	user.refill(now, limits.runsPerMinutePerUser, float64(limits.runsPerMinutePerUser)/60)
	if target.inFlight >= limits.maxConcurrentRuns || user.inFlight >= limits.maxConcurrentRunsPerUser ||
		target.tokens < 1 || user.tokens < 1 {
		return nil, false
	}
	target.inFlight++
	user.inFlight++
	target.tokens--
	user.tokens--
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if target.inFlight > 0 {
				target.inFlight--
			}
			if user.inFlight > 0 {
				user.inFlight--
			}
		})
	}, true
}

// dropIdle evicts entries with no in-flight Run and a full bucket; deleting
// them loses no tokens a queued request could still have used.
func (l *worksheetRunLimiter) dropIdle() {
	for id, w := range l.targets {
		if w.inFlight == 0 && w.tokens >= w.capacity {
			delete(l.targets, id)
		}
	}
	for key, w := range l.users {
		if w.inFlight == 0 && w.tokens >= w.capacity {
			delete(l.users, key)
		}
	}
}
