package store

import (
	"context"
	"testing"
)

// g3CancelledCtx returns a context that has already been cancelled. Issuing a
// query against the pool with it makes pgx return a real error before touching
// the database, which is the only seam available (through the exported API) to
// drive the "query/exec failed" error-return branches that every store method
// guards but that a healthy DB never hits. The store package holds a concrete
// *pgxpool.Pool, not an interface, so a failing fake pool isn't an option
// without a production change.
func g3CancelledCtx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c
}
