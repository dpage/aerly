package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/providers"
)

// g2makePlanOfType creates a one-part plan of the given type with its detail
// block populated, exercising toCreatePartPayload's per-type branches, and
// returns the trip id, plan id and the part id.
func g2makePlanOfType(t *testing.T, e *testEnv, uid int64, planType string, detail map[string]any) (tid, pid, partID int64) {
	t.Helper()
	tid = newTrip(t, e, uid, "Trip-"+planType)
	part := map[string]any{
		"type": planType, "starts_at": g2planOut, "ends_at": g2planOut.Add(2 * time.Hour),
		"start_label": "Start", "end_label": "End",
	}
	if detail != nil {
		part[planType] = detail
	}
	w := e.req(t, "POST", "/api/trips/"+itoa(tid)+"/plans", map[string]any{
		"type": planType, "title": planType,
		"parts": []map[string]any{part},
	}, uid)
	if w.Code != http.StatusCreated {
		t.Fatalf("create %s plan = %d %s", planType, w.Code, w.Body.String())
	}
	pid = int64(decodeBody[map[string]any](t, w)["id"].(float64))
	partID = g2firstPartID(t, e, tid, pid, uid)
	return tid, pid, partID
}

// TestToCreatePartPayloadAllTypesG2 creates a plan of every non-flight type with
// its detail block, covering toCreatePartPayload's per-type satellite branches
// (hotel, train, ground, dining, excursion, ice_cream, meeting, event).
func TestToCreatePartPayloadAllTypesG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2types", false)
	pax := 2
	cases := map[string]map[string]any{
		"hotel":     {"property_name": "Hotel Example", "address": "1 Test St", "guests": pax, "standard_checkin": "15:00", "standard_checkout": "11:00"},
		"train":     {"operator": "Test Rail", "service_no": "T1", "coach": "A", "seat": "1", "class": "std", "platform": "2"},
		"ground":    {"provider": "Test Cars", "phone": "555-0100", "vehicle": "Saloon", "driver": "Test Driver", "pax": pax},
		"dining":    {"party_size": pax, "reservation_name": "Test User", "phone": "555-0100"},
		"excursion": {"provider": "Test Tours", "ticket_count": pax},
		"ice_cream": {"rating": 4, "what_ordered": "Vanilla"},
		"meeting":   {"location": "Room 1", "organiser": "Test User", "platform": "Zoom"},
		"event":     {"performer": "Test Band", "category": "music", "venue_area": "Floor", "url": "https://example.com"},
	}
	for typ, detail := range cases {
		_, _, _ = g2makePlanOfType(t, e, owner, typ, detail)
	}
}

// TestUpdatePlanPartDetailEditsG2 covers applyPartDetailEdit's per-type branches
// (hotel/train/ground/dining/excursion) and the ice-cream edit path with rating
// clamping, plus clampNonNegative's negative-flooring branch.
func TestUpdatePlanPartDetailEditsG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2detail", false)

	// Hotel: edit with a negative guests count → clampNonNegative floors to 0.
	_, _, hPart := g2makePlanOfType(t, e, owner, "hotel", map[string]any{"property_name": "H"})
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(hPart), map[string]any{
		"hotel": map[string]any{"property_name": "Renamed", "guests": -3},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("hotel edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Train.
	_, _, tPart := g2makePlanOfType(t, e, owner, "train", map[string]any{"operator": "R"})
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(tPart), map[string]any{
		"train": map[string]any{"seat": "12A"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("train edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Ground (negative pax → clamped).
	_, _, gPart := g2makePlanOfType(t, e, owner, "ground", map[string]any{"provider": "C"})
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(gPart), map[string]any{
		"ground": map[string]any{"driver": "Test Driver", "pax": -1},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("ground edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Dining.
	_, _, dPart := g2makePlanOfType(t, e, owner, "dining", map[string]any{"reservation_name": "X"})
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(dPart), map[string]any{
		"dining": map[string]any{"party_size": 4},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("dining edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Excursion.
	_, _, xPart := g2makePlanOfType(t, e, owner, "excursion", map[string]any{"provider": "T"})
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(xPart), map[string]any{
		"excursion": map[string]any{"ticket_count": 3},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("excursion edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Ice cream: rating above 5 clamps to 5.
	_, _, iPart := g2makePlanOfType(t, e, owner, "ice_cream", map[string]any{"rating": 3})
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(iPart), map[string]any{
		"ice_cream": map[string]any{"rating": 99, "what_ordered": "Pistachio"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("ice_cream edit high = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	// Ice cream: rating below 0 clamps to 0.
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(iPart), map[string]any{
		"ice_cream": map[string]any{"rating": -5},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("ice_cream edit low = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePlanPartFlightManualRouteG2 covers applyFlightEdit's manual-route
// branch (an IATA edit on an unresolved flight) via manualRouteUpdate, plus
// normalizeIdent's no-op when the ident is unchanged. No resolver is configured,
// so the freshly-created flight is unresolved.
func TestUpdatePlanPartFlightManualRouteG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2flmanual", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", g2planOut)
	partID := g2firstPartID(t, e, tid, pid, owner)

	// Edit the origin/dest IATA on the unresolved flight → manualRouteUpdate.
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"flight": map[string]any{"origin_iata": "MAN", "dest_iata": "JFK"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("flight manual route edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePlanPartFlightReresolveG2 covers applyFlightEdit's ident-change
// re-resolve branch with a resolver configured (reresolveFlightPart success).
func TestUpdatePlanPartFlightReresolveG2(t *testing.T) {
	rf := &providers.ResolvedFlight{
		Ident: "BA999", OriginIATA: "LHR", DestIATA: "JFK",
		ScheduledOut: g2planOut, ScheduledIn: g2planOut.Add(7 * time.Hour),
	}
	e := setup(t, &fakeResolver{rf: rf}, nil)
	owner := e.user(t, "g2flreres", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", g2planOut)
	partID := g2firstPartID(t, e, tid, pid, owner)

	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"flight": map[string]any{"ident": "ba 999"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("flight re-resolve edit = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePlanPartFlightReresolveNoResolverG2 covers reresolveFlightPart's
// no-resolver fallback (the flight is marked unresolved with the new ident).
func TestUpdatePlanPartFlightReresolveNoResolverG2(t *testing.T) {
	e := setup(t, nil, nil) // no resolver
	owner := e.user(t, "g2flnores", false)
	tid := newTrip(t, e, owner, "Trip")
	pid := newFlightPlan(t, e, tid, owner, "BA286", g2planOut)
	partID := g2firstPartID(t, e, tid, pid, owner)
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"flight": map[string]any{"ident": "LH400"},
	}, owner); w.Code != http.StatusOK {
		t.Errorf("flight re-resolve no-resolver = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePlanPartGeocodeG2 covers updatePlanPart's geocode fallback paths: a
// changed address on an unpinned endpoint triggers a geocode, and the unpin
// path re-geocodes. A fakeGeocoder is wired so the resolver's Endpoint method
// resolves.
func TestUpdatePlanPartGeocodeG2(t *testing.T) {
	e := setup(t, nil, nil)
	geo := fakeGeocoder{lat: 51.5, lon: -0.1}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
	owner := e.user(t, "g2ppgeo", false)
	_, _, partID := g2makePlanOfType(t, e, owner, "hotel", map[string]any{"property_name": "H"})

	// Change the start address → geocode runs (startAddrChanged).
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"start_address": "10 New Street, Example City",
		"end_address":   "20 Other Road, Example City",
	}, owner); w.Code != http.StatusOK {
		t.Errorf("address edit geocode = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	// Pin then unpin the start endpoint to exercise the unpin re-geocode path.
	pinned := true
	unpinned := false
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"start_coords_pinned": pinned, "start_lat": 1.0, "start_lon": 2.0,
	}, owner); w.Code != http.StatusOK {
		t.Errorf("pin = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{
		"start_coords_pinned": unpinned,
	}, owner); w.Code != http.StatusOK {
		t.Errorf("unpin re-geocode = %d, want 200; body=%s", w.Code, w.Body.String())
	}
}

// TestUpdatePlanPartStoreErrG2 covers updatePlanPart's UpdatePlanPart store-error
// branch.
func TestUpdatePlanPartStoreErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	admin := e.user(t, "g2ppserr", true)
	tid := newTrip(t, e, admin, "Trip")
	pid := g2dining(t, e, tid, admin)
	partID := g2firstPartID(t, e, tid, pid, admin)
	// UpdatePlanPart writes plan_parts.status; dropping that column errors the
	// update while PlanIDForPart (edit-check, reads id) still works.
	g1dropColumn(t, e, "plan_parts", "status")
	status := "confirmed"
	if w := e.req(t, "PATCH", "/api/plan-parts/"+itoa(partID), map[string]any{"status": status}, admin); w.Code != http.StatusInternalServerError {
		t.Errorf("update part store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestPlanHelperEdgeCasesG2 covers the small assembly helpers directly:
// strPtrIfSet (empty → nil), defaultTime (zero → fallback), endOr (nil →
// fallback), normalizeIdent, and clampNonNegative (nil pass-through).
func TestPlanHelperEdgeCasesG2(t *testing.T) {
	if strPtrIfSet("") != nil {
		t.Error("strPtrIfSet(\"\") should be nil")
	}
	if got := strPtrIfSet("x"); got == nil || *got != "x" {
		t.Errorf("strPtrIfSet(x) = %v", got)
	}
	fallback := time.Now()
	if got := defaultTime(time.Time{}, fallback); !got.Equal(fallback) {
		t.Error("defaultTime(zero) should be fallback")
	}
	set := fallback.Add(time.Hour)
	if got := defaultTime(set, fallback); !got.Equal(set) {
		t.Error("defaultTime(set) should be the set value")
	}
	if got := endOr(nil, fallback); !got.Equal(fallback) {
		t.Error("endOr(nil) should be fallback")
	}
	if got := endOr(&set, fallback); !got.Equal(set) {
		t.Error("endOr(set) should be the value")
	}
	if got := normalizeIdent("ba 286"); got != "BA286" {
		t.Errorf("normalizeIdent = %q, want BA286", got)
	}
	if clampNonNegative(nil) != nil {
		t.Error("clampNonNegative(nil) should be nil")
	}
}

// TestPlanAuthHelpersNilUserG2 covers requirePlanEdit's and requirePartEdit's
// nil-user guards (defensive, unreachable via the authed mux) by calling them
// directly.
func TestPlanAuthHelpersNilUserG2(t *testing.T) {
	e := setup(t, nil, nil)
	ctx := context.Background()
	if err := e.api.requirePlanEdit(ctx, 1, nil, httptest.NewRecorder()); err == nil {
		t.Error("requirePlanEdit(nil) should error")
	}
	if err := e.api.requirePartEdit(ctx, 1, nil, httptest.NewRecorder()); err == nil {
		t.Error("requirePartEdit(nil) should error")
	}
}
