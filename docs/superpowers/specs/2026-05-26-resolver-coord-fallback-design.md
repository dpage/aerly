# Resolver Fallback for Unknown-IATA Coords

## Problem

The embedded IATA → (lat, lon, tz) table at `internal/airports/table.go` is
hand-curated and covers about 240 airports — mostly hubs and busy secondary
cities. When a user adds a flight whose origin or destination IATA is not in
the table, `Store.CreateFlight` writes the row with NULL coord columns, and
the UI shows a "no map" pill (`web/src/components/FlightList.tsx:227`).

The poller does eventually backfill coords from the configured Resolver
(AeroDataBox today), but only for flights surfaced by `ActiveFlights`, which
filters to `scheduled_out - 30 minutes <= NOW()`. Flights more than 30
minutes in the future therefore stay flagged "no map" until the day-of
poll window opens.

Concrete trigger: a BRS↔SID trip (Bristol ↔ Amílcar Cabral, Cape Verde) was
booked weeks ahead. SID was not in the table, so both flight rows had at
least one NULL coord and both showed "no map".

## Goal

When a user creates or updates a flight whose IATA isn't in the embedded
table, fill the missing coords from the configured Resolver at request
time — so the flight appears on the map immediately, not on the next poll
tick (which may be days away).

The embedded table remains the fast path for known airports.

## Non-goals

- No new provider interface. The existing `providers.Resolver.Resolve`
  already returns airport coords; we reuse it.
- No change to the poller. Its 30-min `ActiveFlights` window and 12-h
  late-refresh logic stay as-is.
- No retroactive fix for existing NULL-coord rows. Users re-save the
  flight (which re-runs the IATA lookup) or wait for the poller.
- No "lookup just the missing leg" optimisation. `BackfillFlight` is a
  no-op on already-filled columns, so we always resolve the whole flight
  when anything is missing.
- No expansion of the embedded table beyond adding SID. Table growth is a
  separate decision driven by which destinations users actually hit.

## Design

### Trigger

In `(*API).createFlight` (`internal/handlers/flights.go:118`) and
`(*API).updateFlight` (`internal/handlers/flights.go:158`), immediately
after `Store.CreateFlight` / `Store.UpdateFlight` returns, check the
returned `*store.Flight`:

```
if a.Resolver != nil && (f.OriginLat == nil || f.OriginLon == nil ||
                         f.DestLat == nil   || f.DestLon == nil) {
    f = a.backfillCoordsIfNeeded(ctx, f)
}
```

Flights with all four coord columns populated skip the helper entirely —
the embedded table stays the fast path with zero added latency for the
common case.

### Helper

A single new method on the API receiver:

```go
// backfillCoordsIfNeeded synchronously resolves the flight via the
// configured Resolver and writes any coords / airframe / notes the
// resolver returned, using Store.BackfillFlight (which only fills empty
// columns and preserves user-supplied values). Returns the refreshed
// *store.Flight on success, or f unchanged on any error.
func (a *API) backfillCoordsIfNeeded(ctx context.Context, f *store.Flight) *store.Flight
```

Steps:

1. `rf, err := a.Resolver.Resolve(ctx, f.Ident, f.ScheduledOut)` — same
   call the poller makes at `poller.go:186`.
2. On error: `slog.Warn` (matching `poller.resolveAndUpdate`'s pattern)
   and return `f` unchanged. The caller's create/update still succeeds
   with NULL coords; "no map" persists; poller will retry within its
   normal window.
3. On success: `Store.BackfillFlight(ctx, f.ID, store.BackfillPayload{
   OriginIATA: rf.OriginIATA, OriginLat: rf.OriginLat, OriginLon: rf.OriginLon,
   DestIATA: rf.DestIATA, DestLat: rf.DestLat, DestLon: rf.DestLon,
   ICAO24: rf.ICAO24, Callsign: rf.Callsign, Notes: rf.Notes })` —
   identical payload to `poller.go:197`.
4. `fresh, err := a.Store.FlightByID(ctx, f.ID)`. On error, log and
   return `f` (the underlying row was still updated; we just couldn't
   re-read it).
5. Return `fresh`.

### Why reuse `BackfillFlight`

`BackfillFlight` already enforces "only write a column when the existing
value is empty/NULL" via its CASE / COALESCE clauses
(`internal/store/flights.go:265-282`). That invariant is the reason a
user-typed note or manually-entered ICAO24 isn't clobbered when the
poller later resolves the flight. Reusing the same function here keeps
that protection in one place rather than duplicating the rule in a new
SQL UPDATE.

### Why synchronous

The Resolve call typically takes 1–3 s against AeroDataBox. We pay that
latency at create/update time on the unknown-IATA path only — known IATAs
skip the helper. The win is the UX: the user sees their flight on the map
immediately, not after the next poll tick (which for a trip months out
means "never" for practical purposes).

### Handler integration

In both `createFlight` and `updateFlight`, the swap is one line, placed
right after the existing store call and before the DTO is assembled (so
the response and the SSE publish both reflect the backfilled row).

`createFlight` currently runs `AddPassenger` / `AddShare` loops between
the store call and the DTO build — the backfill should happen before
those loops so a failure mid-loop doesn't leave the DTO inconsistent. In
practice the loops only fail on bad user input, but ordering it this way
keeps the helper return value the canonical source.

### Failure modes

| Resolver state                  | Behaviour                                                                          |
| ------------------------------- | ---------------------------------------------------------------------------------- |
| `a.Resolver == nil`             | Helper never called; flight saved with NULL coords; "no map" persists.             |
| Resolver returns flight         | Coords + airframe + notes backfilled; DTO has coords; map renders immediately.     |
| `ErrFlightNotFound`             | Logged at WARN; flight saved with NULL coords; poller will retry within window.    |
| Network/timeout/transport error | Logged at WARN; flight saved with NULL coords; same retry path as above.           |
| `FlightByID` fails post-write   | Logged at ERROR; helper returns the pre-backfill `f` — DTO under-reports coords but the DB row is correct. Next read will be accurate. |

No failure mode causes the create/update HTTP request to fail. The user's
write always succeeds; the map is a best-effort enhancement.

## Tests

Add to `internal/handlers/handlers_test.go`:

1. **Known IATAs, resolver present** — no resolver call, coords from
   table, DTO has coords. Guards the fast path.
2. **Unknown IATA, resolver returns flight** — resolver called once,
   DTO has coords from resolver, DB row has coords from resolver.
3. **Unknown IATA, resolver returns `ErrFlightNotFound`** — flight
   created with NULL coords, HTTP 201, no error surfaced.
4. **Unknown IATA, resolver returns transport error** — same as above
   (NULL coords, success response, WARN log).
5. **Unknown IATA, `Resolver == nil`** — flight created with NULL
   coords, no panic.
6. **Update changes origin IATA from known→unknown** — resolver called,
   coords backfilled (or NULL if resolver fails).
7. **Update changes both IATAs from unknown→known** — resolver NOT
   called (both table hits fill the coords during `Store.UpdateFlight`,
   so the post-update row has no NULL coord columns).
8. **Update changes one of two unknown IATAs to known, the other stays
   unknown** — helper still runs (the still-unknown leg is NULL).
   `BackfillFlight` preserves the table-derived coord on the now-known
   leg and only fills the still-NULL leg's coord columns.

The existing `fakeResolver` in `handlers_test.go:27` returns a fixed
empty result. Extend it (or introduce a variant) to be configurable
per-test: return a specific `ResolvedFlight`, return `ErrFlightNotFound`,
or return a transport error. Keep the existing zero-value behaviour as
the default to avoid touching unrelated tests.

## Risks

- **AeroDataBox quota.** One extra Resolve call per unknown-IATA create.
  The poller would make this call anyway within 24 h of departure; we're
  shifting the timing, not adding net traffic. Negligible.
- **Latency on create.** 1–3 s added on the unknown path. Acceptable —
  users typing a less-common IATA expect a brief pause more than they
  expect a broken map.
- **Resolver returning a different IATA than the user typed.** Possible
  in theory (e.g. user types a codeshare-only IATA). `BackfillFlight`'s
  IATA columns are gated by "only fill if empty AND new value non-empty",
  so a user-typed IATA is never overwritten — only the coord columns
  beneath it get filled. Behaviour is correct but worth noting.

## Out-of-scope follow-ups (not part of this work)

- Periodically auditing which IATAs hit the fallback most often and
  promoting them to the embedded table (avoids the API cost on busy
  unknown destinations).
- A separate `LookupAirport(iata)` provider method for cases where we
  want airport coords without a flight ident (no current use case).
- Relaxing the poller's `ActiveFlights` window for coord-missing rows
  (the synchronous fallback obviates this — only edge case is a flight
  created while the resolver was down).
