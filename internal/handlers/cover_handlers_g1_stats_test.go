package handlers

import (
	"net/http"
	"testing"
	"time"
)

// TestListMyFlightsG1 drives GET /api/me/flights for the owner of a flight
// plan: the rollup should project the plan's flight part into a FlightDTO.
func TestListMyFlightsG1(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g1flightsowner", false)
	tripID := newTrip(t, e, owner, "Stats trip")
	planID := newFlightPlan(t, e, tripID, owner, "BA286", time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC))

	// The rollup is scoped to plans the viewer is a passenger on.
	if pw := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/passengers",
		map[string]any{"user_id": owner}, owner); pw.Code != http.StatusOK {
		t.Fatalf("add passenger: %d %s", pw.Code, pw.Body.String())
	}

	w := e.req(t, "GET", "/api/me/flights", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	out := decodeBody[[]map[string]any](t, w)
	if len(out) != 1 {
		t.Fatalf("flights = %d, want 1; body=%s", len(out), w.Body.String())
	}
	if out[0]["ident"] != "BA286" {
		t.Errorf("ident = %v, want BA286", out[0]["ident"])
	}

	// A user with no flights gets an empty (non-null) array.
	stranger := e.user(t, "g1flightsstranger", false)
	w = e.req(t, "GET", "/api/me/flights", nil, stranger)
	if w.Code != http.StatusOK {
		t.Fatalf("stranger code = %d, want 200", w.Code)
	}
	if got := decodeBody[[]map[string]any](t, w); len(got) != 0 {
		t.Errorf("stranger flights = %d, want 0", len(got))
	}
}
