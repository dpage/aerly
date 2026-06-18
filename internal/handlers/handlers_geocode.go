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

// deriveTripCountry picks a trip's flag country. It prefers the country the trip
// spends the most time in: a week in a Tallinn hotel outweighs the brief UK cab
// rides and the connecting flights at either end, so a there-and-back trip flies
// the destination's flag and not the origin's. (A plain endpoint count gets this
// wrong — a UK→Estonia round trip has more UK endpoints, the home pickup, the
// airport, and both again on the way back, than Estonian ones — so we weight each
// reverse-geocoded endpoint by the owning part's duration; the country with the
// most dwell time wins, ties broken by earliest part. Parts with no duration
// still count by presence, so a trip of instantaneous markers degrades to a
// simple majority.) When nothing's plotted it falls back to the user-stated
// destination, and deliberately NEVER geocodes the trip name: a freeform name
// like "50's, Hopefully" matches a spurious place (a road in Oregon), flying the
// wrong flag. ok is false when nothing resolved (caller stores the "zz"
// sentinel so it isn't re-queried forever).
func (a *API) deriveTripCountry(ctx context.Context, t *store.Trip) (string, bool) {
	if dwells, err := a.Store.TripPartDwells(ctx, t.ID); err == nil && len(dwells) > 0 {
		weight := map[string]float64{}
		var order []string
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
				if weight[code] == 0 {
					order = append(order, code)
				}
				weight[code] += w
			}
		}
		best, bestW := "", 0.0
		for _, code := range order {
			if weight[code] > bestW {
				best, bestW = code, weight[code]
			}
		}
		if best != "" {
			return best, true
		}
	}
	if dest := strings.TrimSpace(t.Destination); dest != "" {
		if code, ok, err := a.Geocoder.GeocodeCountry(ctx, dest); err == nil && ok {
			return code, true
		}
	}
	return "", false
}

// deriveTripDestination picks the location where the trip spends the most time
// in a single part — a multi-day hotel stay dwarfs the transfers around it — and
// reverse-geocodes it to a "City, Country" label plus its ISO country code. The
// same dwell signal that decides the flag, narrowed to the single longest stay
// so the destination reads as one place. ok is false when no plotted part can be
// ranked (the caller leaves the destination blank for a later sweep). A no-op
// without a geocoder.
func (a *API) deriveTripDestination(ctx context.Context, t *store.Trip) (string, string, bool) {
	if a.Geocoder == nil {
		return "", "", false
	}
	dwells, err := a.Store.TripPartDwells(ctx, t.ID)
	if err != nil || len(dwells) == 0 {
		return "", "", false
	}
	var best store.TripPartDwell
	bestSec := -1.0
	for _, d := range dwells {
		if len(d.Coords) == 0 {
			continue
		}
		if d.Seconds > bestSec {
			bestSec, best = d.Seconds, d
		}
	}
	if bestSec < 0 {
		return "", "", false
	}
	// The arrival/last endpoint is where time is spent (a hotel's location, a
	// transfer's destination).
	c := best.Coords[len(best.Coords)-1]
	place, code, ok, err := a.Geocoder.ReversePlace(ctx, c[0], c[1])
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
