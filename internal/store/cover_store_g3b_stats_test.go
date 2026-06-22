package store

import (
	"testing"
	"time"
)

// TestG3bMyFlightsScanError covers the mid-row Scan-error body in MyFlights.
//
// The store's pool is a concrete *pgxpool.Pool with no mock seam, so the only
// way to drive the defensive `if err := rows.Scan(...); err != nil` branch is a
// real fault against the throwaway test database. We seed a genuine flight the
// viewer is a passenger on (so the query returns a row and rows.Next() is true),
// then ALTER the flight_details.scheduled_out column to text holding a value
// that is not a valid timestamp. The top-level Query still succeeds (the column
// still exists and the projection is valid), but decoding the text value into
// the required time.Time field (&f.ScheduledOut) fails part-way through the
// row, hitting the return inside the scan-error guard.
func TestG3bMyFlightsScanError(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	traveller := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "TS100",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "AAA", EndLabel: "BBB",
			Flight: &FlightDetail{
				Ident: "TS100", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "AAA", DestIATA: "BBB",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if err := s.AddPlanPassenger(ctx, plan.ID, traveller); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}

	// Break the scheduled_out column's type so the row scan into the required
	// time.Time field fails after the query itself has succeeded.
	if _, err := s.pool.Exec(ctx,
		`ALTER TABLE flight_details ALTER COLUMN scheduled_out TYPE text USING 'not-a-timestamp'`); err != nil {
		t.Fatalf("alter column: %v", err)
	}
	if _, err := s.MyFlights(ctx, traveller); err == nil {
		t.Error("MyFlights should fail when scheduled_out cannot be scanned into time.Time")
	}
}
