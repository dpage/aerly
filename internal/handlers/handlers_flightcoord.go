package handlers

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/flightcoord"
)

// resolveFlightCoordsAsync backfills the coordinates of a freshly-committed
// plan's flight parts whose airports aren't in the embedded table, with a single
// resolver lookup, in the background and best-effort. It's the one-shot
// counterpart to the poller's periodic sweep: an imported flight on an off-table
// airport (e.g. NQY → FAO, neither in the embedded table) plots on the map
// within seconds of ingest instead of waiting up to 4h for the next sweep tick.
//
// A no-op without a configured resolver. The embedded-table lookup already ran
// at commit time (the importer / propose step), so FlightPartsByPlanMissingCoords
// returns only legs the table couldn't satisfy — this spends an API call solely
// when a leg is genuinely off-table. On success it republishes the plan so open
// clients pick up the new pins over SSE.
func (a *API) resolveFlightCoordsAsync(tripID, planID int64) {
	if a.Resolver == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		parts, err := a.Store.FlightPartsByPlanMissingCoords(ctx, planID)
		if err != nil {
			slog.Warn("flight backfill: query failed", "plan", planID, "err", err)
			return
		}
		now := time.Now()
		var changed bool
		for _, f := range parts {
			ok, ferr := flightcoord.Fill(ctx, a.Store, a.Resolver, a.AirportResolver, f, now)
			if ferr != nil {
				slog.Warn("flight backfill: fill failed", "plan", planID, "part", f.ID, "err", ferr)
				continue
			}
			changed = changed || ok
		}
		if changed {
			a.publishPlanUpdated(ctx, tripID, planID)
			slog.Debug("flight backfill: filled coords", "plan", planID)
		}
	}()
}
