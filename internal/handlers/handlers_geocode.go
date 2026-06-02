package handlers

import (
	"context"
	"log/slog"
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		t, err := a.Store.TripByID(ctx, tripID)
		if err != nil || t.CountryCode != "" {
			return
		}
		query := t.Destination
		if query == "" {
			query = t.Name
		}
		if query == "" {
			return
		}
		code, ok, gerr := a.Geocoder.GeocodeCountry(ctx, query)
		if gerr != nil {
			return
		}
		if !ok {
			code = tripCountryUnknown
		}
		if err := a.Store.SetTripCountry(ctx, tripID, code); err != nil {
			return
		}
		a.publishTripUpdated(ctx, tripID)
	}()
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
		query := t.Destination
		if query == "" {
			query = t.Name
		}
		code, ok, gerr := a.Geocoder.GeocodeCountry(ctx, query)
		if gerr != nil {
			continue
		}
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
