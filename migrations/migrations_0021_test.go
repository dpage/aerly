package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/testsupport"
)

// seedPartChain inserts a user → trip → plan → plan_part and returns their ids.
func seedPartChain(t *testing.T, pool *pgxpool.Pool, username string) (userID, tripID, planID, partID int64) {
	t.Helper()
	ctx := context.Background()
	userID = testsupport.InsertUser(t, pool, username, false, true)
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, userID,
	).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID,
	).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at) VALUES ($1, NOW()) RETURNING id`, planID,
	).Scan(&partID); err != nil {
		t.Fatalf("insert plan_part: %v", err)
	}
	return
}

// TestMigration0021FlightAlertsCascade verifies the FKs added in 0021 cascade:
// deleting the user removes their flight_alerts rows.
func TestMigration0021FlightAlertsCascade(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()
	userID, tripID, planID, partID := seedPartChain(t, pool, "alert-cascade")

	if _, err := pool.Exec(ctx,
		`INSERT INTO flight_alerts (user_id, plan_part_id, plan_id, trip_id, ident, kind, status, message)
		 VALUES ($1, $2, $3, $4, 'BA286', 'gate', 'Scheduled', 'gate change')`,
		userID, partID, planID, tripID,
	); err != nil {
		t.Fatalf("insert flight_alert: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM flight_alerts WHERE user_id = $1`, userID,
	).Scan(&n); err != nil {
		t.Fatalf("count alerts: %v", err)
	}
	if n != 0 {
		t.Errorf("flight_alerts not cascaded on user delete: %d rows remain", n)
	}
}

// TestMigration0021PositionsPartNotNull verifies positions.plan_part_id is
// NOT NULL after 0021.
func TestMigration0021PositionsPartNotNull(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO positions (plan_part_id, ts, lat, lon) VALUES (NULL, NOW(), 1.0, 2.0)`)
	if err == nil {
		t.Fatal("expected NOT NULL violation inserting a position with null plan_part_id")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "plan_part_id") {
		t.Errorf("unexpected error (want plan_part_id not-null): %v", err)
	}
}

// TestMigration0021TripItUniqueIndex verifies the partial unique index rejects
// a duplicate (trip_id, tripit_uid) while allowing many empty uids.
func TestMigration0021TripItUniqueIndex(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()
	_, tripID, _, _ := seedPartChain(t, pool, "tripit-uniq")

	if _, err := pool.Exec(ctx,
		`INSERT INTO plans (trip_id, type, tripit_uid) VALUES ($1, 'flight', 'UID-1')`, tripID,
	); err != nil {
		t.Fatalf("first tripit plan: %v", err)
	}
	// Same (trip_id, tripit_uid) must be rejected.
	if _, err := pool.Exec(ctx,
		`INSERT INTO plans (trip_id, type, tripit_uid) VALUES ($1, 'flight', 'UID-1')`, tripID,
	); err == nil {
		t.Error("expected unique-violation on duplicate (trip_id, tripit_uid)")
	}
	// Empty uids are exempt (partial index) — two should coexist fine.
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx,
			`INSERT INTO plans (trip_id, type, tripit_uid) VALUES ($1, 'hotel', '')`, tripID,
		); err != nil {
			t.Fatalf("empty-uid plan %d should be allowed: %v", i, err)
		}
	}
}
