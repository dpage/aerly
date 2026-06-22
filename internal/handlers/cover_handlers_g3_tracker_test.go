package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
)

// TestG3ParseWindow exercises parseWindow's branches directly: empty, day
// suffix (valid + invalid + non-positive), stdlib duration (valid + invalid +
// non-positive).
func TestG3ParseWindow(t *testing.T) {
	cases := []struct {
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"", 0, false},
		{"  ", 0, false},
		{"7d", 7 * 24 * time.Hour, true},
		{"0d", 0, false},
		{"-3d", 0, false},
		{"xd", 0, false},
		{"12h", 12 * time.Hour, true},
		{"0s", 0, false},
		{"-1h", 0, false},
		{"garbage", 0, false},
	}
	for _, c := range cases {
		got, ok := parseWindow(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("parseWindow(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// TestG3TrackerAbsoluteDateRange drives the from/to date branch of getTracker:
// absolute date params (which take precedence over the duration window) select
// an in-range part. Covers the fromDate/toDate switch arm including the
// to-end-of-day push.
func TestG3TrackerAbsoluteDateRange(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3tdrowner", false)
	// A flight arriving on a fixed day well away from now, so only the absolute
	// date range can include it.
	arr := time.Date(2027, 3, 15, 12, 0, 0, 0, time.UTC)
	_, _, partID := seedFlightPart(t, e, owner, "DATE1", arr.Add(-3*time.Hour), arr)

	// from only.
	w := e.req(t, "GET", "/api/tracker?from=2027-03-14", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("from-only = %d, body=%s", w.Code, w.Body.String())
	}
	// to only (inclusive end-of-day).
	w = e.req(t, "GET", "/api/tracker?to=2027-03-16", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("to-only = %d", w.Code)
	}
	// Both, framing the arrival day.
	w = e.req(t, "GET", "/api/tracker?from=2027-03-14&to=2027-03-16", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("from+to = %d", w.Code)
	}
	got := decodeBody[api.TrackerResponseDTO](t, w).Parts
	found := false
	for _, p := range got {
		if p.ID == partID {
			found = true
		}
	}
	if !found {
		t.Errorf("absolute date range did not include the in-range part: %+v", got)
	}
}

// TestG3TrackerExplicitWindow drives the explicit window_before/window_after
// branch (parsed via parseWindow, ok=true), bypassing the default.
func TestG3TrackerExplicitWindow(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3tewowner", false)
	now := time.Now()
	_, _, partID := seedFlightPart(t, e, owner, "WIN1", now.Add(-time.Hour), now.Add(2*time.Hour))

	w := e.req(t, "GET", "/api/tracker?window_before=1d&window_after=1d", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("explicit window = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[api.TrackerResponseDTO](t, w).Parts
	if len(got) != 1 || got[0].ID != partID {
		t.Errorf("explicit window did not return the in-window part: %+v", got)
	}
}

// TestG3TrackerStoreErr drives the ConvergencePartsAll store-error 500.
func TestG3TrackerStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3tserr", false)
	g1dropTable(t, e, "plan_parts")
	if w := e.req(t, "GET", "/api/tracker", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("tracker store err = %d, want 500", w.Code)
	}
}

// TestG3TrackerTagSpanStoreErr drives the TaggedTripSpan store-error 500: a tag
// with no explicit window, but the tag table is gone.
func TestG3TrackerTagSpanStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3ttsspan", false)
	g1dropTable(t, e, "trip_tags")
	if w := e.req(t, "GET", "/api/tracker?tag=ski", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("tag span store err = %d, want 500", w.Code)
	}
}

// TestG3TrackerDTOPositionsStoreErr drives the LatestPartPositions error inside
// trackerPartDTOs (107), which getTracker surfaces as a 500 (87): a flight part
// is in-window so ConvergencePartsAll succeeds, but the positions table is gone.
func TestG3TrackerDTOPositionsStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3tdpos", false)
	now := time.Now()
	seedFlightPart(t, e, owner, "POSERR", now.Add(-time.Hour), now.Add(2*time.Hour))
	g1dropTable(t, e, "positions")
	if w := e.req(t, "GET", "/api/tracker", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("tracker positions store err = %d, want 500", w.Code)
	}
}

// TestG3TrackerDTOPassengersStoreErr drives the PassengersByPlan error inside
// trackerPartDTOs (130): ConvergencePartsAll + positions succeed, but
// plan_passengers is gone.
func TestG3TrackerDTOPassengersStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3tdpax", false)
	now := time.Now()
	seedFlightPart(t, e, owner, "PAXERR", now.Add(-time.Hour), now.Add(2*time.Hour))
	g1dropTable(t, e, "plan_passengers")
	if w := e.req(t, "GET", "/api/tracker", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("tracker passengers store err = %d, want 500", w.Code)
	}
}

// TestG3GetTrackerPartDetailStoreErr drives the detail-build error in
// getTrackerPart (206) via trackerPartDetailDTO's LatestPartPositions (229):
// TrackerPartByID + PlanPartByID succeed (they don't touch positions), but the
// flight branch's position lookup fails once positions is dropped.
func TestG3GetTrackerPartDetailStoreErr(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3gtpderr", false)
	now := time.Now()
	_, _, partID := seedFlightPart(t, e, owner, "DETERR", now.Add(-time.Hour), now.Add(2*time.Hour))
	g1dropTable(t, e, "positions")
	if w := e.req(t, "GET", "/api/tracker/part/"+itoa(partID), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("tracker part detail store err = %d, want 500", w.Code)
	}
}

// TestG3GetTrackerPartHappy drives getTrackerPart for a visible flight part:
// returns the part with its flight detail (covers the flight branch of
// trackerPartDetailDTO).
func TestG3GetTrackerPartHappy(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3gtpowner", false)
	now := time.Now()
	_, _, partID := seedFlightPart(t, e, owner, "PART1", now.Add(-time.Hour), now.Add(2*time.Hour))

	w := e.req(t, "GET", "/api/tracker/part/"+itoa(partID), nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("get tracker part = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[api.PlanPartDTO](t, w)
	if got.ID != partID || got.Flight == nil || got.Flight.Ident != "PART1" {
		t.Errorf("unexpected part dto: %+v", got)
	}
}

// TestG3GetTrackerPartBadID drives the invalid-id 400.
func TestG3GetTrackerPartBadID(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3gtpbad", false)
	if w := e.req(t, "GET", "/api/tracker/part/notanumber", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad id = %d, want 400", w.Code)
	}
}

// TestG3TrackerWithPassenger covers the passenger-population loops in
// trackerPartDTOs (the userIDset passenger walk and the per-part Passengers
// append): a tracked flight with a passenger surfaces them on the DTO.
func TestG3TrackerWithPassenger(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3twpowner", false)
	now := time.Now()
	_, planID, partID := seedFlightPart(t, e, owner, "PAX1", now.Add(-time.Hour), now.Add(2*time.Hour))

	// Add the owner as a passenger on the flight plan (self is always allowed).
	if pw := e.req(t, "POST", "/api/plans/"+itoa(planID)+"/passengers",
		map[string]any{"user_id": owner}, owner); pw.Code != http.StatusOK {
		t.Fatalf("add passenger: %d %s", pw.Code, pw.Body.String())
	}

	w := e.req(t, "GET", "/api/tracker", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("tracker = %d, body=%s", w.Code, w.Body.String())
	}
	got := decodeBody[api.TrackerResponseDTO](t, w).Parts
	var part *api.PlanPartDTO
	for i := range got {
		if got[i].ID == partID {
			part = &got[i]
		}
	}
	if part == nil {
		t.Fatalf("part %d not in tracker results", partID)
	}
	found := false
	for _, p := range part.Passengers {
		if p.ID == owner {
			found = true
		}
	}
	if !found {
		t.Errorf("passenger not surfaced on tracker part: %+v", part.Passengers)
	}
}

// TestG3GetTrackerPartHidden drives the visibility 404: a part hidden from the
// viewer is reported as not-found, not forbidden (no existence leak).
func TestG3GetTrackerPartHidden(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g3gtphowner", false)
	viewer := e.user(t, "g3gtphview", false)
	now := time.Now()
	tripID, planID, partID := seedFlightPart(t, e, owner, "HIDDEN1", now.Add(-time.Hour), now.Add(2*time.Hour))
	addMember(t, e, tripID, viewer, "viewer")
	hideFrom(t, e, planID, viewer)

	if w := e.req(t, "GET", "/api/tracker/part/"+itoa(partID), nil, viewer); w.Code != http.StatusNotFound {
		t.Errorf("hidden part = %d, want 404", w.Code)
	}
}
