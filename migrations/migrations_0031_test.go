package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/migrations"
	"github.com/dpage/aerly/internal/testsupport"
)

func TestMigration0031CleansLegacyViewers(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "m31owner", false, true)
	pax := testsupport.InsertUser(t, pool, "m31pax", false, true)         // legacy: viewer + plan passenger, not trip passenger
	keep := testsupport.InsertUser(t, pool, "m31keep", false, true)       // deliberate viewer, NOT a passenger -> must survive
	tripPax := testsupport.InsertUser(t, pool, "m31trippax", false, true) // viewer + trip passenger -> must survive

	var tripID, planID int64
	if err := pool.QueryRow(ctx, `INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner).Scan(&tripID); err != nil {
		t.Fatalf("trip: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID).Scan(&planID); err != nil {
		t.Fatalf("plan: %v", err)
	}
	// Seed the three viewer rows.
	for _, u := range []int64{pax, keep, tripPax} {
		if _, err := pool.Exec(ctx, `INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1,$2,'viewer')`, tripID, u); err != nil {
			t.Fatalf("member %d: %v", u, err)
		}
	}
	// pax: a plan passenger (legacy trigger shape). tripPax: a trip passenger + a plan passenger.
	if _, err := pool.Exec(ctx, `INSERT INTO plan_passengers (plan_id, user_id, via_trip) VALUES ($1,$2,false)`, planID, pax); err != nil {
		t.Fatalf("pax plan_passenger: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trip_passengers (trip_id, user_id) VALUES ($1,$2)`, tripID, tripPax); err != nil {
		t.Fatalf("trip_passenger: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO plan_passengers (plan_id, user_id, via_trip) VALUES ($1,$2,true)`, planID, tripPax); err != nil {
		t.Fatalf("tripPax plan_passenger: %v", err)
	}

	// Apply the 0031 up SQL against the seeded data.
	up, _ := readUpDown(t, "0031_cleanup_legacy_passenger_viewers")
	if _, err := pool.Exec(ctx, up); err != nil {
		t.Fatalf("apply 0031 up: %v", err)
	}

	has := func(u int64) bool {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, u).Scan(&n); err != nil {
			t.Fatalf("count %d: %v", u, err)
		}
		return n > 0
	}
	if has(pax) {
		t.Error("legacy passenger-derived viewer row should have been deleted")
	}
	if !has(keep) {
		t.Error("deliberate viewer (no passenger) must survive")
	}
	if !has(tripPax) {
		t.Error("trip-passenger viewer must survive")
	}
}

var _ = migrations.FS
