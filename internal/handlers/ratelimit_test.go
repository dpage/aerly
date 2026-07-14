package handlers

import (
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestUserRateLimiter(t *testing.T) {
	// Burst of 1 and a refill so slow it won't happen during the test: the first
	// call for a user is allowed, the second is denied.
	l := newUserRateLimiter(rate.Every(time.Hour), 1)
	if !l.allow(1) {
		t.Fatal("first call for user 1 should be allowed")
	}
	if l.allow(1) {
		t.Fatal("second immediate call for user 1 should be denied")
	}
	// A different user has an independent bucket.
	if !l.allow(2) {
		t.Fatal("first call for user 2 should be allowed (independent bucket)")
	}
}
