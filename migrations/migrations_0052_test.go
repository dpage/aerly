package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestMigration0052CheckinPrefDefaultsOff verifies the new alert_prefs column
// exists and defaults to FALSE, so an existing user is opted out of check-in
// reminders until they ask for them (issue #119).
func TestMigration0052CheckinPrefDefaultsOff(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	uid := testsupport.InsertUser(t, pool, "m52user", false, true)
	if _, err := pool.Exec(ctx,
		`INSERT INTO alert_prefs (user_id) VALUES ($1)`, uid); err != nil {
		t.Fatalf("insert alert_prefs: %v", err)
	}
	var checkin bool
	if err := pool.QueryRow(ctx,
		`SELECT checkin FROM alert_prefs WHERE user_id = $1`, uid).Scan(&checkin); err != nil {
		t.Fatalf("select checkin: %v", err)
	}
	if checkin {
		t.Fatal("checkin should default to FALSE")
	}
}

// TestMigration0052CheckinSentCascades verifies the dedupe table's foreign keys
// clean up after themselves: deleting the part takes its sent rows with it, so
// a re-created part isn't wrongly considered already reminded.
func TestMigration0052CheckinSentCascades(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	uid := testsupport.InsertUser(t, pool, "m52cascade", false, true)
	var tripID, planID, partID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, uid).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at) VALUES ($1, NOW()) RETURNING id`, planID).Scan(&partID); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO flight_checkin_sent (plan_part_id, user_id) VALUES ($1, $2)`, partID, uid); err != nil {
		t.Fatalf("insert sent row: %v", err)
	}
	// Idempotent: the same pair twice is a no-op, not a constraint error.
	if _, err := pool.Exec(ctx,
		`INSERT INTO flight_checkin_sent (plan_part_id, user_id) VALUES ($1, $2)
		 ON CONFLICT DO NOTHING`, partID, uid); err != nil {
		t.Fatalf("re-insert sent row: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM plan_parts WHERE id = $1`, partID); err != nil {
		t.Fatalf("delete part: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM flight_checkin_sent WHERE plan_part_id = $1`, partID).Scan(&n); err != nil {
		t.Fatalf("count sent rows: %v", err)
	}
	if n != 0 {
		t.Fatalf("sent rows survived the part: %d", n)
	}
}
