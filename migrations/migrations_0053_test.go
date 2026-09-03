package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

// TestMigration0053AccommodationKind verifies hotel_details carries the free-text
// 'kind' column that lets a stay say it is a campsite, a caravan park or the back
// of a van rather than implying a hotel. Existing rows default to '' ("not
// stated"), which the UI renders as the generic accommodation label.
func TestMigration0053AccommodationKind(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	owner := testsupport.InsertUser(t, pool, "m53owner", false, true)
	var tripID, planID, partID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner,
	).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'hotel') RETURNING id`, tripID,
	).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, starts_at)
		VALUES ($1, NOW()) RETURNING id`, planID,
	).Scan(&partID); err != nil {
		t.Fatalf("insert part: %v", err)
	}

	// A row inserted without naming 'kind' defaults to '' rather than NULL.
	if _, err := pool.Exec(ctx,
		`INSERT INTO hotel_details (plan_part_id, property_name) VALUES ($1, 'Test Campsite')`,
		partID,
	); err != nil {
		t.Fatalf("insert hotel_details: %v", err)
	}
	var kind string
	if err := pool.QueryRow(ctx,
		`SELECT kind FROM hotel_details WHERE plan_part_id = $1`, partID,
	).Scan(&kind); err != nil {
		t.Fatalf("select kind: %v", err)
	}
	if kind != "" {
		t.Fatalf("default kind = %q, want empty", kind)
	}

	// And it accepts arbitrary free text, because the long tail of places to
	// sleep does not fit a constrained set.
	if _, err := pool.Exec(ctx,
		`UPDATE hotel_details SET kind = 'Caravan park' WHERE plan_part_id = $1`, partID,
	); err != nil {
		t.Fatalf("update kind: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT kind FROM hotel_details WHERE plan_part_id = $1`, partID,
	).Scan(&kind); err != nil {
		t.Fatalf("re-select kind: %v", err)
	}
	if kind != "Caravan park" {
		t.Fatalf("kind = %q, want %q", kind, "Caravan park")
	}
}
