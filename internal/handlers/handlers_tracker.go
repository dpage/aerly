package handlers

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/store"
)

// Tracker re-scope (spec §7). Two read views over flight plan_parts + positions,
// gated by the §4 plan-visibility predicate. There is NO leaderboard / ranking:
// the payload is just labelled parts + their latest positions.
//
//   - getTracker      — GET /api/tracker (convergence): every visible flight
//     part whose effective arrival falls within [now-before, now+after], with
//     latest positions. ?window_before / ?window_after are duration strings
//     ("7d", "12h"); ?tag scopes to a tag and, when no explicit window is given,
//     derives the default window from the tagged trips' span (spec §7).
//   - getTrackerPart  — the focused single-flight view (one part + its track).
//     Wired by a later wave when its route lands (handlers.go is owned by Wave
//     0a); the store capability and DTO it returns are exercised by 1C tests.

// defaultTrackerWindow is the fallback half-window when neither an explicit
// param nor a tag-derived span is available — matches the front end's 7d/7d
// default in trackerSlice.ts.
const defaultTrackerWindow = 7 * 24 * time.Hour

func (a *API) getTracker(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))

	// Absolute From/To dates (the front-end date pickers) win; then the legacy
	// duration window (window_before/window_after); then a tag-derived span; then
	// the default window. parseDate accepts YYYY-MM-DD; To is inclusive of the
	// whole day, so push it to end-of-day.
	fromDate, _ := parseDate(strPtrIfSet(r.URL.Query().Get("from")))
	toDate, _ := parseDate(strPtrIfSet(r.URL.Query().Get("to")))
	before, beforeOK := parseWindow(r.URL.Query().Get("window_before"))
	after, afterOK := parseWindow(r.URL.Query().Get("window_after"))

	now := time.Now()
	var from, to time.Time
	switch {
	case fromDate != nil || toDate != nil:
		if fromDate != nil {
			from = *fromDate
		} else {
			from = now.Add(-defaultTrackerWindow)
		}
		if toDate != nil {
			to = toDate.Add(24*time.Hour - time.Second)
		} else {
			to = now.Add(defaultTrackerWindow)
		}
	case !beforeOK && !afterOK && tag != "":
		// No explicit window AND a tag: derive the span from the tagged trips.
		spanFrom, spanTo, ok, err := a.Store.TaggedTripSpan(r.Context(), me.ID, tag)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		if ok {
			from, to = spanFrom, spanTo
		}
	}
	if from.IsZero() && to.IsZero() {
		if !beforeOK {
			before = defaultTrackerWindow
		}
		if !afterOK {
			after = defaultTrackerWindow
		}
		from, to = now.Add(-before), now.Add(after)
	}

	parts, err := a.Store.ConvergencePartsAll(r.Context(), me.ID, from, to, tag)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out, err := a.trackerPartDTOs(r.Context(), parts)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.TrackerResponseDTO{Parts: out})
}

// trackerPartDTOs assembles full PlanPartDTOs for the unified map+list view:
// each part's type detail (via the same loader the trip detail uses) plus, for
// flight parts, the latest position + flown track (batch-loaded once).
func (a *API) trackerPartDTOs(ctx context.Context, parts []*store.PlanPart) ([]api.PlanPartDTO, error) {
	flightIDs := make([]int64, 0, len(parts))
	planIDset := map[int64]struct{}{}
	for _, p := range parts {
		if p.Type == "flight" {
			flightIDs = append(flightIDs, p.ID)
		}
		planIDset[p.PlanID] = struct{}{}
	}
	latest, err := a.Store.LatestPartPositions(ctx, flightIDs)
	if err != nil {
		return nil, err
	}
	tracks, err := a.Store.PartTracks(ctx, flightIDs, 0)
	if err != nil {
		return nil, err
	}

	// Who added each plan + who's on it, so the tracker can show "who is on
	// which flight". Batch-load owners + passengers + the users they reference.
	planIDs := make([]int64, 0, len(planIDset))
	for id := range planIDset {
		planIDs = append(planIDs, id)
	}
	owners, err := a.Store.PlanOwners(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	tripOwners, err := a.Store.TripOwnersByPlan(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	pax, err := a.Store.PassengersByPlan(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	suppliers, err := a.Store.SuppliersByPlan(ctx, planIDs)
	if err != nil {
		return nil, err
	}
	userIDset := map[int64]struct{}{}
	for _, uid := range owners {
		userIDset[uid] = struct{}{}
	}
	for _, ids := range pax {
		for _, uid := range ids {
			userIDset[uid] = struct{}{}
		}
	}
	userIDs := make([]int64, 0, len(userIDset))
	for id := range userIDset {
		userIDs = append(userIDs, id)
	}
	users, err := a.Store.UsersByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	out := make([]api.PlanPartDTO, 0, len(parts))
	for _, p := range parts {
		dto, err := a.partDTOWithPositions(ctx, p, nil, latest[p.ID], tracks[p.ID])
		if err != nil {
			return nil, err
		}
		if u := users[owners[p.PlanID]]; u != nil {
			od := api.ToUserDTO(u)
			dto.Owner = &od
		}
		dto.TripOwnerID = tripOwners[p.PlanID]
		dto.SupplierName = suppliers[p.PlanID]
		for _, uid := range pax[p.PlanID] {
			if u := users[uid]; u != nil {
				dto.Passengers = append(dto.Passengers, api.ToUserDTO(u))
			}
		}
		out = append(out, dto)
	}
	return out, nil
}

// getTrackerPart is the focused single-flight view: one trackable part with its
// full detail, latest position, AND the flown track (the convergence list view
// stays position-only). 404 (not 403) when the viewer can't see it, so part
// existence isn't leaked. Returns a PlanPartDTO (embedding FlightDetailDTO with
// track + latest_position) so the FE can draw the polyline + great-circle.
func (a *API) getTrackerPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "Invalid ID.")
		return
	}
	me := auth.UserFrom(r.Context())
	// Gate on the §4 plan-visibility predicate first: ErrNotFound → 404 covers
	// the hidden case without leaking existence.
	if _, err := a.Store.TrackerPartByID(r.Context(), me.ID, id); err != nil {
		handleStoreErr(w, err)
		return
	}
	part, err := a.Store.PlanPartByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.trackerPartDetailDTO(r.Context(), part)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// trackerPartDetailDTO assembles a single part's PlanPartDTO with its latest
// position and flown track folded in (flight parts only carry positions). Reuses
// the api projection so dto.go's json tags stay the single source of truth.
func (a *API) trackerPartDetailDTO(ctx context.Context, p *store.PlanPart) (api.PlanPartDTO, error) {
	var (
		flight *store.FlightDetail
		latest *store.Position
		track  []*store.Position
		err    error
	)
	if p.Type == "flight" {
		flight, err = a.Store.FlightDetailFor(ctx, p.ID)
		if err != nil {
			return api.PlanPartDTO{}, err
		}
		latestMap, err := a.Store.LatestPartPositions(ctx, []int64{p.ID})
		if err != nil {
			return api.PlanPartDTO{}, err
		}
		latest = latestMap[p.ID]
		trackMap, err := a.Store.PartTracks(ctx, []int64{p.ID}, 0)
		if err != nil {
			return api.PlanPartDTO{}, err
		}
		track = trackMap[p.ID]
	}
	return api.ToPlanPartDTO(p, flight, nil, nil, nil, nil, nil, latest, track), nil
}

// parseWindow parses a window duration string. It accepts a trailing "d" (days)
// in addition to Go's stdlib units (h/m/s) since the front end sends "7d". An
// empty / unparseable / non-positive value reports ok=false so the caller falls
// back to its default.
func parseWindow(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n <= 0 {
			return 0, false
		}
		return time.Duration(n) * 24 * time.Hour, true
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}
