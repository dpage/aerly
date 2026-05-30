package store

import (
	"testing"
	"time"
)

// TestMyFlightsRebuildsFromPlanModel verifies the Statistics rollup source is
// rebuilt from the plan model (flight plan_parts the viewer is a passenger on)
// now that the legacy flights table is gone.
func TestMyFlightsRebuildsFromPlanModel(t *testing.T) {
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
		TripID: trip, Type: "flight", Title: "BA286",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "SFO",
			Flight: &FlightDetail{
				Ident: "BA286", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "SFO",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	// Not a passenger yet → absent from their rollup.
	if got, err := s.MyFlights(ctx, traveller); err != nil || len(got) != 0 {
		t.Fatalf("MyFlights before passenger = %d, %v; want 0", len(got), err)
	}

	if err := s.AddPlanPassenger(ctx, plan.ID, traveller); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}

	got, err := s.MyFlights(ctx, traveller)
	if err != nil {
		t.Fatalf("MyFlights: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("MyFlights = %d, want 1", len(got))
	}
	f := got[0]
	if f.Ident != "BA286" || f.OriginIATA != "LHR" || f.DestIATA != "SFO" {
		t.Errorf("flight carrier wrong: %+v", f.Flight)
	}
	if f.Status != "Scheduled" {
		t.Errorf("status = %q, want Scheduled", f.Status)
	}
	var found bool
	for _, id := range f.PassengerIDs {
		if id == traveller {
			found = true
		}
	}
	if !found {
		t.Errorf("passenger_ids %v should include traveller %d", f.PassengerIDs, traveller)
	}
}
