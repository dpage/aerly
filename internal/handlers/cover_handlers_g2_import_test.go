package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
)

// TestImportBadBodyG2 covers importTrip's parseIngestBody-failure branch:
// invalid JSON makes decode error → 400. (An empty-but-valid body fails later
// at the icalUpload guard, a different branch, so we send malformed JSON here.)
func TestImportBadBodyG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impbad", false)
	if w := e.req(t, "POST", "/api/trips/import", "??", owner); w.Code != http.StatusBadRequest {
		t.Errorf("invalid import body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	// Also cover the icalUpload guard: a valid JSON body with no iCal content.
	if w := e.req(t, "POST", "/api/trips/import", map[string]any{"text": "no calendar here"}, owner); w.Code != http.StatusBadRequest {
		t.Errorf("no-ical import = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestImportUnparseableICSG2 covers importics.Parse's read-error branch: the
// content sniffs as iCal (it starts with BEGIN:VCALENDAR) but carries a single
// line longer than the parser's 4 MiB scanner buffer, so the underlying scan
// errors ("token too long") and Parse returns an error → 400.
func TestImportUnparseableICSG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impparse", false)
	// One unbroken line of 5 MiB after the marker (no newlines) overflows the
	// bufio.Scanner buffer and makes Parse error.
	huge := "BEGIN:VCALENDAR\r\nX-LONG:" + strings.Repeat("A", 5*1024*1024)
	body := map[string]any{"text": huge}
	if w := e.req(t, "POST", "/api/trips/import", body, owner); w.Code != http.StatusBadRequest {
		t.Errorf("unparseable ics = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestImportUnrecognisedSourceG2 covers the MapAll-failure branch (422): a
// syntactically valid calendar that isn't a recognised TripIt/Kayak export.
func TestImportUnrecognisedSourceG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impunrec", false)
	ical := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Example//Unknown//EN\r\n" +
		"BEGIN:VEVENT\r\nUID:evt-1@example.com\r\nDTSTART:20260601T090000Z\r\n" +
		"DTEND:20260601T100000Z\r\nSUMMARY:Something generic\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	w := e.req(t, "POST", "/api/trips/import", map[string]any{"text": ical}, owner)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("unrecognised source = %d, want 422; body=%s", w.Code, w.Body.String())
	}
}

// TestImportCommitErrG2 covers commitImportedPlans' error branch surfacing as a
// 502: dropping plan_parts makes planops.Commit fail after the trip is created.
func TestImportCommitErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impcommit", false)
	g1dropTable(t, e, "plan_parts")
	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}
	if w := e.req(t, "POST", "/api/trips/import", body, owner); w.Code != http.StatusBadGateway {
		t.Errorf("commit err = %d, want 502; body=%s", w.Code, w.Body.String())
	}
}

// TestImportTripDTOErrG2 covers importTrip's tripDTO error branch: the trip is
// created and its plans committed, but building the response DTO fails because
// trip_tags is gone (TagsByTrip errors).
func TestImportTripDTOErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impdto", false)
	g1dropTable(t, e, "trip_tags")
	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}
	if w := e.req(t, "POST", "/api/trips/import", body, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("tripDTO err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestImportGeocodeDeriveAsyncG2 covers geocodeAndDeriveImportedTripAsync end to
// end: with a geocoder set, importing a TripIt export plots the parts and then
// derives the trip's country, which we observe by polling the trip's
// country_code until it is populated.
func TestImportGeocodeDeriveAsyncG2(t *testing.T) {
	e := setup(t, nil, nil)
	// Resolve every address to a fixed point and a fixed reverse-geocoded country
	// so the derive step has something to store.
	geo := fakeGeocoder{lat: 50.8489, lon: 4.3491, country: "be"}
	e.api.Geocoder = geo
	e.api.GeoResolver = geoResolver(geo)
	owner := e.user(t, "g2impgeo", false)

	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}
	w := e.req(t, "POST", "/api/trips/import", body, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", w.Code, w.Body.String())
	}
	res := decodeBody[api.ImportResultDTO](t, w)

	// Poll the trip's country_code until the async geocode+derive populates it.
	ctx := context.Background()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var cc string
		err := e.pool.QueryRow(ctx,
			`SELECT COALESCE(country_code, '') FROM trips WHERE id = $1`, res.Trip.ID).Scan(&cc)
		if err == nil && cc != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("trip country_code was never derived from the imported plans within the deadline")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestImportPlanExistsErrG2 covers commitImportedPlans' PlanExistsByTripItUID
// error branch surfacing as a 502: the trip is created, then the
// already-imported check errors because the plans.tripit_uid column is gone.
func TestImportPlanExistsErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impexists", false)
	// tripit_uid is matched by PlanExistsByTripItUID; dropping it makes that
	// check error for the first TripIt plan (which carries a UID).
	g1dropColumn(t, e, "plans", "tripit_uid")
	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}
	if w := e.req(t, "POST", "/api/trips/import", body, owner); w.Code != http.StatusBadGateway {
		t.Errorf("PlanExists err = %d, want 502; body=%s", w.Code, w.Body.String())
	}
}

// TestImportReuseTripByTripItIDErrG2 covers findOrCreateImportTrip's
// TripByTripItID non-NotFound error branch: dropping a column that TripByTripItID
// selects (but the rest of the import setup does not need) makes the lookup
// error, which propagates as a 500.
func TestImportReuseTripByTripItIDErrG2(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "g2impreuse", false)
	// tripit_id is the column matched by TripByTripItID; dropping it makes the
	// query error (not a clean NotFound) inside findOrCreateImportTrip.
	g1dropColumn(t, e, "trips", "tripit_id")
	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}
	if w := e.req(t, "POST", "/api/trips/import", body, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("TripByTripItID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}
