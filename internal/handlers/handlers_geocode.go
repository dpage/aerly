package handlers

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/store"
)

// geocodePlanAsync fills any missing start/end coordinates on a plan's parts
// from their addresses, then anchors any still-floating local times to the real
// timezone of their coordinates, in the background and best-effort. It's a
// no-op without a configured geocoder (e.g. in tests). On success it
// republishes the plan so open clients pick up the changes over SSE. The
// per-part work lives in geocode.PlanParts so the email-ingest path can reuse it.
func (a *API) geocodePlanAsync(tripID, planID int64) {
	if a.Geocoder == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		changed, err := geocode.PlanParts(ctx, a.Store, a.Geocoder, planID)
		if err != nil {
			return
		}
		if changed {
			a.publishPlanUpdated(ctx, tripID, planID)
			slog.Debug("geocoded + tz-anchored plan parts", "plan", planID)
		}
	}()
}

// tripCountryUnknown is the sentinel stored when a trip's place geocodes but no
// country comes back — so the lazy/backfill trigger doesn't re-query it forever.
// The FE treats it (like "") as "no flag".
const tripCountryUnknown = "zz"

// deriveTripCountryAsync derives a trip's ISO country code from its destination
// (falling back to its name) and caches it on the trip, in the background and
// best-effort. A no-op without a geocoder or when the country is already set. On
// success it republishes the trip so open clients (the trip list) pick up the
// flag without a manual reload.
func (a *API) deriveTripCountryAsync(tripID int64) {
	if a.Geocoder == nil {
		return
	}
	go func() {
		// Reverse-geocoding several endpoints at ~1 req/s can exceed 30s for a
		// busy trip, so allow a minute.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		t, err := a.Store.TripByID(ctx, tripID)
		if err != nil || t.CountryCode != "" {
			return
		}
		code, ok := a.deriveTripCountry(ctx, t)
		if !ok {
			code = tripCountryUnknown
		}
		if err := a.Store.SetTripCountry(ctx, tripID, code); err != nil {
			return
		}
		a.publishTripUpdated(ctx, tripID)
	}()
}

// tripAway weighs a trip's plotted plan-part endpoints by dwell time to decide
// where it actually goes. Each reverse-geocoded endpoint votes for its country,
// weighted by the owning part's duration: a week in a Tallinn hotel outweighs
// the brief UK cab rides and connecting flights at either end, so a there-and-
// back trip flies the destination's flag and not the origin's. Parts with no
// duration still count by presence (a one-second floor), so a trip of
// instantaneous markers degrades to a simple majority; ties break toward the
// earliest part (dwells arrive chronologically).
//
// One correction makes round trips robust when the real destination is
// unplottable — a hotel imported without an address has no coordinate, so it
// casts no vote, leaving only the flights and home↔airport transfers, which pile
// up at the origin and tie toward home. So when the heaviest country is also the
// trip's origin (the first plotted endpoint) and some other country is present,
// the heaviest *non-origin* country wins instead: the away end of the journey.
//
// It also returns a representative coordinate for the winning country (from that
// country's single longest stay) so a caller can reverse-geocode a "City,
// Country" label, and the origin country (for the destination-repair heuristic).
// ok is false when nothing is plotted.
type awayResult struct {
	code   string     // winning country, lowercase ISO 3166-1 alpha-2
	coord  [2]float64 // a representative coordinate within that country
	origin string     // the trip's origin/home country (first plotted endpoint)
}

func (a *API) tripAway(ctx context.Context, tripID int64) (awayResult, bool) {
	if a.Geocoder == nil {
		return awayResult{}, false
	}
	dwells, err := a.Store.TripPartDwells(ctx, tripID)
	if err != nil || len(dwells) == 0 {
		return awayResult{}, false
	}
	weight := map[string]float64{}
	repCoord := map[string][2]float64{}
	repSec := map[string]float64{}
	var order []string
	origin := ""
	for _, d := range dwells {
		// A one-second floor so a part with no duration still registers its
		// presence; real multi-hour/day stays dwarf it and decide the winner.
		w := d.Seconds
		if w <= 0 {
			w = 1
		}
		for _, c := range d.Coords {
			code, ok, gerr := a.Geocoder.ReverseCountry(ctx, c[0], c[1])
			if gerr != nil || !ok || code == "" {
				continue
			}
			if origin == "" {
				origin = code
			}
			if weight[code] == 0 {
				order = append(order, code)
			}
			weight[code] += w
			// Keep a coordinate from each country's longest single stay, so a
			// reverse-geocoded label names where time was actually spent rather
			// than a fly-through.
			if w > repSec[code] {
				repSec[code], repCoord[code] = w, c
			}
		}
	}
	if len(order) == 0 {
		return awayResult{}, false
	}
	best, bestW := "", 0.0
	for _, code := range order {
		if weight[code] > bestW {
			best, bestW = code, weight[code]
		}
	}
	if best == origin {
		// The origin only wins when the real destination is unplotted; prefer the
		// heaviest country that isn't home.
		altBest, altW := "", 0.0
		for _, code := range order {
			if code == origin {
				continue
			}
			if weight[code] > altW {
				altBest, altW = code, weight[code]
			}
		}
		if altBest != "" {
			best = altBest
		}
	}
	return awayResult{code: best, coord: repCoord[best], origin: origin}, true
}

// deriveTripCountry picks a trip's flag country from where it spends the most
// time (see tripAway). When nothing's plotted it falls back to the user-stated
// destination, and deliberately NEVER geocodes the trip name: a freeform name
// like "50's, Hopefully" matches a spurious place (a road in Oregon), flying the
// wrong flag. ok is false when nothing resolved (caller stores the "zz" sentinel
// so it isn't re-queried forever).
func (a *API) deriveTripCountry(ctx context.Context, t *store.Trip) (string, bool) {
	if away, ok := a.tripAway(ctx, t.ID); ok && away.code != "" {
		return away.code, true
	}
	if dest := strings.TrimSpace(t.Destination); dest != "" {
		if code, ok, err := a.Geocoder.GeocodeCountry(ctx, dest); err == nil && ok {
			return code, true
		}
	}
	return "", false
}

// deriveTripDestination reverse-geocodes the trip's away end (see tripAway) to a
// "City, Country" label plus its ISO country code, so the destination and the
// flag are derived from the same place and always agree. ok is false when no
// plotted part can be ranked (the caller leaves the destination blank for a
// later sweep). A no-op without a geocoder.
func (a *API) deriveTripDestination(ctx context.Context, t *store.Trip) (string, string, bool) {
	away, ok := a.tripAway(ctx, t.ID)
	if !ok {
		return "", "", false
	}
	place, code, ok, err := a.Geocoder.ReversePlace(ctx, away.coord[0], away.coord[1])
	if err != nil || !ok {
		return "", "", false
	}
	return place, code, true
}

// deriveAndStoreTripPlace fills a trip's destination (and, when blank, its flag
// country) from where its plans spend their time. Best-effort; returns true if
// it wrote anything so the caller can republish. The destination is set only
// when currently blank, never clobbering a user-stated one (the conditional
// UPDATE enforces this even against a concurrent edit). The flag is kept
// consistent with the destination: when we derive a country it adopts it
// (correcting an empty/stale/origin code); otherwise, when the flag is unset or
// the "zz" unknown sentinel, it falls back to the per-country dwell aggregation.
func (a *API) deriveAndStoreTripPlace(ctx context.Context, t *store.Trip) bool {
	changed := false
	dest, code, ok := a.deriveTripDestination(ctx, t)
	if ok && strings.TrimSpace(t.Destination) == "" {
		if set, err := a.Store.SetTripDestination(ctx, t.ID, dest); err == nil && set {
			t.Destination = dest
			changed = true
		}
	}
	switch {
	case code != "":
		// Align the flag with the destination we display, correcting an empty,
		// stale ("zz"), or origin country code.
		if t.CountryCode != code {
			if err := a.Store.SetTripCountry(ctx, t.ID, code); err == nil {
				t.CountryCode = code
				changed = true
			}
		}
	case t.CountryCode == "" || t.CountryCode == tripCountryUnknown:
		c := ""
		if dc, dok := a.deriveTripCountry(ctx, t); dok {
			c = dc
		}
		if c == "" {
			c = tripCountryUnknown
		}
		if t.CountryCode != c {
			if err := a.Store.SetTripCountry(ctx, t.ID, c); err == nil {
				t.CountryCode = c
				changed = true
			}
		}
	}
	return changed
}

// BackfillTripDestinations fills a destination (and flag) for trips that have
// none — e.g. imported from a calendar, whose feeds carry no destination field —
// from where their plans spend the most time. Best-effort, idempotent, paced by
// the geocoder; a no-op without one. Runs at startup after part coordinates are
// backfilled, so the dwell locations are already plotted.
func (a *API) BackfillTripDestinations(ctx context.Context) {
	if a.Geocoder == nil {
		return
	}
	trips, err := a.Store.TripsNeedingDestination(ctx)
	if err != nil {
		slog.Warn("destination backfill: query failed", "err", err)
		return
	}
	var fixed int
	for _, t := range trips {
		if a.deriveAndStoreTripPlace(ctx, t) {
			fixed++
			a.publishTripUpdated(ctx, t.ID)
		}
	}
	if fixed > 0 {
		slog.Info("destination backfill: derived trip destinations", "trips", fixed)
	}
}

// ReconcileTripPlaces re-derives the flag of trips whose place was settled
// before the away-end derivation existed — when a flag could be computed against
// half-geocoded plans (e.g. before a hotel's address geocoded) and then frozen,
// because the country/destination backfills only ever touch trips whose field is
// still blank. It runs once per trip (gated by place_reconciled) so restarts
// don't re-geocode the whole history.
//
// The flag (country_code) is only ever machine-derived, so it's re-derived and
// overwritten freely. The destination is user-editable, so it's only rewritten
// in the one unambiguous bug case: the stored destination resolves to the trip's
// own origin country (e.g. "Greater London" on a London→Dublin→London trip)
// while the away end places the trip elsewhere. A correct or hand-edited
// destination is left untouched. Best-effort, paced by the geocoder; a no-op
// without one. Runs at startup after coordinates, countries and destinations are
// backfilled, so the dwell data is complete.
func (a *API) ReconcileTripPlaces(ctx context.Context) {
	if a.Geocoder == nil {
		return
	}
	trips, err := a.Store.TripsNeedingPlaceReconcile(ctx)
	if err != nil {
		slog.Warn("place reconcile: query failed", "err", err)
		return
	}
	var flags, dests int
	for _, t := range trips {
		away, ok := a.tripAway(ctx, t.ID)
		if !ok || away.code == "" {
			// Nothing resolved this pass; leave it unmarked to retry once its
			// parts plot, rather than freezing an underived flag.
			continue
		}
		changed := false
		if away.code != t.CountryCode {
			if err := a.Store.SetTripCountry(ctx, t.ID, away.code); err == nil {
				t.CountryCode = away.code
				flags++
				changed = true
			}
		}
		// Repair only an origin-biased destination: it resolves to the origin
		// country while the trip clearly goes elsewhere.
		if dest := strings.TrimSpace(t.Destination); dest != "" && away.origin != "" && away.code != away.origin {
			if cur, ok, err := a.Geocoder.GeocodeCountry(ctx, dest); err == nil && ok && cur == away.origin {
				if place, _, ok, err := a.Geocoder.ReversePlace(ctx, away.coord[0], away.coord[1]); err == nil && ok && place != "" && place != dest {
					if err := a.Store.OverwriteTripDestination(ctx, t.ID, place); err == nil {
						t.Destination = place
						dests++
						changed = true
					}
				}
			}
		}
		if err := a.Store.MarkTripPlaceReconciled(ctx, t.ID); err != nil {
			slog.Warn("place reconcile: mark failed", "err", err, "trip", t.ID)
		}
		if changed {
			a.publishTripUpdated(ctx, t.ID)
		}
	}
	if flags > 0 || dests > 0 {
		slog.Info("place reconcile: corrected trips", "flags", flags, "destinations", dests)
	}
}

// BackfillTripCountries derives the ISO country code for any trip that doesn't
// have one yet (e.g. created before the flag feature, or via email ingest).
// Best-effort, idempotent, paced by the geocoder's rate limit; a no-op without a
// geocoder. Runs in the background at startup.
func (a *API) BackfillTripCountries(ctx context.Context) {
	if a.Geocoder == nil {
		return
	}
	trips, err := a.Store.TripsNeedingCountry(ctx)
	if err != nil {
		slog.Warn("country backfill: query failed", "err", err)
		return
	}
	var fixed int
	for _, t := range trips {
		code, ok := a.deriveTripCountry(ctx, t)
		if !ok {
			code = tripCountryUnknown
		}
		if err := a.Store.SetTripCountry(ctx, t.ID, code); err == nil {
			fixed++
		}
	}
	if fixed > 0 {
		slog.Info("country backfill: derived trip countries", "trips", fixed)
	}
}

// BackfillPartCoordinates geocodes any historical plan parts that have a
// free-text address but no coordinates — plans ingested before address
// geocoding existed, or while Nominatim was unavailable — so they finally plot
// on the map. Best-effort and idempotent (a part with coordinates no longer
// matches); a no-op without a configured geocoder. Runs in the background at
// startup. Geocoding is rate-limited (≈1 req/s via Nominatim), so this paces
// itself; we don't publish SSE per plan — open clients pick the coordinates up
// on their next trip fetch.
func (a *API) BackfillPartCoordinates(ctx context.Context) {
	if a.Geocoder == nil {
		return
	}
	planIDs, err := a.Store.PlanIDsNeedingGeocode(ctx)
	if err != nil {
		slog.Warn("geocode backfill: query failed", "err", err)
		return
	}
	var fixed int
	for _, planID := range planIDs {
		changed, gerr := geocode.PlanParts(ctx, a.Store, a.Geocoder, planID)
		if gerr != nil {
			slog.Warn("geocode backfill: plan failed", "err", gerr, "plan", planID)
			continue
		}
		if changed {
			fixed++
		}
	}
	if fixed > 0 {
		slog.Info("geocode backfill: geocoded plan parts", "plans", fixed)
	}
}

// BackfillPartTimezones anchors any historical parts that have coordinates but
// no timezone (ingested before coordinate-based tz resolution existed) to their
// real zone, shifting the stored instant so the local wall-clock is preserved.
// Best-effort and idempotent — once a part has a tz it is skipped. Logs a
// summary; never fatal.
func (a *API) BackfillPartTimezones(ctx context.Context) {
	parts, err := a.Store.PartsNeedingTZ(ctx)
	if err != nil {
		slog.Warn("tz backfill: query failed", "err", err)
		return
	}
	var fixed int
	for _, p := range parts {
		payload := store.UpdatePlanPartPayload{}
		geocode.ResolvePartTZ(p, &payload, p.StartLat, p.StartLon, p.EndLat, p.EndLon)
		if payload.IsEmpty() {
			continue
		}
		if _, uerr := a.Store.UpdatePlanPart(ctx, p.ID, payload); uerr == nil {
			fixed++
		}
	}
	if fixed > 0 {
		slog.Info("tz backfill: anchored part timezones", "parts", fixed)
	}
}
