package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestMigration0051VehicleHireType verifies the widened plans_type_check
// admits 'vehicle_hire' and that its satellite table exists with the
// expected columns.
func TestMigration0051VehicleHireType(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "m51owner", false, true)
	var tripID, planID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner,
	).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'vehicle_hire') RETURNING id`, tripID,
	).Scan(&planID); err != nil {
		t.Fatalf("expected a vehicle_hire plan to insert: %v", err)
	}
	if planID == 0 {
		t.Fatal("expected a vehicle_hire plan to insert")
	}

	// The satellite table exists with the expected columns.
	var n int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'vehicle_hire_details'
		  AND column_name IN ('plan_part_id','category','vehicle','transmission',
		                      'fuel_policy','mileage','excess_amount',
		                      'excess_currency','deposit_amount','deposit_currency')`,
	).Scan(&n); err != nil {
		t.Fatalf("column query: %v", err)
	}
	if n != 10 {
		t.Fatalf("vehicle_hire_details columns = %d, want 10", n)
	}
}

// TestMigration0051RejectsUnknownType verifies plans_type_check still rejects
// a type that was never admitted.
func TestMigration0051RejectsUnknownType(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "m51rejowner", false, true)
	var tripID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner,
	).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'hovercraft')`, tripID,
	); err == nil {
		t.Fatal("expected plans_type_check to reject an unknown type")
	}
}
