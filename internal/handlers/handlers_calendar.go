package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Read-only iCal feeds (spec §8) and their token management (spec §5.2/§8).
//
// The .ics feeds authenticate by the ?token= query param (NOT the session
// cookie, since calendar clients won't carry it). The token resolves to its
// owning user, and the feed is rendered AS that user with the §4 visibility
// predicate applied in the store query — so a plan hidden from the token owner
// never appears, and another user's token can never surface the owner's
// private plans.
//
// The token-management endpoints are session-authed and let a logged-in user
// list / issue (regenerate) / revoke their own per-scope tokens.

// --- Feed handlers (token-authed, no session) ---

func (a *API) calendarMe(w http.ResponseWriter, r *http.Request) {
	info, ok := a.calendarTokenInfo(w, r, "me", 0)
	if !ok {
		return
	}
	events, err := a.Store.CalendarEventsForUser(r.Context(), info.UserID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeICS(w, "Aerly", events)
}

func (a *API) calendarTrip(w http.ResponseWriter, r *http.Request) {
	id, ok := parseICSPathID(r.URL.Path, "/api/calendar/trip/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, ok := a.calendarTokenInfo(w, r, "trip", id)
	if !ok {
		return
	}
	events, err := a.Store.CalendarEventsForTrip(r.Context(), info.UserID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeICS(w, "Aerly Trip", events)
}

func (a *API) calendarPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := parseICSPathID(r.URL.Path, "/api/calendar/plan/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	info, ok := a.calendarTokenInfo(w, r, "plan", id)
	if !ok {
		return
	}
	events, err := a.Store.CalendarEventsForPlan(r.Context(), info.UserID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	writeICS(w, "Aerly Plan", events)
}

// calendarTokenInfo resolves the ?token= query param to its owner and verifies
// the token was issued for exactly this feed (wantScope + wantResource). A
// per-resource token only authorizes its own resource, so presenting a "trip 5"
// token at the "trip 6" feed (or at a different scope) yields 401 rather than
// silently serving another resource. Writes a 401 and returns ok=false when the
// token is absent, unknown, or mismatched.
func (a *API) calendarTokenInfo(w http.ResponseWriter, r *http.Request, wantScope string, wantResource int64) (*store.CalendarTokenInfo, bool) {
	tok := strings.TrimSpace(r.URL.Query().Get("token"))
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return nil, false
	}
	info, err := a.Store.CalendarTokenByValue(r.Context(), tok)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return nil, false
		}
		serverError(w, err)
		return nil, false
	}
	if info.Scope != wantScope || info.ResourceID != wantResource {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return nil, false
	}
	return info, true
}

// parseICSPathID extracts the {id} from a "<prefix>{id}.ics" request path. The
// trip/plan feeds are registered as prefix patterns (Go 1.22 ServeMux can't
// match a wildcard mid-segment), so we parse the trailing segment here.
func parseICSPathID(path, prefix string) (int64, bool) {
	rest := strings.TrimPrefix(path, prefix)
	if rest == path { // prefix not present
		return 0, false
	}
	rest = strings.TrimSuffix(rest, ".ics")
	if rest == "" || strings.Contains(rest, "/") {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func writeICS(w http.ResponseWriter, calName string, events []*store.CalendarEvent) {
	body := renderICS(calName, events)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// --- One-shot trip export (session-authed download) ---

// exportTrip serves the visible plans of one trip as a downloadable .ics file —
// the inverse of the TripIt/Kayak import. Unlike the subscribe feeds it's
// session-authed (no token), renders as the logged-in viewer with the §4
// visibility predicate, and is marked as an attachment so the browser saves it
// rather than handing it to a calendar client as a live subscription.
func (a *API) exportTrip(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	ok, err := a.canViewTrip(r.Context(), id, me)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	trip, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	events, err := a.Store.CalendarEventsForTrip(r.Context(), me.ID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	body := renderICS(trip.Name, events)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+downloadFilename(trip.Name, "ics")+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// exportTripPDF serves the visible plans of one trip as a downloadable PDF
// itinerary (issue #90). Like exportTrip it is session-authed and scoped to the
// viewer's §4 visibility, so hidden plans never leak; it differs only in the
// rendered format and in honouring the caller's A4/US-Letter page-size
// preference.
func (a *API) exportTripPDF(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	ok, err := a.canViewTrip(r.Context(), id, me)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "Not found.")
		return
	}
	trip, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	events, err := a.Store.CalendarEventsForTrip(r.Context(), me.ID, id)
	if err != nil {
		serverError(w, err)
		return
	}
	body := renderItineraryPDF(trip, events, me.PaperSize)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		`attachment; filename="`+downloadFilename(trip.Name, "pdf")+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// downloadFilename turns a trip name into a safe ASCII download filename with
// the given extension. Anything outside [A-Za-z0-9] collapses to a hyphen so the
// value is safe to drop unquoted-ish into the Content-Disposition header and
// onto any filesystem; an empty/blank name falls back to "trip".
func downloadFilename(name, ext string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "trip"
	}
	return slug + "." + ext
}

// --- Token-management handlers (session-authed) ---
//
// Shapes match the frontend contract in web/src/api/client.ts + types.ts:
//   GET    /api/calendar/tokens            -> CalendarToken[]
//   POST   /api/calendar/tokens {scope,id} -> CalendarToken   (issue/regenerate)
//   DELETE /api/calendar/tokens/{token}    -> 204
// where CalendarToken = { scope, resource_id, token, url, created_at }. Tokens
// are now keyed per (user, scope, resource_id): each trip/plan feed has its own
// independently-revocable token (resource_id 0 for the "me" scope).

type issueCalendarTokenInput struct {
	Scope string `json:"scope"`
	// ID is the trip or plan id for scope=="trip"/"plan"; it is both stored as
	// the token's resource_id and folded into the feed URL. The "me" scope
	// ignores it (resource_id 0).
	ID int64 `json:"id"`
}

func (a *API) listCalendarTokens(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	toks, err := a.Store.ListCalendarTokens(r.Context(), u.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	out := make([]api.CalendarTokenDTO, 0, len(toks))
	for _, t := range toks {
		out = append(out, a.calendarTokenDTO(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) issueCalendarToken(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	var in issueCalendarTokenInput
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body.")
		return
	}
	scope := strings.TrimSpace(in.Scope)
	switch scope {
	case "me", "trip", "plan":
	default:
		writeError(w, http.StatusBadRequest, "Invalid scope.")
		return
	}
	if (scope == "trip" || scope == "plan") && in.ID <= 0 {
		writeError(w, http.StatusBadRequest, "An ID is required for trip/plan scope.")
		return
	}
	// Don't mint a token for a resource the caller can't actually see. The feed
	// renderer already scopes events to the token owner, but issuing tokens for
	// arbitrary ids is confusing and a needless defense-in-depth gap; 404 like
	// the resource doesn't exist for them.
	switch scope {
	case "trip":
		ok, err := a.Store.CanViewTrip(r.Context(), in.ID, u.ID)
		if err != nil {
			serverError(w, err)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
	case "plan":
		ok, err := a.Store.CanViewPlan(r.Context(), in.ID, u.ID, u.IsSuperuser)
		if err != nil {
			serverError(w, err)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
	}
	// Issue (regenerate), revoking any prior token for this exact resource only.
	tok, err := a.Store.RegenerateCalendarToken(r.Context(), u.ID, scope, in.ID)
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a.calendarTokenDTO(tok))
}

func (a *API) revokeCalendarToken(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	token := r.PathValue("token")
	if token == "" {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.RevokeCalendarToken(r.Context(), u.ID, token); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		serverError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// calendarTokenDTO builds the wire shape, deriving the ready-to-use feed URL
// from the public base URL, scope, and (for trip/plan) the resource id stored
// on the token.
func (a *API) calendarTokenDTO(t *store.CalendarToken) api.CalendarTokenDTO {
	return api.ToCalendarTokenDTO(t, a.calendarFeedURL(t.Scope, t.ResourceID, t.Token))
}

func (a *API) calendarFeedURL(scope string, id int64, token string) string {
	base := ""
	if a.Config != nil {
		base = strings.TrimRight(a.Config.PublicURL, "/")
	}
	var path string
	switch scope {
	case "trip":
		path = "/api/calendar/trip/" + strconv.FormatInt(id, 10) + ".ics"
	case "plan":
		path = "/api/calendar/plan/" + strconv.FormatInt(id, 10) + ".ics"
	default:
		path = "/api/calendar/me.ics"
	}
	return base + path + "?token=" + token
}
