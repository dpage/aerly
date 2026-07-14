// Package handlers wires the JSON HTTP API endpoints.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dpage/aerly/internal/attachments"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/config"
	"github.com/dpage/aerly/internal/emailingest"
	"github.com/dpage/aerly/internal/feeds"
	"github.com/dpage/aerly/internal/geocode"
	aerlymaps "github.com/dpage/aerly/internal/maps"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/push"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

type API struct {
	Store    *store.Store
	Auth     *auth.Handler
	Hub      *sse.Hub
	Config   *config.Config
	Resolver providers.Resolver // may be nil if no resolver is configured
	// AirportResolver is the date-free IATA→coords fallback used by the
	// post-ingest coord backfill for off-table airports on flights outside the
	// resolver's ±180-day window. May be nil.
	AirportResolver providers.AirportResolver

	// Push delivers Web Push notifications. Always non-nil (New sets it), but
	// a no-op unless VAPID keys are configured (Push.Enabled()). Typed as an
	// interface so tests can substitute a fake that records what was pushed.
	Push pusher

	// Extractor backs the paste/upload ingest endpoints (the LLM seam). May
	// be nil when no LLM provider is configured — the ingest endpoints then
	// return 503.
	Extractor planops.Extractor

	// Geocoder fills missing part coordinates from their addresses so they can
	// be mapped. nil → geocoding is skipped (e.g. in tests).
	Geocoder geocode.Geocoder

	// POIs resolves nearby points of interest (Geoapify; nil when no key is set,
	// which disables the Explore feature).
	// nil → POI lookups return 501.
	POIs providers.POIResolver

	// Maps resolves pasted Google Maps URLs (incl. short links) to coordinates
	// for the plan coordinate override. Defaulted in New().
	Maps *aerlymaps.Resolver

	// Attachments is the blob store backing per-plan file attachments (issue
	// #91). nil when ATTACHMENTS_STORE is unset — the upload/download/delete
	// endpoints then return 503 and the UI hides the affordance.
	Attachments attachments.Storage

	// Feeds refreshes trip-scoped iCal feed subscriptions on demand (when a feed
	// is added or its URL edited). Always non-nil (New sets it); the poller runs
	// the periodic sweep.
	Feeds *feeds.Service

	// SendVerifyEmail dispatches the verification message. Defaulted in
	// New() to the real sendmail pipe; tests can override.
	SendVerifyEmail func(ctx context.Context, to, token string) error

	// StartedAt is the process start time, used by the superuser "About"
	// panel to report uptime. Defaulted to time.Now() in New().
	StartedAt time.Time
}

func New(s *store.Store, a *auth.Handler, hub *sse.Hub, cfg *config.Config, r providers.Resolver) *API {
	api := &API{Store: s, Auth: a, Hub: hub, Config: cfg, Resolver: r, StartedAt: time.Now()}
	api.SendVerifyEmail = api.defaultSendVerifyEmail
	api.Maps = aerlymaps.NewResolver()
	api.Push = push.NewSender(s, cfg.WebPushVAPIDPublic, cfg.WebPushVAPIDPrivate, cfg.WebPushSubject)
	api.Feeds = feeds.NewService(s, "aerly (+"+cfg.PublicURL+")", 0)
	return api
}

func (a *API) defaultSendVerifyEmail(ctx context.Context, to, token string) error {
	msg := emailingest.BuildVerifyEmail(emailingest.VerifyInput{
		FromAddr:  a.Config.EmailIngestAddress,
		ToAddr:    to,
		PublicURL: a.Config.PublicURL,
		Token:     token,
	})
	return emailingest.Send(ctx, a.Config.EmailIngestSendmail, a.Config.EmailIngestAddress, msg)
}

// Register attaches every /api/* route. All routes require an authenticated
// session; routes that mutate the user table additionally require superuser.
func (a *API) Register(mux *http.ServeMux) {
	req := a.Auth.Require
	sup := a.Auth.RequireSuperuser

	mux.Handle("GET /api/me", req(http.HandlerFunc(a.getMe)))
	mux.Handle("PATCH /api/me", req(http.HandlerFunc(a.updateMe)))
	mux.Handle("GET /api/config", req(http.HandlerFunc(a.getConfig)))
	mux.Handle("GET /api/version", req(http.HandlerFunc(a.getVersion)))
	mux.Handle("GET /api/events", req(http.HandlerFunc(a.events)))
	mux.Handle("GET /api/me/flights", req(http.HandlerFunc(a.listMyFlights)))
	mux.Handle("GET /api/me/emails", req(http.HandlerFunc(a.listMyEmails)))
	mux.Handle("POST /api/me/emails", req(http.HandlerFunc(a.addMyEmail)))
	mux.Handle("POST /api/me/emails/{id}/resend", req(http.HandlerFunc(a.resendMyEmail)))
	mux.Handle("POST /api/me/emails/{id}/primary", req(http.HandlerFunc(a.setPrimaryMyEmail)))
	mux.Handle("DELETE /api/me/emails/{id}", req(http.HandlerFunc(a.deleteMyEmail)))
	mux.Handle("GET /api/me/auto-shares", req(http.HandlerFunc(a.listMyAutoShares)))
	mux.Handle("PUT /api/me/auto-shares/{userId}", req(http.HandlerFunc(a.setMyAutoShare)))
	mux.Handle("DELETE /api/me/auto-shares/{userId}", req(http.HandlerFunc(a.deleteMyAutoShare)))

	// The legacy /api/flights CRUD surface (list/create/get/update/delete +
	// passengers/shares) was retired in Wave 3 — flights now live in the plan
	// model. The stateless resolver lookup survives: it does NOT touch the
	// dropped flights table and the FE's manual flight-add still uses it.
	mux.Handle("POST /api/flights/resolve", req(http.HandlerFunc(a.resolveFlight)))

	mux.Handle("POST /api/maps/resolve", req(http.HandlerFunc(a.resolveMapsURL)))

	// Web Push (PWA push notifications). vapid-key is what the client needs to
	// subscribe; subscriptions register/unregister a device; prefs are the
	// per-kind toggles. All no-ops/disabled responses when VAPID is unset.
	mux.Handle("GET /api/push/vapid-key", req(http.HandlerFunc(a.getPushVAPIDKey)))
	mux.Handle("POST /api/push/subscriptions", req(http.HandlerFunc(a.subscribePush)))
	mux.Handle("DELETE /api/push/subscriptions", req(http.HandlerFunc(a.unsubscribePush)))
	mux.Handle("GET /api/push/prefs", req(http.HandlerFunc(a.getPushPrefs)))
	mux.Handle("PATCH /api/push/prefs", req(http.HandlerFunc(a.setPushPref)))

	mux.Handle("GET /api/notifications", req(http.HandlerFunc(a.getNotifications)))
	mux.Handle("GET /api/alerts", req(http.HandlerFunc(a.listAlerts)))
	mux.Handle("POST /api/alerts/read", req(http.HandlerFunc(a.markAlertsRead)))
	mux.Handle("DELETE /api/alerts", req(http.HandlerFunc(a.clearAlerts)))
	mux.Handle("DELETE /api/alerts/{source}/{id}", req(http.HandlerFunc(a.deleteAlert)))

	mux.Handle("GET /api/friends", req(http.HandlerFunc(a.listFriends)))
	mux.Handle("POST /api/friends/invite", req(http.HandlerFunc(a.inviteFriend)))
	mux.Handle("DELETE /api/friends/outgoing", req(http.HandlerFunc(a.cancelOutgoingInvite)))
	mux.Handle("POST /api/friends/accept-token", req(http.HandlerFunc(a.acceptFriendToken)))
	mux.Handle("POST /api/friends/{userId}/accept", req(http.HandlerFunc(a.acceptFriend)))
	mux.Handle("DELETE /api/friends/{userId}", req(http.HandlerFunc(a.removeFriend)))

	// Superuser-only "About" / diagnostics: running build (commit hash, build
	// time) plus runtime and configuration facts. Gated server-side too, so the
	// build provenance is never exposed to ordinary users.
	mux.Handle("GET /api/admin/info", sup(http.HandlerFunc(a.getAdminInfo)))

	mux.Handle("GET /api/users", req(http.HandlerFunc(a.listUsers)))
	mux.Handle("POST /api/users", sup(http.HandlerFunc(a.inviteUser)))
	mux.Handle("PATCH /api/users/{id}", sup(http.HandlerFunc(a.updateUser)))
	mux.Handle("DELETE /api/users/{id}", sup(http.HandlerFunc(a.deleteUser)))

	// --- Trip-planning core redesign (spec §5.2). Bodies are filled in by
	// the Wave 1/2 feature agents in their per-area handler files. ---

	// Trips, members, tags (Wave 1A).
	mux.Handle("GET /api/trips", req(http.HandlerFunc(a.listTrips)))
	mux.Handle("POST /api/trips", req(http.HandlerFunc(a.createTrip)))
	mux.Handle("POST /api/trips/import", req(http.HandlerFunc(a.importTrip)))
	mux.Handle("GET /api/trips/export.pdf", req(http.HandlerFunc(a.exportTripsPDF)))
	mux.Handle("GET /api/trips/{id}", req(http.HandlerFunc(a.getTrip)))
	mux.Handle("GET /api/trips/{id}/export.ics", req(http.HandlerFunc(a.exportTrip)))
	mux.Handle("GET /api/trips/{id}/export.pdf", req(http.HandlerFunc(a.exportTripPDF)))
	mux.Handle("PATCH /api/trips/{id}", req(http.HandlerFunc(a.updateTrip)))
	mux.Handle("DELETE /api/trips/{id}", req(http.HandlerFunc(a.deleteTrip)))
	mux.Handle("POST /api/trips/{id}/members", req(http.HandlerFunc(a.addTripMember)))
	mux.Handle("DELETE /api/trips/{id}/members/{userId}", req(http.HandlerFunc(a.removeTripMember)))
	mux.Handle("POST /api/trips/{id}/passengers", req(http.HandlerFunc(a.addTripPassenger)))
	mux.Handle("DELETE /api/trips/{id}/passengers/{userId}", req(http.HandlerFunc(a.removeTripPassenger)))
	mux.Handle("PUT /api/trips/{id}/tags", req(http.HandlerFunc(a.setTripTags)))
	mux.Handle("PUT /api/trips/{id}/share-all-friends", req(http.HandlerFunc(a.setTripShareAllFriends)))
	mux.Handle("POST /api/trips/{id}/notify-shares", req(http.HandlerFunc(a.notifyTripShares)))
	mux.Handle("POST /api/trips/{id}/share-by-email", req(http.HandlerFunc(a.shareTripByEmail)))
	mux.Handle("GET /api/tags/suggest", req(http.HandlerFunc(a.suggestTags)))
	mux.Handle("GET /api/trips/{id}/pois", req(http.HandlerFunc(a.getTripPOIs)))

	// Trip-scoped iCal feed subscriptions ("external plans"). Feeds are managed
	// from the Edit trip dialog (edit perm); the cached events are read by any
	// trip viewer behind the per-viewer "Show external plans" toggle.
	mux.Handle("GET /api/trips/{id}/feeds", req(http.HandlerFunc(a.listTripFeeds)))
	mux.Handle("POST /api/trips/{id}/feeds", req(http.HandlerFunc(a.addTripFeed)))
	mux.Handle("PATCH /api/trips/{id}/feeds/{feedId}", req(http.HandlerFunc(a.updateTripFeed)))
	mux.Handle("DELETE /api/trips/{id}/feeds/{feedId}", req(http.HandlerFunc(a.deleteTripFeed)))
	mux.Handle("GET /api/trips/{id}/external-events", req(http.HandlerFunc(a.listTripExternalEvents)))

	// Plans, parts, passengers, visibility, move (Wave 1B).
	mux.Handle("POST /api/trips/{id}/plans", req(http.HandlerFunc(a.createPlan)))
	mux.Handle("PATCH /api/plans/{id}", req(http.HandlerFunc(a.updatePlan)))
	mux.Handle("DELETE /api/plans/{id}", req(http.HandlerFunc(a.deletePlan)))
	mux.Handle("POST /api/plans/{id}/passengers", req(http.HandlerFunc(a.addPlanPassenger)))
	mux.Handle("DELETE /api/plans/{id}/passengers/{userId}", req(http.HandlerFunc(a.removePlanPassenger)))
	mux.Handle("PUT /api/plans/{id}/visibility", req(http.HandlerFunc(a.setPlanVisibility)))
	mux.Handle("PUT /api/plans/{id}/share-all-friends", req(http.HandlerFunc(a.setPlanShareAllFriends)))
	mux.Handle("POST /api/plans/{id}/notify-shares", req(http.HandlerFunc(a.notifyPlanShares)))
	mux.Handle("POST /api/plans/{id}/share-by-email", req(http.HandlerFunc(a.sharePlanByEmail)))
	mux.Handle("POST /api/plans/{id}/move", req(http.HandlerFunc(a.movePlan)))
	mux.Handle("POST /api/plans/{id}/link", req(http.HandlerFunc(a.linkPlans)))
	mux.Handle("PATCH /api/plan-parts/{id}", req(http.HandlerFunc(a.updatePlanPart)))
	mux.Handle("POST /api/plan-parts/{id}/split", req(http.HandlerFunc(a.splitPlanPart)))
	mux.Handle("POST /api/plan-parts/{id}/dismiss", req(http.HandlerFunc(a.dismissPlanPart)))

	// Plan attachments (issue #91). Upload/list live under the plan; download +
	// delete address a single attachment by id. All 503 unless a store is wired.
	mux.Handle("POST /api/plans/{id}/attachments", req(http.HandlerFunc(a.uploadAttachment)))
	mux.Handle("GET /api/attachments/{id}", req(http.HandlerFunc(a.downloadAttachment)))
	mux.Handle("DELETE /api/attachments/{id}", req(http.HandlerFunc(a.deleteAttachment)))

	// Ingest (Wave 2A).
	mux.Handle("POST /api/trips/{id}/ingest", req(http.HandlerFunc(a.ingestTrip)))
	mux.Handle("POST /api/trips/{id}/ingest/confirm", req(http.HandlerFunc(a.ingestTripConfirm)))

	// iCal feeds (Wave 1D). Token-authed via ?token=, NOT the session cookie,
	// so they are registered without the req() session guard.
	//
	// Go 1.22 ServeMux can't express a wildcard that doesn't span a whole path
	// segment (e.g. "{id}.ics"), so the trip/plan feeds are registered as
	// prefix patterns and the handler parses the trailing "{id}.ics". The
	// public URLs stay exactly /api/calendar/{trip,plan}/{id}.ics.
	mux.Handle("GET /api/calendar/me.ics", http.HandlerFunc(a.calendarMe))
	mux.Handle("GET /api/calendar/trip/", http.HandlerFunc(a.calendarTrip))
	mux.Handle("GET /api/calendar/plan/", http.HandlerFunc(a.calendarPlan))

	// Calendar token management (Wave 1D) — session-authed, matching the FE
	// contract in web/src/api/client.ts (list/issue/revoke per-scope tokens).
	mux.Handle("GET /api/calendar/tokens", req(http.HandlerFunc(a.listCalendarTokens)))
	mux.Handle("POST /api/calendar/tokens", req(http.HandlerFunc(a.issueCalendarToken)))
	mux.Handle("DELETE /api/calendar/tokens/{token}", req(http.HandlerFunc(a.revokeCalendarToken)))

	// Tracker (Wave 1C).
	mux.Handle("GET /api/tracker", req(http.HandlerFunc(a.getTracker)))
	// Focused single-flight view: one part with its full detail + flown track.
	mux.Handle("GET /api/tracker/part/{id}", req(http.HandlerFunc(a.getTrackerPart)))

	// Alerts (Wave 2B).
	mux.Handle("GET /api/alert-prefs", req(http.HandlerFunc(a.getAlertPrefs)))
	mux.Handle("PUT /api/alert-prefs", req(http.HandlerFunc(a.setAlertPrefs)))
	mux.Handle("POST /api/plans/{id}/alerts/optin", req(http.HandlerFunc(a.addPlanAlertOptin)))
	mux.Handle("DELETE /api/plans/{id}/alerts/optin", req(http.HandlerFunc(a.removePlanAlertOptin)))
	// Upcoming-plan reminders (issue #11): trip-level opt-in + per-plan override.
	mux.Handle("PUT /api/trips/{id}/reminder", req(http.HandlerFunc(a.setTripReminder)))
	mux.Handle("DELETE /api/trips/{id}/reminder", req(http.HandlerFunc(a.deleteTripReminder)))
	mux.Handle("PUT /api/plans/{id}/reminder", req(http.HandlerFunc(a.setPlanReminder)))
	mux.Handle("DELETE /api/plans/{id}/reminder", req(http.HandlerFunc(a.deletePlanReminder)))
}

// events streams SSE to the caller. Builds a Subscription from the auth
// context + ?show_all=1 query param (only honored for superusers).
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	a.Hub.Stream(w, r, sse.Subscription{
		ViewerID:    me.ID,
		IsSuperuser: me.IsSuperuser,
		ShowAll:     wantsShowAll(r, me),
	})
}

// wantsShowAll returns true when the caller asked for ?show_all=1 AND is
// a superuser. Non-superusers cannot opt into the all-resources view.
func wantsShowAll(r *http.Request, u *store.User) bool {
	if u == nil || !u.IsSuperuser {
		return false
	}
	v := r.URL.Query().Get("show_all")
	return v == "1" || v == "true"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// serverError logs the underlying error and returns a generic 500 body. Use
// it instead of writeError(w, 500, err.Error()) so raw store/SQL errors (which
// can expose schema details) never reach the client.
func serverError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "Internal error.")
}

func handleStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "Not found.")
	case errors.Is(err, store.ErrLastOwner):
		writeError(w, http.StatusConflict, "Cannot remove the last owner of the trip.")
	default:
		// Never echo the raw store/SQL error to the client — it can expose
		// schema, column and constraint names. Log it server-side and return
		// a generic message.
		slog.Error("store error", "err", err)
		writeError(w, http.StatusInternalServerError, "Internal error.")
	}
}

func pathID(r *http.Request, name string) (int64, error) {
	return strconv.ParseInt(r.PathValue(name), 10, 64)
}

func decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}
