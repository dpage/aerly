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
	"errors"
	"log/slog"
	"strings"
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
	MarkFlightPartResolved(ctx context.Context, partID int64, originCode, originLabel, destCode, destLabel string) error
}

// Fill resolves any NULL coordinate columns on a single flight part. It tries
// the embedded airports table first (free, in-memory), then two provider
// fallbacks for whatever the table couldn't satisfy — but only when a provider
// is configured and the row's last_resolved_at throttle allows it:
//
//   - the flight lookup (resolver) maps ident+date to a whole flight, enriching
//     the row with airframe / gates / terminals as a bonus, but is bounded to
//     the provider's ±180-day schedule window;
//   - the airport lookup (airportResolver) maps a bare IATA code straight to
//     coordinates, with no date window — so it plots off-table airports on
//     flights too old or too far ahead for the flight lookup (e.g. an imported
//     KBP-ARN from last year). It only fires for a leg the table missed and the
//     flight lookup didn't already fill.
//
// It returns whether the row actually changed, so the caller can decide to
// republish over SSE.
//
// Best-effort per row: a resolve failure is logged (and the throttle still
// bumped, so an unreachable flight doesn't burn quota every tick), and only a
// backfill *write* error propagates to the caller.
func Fill(ctx context.Context, st Backfiller, resolver providers.Resolver, airportResolver providers.AirportResolver, f *store.Flight, now time.Time) (bool, error) {
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

	// Provider slow path — only when something the table can't satisfy remains,
	// at least one provider is configured, and the throttle allows it.
	if (originNeedsResolver || destNeedsResolver) && (resolver != nil || airportResolver != nil) && throttleAllowed(f, now) {
		var originName, destName string
		resolvedAny := false

		// Flight lookup. Merges only the legs the table couldn't fill: a
		// table-derived coord on a satisfied leg must NOT be clobbered, since
		// BackfillFlightPart's "only fill empty columns" rule short-circuits a
		// leg whose payload lat+lon are both zero, so a resolver returning zero
		// coords for a table-known leg would otherwise lose the table value.
		var flightErr error
		if resolver != nil {
			rf, rerr := resolver.Resolve(ctx, f.Ident, f.ScheduledOut)
			flightErr = rerr
			if rerr == nil {
				if originNeedsResolver {
					update.OriginIATA = rf.OriginIATA
					update.OriginLat, update.OriginLon = rf.OriginLat, rf.OriginLon
					originName = rf.OriginName
				}
				if destNeedsResolver {
					update.DestIATA = rf.DestIATA
					update.DestLat, update.DestLon = rf.DestLat, rf.DestLon
					destName = rf.DestName
				}
				update.ICAO24, update.Callsign = rf.ICAO24, rf.Callsign
				update.Notes = rf.Notes
				update.OriginTerminal, update.DestTerminal = rf.OriginTerminal, rf.DestTerminal
				changed = true
				resolvedAny = true
			} else {
				slog.Warn("flightcoord: resolve failed", "ident", f.Ident, "id", f.ID, "err", rerr)
			}
		}

		// Airport fallback for any leg still without coordinates. Skipped after a
		// transient flight-lookup error (e.g. a 429), which the airport endpoint
		// would likely hit too — but run when the flight lookup was absent,
		// succeeded but left a leg unfilled, or returned not-found (which is what
		// an out-of-window old flight reports).
		if airportResolver != nil && (resolver == nil || flightErr == nil || errors.Is(flightErr, providers.ErrFlightNotFound)) {
			if originNeedsResolver && update.OriginLat == 0 && update.OriginLon == 0 && f.OriginIATA != "" {
				if ap, aerr := airportResolver.ResolveAirport(ctx, f.OriginIATA); aerr == nil {
					update.OriginIATA = ap.IATA
					update.OriginLat, update.OriginLon = ap.Lat, ap.Lon
					originName = ap.Name
					changed, resolvedAny = true, true
				} else if !errors.Is(aerr, providers.ErrAirportNotFound) {
					slog.Warn("flightcoord: airport resolve failed", "iata", f.OriginIATA, "id", f.ID, "err", aerr)
				}
			}
			if destNeedsResolver && update.DestLat == 0 && update.DestLon == 0 && f.DestIATA != "" {
				if ap, aerr := airportResolver.ResolveAirport(ctx, f.DestIATA); aerr == nil {
					update.DestIATA = ap.IATA
					update.DestLat, update.DestLon = ap.Lat, ap.Lon
					destName = ap.Name
					changed, resolvedAny = true, true
				} else if !errors.Is(aerr, providers.ErrAirportNotFound) {
					slog.Warn("flightcoord: airport resolve failed", "iata", f.DestIATA, "id", f.ID, "err", aerr)
				}
			}
		}

		// A provider returned a record for at least one leg: flip resolved and
		// upgrade the part's bare IATA labels to the friendly "Name (CODE)" form
		// (preserving any hand-edited label). Use the resolved code for a leg we
		// filled, so a leg whose stored code was blank still gets a sensible label.
		if resolvedAny {
			effOrigin, effDest := f.OriginIATA, f.DestIATA
			if originNeedsResolver && update.OriginIATA != "" {
				effOrigin = update.OriginIATA
			}
			if destNeedsResolver && update.DestIATA != "" {
				effDest = update.DestIATA
			}
			if merr := st.MarkFlightPartResolved(ctx, f.ID,
				f.OriginIATA, airports.Label(effOrigin, originName),
				f.DestIATA, airports.Label(effDest, destName)); merr != nil {
				slog.Error("flightcoord: mark resolved", "id", f.ID, "err", merr)
			}
		}

		// Always bump last_resolved_at so unreachable flights don't burn API
		// quota on every attempt. Empty strings mean "don't overwrite airframe";
		// only the flight lookup supplies airframe data, so an airport-only fill
		// leaves these blank.
		if terr := st.RefreshFlightPartAirframe(ctx, f.ID, update.ICAO24, update.Callsign); terr != nil {
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

// RouteUpdateFromResolved maps a provider resolve into a store.FlightRouteUpdate
// that overwrites the route, schedule, airframe, friendly labels, timezones and
// coordinates and marks the flight resolved. The part's start/end instants
// mirror the schedule. Shared by the flight-edit handler (re-resolve on an ident
// change) and the one-time relabel backfill. Ident is left for the caller.
func RouteUpdateFromResolved(rf *providers.ResolvedFlight) store.FlightRouteUpdate {
	resolved := true
	origin := strings.ToUpper(strings.TrimSpace(rf.OriginIATA))
	dest := strings.ToUpper(strings.TrimSpace(rf.DestIATA))
	originLabel := airports.Label(origin, rf.OriginName)
	destLabel := airports.Label(dest, rf.DestName)
	up := store.FlightRouteUpdate{
		Resolved:       &resolved,
		OriginIATA:     &origin,
		DestIATA:       &dest,
		ICAO24:         strPtrIfSet(rf.ICAO24),
		Callsign:       strPtrIfSet(rf.Callsign),
		OriginGate:     strPtrIfSet(rf.OriginGate),
		DestGate:       strPtrIfSet(rf.DestGate),
		OriginTerminal: strPtrIfSet(rf.OriginTerminal),
		DestTerminal:   strPtrIfSet(rf.DestTerminal),
		AircraftType:   strPtrIfSet(rf.AircraftType),
		StartLabel:     &originLabel,
		EndLabel:       &destLabel,
	}
	if !rf.ScheduledOut.IsZero() {
		so := rf.ScheduledOut
		up.ScheduledOut, up.StartsAt = &so, &so
	}
	if !rf.ScheduledIn.IsZero() {
		si := rf.ScheduledIn
		up.ScheduledIn, up.EndsAt = &si, &si
	}
	if tz, ok := airports.LookupTZ(origin); ok {
		up.StartTZ = &tz
	}
	if tz, ok := airports.LookupTZ(dest); ok {
		up.EndTZ = &tz
	}
	up.StartLat, up.StartLon, up.ClearStartCoords = AirportCoords(origin, rf.OriginLat, rf.OriginLon)
	up.EndLat, up.EndLon, up.ClearEndCoords = AirportCoords(dest, rf.DestLat, rf.DestLon)
	return up
}

// AirportCoords picks an airport's coordinates: the provider's value when
// non-zero, else the embedded table. When neither has it (an off-table code
// with no provider fix) it signals a clear, so a stale map pin is dropped.
func AirportCoords(code string, provLat, provLon float64) (lat, lon *float64, clear bool) {
	if provLat != 0 || provLon != 0 {
		return &provLat, &provLon, false
	}
	if la, lo, ok := airports.Lookup(code); ok {
		return &la, &lo, false
	}
	return nil, nil, true
}

func strPtrIfSet(s string) *string {
	if t := strings.TrimSpace(s); t != "" {
		return &t
	}
	return nil
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
