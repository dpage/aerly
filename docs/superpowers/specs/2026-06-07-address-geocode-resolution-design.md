# Address geocode resolution + "not located" UI hint

Date: 2026-06-07

## Problem

A hotel plan part with a full, correct street address ("Ukino Palmeiras Village",
`Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal`)
never gained map coordinates. Two independent gaps caused this:

1. **The address didn't resolve in Nominatim.** The raw free-text address returns
   no result — the building/unit detail ("Bloco E3-IV") plus a region ("Algarve")
   rather than a town defeats Nominatim's single-shot match. Yet the same hotel
   resolves cleanly via its property name (`Ukino Palmeiras Village, Portugal`) or
   its postcode (`8400-450, Porches, Portugal`).

2. **No feedback.** The sync-on-edit geocode (`handlers_plans.go:557-569`) calls
   `Geocoder.Geocode` with the raw address and silently stores the part with NULL
   coordinates on a miss. Nothing in the UI tells the user the address couldn't be
   placed on the map, so the failure is invisible.

The richer shared resolver `geocodeEndpoint` (`internal/geocode/planparts.go`) —
used by the async/startup backfill — already does address → UK-postcode →
airport-label, but (a) the edit path doesn't use it, and (b) it deliberately
refuses to geocode a bare place/property name to avoid ambiguous names ("Hilton")
resolving to the wrong continent. That anti-ambiguity rule is correct and must be
preserved.

Out of scope: address autocomplete and any move off the free public OSM Nominatim
server (deferred — would require an autocomplete-permitted provider).

## Approach

Strengthen the shared resolver with **country-qualified** fallbacks, unify the edit
path onto it, and surface geocode failures in the UI as a derived "unlocated"
state. The country signal comes from the address's trailing comma segment
(e.g. `…, Portugal` → `Portugal`); qualifying the previously-unsafe lookups with a
country keeps the wrong-continent guard intact.

## Backend changes — `internal/geocode/planparts.go`

### New resolution chain in `geocodeEndpoint` (non-flight endpoints)

1. IATA code in label → airport table *(unchanged)*
2. Full normalized address *(unchanged)*
3. UK postcode → `"<pc>, United Kingdom"` *(unchanged)*
4. **NEW** — generic postcode + country tail → e.g. `"8400-450, Portugal"`
5. Airport-like label, bare *(unchanged)*
6. **NEW** — label + country tail → e.g. `"Ukino Palmeiras Village, Portugal"`

Steps 4 and 6 only fire when earlier steps miss. Step 6 is **never** queried with a
bare label — only with a country tail present; when no country can be extracted,
steps 4 and 6 are skipped entirely (graceful, preserves the ambiguity guard).
Flight parts continue to use only the address step (no label lookups).

### Helpers

- `countryFromAddress(address string) string` — returns the trimmed last
  comma-separated segment of the normalized address, or `""` when absent. No
  validation against a country list (Nominatim tolerates a bad tail by simply
  failing to resolve, which is harmless because it's a last-resort fallback).
- Generic postcode regex: `\b\d{4,5}(?:-\d{3,4})?\b` — matches PT (`8400-450`),
  ES (`03001`), and similar numeric postcodes. Always paired with a country tail
  and only tried after the full address misses.

### Edit-path unification — `internal/handlers/handlers_plans.go`

Export `geocode.Endpoint(ctx, g, partType, address, label)` wrapping the existing
`geocodeEndpoint`. The sync edit handler (currently lines 557-569) stops calling
`a.Geocoder.Geocode` raw and calls `geocode.Endpoint` instead, passing the part's
label and the plan type. The existing guards stay in the handler:

- only geocode when the address actually changed (`*in.StartAddress != cur.StartAddress`),
- and only when no explicit coordinates were supplied (`in.StartLat == nil`),

so re-location-on-edit semantics are unchanged — the handler still re-geocodes a
changed address even when the part already had coordinates, whereas the backfill
only fills NULLs. Both code paths now share one resolver.

## Frontend changes — UI hint

### Shared predicate

Add to `web/src/lib/` (e.g. `trip-format.ts` or a small `geo.ts`):

```ts
// An endpoint is "unlocated" when the user gave an address but it didn't
// resolve to coordinates. Targets geocode failures only — flights carry IATA
// labels, not addresses, so quota/resolver gaps are NOT flagged here.
export function startUnlocated(p: PlanPart): boolean {
  return !!p.start_address && p.start_lat == null;
}
export function endUnlocated(p: PlanPart): boolean {
  return !!p.end_address && p.end_lat == null;
}
export function isUnlocated(p: PlanPart): boolean {
  return startUnlocated(p) || endUnlocated(p);
}
```

### Three surfaces (all reuse the predicate)

1. **Edit dialog** (`PlanEditDialog`, `EndFields`): inline warning under the
   Address field when that endpoint is unlocated on the loaded part
   ("Couldn't locate this address on the map — try a simpler form, e.g. the
   property name and town"). Plus a post-save snackbar when the just-saved part is
   still unlocated after the trip reload ("Saved — couldn't place '<address>' on
   the map"). Geocoding is server-side on save and the dialog closes, so the
   snackbar provides the immediate signal; the inline warning shows whenever an
   unlocated part is reopened.
2. **Timeline tile** (`TripTimeline`): a small "location-off" icon/chip with a
   tooltip near the place line when `isUnlocated(part)`.
3. **Map view** (`PlanMapView` or its container): a caption/alert counting
   unlocated addressed parts in the current trip — "N location(s) couldn't be
   placed on the map."

No new API endpoint and no frontend geocoding — all three are derived purely from
the reloaded `PlanPart` (which already carries `start_address`/`start_lat` etc.).

## Testing

### Backend
- `geocodeEndpoint`: full address misses but `"<label>, <country>"` hits → coords
  returned via step 6; full address misses but `"<postcode>, <country>"` hits →
  step 4. Use a fake `Geocoder` keyed by exact query string.
- Ambiguity guard: assert a bare label (no country in address) is **never** passed
  to the geocoder.
- `countryFromAddress`: tail extraction (present / absent / single-segment).
- Generic postcode regex: matches `8400-450`, `03001`; rejects obvious non-codes.
- Edit handler: a `PATCH /api/plan-parts/{id}` changing the address to one that only
  resolves via a fallback ends up with coordinates (proves the edit path uses the
  shared chain).

### Frontend
- `isUnlocated` / `startUnlocated` / `endUnlocated` predicate units (address+no
  coord → true; coord present → false; no address → false; flight with IATA label
  and no coord → false).
- Render tests: the edit-dialog warning, the timeline chip, and the map notice
  appear only when a part is unlocated.

## Files touched

- `internal/geocode/planparts.go` — new fallbacks, helpers, exported `Endpoint`
- `internal/handlers/handlers_plans.go` — edit path calls `geocode.Endpoint`
- `web/src/lib/trip-format.ts` (or new `geo.ts`) — predicate
- `web/src/components/PlanEditDialog.tsx` — inline warning + save snackbar
- `web/src/pages/TripTimeline.tsx` — tile chip
- `web/src/components/PlanMapView.tsx` (or container) — map notice
- Corresponding `_test.go` and frontend test files
