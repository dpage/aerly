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

	before, beforeOK := parseWindow(r.URL.Query().Get("window_before"))
	after, afterOK := parseWindow(r.URL.Query().Get("window_after"))

	now := time.Now()
	var from, to time.Time

	// When the caller gave no explicit window AND a tag is set, derive the
	// default span server-side from the tagged trips' min/max (spec §7). An
	// explicit window always wins — it stays overridable.
	if !beforeOK && !afterOK && tag != "" {
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

	parts, err := a.Store.ConvergenceParts(r.Context(), me.ID, from, to, tag)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		ids = append(ids, p.PlanPartID)
	}
	latest, err := a.Store.LatestPartPositions(r.Context(), ids)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.TrackerPartDTO, 0, len(parts))
	for _, p := range parts {
		out = append(out, toTrackerPartDTO(p, latest[p.PlanPartID]))
	}
	writeJSON(w, http.StatusOK, out)
}

// getTrackerPart is the focused single-flight view: one trackable part with its
// full detail, latest position, AND the flown track (the convergence list view
// stays position-only). 404 (not 403) when the viewer can't see it, so part
// existence isn't leaked. Returns a PlanPartDTO (embedding FlightDetailDTO with
// track + latest_position) so the FE can draw the polyline + great-circle.
func (a *API) getTrackerPart(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
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

// toTrackerPartDTO projects a store.TrackerPart (+ optional latest position)
// into the locked TrackerPartDTO. Built here rather than in the api package so
// dto.go's json tags stay untouched.
func toTrackerPartDTO(p *store.TrackerPart, latest *store.Position) api.TrackerPartDTO {
	dto := api.TrackerPartDTO{
		PlanPartID:  p.PlanPartID,
		PlanID:      p.PlanID,
		TripID:      p.TripID,
		OwnerID:     p.OwnerID,
		Title:       p.Title,
		Status:      p.Status,
		EffectiveAt: p.EffectiveAt,
		Ident:       p.Ident,
		DestIATA:    p.DestIATA,
	}
	if latest != nil {
		pd := api.ToPositionDTO(latest)
		dto.LatestPosition = &pd
	}
	return dto
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
