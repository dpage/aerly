package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// geocodePlanAsync fills any missing start/end coordinates on a plan's parts
// from their addresses, in the background and best-effort. It's a no-op without
// a configured geocoder (e.g. in tests). On success it republishes the plan so
// open clients pick up the new coordinates over SSE.
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
			if p.StartAddress != "" && p.StartLat == nil {
				if lat, lon, ok, err := a.Geocoder.Geocode(ctx, p.StartAddress); err == nil && ok {
					if _, err := a.Store.UpdatePlanPart(ctx, p.ID, store.UpdatePlanPartPayload{StartLat: &lat, StartLon: &lon}); err == nil {
						changed = true
					}
				}
			}
			if p.EndAddress != "" && p.EndLat == nil {
				if lat, lon, ok, err := a.Geocoder.Geocode(ctx, p.EndAddress); err == nil && ok {
					if _, err := a.Store.UpdatePlanPart(ctx, p.ID, store.UpdatePlanPartPayload{EndLat: &lat, EndLon: &lon}); err == nil {
						changed = true
					}
				}
			}
		}
		if changed {
			a.publishPlanUpdated(ctx, tripID, planID)
			slog.Debug("geocoded plan parts", "plan", planID)
		}
	}()
}
