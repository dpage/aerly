package store

import "testing"

// TestG1Pool covers the Store.Pool accessor.
func TestG1Pool(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	if s.Pool() != s.pool {
		t.Fatalf("Pool() should return the underlying pool")
	}
}
