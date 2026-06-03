package handlers

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
)

// TestImportICalCreatesTrip imports a whole TripIt .ics: a trip is created from
// the envelope (name/dates) and all bookings land in it — no LLM, no current
// trip. Re-importing the same file is idempotent: the trip is reused and every
// plan is skipped as already-imported.
func TestImportICalCreatesTrip(t *testing.T) {
	e := setup(t, nil, nil) // no extractor
	owner := e.user(t, "owner", false)
	body := map[string]any{"text": readICS(t, "pgconfeu_2016.ics")}

	w := e.req(t, "POST", "/api/trips/import", body, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("import = %d: %s", w.Code, w.Body.String())
	}
	res := decodeBody[api.ImportResultDTO](t, w)

	if res.Trip.Name != "PGConf.EU 2016" {
		t.Errorf("trip name = %q, want PGConf.EU 2016", res.Trip.Name)
	}
	// 4 flights + 2 taxis + 1 hotel.
	if res.Added != 7 || res.Skipped != 0 {
		t.Fatalf("first import added=%d skipped=%d, want 7/0", res.Added, res.Skipped)
	}

	// Re-import the same .ics → same trip, nothing added, everything skipped.
	w = e.req(t, "POST", "/api/trips/import", body, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("re-import = %d: %s", w.Code, w.Body.String())
	}
	res2 := decodeBody[api.ImportResultDTO](t, w)
	if res2.Trip.ID != res.Trip.ID {
		t.Errorf("re-import made a new trip %d (was %d) — not idempotent", res2.Trip.ID, res.Trip.ID)
	}
	if res2.Added != 0 || res2.Skipped != 7 {
		t.Errorf("re-import added=%d skipped=%d, want 0/7", res2.Added, res2.Skipped)
	}

	// The trip really holds the 7 plans (and only 7 after the re-import).
	gw := e.req(t, "GET", fmt.Sprintf("/api/trips/%d", res.Trip.ID), nil, owner)
	if gw.Code != http.StatusOK {
		t.Fatalf("get trip = %d: %s", gw.Code, gw.Body.String())
	}
	trip := decodeBody[struct {
		Plans []api.PlanDTO `json:"plans"`
	}](t, gw)
	if len(trip.Plans) != 7 {
		t.Errorf("trip has %d plans after re-import, want 7", len(trip.Plans))
	}
}

// TestImportRejectsNonICal: posting something that isn't iCal is a 400, not a
// silently-empty trip.
func TestImportRejectsNonICal(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	w := e.req(t, "POST", "/api/trips/import", map[string]any{"text": "just some text"}, owner)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-ical import = %d, want 400: %s", w.Code, w.Body.String())
	}
}
