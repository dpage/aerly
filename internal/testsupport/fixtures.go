package testsupport

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// InsertUser inserts a user row directly and returns its id.
func InsertUser(t *testing.T, pool *pgxpool.Pool, username string, superuser, active bool) int64 {
	t.Helper()
	var id int64
	err := pool.QueryRow(context.Background(), `
		INSERT INTO users (username, name, is_superuser, is_active)
		VALUES ($1, $1, $2, $3) RETURNING id`,
		username, superuser, active,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert user %q: %v", username, err)
	}
	return id
}

// InsertFlight / InsertPosition (flight_id-keyed) were removed in Wave 3 with
// the legacy flights / positions.flight_id surface. Tests that need a flight
// seed a plan + plan_part + flight_details directly (see the poller test
// helpers); position fixtures key on plan_part_id via store.InsertPartPosition.
