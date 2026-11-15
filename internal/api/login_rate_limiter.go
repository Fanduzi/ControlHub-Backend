package api

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	loginRateWindow          = 60 * time.Second
	loginAttemptsPerRemoteIP = 120
	loginAttemptsPerAccount  = 30
	loginMaxTrackedKeys      = 10000
	loginBodyMaxBytes        = 16384
)

type loginWindowCounter struct {
	start time.Time
	count int
}

type loginRateLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	entries map[string]*loginWindowCounter
}

func newLoginRateLimiter(now func() time.Time) *loginRateLimiter {
	return &loginRateLimiter{
		now:     now,
		entries: make(map[string]*loginWindowCounter),
	}
}

func (l *loginRateLimiter) allow(remoteAddr, email string) bool {
	ip := remoteAddr
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		ip = host
	}
	acct := strings.TrimSpace(strings.ToLower(email))
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	ipKey := "ip:" + ip
	acctKey := "account:" + acct

	ipCount := windowCount(l.entries[ipKey], now)
	acctCount := windowCount(l.entries[acctKey], now)
	if ipCount >= loginAttemptsPerRemoteIP || acctCount >= loginAttemptsPerAccount {
		return false
	}

	missing := 0
	if l.entries[ipKey] == nil {
		missing++
	}
	if l.entries[acctKey] == nil {
		missing++
	}
	if len(l.entries)+missing > loginMaxTrackedKeys {
		for k, e := range l.entries {
			if now.Sub(e.start) >= loginRateWindow {
				delete(l.entries, k)
			}
		}
		missing = 0
		if l.entries[ipKey] == nil {
			missing++
		}
		if l.entries[acctKey] == nil {
			missing++
		}
		if len(l.entries)+missing > loginMaxTrackedKeys {
			return false
		}
	}

	l.entries[ipKey] = bumpWindow(l.entries[ipKey], now)
	l.entries[acctKey] = bumpWindow(l.entries[acctKey], now)
	return true
}

func windowCount(c *loginWindowCounter, now time.Time) int {
	if c == nil || now.Sub(c.start) >= loginRateWindow {
		return 0
	}
	return c.count
}

func bumpWindow(c *loginWindowCounter, now time.Time) *loginWindowCounter {
	if c == nil || now.Sub(c.start) >= loginRateWindow {
		return &loginWindowCounter{start: now, count: 1}
	}
	c.count++
	return c
}

func writeLoginRateLimited(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "60")
	writeJSONError(w, http.StatusTooManyRequests, "login_rate_limited", "too many login attempts; try again later")
}
