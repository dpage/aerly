package store

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// Shared test helpers for the store package. These used to live in
// flights_test.go, which was deleted with the legacy flight surface in Wave 3.

func newStore(t *testing.T) *Store {
	t.Helper()
	return New(testsupport.NewPool(t))
}

var ctx = context.Background()

var loginSeq atomic.Int64

// mkUser inserts a fresh user (unique login) and returns its id; rows with a
// created_by FK need a valid user to point at.
func mkUser(t *testing.T, s *Store) int64 {
	t.Helper()
	return testsupport.InsertUser(t, s.pool,
		fmt.Sprintf("creator%d", loginSeq.Add(1)), false, true)
}
