package handlers

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestG4ParseICSPathID covers the pure-function edge cases of parseICSPathID
// that the routed feed handlers can't reach: a prefix that isn't present, an
// empty trailing segment, and a segment containing a slash.
func TestG4ParseICSPathID(t *testing.T) {
	if _, ok := parseICSPathID("/api/calendar/trip/5.ics", "/nope/"); ok {
		t.Error("missing prefix should be not-ok")
	}
	if _, ok := parseICSPathID("/api/calendar/trip/.ics", "/api/calendar/trip/"); ok {
		t.Error("empty id should be not-ok")
	}
	if _, ok := parseICSPathID("/api/calendar/trip/5/6.ics", "/api/calendar/trip/"); ok {
		t.Error("slash in id should be not-ok")
	}
	if id, ok := parseICSPathID("/api/calendar/trip/7.ics", "/api/calendar/trip/"); !ok || id != 7 {
		t.Errorf("valid id = %d, ok=%v, want 7,true", id, ok)
	}
}

// TestG4CalendarFeedStoreErrs covers the serverError (500) branches of the three
// token-authed feed handlers when the underlying event query fails (plan_parts
// dropped) — and the calendarTokenInfo serverError branch when the token lookup
// itself fails (calendar_tokens dropped).
func TestG4CalendarFeedStoreErrs(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "g4feedowner", false)
	trip := seedTrip(t, e, owner)
	plan := seedPlan(t, e, trip, owner, "G4 Flight")
	seedPart(t, e, plan)
	meTok, _ := e.store.CalendarToken(context.Background(), owner, "me", 0)
	tripTok, _ := e.store.CalendarToken(context.Background(), owner, "trip", trip)
	planTok, _ := e.store.CalendarToken(context.Background(), owner, "plan", plan)

	// Dropping plan_parts breaks the event queries but leaves token lookup intact.
	g1dropTable(t, e, "plan_parts")

	for _, c := range []struct {
		name, path string
	}{
		{"me", "/api/calendar/me.ics?token=" + meTok.Token},
		{"trip", "/api/calendar/trip/" + itoa(trip) + ".ics?token=" + tripTok.Token},
		{"plan", "/api/calendar/plan/" + itoa(plan) + ".ics?token=" + planTok.Token},
	} {
		if w := rawGet(e, c.path); w.Code != http.StatusInternalServerError {
			t.Errorf("%s feed store err = %d, want 500; body=%s", c.name, w.Code, w.Body.String())
		}
	}
}

// TestG4CalendarTokenInfoStoreErr covers the serverError branch of
// calendarTokenInfo: a generic (non-NotFound) error from CalendarTokenByValue.
func TestG4CalendarTokenInfoStoreErr(t *testing.T) {
	e := setup(t, nil, calCfg())
	e.user(t, "g4tokinfo", false)
	g1dropTable(t, e, "calendar_tokens")
	if w := rawGet(e, "/api/calendar/me.ics?token=anything"); w.Code != http.StatusInternalServerError {
		t.Errorf("token lookup store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4CalendarPlanBadPath covers the parseICSPathID-fail → 404 branch of
// calendarPlan (a non-numeric id segment).
func TestG4CalendarPlanBadPath(t *testing.T) {
	e := setup(t, nil, calCfg())
	if w := rawGet(e, "/api/calendar/plan/abc.ics?token=x"); w.Code != http.StatusNotFound {
		t.Errorf("bad plan id = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ListCalendarTokensStoreErr covers the serverError branch of
// listCalendarTokens.
func TestG4ListCalendarTokensStoreErr(t *testing.T) {
	e := setup(t, nil, calCfg())
	uid := e.user(t, "g4listtok", false)
	g1dropTable(t, e, "calendar_tokens")
	if w := e.req(t, "GET", "/api/calendar/tokens", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("list tokens store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IssueCalendarTokenBadBody covers the decode-error 400 branch of
// issueCalendarToken.
func TestG4IssueCalendarTokenBadBody(t *testing.T) {
	e := setup(t, nil, calCfg())
	uid := e.user(t, "g4issuebad", false)
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"bogus": 1}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("issue bad body = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestG4IssueCalendarTokenPlanScope covers the plan-scope path of
// issueCalendarToken: a 404 for an unviewable plan, and a happy-path issue whose
// feed URL is the plan feed (covering calendarFeedURL's plan case).
func TestG4IssueCalendarTokenPlanScope(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "g4planowner", false)
	trip := seedTrip(t, e, owner)
	plan := seedPlan(t, e, trip, owner, "G4 Plan")
	seedPart(t, e, plan)

	// Unviewable plan id → 404.
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "plan", "id": plan + 9999}, owner); w.Code != http.StatusNotFound {
		t.Errorf("plan token unseen = %d, want 404; body=%s", w.Code, w.Body.String())
	}

	// Viewable plan → 200 with a plan feed URL.
	w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "plan", "id": plan}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("plan token issue = %d; body=%s", w.Code, w.Body.String())
	}
	tok := decodeBody[map[string]any](t, w)
	if u, _ := tok["url"].(string); !strings.Contains(u, "/api/calendar/plan/"+itoa(plan)+".ics?token=") {
		t.Errorf("plan token url = %q, want plan feed url", u)
	}
}

// TestG4IssueCalendarTokenStoreErrs covers the serverError branches of
// issueCalendarToken: a CanViewTrip failure (trip_members dropped), a
// CanViewPlan failure (plans dropped), and a RegenerateCalendarToken failure for
// the "me" scope (calendar_tokens dropped, which skips the can-view checks).
func TestG4IssueCalendarTokenStoreErrs(t *testing.T) {
	// CanViewTrip error.
	e := setup(t, nil, calCfg())
	uid := e.user(t, "g4cvtrip", false)
	trip := seedTrip(t, e, uid)
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "trip", "id": trip}, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("CanViewTrip err = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	// CanViewPlan error.
	e2 := setup(t, nil, calCfg())
	uid2 := e2.user(t, "g4cvplan", false)
	trip2 := seedTrip(t, e2, uid2)
	plan2 := seedPlan(t, e2, trip2, uid2, "P")
	g1dropTable(t, e2, "plans")
	if w := e2.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "plan", "id": plan2}, uid2); w.Code != http.StatusInternalServerError {
		t.Errorf("CanViewPlan err = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	// RegenerateCalendarToken error for "me" scope (no can-view check runs).
	e3 := setup(t, nil, calCfg())
	uid3 := e3.user(t, "g4regen", false)
	g1dropTable(t, e3, "calendar_tokens")
	if w := e3.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "me"}, uid3); w.Code != http.StatusInternalServerError {
		t.Errorf("Regenerate err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4RevokeCalendarTokenStoreErr covers the serverError branch of
// revokeCalendarToken (a generic, non-NotFound failure).
func TestG4RevokeCalendarTokenStoreErr(t *testing.T) {
	e := setup(t, nil, calCfg())
	uid := e.user(t, "g4revstore", false)
	g1dropTable(t, e, "calendar_tokens")
	if w := e.req(t, "DELETE", "/api/calendar/tokens/sometoken", nil, uid); w.Code != http.StatusInternalServerError {
		t.Errorf("revoke store err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4DownloadFilename covers downloadFilename's blank-name fallback: a name
// made entirely of non-alphanumerics collapses to "trip".
func TestG4DownloadFilename(t *testing.T) {
	if got := downloadFilename("!!! ---", "ics"); got != "trip.ics" {
		t.Errorf("downloadFilename(blank) = %q, want trip.ics", got)
	}
}

// TestG4ExportCanViewTripErrs covers the canViewTrip store-error branches of
// exportTrip, exportTripPDF and exportTripsPDF (a non-superuser whose
// CanViewTrip query fails because trip_members is gone).
func TestG4ExportCanViewTripErrs(t *testing.T) {
	for _, path := range []string{"/export.ics", "/export.pdf"} {
		e := setup(t, nil, calCfg())
		owner := e.user(t, "g4cve"+path[len(path)-3:], false)
		trip := seedTrip(t, e, owner)
		g1dropTable(t, e, "trip_members")
		if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+path, nil, owner); w.Code != http.StatusInternalServerError {
			t.Errorf("export%s canViewTrip err = %d, want 500; body=%s", path, w.Code, w.Body.String())
		}
	}

	// exportTripsPDF: same, via the ?ids= multi-trip path.
	e := setup(t, nil, calCfg())
	owner := e.user(t, "g4cvetrips", false)
	trip := seedTrip(t, e, owner)
	g1dropTable(t, e, "trip_members")
	if w := e.req(t, "GET", "/api/trips/export.pdf?ids="+itoa(trip), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("exportTripsPDF canViewTrip err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ExportTripStoreErrs covers the store-error branches of exportTrip: a
// TripByID failure (via a superuser who clears the canViewTrip check without a
// DB read, then a dropped trips column fails TripByID) and a
// CalendarEventsForTrip failure (plan_parts dropped).
func TestG4ExportTripStoreErrs(t *testing.T) {
	// TripByID error: superuser short-circuits canViewTrip, dropped column fails
	// the subsequent TripByID select.
	e := setup(t, nil, calCfg())
	super := e.user(t, "g4expsuper", true)
	trip := seedTrip(t, e, super)
	g1dropColumn(t, e, "trips", "share_all_friends_role")
	if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.ics", nil, super); w.Code != http.StatusInternalServerError {
		t.Errorf("export TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	// CalendarEventsForTrip error: trip readable, but the event query fails.
	e2 := setup(t, nil, calCfg())
	owner := e2.user(t, "g4expevents", false)
	trip2 := seedTrip(t, e2, owner)
	g1dropTable(t, e2, "plan_parts")
	if w := e2.req(t, "GET", "/api/trips/"+itoa(trip2)+"/export.ics", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("export events err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ExportTripPDFExternal covers the ?external=1 branch of exportTripPDF,
// which folds external trip-feed events into the rendered itinerary.
func TestG4ExportTripPDFExternal(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "g4pdfext", false)
	trip := seedTrip(t, e, owner)
	plan := seedPlan(t, e, trip, owner, "G4 Flight")
	seedPart(t, e, plan)
	// Seed one external feed (+ event) so the external loop body runs.
	var feedID int64
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO trip_feeds (trip_id, url, name, timezone)
		 VALUES ($1, 'https://feed.example.com/cal.ics', 'G4 Feed', 'UTC') RETURNING id`,
		trip).Scan(&feedID); err != nil {
		t.Fatalf("seed feed: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_feed_events (feed_id, uid, summary, starts_at, ends_at, all_day)
		 VALUES ($1, 'g4-ext-uid', 'External Conf', NOW(), NOW() + INTERVAL '1 hour', false)`,
		feedID); err != nil {
		t.Fatalf("seed feed event: %v", err)
	}
	w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf?external=1", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("export pdf external = %d; body=%s", w.Code, w.Body.String())
	}
	if !strings.HasPrefix(w.Body.String(), "%PDF-1.4") {
		t.Errorf("not a PDF")
	}
}

// TestG4ExportTripPDFStoreErrs covers exportTripPDF's TripByID and
// visiblePlanDTOs store-error branches.
func TestG4ExportTripPDFStoreErrs(t *testing.T) {
	// TripByID error via superuser + dropped column.
	e := setup(t, nil, calCfg())
	super := e.user(t, "g4pdfsuper", true)
	trip := seedTrip(t, e, super)
	g1dropColumn(t, e, "trips", "share_all_friends_role")
	if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf", nil, super); w.Code != http.StatusInternalServerError {
		t.Errorf("pdf TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	// visiblePlanDTOs error: trip readable with a plan, but its part query fails.
	e2 := setup(t, nil, calCfg())
	owner := e2.user(t, "g4pdfplans", false)
	trip2 := seedTrip(t, e2, owner)
	plan2 := seedPlan(t, e2, trip2, owner, "G4 Flight")
	seedPart(t, e2, plan2)
	g1dropTable(t, e2, "plan_parts")
	if w := e2.req(t, "GET", "/api/trips/"+itoa(trip2)+"/export.pdf", nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("pdf plans err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ExportTripsPDFStoreErrs covers exportTripsPDF's per-trip error branches:
// a TripByID error that isn't ErrNotFound (superuser clears canViewTrip, dropped
// column fails TripByID) and a visiblePlanDTOs error.
func TestG4ExportTripsPDFStoreErrs(t *testing.T) {
	// TripByID generic error.
	e := setup(t, nil, calCfg())
	super := e.user(t, "g4tpdfsuper", true)
	trip := seedTrip(t, e, super)
	g1dropColumn(t, e, "trips", "share_all_friends_role")
	if w := e.req(t, "GET", "/api/trips/export.pdf?ids="+itoa(trip), nil, super); w.Code != http.StatusInternalServerError {
		t.Errorf("trips pdf TripByID err = %d, want 500; body=%s", w.Code, w.Body.String())
	}

	// visiblePlanDTOs error.
	e2 := setup(t, nil, calCfg())
	owner := e2.user(t, "g4tpdfplans", false)
	trip2 := seedTrip(t, e2, owner)
	plan2 := seedPlan(t, e2, trip2, owner, "G4 Flight")
	seedPart(t, e2, plan2)
	g1dropTable(t, e2, "plan_parts")
	if w := e2.req(t, "GET", "/api/trips/export.pdf?ids="+itoa(trip2), nil, owner); w.Code != http.StatusInternalServerError {
		t.Errorf("trips pdf plans err = %d, want 500; body=%s", w.Code, w.Body.String())
	}
}

// TestG4ExportTripsPDFSkipsMissingTrip covers the ErrNotFound→continue branch of
// exportTripsPDF: a viewable id that has been deleted is skipped, and with no
// remaining viewable trips the handler 404s.
func TestG4ExportTripsPDFSkipsMissingTrip(t *testing.T) {
	e := setup(t, nil, calCfg())
	super := e.user(t, "g4tpdfmiss", true)
	// A superuser can "view" any id (canViewTrip short-circuits true), so a
	// non-existent id reaches TripByID and yields ErrNotFound → skipped → 404.
	if w := e.req(t, "GET", "/api/trips/export.pdf?ids=999999", nil, super); w.Code != http.StatusNotFound {
		t.Errorf("missing trip = %d, want 404; body=%s", w.Code, w.Body.String())
	}
}
