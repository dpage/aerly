package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dpage/aerly/internal/testsupport"
	"github.com/dpage/aerly/migrations"
)

// seed0055 inserts a user, trip and hotel plan, returning the plan id.
func seed0055(t *testing.T, pool *pgxpool.Pool, username string) (uid, tripID, planID int64) {
	t.Helper()
	ctx := context.Background()
	uid = testsupport.InsertUser(t, pool, username, false, true)
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, uid).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'hotel') RETURNING id`, tripID).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	return uid, tripID, planID
}

// TestMigration0055ZoneSourceConstraints verifies a zone and its source must
// agree: a real zone needs confirmed/inferred, an empty one unknown/floating.
func TestMigration0055ZoneSourceConstraints(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()
	uid, _, planID := seed0055(t, pool, "m55user")

	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, start_tz, start_tz_source)
		 VALUES ($1, NOW(), 'Europe/Paris', 'confirmed')`, planID); err != nil {
		t.Fatalf("zone + confirmed should insert: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, start_tz, start_tz_source)
		 VALUES ($1, NOW(), '', 'confirmed')`, planID); err == nil {
		t.Fatal("empty zone + confirmed should violate the constraint")
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, start_tz, start_tz_source)
		 VALUES ($1, NOW(), 'Europe/Paris', 'floating')`, planID); err == nil {
		t.Fatal("zone + floating should violate the constraint")
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, end_tz, end_tz_source)
		 VALUES ($1, NOW(), '', 'inferred')`, planID); err == nil {
		t.Fatal("empty end zone + inferred should violate the constraint")
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at, start_tz_source)
		 VALUES ($1, NOW(), 'guessed')`, planID); err == nil {
		t.Fatal("an unrecognised source should violate the constraint")
	}

	// No zone and no source takes the defaults, which satisfy the constraint.
	var startSrc, endSrc string
	if err := pool.QueryRow(ctx,
		`INSERT INTO plan_parts (plan_id, starts_at) VALUES ($1, NOW())
		 RETURNING start_tz_source, end_tz_source`, planID).Scan(&startSrc, &endSrc); err != nil {
		t.Fatalf("default insert: %v", err)
	}
	if startSrc != "unknown" || endSrc != "unknown" {
		t.Fatalf("defaults = %q/%q, want unknown/unknown", startSrc, endSrc)
	}

	var homeTZ string
	if err := pool.QueryRow(ctx,
		`SELECT home_tz FROM users WHERE id = $1`, uid).Scan(&homeTZ); err != nil {
		t.Fatalf("select home_tz: %v", err)
	}
	if homeTZ != "" {
		t.Fatalf("home_tz = %q, want ''", homeTZ)
	}
}

// TestMigration0055Backfill reverts 0055, seeds rows in the old shape, then
// re-applies it to check the backfill classifies each existing zone.
func TestMigration0055Backfill(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()
	_, tripID, hotelPlan := seed0055(t, pool, "m55backfill")

	down, err := migrations.FS.ReadFile("0055_tz_source.down.sql")
	if err != nil {
		t.Fatalf("read down: %v", err)
	}
	up, err := migrations.FS.ReadFile("0055_tz_source.up.sql")
	if err != nil {
		t.Fatalf("read up: %v", err)
	}
	if _, err := pool.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply down: %v", err)
	}

	var flightPlan int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID).Scan(&flightPlan); err != nil {
		t.Fatalf("insert flight plan: %v", err)
	}
	insertPart := func(planID int64, startTZ, endTZ string, withEnd bool) int64 {
		t.Helper()
		var id int64
		q := `INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_tz, end_tz)
		      VALUES ($1, NOW(), CASE WHEN $4 THEN NOW() + interval '1 hour' END, $2, $3) RETURNING id`
		if err := pool.QueryRow(ctx, q, planID, startTZ, endTZ, withEnd).Scan(&id); err != nil {
			t.Fatalf("insert part: %v", err)
		}
		return id
	}
	zoned := insertPart(hotelPlan, "Europe/Paris", "Europe/Paris", true)
	floating := insertPart(hotelPlan, "", "", true)
	noEnd := insertPart(hotelPlan, "", "", false)
	resolved := insertPart(flightPlan, "", "", true)
	unresolved := insertPart(flightPlan, "", "", true)
	for id, res := range map[int64]bool{resolved: true, unresolved: false} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO flight_details (plan_part_id, ident, scheduled_out, scheduled_in, resolved)
			 VALUES ($1, 'ZZ1', NOW(), NOW(), $2)`, id, res); err != nil {
			t.Fatalf("insert flight_details: %v", err)
		}
	}

	var feedID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trip_feeds (trip_id, url) VALUES ($1, 'https://example.com/a.ics') RETURNING id`,
		tripID).Scan(&feedID); err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO trip_feed_events (feed_id, uid, starts_at, start_tz)
		 VALUES ($1, 'zoned', NOW(), 'Asia/Tokyo'), ($1, 'bare', NOW(), '')`, feedID); err != nil {
		t.Fatalf("insert feed events: %v", err)
	}

	if _, err := pool.Exec(ctx, string(up)); err != nil {
		t.Fatalf("re-apply up: %v", err)
	}

	want := map[int64][2]string{
		zoned:      {"confirmed", "confirmed"},
		floating:   {"floating", "floating"},
		noEnd:      {"floating", "unknown"},
		resolved:   {"unknown", "unknown"},
		unresolved: {"floating", "floating"},
	}
	for id, w := range want {
		var s, e string
		if err := pool.QueryRow(ctx,
			`SELECT start_tz_source, end_tz_source FROM plan_parts WHERE id = $1`, id).Scan(&s, &e); err != nil {
			t.Fatalf("select part %d: %v", id, err)
		}
		if s != w[0] || e != w[1] {
			t.Errorf("part %d sources = %q/%q, want %q/%q", id, s, e, w[0], w[1])
		}
	}

	for uid, w := range map[string]string{"zoned": "inferred", "bare": "unknown"} {
		var got string
		if err := pool.QueryRow(ctx,
			`SELECT start_tz_source FROM trip_feed_events WHERE uid = $1`, uid).Scan(&got); err != nil {
			t.Fatalf("select feed event %s: %v", uid, err)
		}
		if got != w {
			t.Errorf("feed event %s source = %q, want %q", uid, got, w)
		}
	}
}
