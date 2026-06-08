package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

func TestMigration0030(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	for _, tbl := range []string{"pending_shares", "notifications"} {
		if !tableExists(t, pool, tbl) {
			t.Errorf("expected table %q after up", tbl)
		}
	}
	if !columnExists(t, pool, "trips", "share_all_friends_role") {
		t.Error("trips.share_all_friends_role missing")
	}
	if !columnExists(t, pool, "plans", "share_all_friends") {
		t.Error("plans.share_all_friends missing")
	}

	// The passenger⇒viewer trigger must be GONE: inserting a plan_passenger no
	// longer creates a trip_members row.
	uid := testsupport.InsertUser(t, pool, "m30owner", false, true)
	pax := testsupport.InsertUser(t, pool, "m30pax", false, true)
	var tripID, planID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, uid).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2)`, planID, pax); err != nil {
		t.Fatalf("insert plan_passenger: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, pax).Scan(&n); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if n != 0 {
		t.Errorf("trigger should be dropped: got %d trip_members rows, want 0", n)
	}
}
