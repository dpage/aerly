package geocode

import (
	"context"
	"time"

	"github.com/dpage/aerly/internal/geotz"
	"github.com/dpage/aerly/internal/store"
)

// PlanParts fills any missing start/end coordinates on a plan's parts from their
// addresses, then anchors any still-floating local times to the real timezone of
// their coordinates. It is best-effort: a geocode miss or a single failed update
// is skipped rather than aborting the rest. Returns whether any part changed so
// the caller can decide to republish over SSE. A nil geocoder is a no-op (e.g. in
// tests), as is a nil store.
//
// This is the shared core behind the HTTP handler's geocodePlanAsync and the
// email-ingest path — both need a committed plan's addressed parts plotted on the
// map, so the logic lives here in the neutral geocode package (handlers already
// imports emailingest, so emailingest can't import handlers).
func PlanParts(ctx context.Context, st *store.Store, g Geocoder, planID int64) (bool, error) {
	if st == nil || g == nil {
		return false, nil
	}
	parts, err := st.PartsByPlan(ctx, planID)
	if err != nil {
		return false, err
	}
	var changed bool
	for _, p := range parts {
		payload := store.UpdatePlanPartPayload{}
		startLat, startLon := p.StartLat, p.StartLon
		endLat, endLon := p.EndLat, p.EndLon

		// Fill missing coordinates from the address, falling back to the place
		// label (e.g. "Alicante Airport", "Melia Benidorm") when there's no
		// address OR the address doesn't resolve — so a transfer's airport
		// endpoint, which often arrives as a bare name, still plots rather than
		// collapsing onto the other end. Flight parts are skipped: their labels
		// are IATA codes located via the airport table / poller, which we must
		// not pre-empt with a fuzzy name lookup.
		if startLat == nil {
			if lat, lon, ok := geocodeEndpoint(ctx, g, p.Type, p.StartAddress, p.StartLabel); ok {
				payload.StartLat, payload.StartLon = &lat, &lon
				startLat, startLon = &lat, &lon
			}
		}
		if endLat == nil {
			if lat, lon, ok := geocodeEndpoint(ctx, g, p.Type, p.EndAddress, p.EndLabel); ok {
				payload.EndLat, payload.EndLon = &lat, &lon
				endLat, endLon = &lat, &lon
			}
		}

		// Anchor floating local times (tz unknown) to the zone of their
		// coordinates, shifting the instant so the displayed wall-clock is
		// preserved. Flights already carry a tz, so they're untouched.
		ResolvePartTZ(p, &payload, startLat, startLon, endLat, endLon)

		if !payload.IsEmpty() {
			if _, uerr := st.UpdatePlanPart(ctx, p.ID, payload); uerr == nil {
				changed = true
			}
		}
	}
	return changed, nil
}

// geocodeEndpoint resolves an endpoint to coordinates: the postal address first,
// then the place label as a fallback when there's no address or the address
// didn't resolve. Flight parts never fall back to the label (their labels are
// IATA codes, located via the airport table / poller). Returns ok=false when
// nothing resolved.
func geocodeEndpoint(ctx context.Context, g Geocoder, partType, address, label string) (float64, float64, bool) {
	if address != "" {
		if lat, lon, ok, err := g.Geocode(ctx, address); err == nil && ok {
			return lat, lon, true
		}
	}
	if partType != "flight" && label != "" {
		if lat, lon, ok, err := g.Geocode(ctx, label); err == nil && ok {
			return lat, lon, true
		}
	}
	return 0, 0, false
}

// ResolvePartTZ resolves a still-empty start/end tz from the part's coordinates
// and rewrites the stored instant so the local wall-clock is unchanged. A hotel's
// checkout (no end coordinates of its own) inherits the start tz. Does nothing for
// parts that already have a tz, or that have no usable coordinate.
func ResolvePartTZ(
	p *store.PlanPart,
	payload *store.UpdatePlanPartPayload,
	startLat, startLon, endLat, endLon *float64,
) {
	// The part's primary tz: from the start coordinate, else the end.
	primary := ""
	if startLat != nil && startLon != nil {
		if tz, ok := geotz.Lookup(*startLat, *startLon); ok {
			primary = tz
		}
	} else if endLat != nil && endLon != nil {
		if tz, ok := geotz.Lookup(*endLat, *endLon); ok {
			primary = tz
		}
	}

	if p.StartTZ == "" && primary != "" {
		tz := primary
		payload.StartTZ = &tz
		if s, ok := reinterpretLocal(p.StartsAt, tz); ok {
			payload.StartsAt = &s
		}
	}

	if p.EndTZ == "" && p.EndsAt != nil {
		etz := primary
		if endLat != nil && endLon != nil {
			if tz, ok := geotz.Lookup(*endLat, *endLon); ok {
				etz = tz
			}
		}
		if etz != "" {
			payload.EndTZ = &etz
			if e, ok := reinterpretLocal(*p.EndsAt, etz); ok {
				payload.EndsAt = &e
			}
		}
	}
}

// reinterpretLocal takes an instant whose UTC wall-clock digits are really a
// floating *local* time (the ingest convention for tz-less parts) and returns
// the instant those same digits denote in tzName. e.g. 16:00Z + "America/
// New_York" → 20:00Z (16:00 EDT). Returns ok=false if the zone won't load.
func reinterpretLocal(t time.Time, tzName string) (time.Time, bool) {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return t, false
	}
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), u.Hour(), u.Minute(), u.Second(), u.Nanosecond(), loc), true
}
