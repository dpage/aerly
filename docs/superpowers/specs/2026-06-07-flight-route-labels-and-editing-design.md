# Flight route labels & editing — design

**Date:** 2026-06-07
**Status:** Approved (pending spec review)

## Problem

Two related shortcomings in how flight plans surface and accept edits:

1. **Labels are bare IATA codes.** A flight's timeline place line reads `NQY → FAO`
   because the part's `start_label`/`end_label` default to the IATA codes at ingest
   (`internal/planops/propose.go:270-274`). We want the friendly `Newquay (NQY) →
   Faro (FAO)`.

2. **A flight's route is not editable, and the field that *is* editable is the wrong
   one.** The Edit dialog (`web/src/components/PlanEditDialog.tsx`) lets an editor
   change the part's free-text place *label*, but that label is decorative — it never
   touches `flight_details.{origin,dest}_iata`, which is what the poller tracks against
   and what the map's `FlightDetailCard` shows. So the timeline and the map/tracker can
   silently diverge, and there is no supported way to correct a wrong route.

## Key insight: identity vs. derived data

A flight's **identity** is its `ident` (flight number) + date. Everything else — the
route IATAs, schedule, gates, coordinates, live position, status — is **derived** by
resolving that identity against the flight data provider (AeroDataBox).

Therefore a directly-editable IATA is conceptually backwards: for any flight the
provider can track, the next poll re-derives the route and overwrites whatever the user
typed. The IATA is a resolution *output*, not user input.

The codebase already embodies this at ingest: `enrichFlight`
(`internal/planops/propose.go:328`) resolves by ident; on success it adopts the
provider's route/schedule, and on failure it falls back to the email's own IATAs
(`flightFromLeg`). The poller then keeps retrying unresolved flights. "Resolve, else
fall back to manual data" is the existing architecture — this design extends it to the
edit path.

## Design

### 1. Friendly labels (display / read path)

**Label format:** `Name (CODE)`, e.g. `London Heathrow (LHR)`, `Faro (FAO)`. Falls back
to the bare code (`FAO`) when no name is available — never blank.

**Name source, in order:**
- Embedded airports table `Entry.Name` (`internal/airports`) — free, in-memory, covers
  ~222 busy airports.
- Provider airport name — for off-table airports (NQY, FAO, …).
- Bare code — when neither resolves.

**New helper:** `airports.Label(iata, providerName string) string` returning
`"Name (CODE)"` per the rules above.

**Thread provider names through:** add `OriginName` / `DestName` to
`providers.ResolvedFlight`, populated from `adbAirport.Name` in
`internal/providers/aerodatabox.go`. The `CachedResolver` passes them through; the stub
resolver fills from the embedded table.

**Where labels are built** — wherever a flight is resolved and the label would otherwise
be a bare code:
- **Ingest** (`propose.go:270-274`): replace `out.StartLabel = fd.OriginIATA` with
  `airports.Label(fd.OriginIATA, rf.OriginName)` (same for dest).
- **Post-ingest backfill + 4h poller sweep** (`internal/flightcoord/fill.go`): these
  already resolve off-table airports for coordinates; extend the write to set the
  friendly `start_label`/`end_label` when they are still bare codes.

### 2. Resolved/unresolved state

**New column:** `flight_details.resolved BOOLEAN NOT NULL DEFAULT false`.
- `true` when a provider resolve returned a record; `false` when we fell back to
  manual/email data.
- Set at ingest (`enrichFlight` success ⇒ true, `flightFromLeg` fallback ⇒ false), in
  `flightcoord.Fill` / the poller on a successful resolve, and on an ident edit.
- Exposed on the flight DTO (`internal/api/dto.go`).

This is the single source of truth for "is this flight provider-tracked," replacing
unreliable heuristics (`last_resolved_at` is bumped even on a *failed* resolve, so it is
not a success signal).

### 3. Editing mechanics

**Primary edit control is the `ident`** (plus date via the existing start time), not the
IATA.
- On save, if the ident changed → re-resolve synchronously, reusing the `enrichFlight`
  path (one provider call; acceptable on a user-initiated save).
- **Resolve-or-fallback, never reject.** Success → adopt provider route/schedule,
  `resolved=true`. Failure (typo, obscure charter, or quota exhausted) → the save still
  succeeds; the flight keeps its last-known values as a starting point, `resolved=false`,
  and the background poller keeps retrying. Mirrors ingest, which never rejects an ident.

**Origin/Dest IATA fields** are shown always, but **editable only when
`resolved=false`** — i.e. the provider has no record, so the user's typed route is the
only source of truth and nothing will clobber it. When `resolved=true` they render
read-only with a hint (e.g. "from flight data").
- Editing an IATA (unresolved case) writes `flight_details.{origin,dest}_iata`, rebuilds
  the label via `airports.Label`, and recomputes coords/tz from the new code **when it is
  on-table**. It does not re-resolve, so it is never clobbered. **Off-table limitation:**
  an unresolved flight cannot be resolved by ident, so a manually-typed off-table code
  gets no coordinates from the table or provider — its map pin is simply omitted until
  the flight later resolves. Acceptable: manual off-table route corrections are rare, and
  this matches today's behaviour for any flight the provider can't place.

**Backend wiring:** `UpdatePlanPartInput.flight` (ident + IATAs) — currently decoded into
the API type but ignored — is wired through `updatePlanPartReq` →
`store.UpdatePlanPartPayload` → a new `flight_details` write in the store. The handler
triggers a re-resolve when the ident changed. Labels are rebuilt via the Section 1
helper throughout.

**Edit dialog (`PlanEditDialog.tsx`):** for flight parts only, the "From"/"To" endpoint
sections gain an editable Ident field and Origin/Dest IATA fields (3-char, uppercased),
gated read-only by `resolved`. Non-flight parts are unchanged.

### 4. Backfill of existing flights

A one-time, quota-aware command (modelled on the existing flightcoord backfill) that
re-resolves every existing flight and rewrites labels:
- Table airports relabel for free; off-table go through the provider.
- **Quota-aware:** when AeroDataBox returns quota-exhausted, the job stops cleanly, logs
  how far it got, and is re-runnable to resume — rather than silently churning (per the
  known AeroDataBox monthly-quota issue).
- Run as a one-shot command after deploy, not an automatic migration.
- It also corrects the `resolved` flag on existing rows (the migration defaults them to
  `false`; the backfill's re-resolve pass sets the true state).

## Edge cases

- **Off-table with provider name** → `Faro (FAO)`. **Off-table without a name / quota
  out** → bare `FAO`.
- **Multi-leg flights** — labels built per leg.
- **Editing the ident of a resolved flight to an unresolvable value** → keep the
  last-known route as the editable starting point, flip `resolved=false`, IATA fields
  become editable; poller may re-resolve later. (Chosen over clearing to blank.)
- **Migration window** — existing rows default to `resolved=false`, so old flights
  briefly show IATA-editable until the backfill runs. Acceptable; backfill runs right
  after deploy.

## Testing

**Backend (Go):**
- `airports.Label`: table name, provider-name fallback, bare-code fallback.
- `aerodatabox` parsing populates `ResolvedFlight.Origin/DestName`.
- `propose`/`enrichFlight`: labels build as `Name (CODE)` for resolved and
  email-fallback paths; `resolved` flag set correctly.
- `flightcoord.Fill`: a successful resolve writes friendly labels and flips `resolved`.
- Store + handler: ident edit re-resolves and rebuilds label; IATA edit on an unresolved
  flight writes `flight_details` and is not clobbered; IATA edit ignored when
  `resolved=true`.
- Backfill command: quota-exhausted response stops cleanly and is re-runnable; table
  airports resolve without a provider call.

**Frontend (Vitest):**
- Edit dialog: ident field present for flights; IATA fields editable when
  `resolved=false`, read-only when `resolved=true`; save sends the `flight` payload.
- Tile renders `Name (CODE)`.
- The existing unknown-departure-gate regression test still passes.

## Out of scope

- Editing other per-type detail fields (hotel room/phone, train coach/seat, etc.) — a
  separate gap noted during analysis, not addressed here.
- An airport-name cache table — names flow provider/table → label column; no new storage
  needed.
- Merging the label and IATA into a single field — the two-field model (free-text label
  for display, IATA for routing) is retained deliberately.
