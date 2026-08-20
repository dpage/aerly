// Package providers holds the external flight-data integrations: position
// trackers (Tracker) and schedule resolvers (Resolver), together with a
// dead-reckoning wrapper that fills coverage gaps.
//
// Concrete implementations:
//
//   - Stub        — in-memory; interpolates positions from the schedule alone.
//   - OpenSky     — ADS-B state vectors from opensky-network.org.
//   - DeadReckoner — wraps any Tracker and synthesises a position when the
//     inner tracker returns no fresh fix.
//   - AeroDataBox — schedule + airport + airframe lookups via RapidAPI.
package providers

import (
	"context"
	"errors"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// ErrFlightNotFound is returned by Resolver.Resolve (and helpers) when the
// upstream provider has no record of the requested flight. Callers can use
// it to drive fallback behaviour — e.g. AeroDataBox.Resolve tries several
// pad-length variants of the same ident before giving up.
var ErrFlightNotFound = errors.New("flight not found")

// ErrAirportNotFound is returned by AirportResolver.ResolveAirport when the
// upstream has no record (or no usable coordinates) for the requested IATA
// code. Callers treat it like a table miss — the airport simply stays
// unplotted — rather than a hard error.
var ErrAirportNotFound = errors.New("airport not found")

// ErrFlightUnscheduled is returned when the upstream knows the flight
// number for the requested date but has not published a schedule for it
// yet (or returned schedule fields we can't parse). Distinct from
// ErrFlightNotFound so the caller can surface a clearer
// "schedule not available" message than the store's bare
// "scheduled_out required".
var ErrFlightUnscheduled = errors.New("flight has no published schedule for that date yet")

// RateLimitReporter is an optional hook a provider invokes when an upstream
// rejects a request for rate-limit / quota reasons (HTTP 429). It exists so
// the operator can be alerted even though the tracker layer (DeadReckoner)
// swallows the error to fall back to extrapolation, hiding it from the poller.
//
// provider is a short label for the integration ("OpenSky", "AeroDataBox");
// detail is a human phrase for the alert body — the upstream's own message
// when we have one, otherwise a remediation hint. Implementations must treat
// a nil reporter as "no hook configured" and call it at most once per
// rejection (not once per internal retry).
type RateLimitReporter func(provider, detail string)

// Tracker fetches (or fabricates) a single positional fix for one flight at
// the given wall-clock time. Implementations should return:
//
//   - a non-nil *store.Position with IsEstimated set appropriately, OR
//   - nil, nil  if no fix is available (e.g. ADS-B silence; the caller may
//     hand the situation to a fallback such as a DeadReckoner).
//
// Trackers are NOT responsible for updating any of the flight's schedule /
// status fields — that derivation happens in SQL from the times alone.
type Tracker interface {
	Track(ctx context.Context, f *store.Flight, now time.Time) (*store.Position, error)
}

// BatchTracker is an optional extension a Tracker may implement when the
// upstream can answer for many airframes in a single call. The poller hands
// the whole set of flights it is about to process to Prefetch, and the
// per-flight Track calls that follow are then served from whatever Prefetch
// cached, without further upstream traffic.
//
// This matters because OpenSky bills per request, not per airframe: one
// /states/all filtered by fifty icao24 addresses costs the same single credit
// as one filtered by a single address. Polling each flight separately turned a
// busy day into N credits per tick and exhausted the daily allowance within the
// hour; batching makes the cost flat regardless of how many flights are in the
// air.
//
// Prefetch is best-effort and returns nothing: a failed batch must leave the
// tracker in a state where Track reports "no fix" rather than silently
// re-issuing one request per flight, since that fan-out is the very thing
// being avoided. Callers should invoke it at most once per tick.
type BatchTracker interface {
	Tracker
	Prefetch(ctx context.Context, flights []*store.Flight)
}

// Prefetch primes t for the given tick when the tracker (or anything it wraps)
// supports batching, and is a no-op otherwise. Wrappers such as SpeedGate and
// DeadReckoner satisfy BatchTracker by forwarding to their inner tracker, so a
// fully composed chain still reaches the OpenSky batch underneath.
func Prefetch(ctx context.Context, t Tracker, flights []*store.Flight) {
	if bt, ok := t.(BatchTracker); ok {
		bt.Prefetch(ctx, flights)
	}
}

// ResolvedFlight is the airline-data-source view of a single scheduled
// flight, used to autofill the Add Flight dialog from just an ident + date.
type ResolvedFlight struct {
	Ident        string
	ScheduledOut time.Time
	ScheduledIn  time.Time
	OriginIATA   string
	OriginLat    float64
	OriginLon    float64
	DestIATA     string
	DestLat      float64
	DestLon      float64
	// Origin/DestName are the provider's human-readable airport names (e.g.
	// "Faro"), used to build friendly "Name (CODE)" place labels for airports
	// the embedded table doesn't carry. Empty when the provider omits them.
	OriginName string
	DestName   string
	ICAO24     string // 24-bit Mode-S hex address (lowercase) when known
	Callsign   string // ICAO radio callsign (e.g. "DLH493"); empty when not yet assigned
	Notes      string // free-text summary — typically airline + aircraft model
	// AircraftType is the human-readable airframe model (e.g. "Boeing
	// 777-300ER"), surfaced on the flight tile. Empty when the provider hasn't
	// assigned an airframe yet.
	AircraftType string
	// Gate / terminal as reported on the departure/arrival movement. Many
	// airports populate these; absent → empty string. Gate changes are what
	// the gate-change alert detects, so the resolver surfaces the live value
	// on every resolve (not just first-fill).
	OriginGate     string
	DestGate       string
	OriginTerminal string
	DestTerminal   string
	// DestBaggageBelt is the arrival baggage belt/carousel. Arrival-only (no
	// departure equivalent); empty when the provider hasn't published it yet.
	// Live and updatable like gate — a change drives a belt-change alert.
	DestBaggageBelt string
	// EstimatedIn / ActualIn are the live arrival times: the airline's current
	// estimate, and the observed touchdown once it has happened. Both nil for a
	// flight the provider only holds a timetable for, which is why everything
	// downstream falls back to ScheduledIn. They are what lets a flight running
	// late stay Enroute past its timetabled arrival instead of being declared
	// Arrived whilst it is still in the air.
	//
	// There is deliberately no EstimatedOut / ActualOut counterpart. The
	// provider's actual departure is wheels-off rather than off-block, so it
	// runs a taxi-time later than the scheduled gate departure it would be
	// compared against, and feeding it into the departure-delay calculation
	// would report a phantom ten-minute delay on almost every flight.
	EstimatedIn *time.Time
	ActualIn    *time.Time
}

// Resolver maps a flight number + departure date to a ResolvedFlight. The
// concrete implementation is whatever airline-data provider the operator
// has configured (AeroDataBox today; AviationStack / FlightStats / similar
// could slot in here too).
type Resolver interface {
	Resolve(ctx context.Context, ident string, date time.Time) (*ResolvedFlight, error)
}

// Airport is the provider's view of a single airport, keyed by IATA code. It
// carries just what the coordinate backfill needs — coordinates plus a display
// name — for airports the embedded table doesn't cover.
type Airport struct {
	IATA string
	Name string // human-readable airport name, for "Name (CODE)" labels
	Lat  float64
	Lon  float64
	TZ   string // IANA timezone, when the provider reports one; else ""
}

// AirportResolver maps a bare IATA code to its coordinates and name. It is
// deliberately separate from Resolver: a flight-number lookup is bounded to the
// provider's ±180-day schedule window, but an airport's location is static, so
// the airport endpoint answers for a flight of any age. This is what backfills
// off-table airports on imported flights too old (or too far ahead) for the
// flight lookup to touch. AeroDataBox satisfies both interfaces.
type AirportResolver interface {
	ResolveAirport(ctx context.Context, iata string) (*Airport, error)
}
