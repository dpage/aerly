package handlers

import (
	"sync"

	"golang.org/x/time/rate"
)

// userRateLimiter is a small per-user token-bucket limiter. It guards the
// LLM-backed ingest endpoint so a single authenticated editor can't drive
// unbounded paid-LLM spend by hammering propose. State is in-memory (one bucket
// per user id, kept for the process lifetime — bounded by the user count) and
// safe for concurrent use.
type userRateLimiter struct {
	mu     sync.Mutex
	limits map[int64]*rate.Limiter
	every  rate.Limit
	burst  int
}

// newUserRateLimiter builds a limiter that refills at `every` tokens/second and
// tolerates short bursts up to `burst`.
func newUserRateLimiter(every rate.Limit, burst int) *userRateLimiter {
	return &userRateLimiter{limits: make(map[int64]*rate.Limiter), every: every, burst: burst}
}

// allow reports whether the user may perform one more action now, consuming a
// token when it returns true.
func (u *userRateLimiter) allow(userID int64) bool {
	u.mu.Lock()
	l := u.limits[userID]
	if l == nil {
		l = rate.NewLimiter(u.every, u.burst)
		u.limits[userID] = l
	}
	u.mu.Unlock()
	return l.Allow()
}
