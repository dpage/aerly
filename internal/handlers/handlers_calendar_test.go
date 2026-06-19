package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/store"
)

// rawGet issues an anonymous GET (no session cookie) and returns the recorder —
// used for the token-authed .ics feeds.
func rawGet(e *testEnv, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	e.mux.ServeHTTP(w, r)
	return w
}

// seedTrip / seedPlan / seedPart insert fixtures via raw SQL, since the plan
// CRUD store methods are stubbed in Wave 1B and the calendar feed only needs
// rows in place.
func seedTrip(t *testing.T, e *testEnv, owner int64) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, owner).Scan(&id); err != nil {
		t.Fatalf("seed trip: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1,$2,'owner')`, id, owner); err != nil {
		t.Fatalf("seed owner member: %v", err)
	}
	return id
}

func seedMember(t *testing.T, e *testEnv, trip, user int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1,$2,'viewer')`, trip, user); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

func seedPlan(t *testing.T, e *testEnv, trip, owner int64, title string) int64 {
	t.Helper()
	var id int64
	if err := e.pool.QueryRow(context.Background(),
		`INSERT INTO plans (trip_id, type, title, created_by) VALUES ($1,'flight',$2,$3) RETURNING id`,
		trip, title, owner).Scan(&id); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	return id
}

func seedPart(t *testing.T, e *testEnv, plan int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO plan_parts (plan_id, starts_at, ends_at, start_tz, end_tz, start_label)
		 VALUES ($1, NOW(), NOW() + INTERVAL '2 hours', 'Europe/London', 'America/New_York', 'LHR')`,
		plan); err != nil {
		t.Fatalf("seed part: %v", err)
	}
}

func hidePlanFrom(t *testing.T, e *testEnv, plan, user int64) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO plan_visibility (plan_id, mode) VALUES ($1,'hidden_from')`, plan); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	if _, err := e.pool.Exec(context.Background(),
		`INSERT INTO plan_visibility_members (plan_id, user_id) VALUES ($1,$2)`, plan, user); err != nil {
		t.Fatalf("set visibility member: %v", err)
	}
}

func calCfg() *config.Config {
	return &config.Config{PublicURL: "https://aerly.test"}
}

// TestCalendarTokenManagementEndpoints exercises the FE-contract shapes:
// GET/POST /api/calendar/tokens and DELETE /api/calendar/tokens/{token}.
func TestCalendarTokenManagementEndpoints(t *testing.T) {
	e := setup(t, nil, calCfg())
	uid := e.user(t, "cal-user", false)

	// Empty list initially.
	w := e.req(t, "GET", "/api/calendar/tokens", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("empty list = %q, want []", got)
	}

	// Issue a "me" token.
	w = e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "me"}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("issue me = %d %s", w.Code, w.Body.String())
	}
	tok := decodeBody[map[string]any](t, w)
	if tok["scope"] != "me" || tok["token"] == "" {
		t.Fatalf("issue me bad shape: %v", tok)
	}
	meURL, _ := tok["url"].(string)
	if !strings.HasPrefix(meURL, "https://aerly.test/api/calendar/me.ics?token=") {
		t.Errorf("me url = %q, want me.ics feed url", meURL)
	}
	if _, ok := tok["created_at"]; !ok {
		t.Error("token missing created_at")
	}

	// Issue a "trip" token with an id → url carries that id. The trip must be
	// one the caller can see, so seed a couple owned by uid.
	tripA := seedTrip(t, e, uid)
	tripB := seedTrip(t, e, uid)
	w = e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "trip", "id": tripA}, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("issue trip = %d %s", w.Code, w.Body.String())
	}
	tripTok := decodeBody[map[string]any](t, w)
	if u, _ := tripTok["url"].(string); !strings.Contains(u, "/api/calendar/trip/"+itoa(tripA)+".ics?token=") {
		t.Errorf("trip url = %q, want trip/%d.ics", u, tripA)
	}
	if rid, _ := tripTok["resource_id"].(float64); int64(rid) != tripA {
		t.Errorf("trip token resource_id = %v, want %d", tripTok["resource_id"], tripA)
	}

	// A second trip (different id) gets its own distinct token — per-resource
	// granularity, so regenerating one trip's feed never revokes another's.
	w = e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "trip", "id": tripB}, uid)
	trip43 := decodeBody[map[string]any](t, w)
	if trip43["token"] == tripTok["token"] {
		t.Error("distinct trip ids shared a token")
	}

	// A trip the caller can't see → 404 (no token minted for arbitrary ids).
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "trip", "id": tripB + 999}, uid); w.Code != http.StatusNotFound {
		t.Errorf("token for unseen trip = %d, want 404", w.Code)
	}

	// trip/plan scope without id → 400.
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "plan"}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("plan scope without id = %d, want 400", w.Code)
	}
	// bad scope → 400.
	if w := e.req(t, "POST", "/api/calendar/tokens", map[string]any{"scope": "x"}, uid); w.Code != http.StatusBadRequest {
		t.Errorf("bad scope = %d, want 400", w.Code)
	}

	// List now has 3 tokens (me + trip 42 + trip 43).
	w = e.req(t, "GET", "/api/calendar/tokens", nil, uid)
	list := decodeBody[[]map[string]any](t, w)
	if len(list) != 3 {
		t.Fatalf("list len = %d, want 3", len(list))
	}

	// Revoke the me token.
	meToken := tok["token"].(string)
	if w := e.req(t, "DELETE", "/api/calendar/tokens/"+meToken, nil, uid); w.Code != http.StatusNoContent {
		t.Errorf("revoke = %d, want 204", w.Code)
	}
	// Revoke again → 404.
	if w := e.req(t, "DELETE", "/api/calendar/tokens/"+meToken, nil, uid); w.Code != http.StatusNotFound {
		t.Errorf("double revoke = %d, want 404", w.Code)
	}

	// Token management requires a session.
	if w := e.req(t, "GET", "/api/calendar/tokens", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anon list = %d, want 401", w.Code)
	}
}

// TestCalendarFeedTokenAuthAndVisibility: the .ics feeds are token-authed (no
// session) and render as the token owner with the §4 predicate, so a plan
// hidden from the owner is absent and another user's token can't see private
// plans.
func TestCalendarFeedTokenAuthAndVisibility(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "owner", false)
	member := e.user(t, "member", false)

	trip := seedTrip(t, e, owner)
	seedMember(t, e, trip, member)
	e.befriend(t, owner, member) // friend gate: a trip member must be an accepted friend (spec §4)
	pub := seedPlan(t, e, trip, owner, "Public Flight")
	seedPart(t, e, pub)
	hid := seedPlan(t, e, trip, owner, "Hidden Flight")
	seedPart(t, e, hid)
	hidePlanFrom(t, e, hid, member)

	// Issue per-user "me" tokens plus per-resource trip/plan tokens (each feed
	// now needs a token issued for exactly its scope+resource).
	ownerTok, err := e.store.CalendarToken(context.Background(), owner, "me", 0)
	if err != nil {
		t.Fatalf("owner token: %v", err)
	}
	memberTok, err := e.store.CalendarToken(context.Background(), member, "me", 0)
	if err != nil {
		t.Fatalf("member token: %v", err)
	}
	memberTripTok, err := e.store.CalendarToken(context.Background(), member, "trip", trip)
	if err != nil {
		t.Fatalf("member trip token: %v", err)
	}
	memberPlanTok, err := e.store.CalendarToken(context.Background(), member, "plan", hid)
	if err != nil {
		t.Fatalf("member plan token: %v", err)
	}

	feed := func(path string) string {
		w := rawGet(e, path)
		if w.Code != http.StatusOK {
			t.Fatalf("feed %s = %d %s", path, w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
			t.Errorf("feed content-type = %q, want text/calendar", ct)
		}
		return w.Body.String()
	}

	// Owner's me feed has both plans.
	ownerFeed := feed("/api/calendar/me.ics?token=" + ownerTok.Token)
	if !strings.Contains(ownerFeed, "Public Flight") || !strings.Contains(ownerFeed, "Hidden Flight") {
		t.Errorf("owner feed missing a plan:\n%s", ownerFeed)
	}

	// Member's me feed is empty: the member is neither the owner nor a passenger
	// on this trip, just a shared viewer — so a friend's trip must NOT appear on
	// their personal feed (issue #76). The hidden plan must of course stay absent.
	memberFeed := feed("/api/calendar/me.ics?token=" + memberTok.Token)
	if strings.Contains(memberFeed, "Public Flight") {
		t.Errorf("member me feed LEAKED a friend's trip (issue #76):\n%s", memberFeed)
	}
	if strings.Contains(memberFeed, "Hidden Flight") {
		t.Errorf("member feed LEAKED hidden plan:\n%s", memberFeed)
	}

	// Trip feed for member (its own trip token): same — hidden absent.
	tripFeed := feed("/api/calendar/trip/" + itoa(trip) + ".ics?token=" + memberTripTok.Token)
	if strings.Contains(tripFeed, "Hidden Flight") {
		t.Errorf("member trip feed LEAKED hidden plan:\n%s", tripFeed)
	}

	// Single-plan feed for the hidden plan via the member's plan token → empty
	// (no VEVENT), because the member can't see it.
	planFeed := feed("/api/calendar/plan/" + itoa(hid) + ".ics?token=" + memberPlanTok.Token)
	if strings.Contains(planFeed, "BEGIN:VEVENT") {
		t.Errorf("member single-plan feed for hidden plan should have no events:\n%s", planFeed)
	}

	// Per-resource enforcement: a "me" token must NOT authorize a trip/plan feed.
	if w := rawGet(e, "/api/calendar/trip/"+itoa(trip)+".ics?token="+memberTok.Token); w.Code != http.StatusUnauthorized {
		t.Errorf("me token at trip feed = %d, want 401", w.Code)
	}
	// A token for one trip must not authorize a different trip's feed.
	if w := rawGet(e, "/api/calendar/trip/"+itoa(trip+1)+".ics?token="+memberTripTok.Token); w.Code != http.StatusUnauthorized {
		t.Errorf("trip token at other trip feed = %d, want 401", w.Code)
	}
	// A trip token must not authorize the plan feed for the same id.
	if w := rawGet(e, "/api/calendar/plan/"+itoa(trip)+".ics?token="+memberTripTok.Token); w.Code != http.StatusUnauthorized {
		t.Errorf("trip token at plan feed = %d, want 401", w.Code)
	}

	// Missing token → 401.
	if w := rawGet(e, "/api/calendar/me.ics"); w.Code != http.StatusUnauthorized {
		t.Errorf("no-token feed = %d, want 401", w.Code)
	}
	// Bad token → 401.
	if w := rawGet(e, "/api/calendar/me.ics?token=garbage"); w.Code != http.StatusUnauthorized {
		t.Errorf("bad-token feed = %d, want 401", w.Code)
	}
	// Bad trip id segment → 404.
	if w := rawGet(e, "/api/calendar/trip/abc.ics?token="+ownerTok.Token); w.Code != http.StatusNotFound {
		t.Errorf("bad trip id = %d, want 404", w.Code)
	}
}

// TestExportTripICS: the session-authed one-shot export downloads the viewer's
// visible plans as an .ics attachment, hides plans the viewer can't see, names
// the file after the trip, and 404s a trip the caller can't view.
func TestExportTripICS(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "exp-owner", false)
	member := e.user(t, "exp-member", false)
	stranger := e.user(t, "exp-stranger", false)

	trip := seedTrip(t, e, owner)
	// Give the trip a recognisable name for the filename assertion.
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE trips SET name = 'Paris 2026!' WHERE id = $1`, trip); err != nil {
		t.Fatalf("rename trip: %v", err)
	}
	seedMember(t, e, trip, member)
	e.befriend(t, owner, member)
	pub := seedPlan(t, e, trip, owner, "Public Flight")
	seedPart(t, e, pub)
	hid := seedPlan(t, e, trip, owner, "Hidden Flight")
	seedPart(t, e, hid)
	hidePlanFrom(t, e, hid, member)

	// Owner export: both plans, attachment disposition, trip-named file.
	w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.ics", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("owner export = %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/calendar") {
		t.Errorf("content-type = %q, want text/calendar", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="Paris-2026.ics"` {
		t.Errorf("content-disposition = %q", cd)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Public Flight") || !strings.Contains(body, "Hidden Flight") {
		t.Errorf("owner export missing a plan:\n%s", body)
	}

	// Member export: the hidden plan must not leak.
	w = e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.ics", nil, member)
	if w.Code != http.StatusOK {
		t.Fatalf("member export = %d %s", w.Code, w.Body.String())
	}
	memberBody := w.Body.String()
	if !strings.Contains(memberBody, "Public Flight") {
		t.Errorf("member export missing public plan:\n%s", memberBody)
	}
	if strings.Contains(memberBody, "Hidden Flight") {
		t.Errorf("member export LEAKED hidden plan:\n%s", memberBody)
	}

	// A stranger can't view the trip → 404.
	if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.ics", nil, stranger); w.Code != http.StatusNotFound {
		t.Errorf("stranger export = %d, want 404", w.Code)
	}
	// Export requires a session.
	if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.ics", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anon export = %d, want 401", w.Code)
	}
	// Bad id → 400.
	if w := e.req(t, "GET", "/api/trips/abc/export.ics", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad id export = %d, want 400", w.Code)
	}
}

// TestExportTripPDF: the session-authed PDF itinerary downloads the viewer's
// visible plans as a PDF attachment, hides plans the viewer can't see, names the
// file after the trip, honours the caller's A4/Letter preference, and 404s a
// trip the caller can't view.
func TestExportTripPDF(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "pdf-owner", false)
	member := e.user(t, "pdf-member", false)
	stranger := e.user(t, "pdf-stranger", false)

	trip := seedTrip(t, e, owner)
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE trips SET name = 'Berlin 2026!' WHERE id = $1`, trip); err != nil {
		t.Fatalf("rename trip: %v", err)
	}
	seedMember(t, e, trip, member)
	e.befriend(t, owner, member)
	pub := seedPlan(t, e, trip, owner, "Public Flight")
	seedPart(t, e, pub)
	hid := seedPlan(t, e, trip, owner, "Hidden Flight")
	seedPart(t, e, hid)
	hidePlanFrom(t, e, hid, member)

	// Owner export: PDF attachment, trip-named file, both plans, default A4.
	w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf", nil, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("owner export = %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("content-type = %q, want application/pdf", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="Berlin-2026.pdf"` {
		t.Errorf("content-disposition = %q", cd)
	}
	body := w.Body.String()
	if !strings.HasPrefix(body, "%PDF-1.4") {
		t.Errorf("owner export is not a PDF: %q", body[:min(16, len(body))])
	}
	if !strings.Contains(body, "Public Flight") || !strings.Contains(body, "Hidden Flight") {
		t.Errorf("owner export missing a plan")
	}
	if !strings.Contains(body, "595.28 841.89") {
		t.Errorf("owner export should default to A4 dimensions")
	}

	// Member export: the hidden plan must not leak.
	w = e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf", nil, member)
	if w.Code != http.StatusOK {
		t.Fatalf("member export = %d %s", w.Code, w.Body.String())
	}
	memberBody := w.Body.String()
	if !strings.Contains(memberBody, "Public Flight") {
		t.Errorf("member export missing public plan")
	}
	if strings.Contains(memberBody, "Hidden Flight") {
		t.Errorf("member export LEAKED hidden plan")
	}

	// The page-size preference is honoured: switch to US Letter, re-export.
	letter := "letter"
	if _, err := e.store.UpdateUser(context.Background(), owner,
		store.UpdateUserPayload{PaperSize: &letter}); err != nil {
		t.Fatalf("set paper size: %v", err)
	}
	w = e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf", nil, owner)
	if !strings.Contains(w.Body.String(), "612.00 792.00") {
		t.Errorf("letter preference not reflected in MediaBox")
	}

	// A stranger can't view the trip → 404.
	if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf", nil, stranger); w.Code != http.StatusNotFound {
		t.Errorf("stranger export = %d, want 404", w.Code)
	}
	// Export requires a session.
	if w := e.req(t, "GET", "/api/trips/"+itoa(trip)+"/export.pdf", nil, 0); w.Code != http.StatusUnauthorized {
		t.Errorf("anon export = %d, want 401", w.Code)
	}
	// Bad id → 400.
	if w := e.req(t, "GET", "/api/trips/abc/export.pdf", nil, owner); w.Code != http.StatusBadRequest {
		t.Errorf("bad id export = %d, want 400", w.Code)
	}
}

// TestCalendarFeedUpdatesReflectPartChanges: a delayed part re-renders (the
// single-plan feed stays live — re-fetch shows the new time and LAST-MODIFIED).
func TestCalendarFeedUpdatesReflectPartChanges(t *testing.T) {
	e := setup(t, nil, calCfg())
	owner := e.user(t, "owner-live", false)
	trip := seedTrip(t, e, owner)
	plan := seedPlan(t, e, trip, owner, "Live Flight")
	seedPart(t, e, plan)
	tok, _ := e.store.CalendarToken(context.Background(), owner, "plan", plan)

	first := rawGet(e, "/api/calendar/plan/"+itoa(plan)+".ics?token="+tok.Token).Body.String()
	if !strings.Contains(first, "BEGIN:VEVENT") {
		t.Fatalf("expected a VEVENT:\n%s", first)
	}

	// Push the part 90 minutes later (simulated delay).
	if _, err := e.pool.Exec(context.Background(),
		`UPDATE plan_parts SET starts_at = starts_at + INTERVAL '90 minutes', updated_at = NOW()
		   WHERE plan_id = $1`, plan); err != nil {
		t.Fatalf("delay part: %v", err)
	}
	second := rawGet(e, "/api/calendar/plan/"+itoa(plan)+".ics?token="+tok.Token).Body.String()
	if first == second {
		t.Error("feed did not change after the part time moved")
	}
}
