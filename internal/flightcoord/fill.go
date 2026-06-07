// Package flightcoord backfills the latitude/longitude of a flight plan_part's
// origin and destination: first from the embedded IATA table (free, in-memory),
// then from a Resolver when the table can't satisfy a leg. It is the shared core
// behind two callers that must fill coords identically:
//
//   - the poller's periodic NULL-coord sweep (every 4h), and
//   - the one-shot backfill fired right after a flight is ingested, so an
//     imported flight on an off-table airport (e.g. NQY/FAO) plots on the map
//     within seconds instead of waiting for the next sweep.
//
// Living in its own package keeps it importable from both the poller and the
// handlers without an import cycle.
package flightcoord

import (
	"context"
	"log/slog"
	"time"

	"github.com/dpage/aerly/internal/airports"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/store"
)

// Throttle is the minimum gap between resolver attempts for one flight row,
// keyed off its last_resolved_at. It doubles as the poller's sweep cadence: a
// stuck far-future flight self-heals once its schedule publishes without
// burning more than a handful of API calls per day. A freshly-ingested part has
// a nil last_resolved_at, so the post-ingest backfill is never throttled.
const Throttle = 4 * time.Hour

// Backfiller is the slice of *store.Store that Fill needs. An interface keeps
// the merge logic unit-testable without a live database.
type Backfiller interface {
	BackfillFlightPart(ctx context.Context, partID int64, in store.BackfillPayload) error
	RefreshFlightPartAirframe(ctx context.Context, partID int64, icao24, callsign string) error
}

// Fill resolves any NULL coordinate columns on a single flight part. It tries
// the embedded airports table first (free, in-memory), then the resolver for
// whatever the table couldn't satisfy — but only when a resolver is configured
// and the row's last_resolved_at throttle allows it. It returns whether the row
// actually changed, so the caller can decide to republish over SSE.
//
// Best-effort per row: a resolve failure is logged (and the throttle still
// bumped, so an unreachable flight doesn't burn quota every tick), and only a
// backfill *write* error propagates to the caller.
func Fill(ctx context.Context, st Backfiller, resolver providers.Resolver, f *store.Flight, now time.Time) (bool, error) {
	var update store.BackfillPayload
	changed := false
	var originNeedsResolver, destNeedsResolver bool

	// Table fast path.
	if f.OriginLat == nil && f.OriginIATA != "" {
		if lat, lon, ok := airports.Lookup(f.OriginIATA); ok {
			update.OriginIATA, update.OriginLat, update.OriginLon = f.OriginIATA, lat, lon
			changed = true
		} else {
			originNeedsResolver = true
		}
	}
	if f.DestLat == nil && f.DestIATA != "" {
		if lat, lon, ok := airports.Lookup(f.DestIATA); ok {
			update.DestIATA, update.DestLat, update.DestLon = f.DestIATA, lat, lon
			changed = true
		} else {
			destNeedsResolver = true
		}
	}

	// Resolver slow path — only when something the table can't satisfy remains,
	// a resolver is configured, and the throttle allows it.
	if (originNeedsResolver || destNeedsResolver) && resolver != nil && throttleAllowed(f, now) {
		rf, rerr := resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
		if rerr == nil {
			// Merge only the legs the table couldn't fill. A table-derived coord
			// on a satisfied leg must NOT be clobbered: BackfillFlightPart's
			// "only fill empty columns" rule short-circuits a leg whose payload
			// lat+lon are both zero, so a resolver returning zero coords for a
			// table-known leg would otherwise lose the table value.
			if originNeedsResolver {
				update.OriginIATA = rf.OriginIATA
				update.OriginLat, update.OriginLon = rf.OriginLat, rf.OriginLon
			}
			if destNeedsResolver {
				update.DestIATA = rf.DestIATA
				update.DestLat, update.DestLon = rf.DestLat, rf.DestLon
			}
			update.ICAO24, update.Callsign = rf.ICAO24, rf.Callsign
			update.Notes = rf.Notes
			update.OriginTerminal, update.DestTerminal = rf.OriginTerminal, rf.DestTerminal
			changed = true
		} else {
			slog.Warn("flightcoord: resolve failed", "ident", f.Ident, "id", f.ID, "err", rerr)
		}
		// Always bump last_resolved_at so unreachable flights don't burn API
		// quota on every attempt. Empty strings mean "don't overwrite airframe".
		icao24, callsign := "", ""
		if rerr == nil {
			icao24, callsign = rf.ICAO24, rf.Callsign
		}
		if terr := st.RefreshFlightPartAirframe(ctx, f.ID, icao24, callsign); terr != nil {
			slog.Error("flightcoord: bump last_resolved_at", "id", f.ID, "err", terr)
		}
	}

	if !changed {
		return false, nil
	}
	if err := st.BackfillFlightPart(ctx, f.ID, update); err != nil {
		return false, err
	}
	return true, nil
}

// throttleAllowed reports whether enough time has passed since the last resolver
// attempt for this flight to merit another. A nil last_resolved_at (never tried,
// e.g. a freshly-ingested part) means yes.
func throttleAllowed(f *store.Flight, now time.Time) bool {
	if f.LastResolvedAt == nil {
		return true
	}
	return now.Sub(*f.LastResolvedAt) >= Throttle
}
