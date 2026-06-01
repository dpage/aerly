package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/geotz"
	"github.com/dpage/aerly/internal/store"
)

// geocodePlanAsync fills any missing start/end coordinates on a plan's parts
// from their addresses, then anchors any still-floating local times to the real
// timezone of their coordinates, in the background and best-effort. It's a
// no-op without a configured geocoder (e.g. in tests). On success it
// republishes the plan so open clients pick up the changes over SSE.
func (a *API) geocodePlanAsync(tripID, planID int64) {
	if a.Geocoder == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		parts, err := a.Store.PartsByPlan(ctx, planID)
		if err != nil {
			return
		}
		var changed bool
		for _, p := range parts {
			payload := store.UpdatePlanPartPayload{}
			startLat, startLon := p.StartLat, p.StartLon
			endLat, endLon := p.EndLat, p.EndLon

			// Fill missing coordinates from the addresses.
			if p.StartAddress != "" && startLat == nil {
				if lat, lon, ok, gerr := a.Geocoder.Geocode(ctx, p.StartAddress); gerr == nil && ok {
					payload.StartLat, payload.StartLon = &lat, &lon
					startLat, startLon = &lat, &lon
				}
			}
			if p.EndAddress != "" && endLat == nil {
				if lat, lon, ok, gerr := a.Geocoder.Geocode(ctx, p.EndAddress); gerr == nil && ok {
					payload.EndLat, payload.EndLon = &lat, &lon
					endLat, endLon = &lat, &lon
				}
			}

			// Anchor floating local times (tz unknown) to the zone of their
			// coordinates, shifting the instant so the displayed wall-clock is
			// preserved. Flights already carry a tz, so they're untouched.
			resolvePartTZ(p, &payload, startLat, startLon, endLat, endLon)

			if !payload.IsEmpty() {
				if _, uerr := a.Store.UpdatePlanPart(ctx, p.ID, payload); uerr == nil {
					changed = true
				}
			}
		}
		if changed {
			a.publishPlanUpdated(ctx, tripID, planID)
			slog.Debug("geocoded + tz-anchored plan parts", "plan", planID)
		}
	}()
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
		resolvePartTZ(p, &payload, p.StartLat, p.StartLon, p.EndLat, p.EndLon)
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

// resolvePartTZ resolves a still-empty start/end tz from the part's coordinates
// and rewrites the stored instant so the local wall-clock is unchanged. A
// hotel's checkout (no end coordinates of its own) inherits the start tz. Does
// nothing for parts that already have a tz, or that have no usable coordinate.
func resolvePartTZ(
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
