# Address Geocode Resolution + "Not Located" UI Hint — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make hotel/address geocoding resolve more reliably (country-qualified name lookup + address tail-backoff, country-agnostic) and surface "couldn't be placed on the map" to the user in the edit dialog, timeline tile, and map.

**Architecture:** The shared resolver `geocodeEndpoint` (`internal/geocode/planparts.go`) gains a name+country fallback and a tail-backoff fallback that replace the UK-only postcode rule. The edit handler is unified onto the same chain via a new exported `geocode.Endpoint`. The frontend derives an "unlocated" state (address present, no coordinate) from the reloaded `PlanPart` and shows it in three places — no new API.

**Tech Stack:** Go (backend, `testing`), React + TypeScript + MUI + Zustand (frontend), Vitest + Testing Library.

**Spec:** `docs/superpowers/specs/2026-06-07-address-geocode-resolution-design.md`

---

## File Structure

- `internal/geocode/planparts.go` — new `countryFromAddress`, `addressTails`, rewritten `geocodeEndpoint`, exported `Endpoint`; remove `ukPostcode` regexp.
- `internal/geocode/planparts_query_test.go` — extend stub + cases.
- `internal/handlers/handlers_plans.go` — edit path calls `geocode.Endpoint`.
- `internal/handlers/geocode_backfill_test.go` — extend `fakeGeocoder` with a query map.
- `internal/handlers/plan_part_geocode_edit_test.go` — new handler test (Create).
- `web/src/lib/geo.ts` — new `startUnlocated`/`endUnlocated`/`isUnlocated`/`unlocatedCount` (Create).
- `web/src/lib/geo.test.ts` — predicate unit tests (Create).
- `web/src/state/plansSlice.ts` — `updatePlanPart` returns the updated `PlanPart`.
- `web/src/state/*` slice interface — matching return-type change.
- `web/src/components/PlanEditDialog.tsx` — inline warning + post-save notice.
- `web/src/components/PlanEditDialog.test.tsx` — warning render test (Create if absent).
- `web/src/pages/TripTimeline.tsx` — tile chip.
- `web/src/components/PlanMapView.tsx` — map notice.

---

## Task 1: Backend helpers — `countryFromAddress` and `addressTails`

**Files:**
- Modify: `internal/geocode/planparts.go`
- Test: `internal/geocode/planparts_query_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/geocode/planparts_query_test.go`:

```go
func TestCountryFromAddress(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Quinta das Palmeiras, Porches, Portugal", "Portugal"},
		{"Honeysuckle Cottage\n1 Example Lane\nAB12 3CD\nUnited Kingdom", "United Kingdom"},
		{"Nowhere Addr", ""},   // single segment → no country tail
		{"", ""},
	}
	for _, c := range cases {
		if got := countryFromAddress(normalizeAddress(c.in)); got != c.want {
			t.Errorf("countryFromAddress(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAddressTails(t *testing.T) {
	addr := "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal"
	got := addressTails(addr, 4)
	want := []string{
		"Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal",
		"Alporchinhos, 8400-450 Porches, Algarve, Portugal",
		"8400-450 Porches, Algarve, Portugal",
		"Porches, Algarve, Portugal",
	}
	if len(got) != len(want) {
		t.Fatalf("addressTails len = %d (%v), want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tail[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Too few segments → no shortened tail that isn't the bare last segment.
	if tails := addressTails("Porches, Portugal", 4); len(tails) != 0 {
		t.Errorf("2-segment tails = %v, want none", tails)
	}
	if tails := addressTails("Nowhere", 4); len(tails) != 0 {
		t.Errorf("1-segment tails = %v, want none", tails)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/geocode/ -run 'TestCountryFromAddress|TestAddressTails' -v`
Expected: FAIL — `undefined: countryFromAddress` / `undefined: addressTails`.

- [ ] **Step 3: Implement the helpers**

In `internal/geocode/planparts.go`, add (near `normalizeAddress`):

```go
// countryFromAddress returns the trimmed last comma-separated segment of an
// address to qualify a name lookup, or "" when the address has no distinct tail
// (fewer than two segments). Pass a normalized address (newlines already
// collapsed to commas).
func countryFromAddress(address string) string {
	segs := strings.Split(address, ",")
	if len(segs) < 2 {
		return ""
	}
	return strings.TrimSpace(segs[len(segs)-1])
}

// addressTails returns progressively shorter versions of a comma-separated
// address, each dropping one more leading segment, most-specific first. It omits
// the full address (already tried by the caller) and the bare final segment
// (too coarse — usually just the country), and returns at most max candidates.
func addressTails(address string, max int) []string {
	segs := strings.Split(address, ",")
	for i := range segs {
		segs[i] = strings.TrimSpace(segs[i])
	}
	var tails []string
	for i := 1; i <= len(segs)-2 && len(tails) < max; i++ {
		tails = append(tails, strings.Join(segs[i:], ", "))
	}
	return tails
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/geocode/ -run 'TestCountryFromAddress|TestAddressTails' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/geocode/planparts.go internal/geocode/planparts_query_test.go
git commit -m "Add countryFromAddress and addressTails geocode helpers"
```

---

## Task 2: Backend resolution chain — name+country, tail backoff, exported Endpoint

**Files:**
- Modify: `internal/geocode/planparts.go` (rewrite `geocodeEndpoint`, remove `ukPostcode`, add `Endpoint`)
- Test: `internal/geocode/planparts_query_test.go`

- [ ] **Step 1: Update the test stub and cases (failing tests first)**

In `internal/geocode/planparts_query_test.go`, replace the `g := stubGeo{...}` map and the `cases` slice inside `TestGeocodeEndpoint` with:

```go
	g := stubGeo{resolves: map[string][2]float64{
		"1 Main St":                    {1, 2},
		"Alicante Airport":             {38, -0.5},
		"London Heathrow Terminal 5":   {51, -0.4},
		"AB12 3CD, United Kingdom":     {51.6, -1.5},
		"Ukino Palmeiras Village, Portugal": {37.1, -8.38}, // name + country
		"8400-450 Porches, Portugal":   {37.0, -8.0},       // a resolvable tail
		"Honeysuckle Cottage":          {9, 9},             // bare name — must NEVER be queried
	}}
	ctx := context.Background()
	cases := []struct {
		name, pt, addr, label string
		wantOK                bool
		wantLat               float64
	}{
		{"address resolves", "hotel", "1 Main St", "Hotel", true, 1},
		{"no address, airport label fallback", "ground", "", "Alicante Airport", true, 38},
		{"address fails, terminal label fallback", "ground", "Nowhere Addr", "London Heathrow Terminal 5", true, 51},
		{"bare ambiguous label is NEVER geocoded", "ground", "", "Honeysuckle Cottage", false, 0},
		{"flight never uses label", "flight", "", "LHR", false, 0},
		{"flight still uses a resolving address", "flight", "1 Main St", "LHR", true, 1},
		{"airport label that doesn't resolve", "ground", "", "Faro Airport", false, 0},
		// Full address fails; a shortened tail resolves (incl. multi-line, normalised
		// first). The embedded postcode rides along in the tail — no postcode regex.
		{"tail backoff (one line)", "ground", "Honeysuckle Cottage, 1 Example Lane, AB12 3CD, United Kingdom", "Honeysuckle Cottage", true, 51.6},
		{"tail backoff (multi-line)", "ground", "Honeysuckle Cottage\n1 Example Lane\nAB12 3CD\nUnited Kingdom", "Honeysuckle Cottage", true, 51.6},
		// Full address fails; the property name + country tail resolves the exact hotel.
		{"name + country wins", "hotel", "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal", "Ukino Palmeiras Village", true, 37.1},
		// No usable name; a shortened tail resolves instead.
		{"tail backoff when name absent", "hotel", "Bloco E3-IV, Alporchinhos, 8400-450 Porches, Portugal", "", true, 37.0},
		// 2-segment address can't reach a non-bare tail, and the name lookup is only
		// ever country-qualified — the bare "Honeysuckle Cottage" entry stays untouched.
		{"name fallback appends country, never bare", "ground", "Honeysuckle Cottage, Atlantis", "Honeysuckle Cottage", false, 0},
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/geocode/ -run TestGeocodeEndpoint -v`
Expected: FAIL — the new cases (`name + country wins`, `tail backoff when name absent`) don't resolve under the current chain.

- [ ] **Step 3: Rewrite `geocodeEndpoint`, remove `ukPostcode`, add `Endpoint`**

In `internal/geocode/planparts.go`:

(a) Delete the `ukPostcode` var (the `var ukPostcode = regexp.MustCompile(...)` block) and remove `"regexp"` from the import list.

(b) Replace the whole `geocodeEndpoint` function with:

```go
// geocodeEndpoint resolves an endpoint to coordinates, most reliable signal first:
//  1. an IATA airport code in the label via the airport table (non-flight only) —
//     deterministic, no network;
//  2. the full postal address (normalised to one line);
//  3. the place/property name + the address's country tail (non-flight only) —
//     never the bare name, so a generic name ("Hilton") can't resolve on the
//     wrong continent; skipped when there's no label or no country tail;
//  4. tail backoff: progressively shorter versions of the address (drop the
//     leading segment, first hit wins) — country-agnostic, a postcode rides along
//     in whatever tail resolves;
//  5. an airport-like label ("… Airport"/"… Terminal") via the geocoder, bare.
//
// Flight parts never use the label. ok=false when nothing resolved.
func geocodeEndpoint(ctx context.Context, g Geocoder, partType, address, label string) (float64, float64, bool) {
	if partType != "flight" {
		if code := iataIn(label); code != "" {
			if lat, lon, ok := airports.Lookup(code); ok {
				return lat, lon, true
			}
		}
	}
	addr := normalizeAddress(address)
	if addr != "" {
		if lat, lon, ok, err := g.Geocode(ctx, addr); err == nil && ok {
			return lat, lon, true
		}
	}
	if partType != "flight" && strings.TrimSpace(label) != "" {
		if country := countryFromAddress(addr); country != "" {
			if lat, lon, ok, err := g.Geocode(ctx, label+", "+country); err == nil && ok {
				return lat, lon, true
			}
		}
	}
	for _, tail := range addressTails(addr, 4) {
		if lat, lon, ok, err := g.Geocode(ctx, tail); err == nil && ok {
			return lat, lon, true
		}
	}
	if partType != "flight" && isAirportLabel(label) {
		if lat, lon, ok, err := g.Geocode(ctx, label); err == nil && ok {
			return lat, lon, true
		}
	}
	return 0, 0, false
}

// Endpoint resolves a single plan-part endpoint to coordinates using the shared
// fallback chain. Exported so the edit handler resolves a changed address
// identically to the backfill path.
func Endpoint(ctx context.Context, g Geocoder, partType, address, label string) (lat, lon float64, ok bool) {
	return geocodeEndpoint(ctx, g, partType, address, label)
}
```

(c) Update the doc comment block above the function list at lines ~78-90 if it still references the old UK-postcode step — replace its numbered list with the one in (b) so the comment matches.

- [ ] **Step 4: Run the geocode package tests**

Run: `go test ./internal/geocode/ -v`
Expected: PASS — all `TestGeocodeEndpoint` cases (existing + new), `TestCountryFromAddress`, `TestAddressTails`, `TestGeocodeEndpoint_IATAFromLabel`, and the Nominatim tests.

- [ ] **Step 5: Verify the whole backend still builds**

Run: `go build ./...`
Expected: no errors (confirms `regexp` removal left no dangling reference).

- [ ] **Step 6: Commit**

```bash
git add internal/geocode/planparts.go internal/geocode/planparts_query_test.go
git commit -m "Replace UK postcode rule with name+country and tail-backoff geocoding"
```

---

## Task 3: Unify the edit handler onto `geocode.Endpoint`

**Files:**
- Modify: `internal/handlers/handlers_plans.go:557-569`
- Modify: `internal/handlers/geocode_backfill_test.go` (extend `fakeGeocoder`)
- Create: `internal/handlers/plan_part_geocode_edit_test.go`

- [ ] **Step 1: Extend the test geocoder to be query-aware**

In `internal/handlers/geocode_backfill_test.go`, add a `resolves` field and honour it in `Geocode`:

Change the struct (lines 15-19) to:

```go
type fakeGeocoder struct {
	lat, lon float64
	country  string
	byCoord  map[[2]float64]string
	resolves map[string][2]float64 // when set, Geocode answers per exact query
}
```

Change `Geocode` (lines 21-23) to:

```go
func (f fakeGeocoder) Geocode(_ context.Context, q string) (float64, float64, bool, error) {
	if f.resolves != nil {
		if c, ok := f.resolves[q]; ok {
			return c[0], c[1], true, nil
		}
		return 0, 0, false, nil
	}
	return f.lat, f.lon, true, nil
}
```

(Existing tests don't set `resolves`, so they keep the fixed-coordinate behaviour.)

- [ ] **Step 2: Write the failing handler test**

Create `internal/handlers/plan_part_geocode_edit_test.go`:

```go
package handlers

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/store"
)

// TestEditAddressUsesFallbackChain proves the PATCH edit path geocodes a changed
// address through the shared fallback chain (geocode.Endpoint), not a single raw
// lookup: the full messy address doesn't resolve, but the property name + country
// tail does, so the part ends up with the fallback coordinates.
func TestEditAddressUsesFallbackChain(t *testing.T) {
	e := setup(t, nil, nil)
	// Only the name+country query resolves — the raw full address does not.
	e.api.Geocoder = fakeGeocoder{resolves: map[string][2]float64{
		"Ukino Palmeiras Village, Portugal": {37.1, -8.38},
	}}
	ctx := context.Background()
	uid := e.user(t, "traveller", false)

	trip, err := e.store.CreateTrip(ctx, store.CreateTripPayload{Name: "Algarve"}, uid)
	if err != nil {
		t.Fatal(err)
	}
	checkin := time.Date(2026, 6, 8, 15, 0, 0, 0, time.UTC)
	checkout := time.Date(2026, 6, 12, 11, 0, 0, 0, time.UTC)
	plan, err := e.store.CreatePlan(ctx, store.CreatePlanPayload{
		TripID: trip.ID, Type: "hotel", Title: "Ukino Palmeiras Village",
		Parts: []store.CreatePlanPartPayload{{
			StartsAt: checkin, EndsAt: &checkout,
			StartLabel: "Ukino Palmeiras Village",
			Hotel:      &store.HotelDetail{PropertyName: "Ukino Palmeiras Village"},
		}},
	}, uid)
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := e.store.PartsByPlan(ctx, plan.ID)
	partID := parts[0].ID

	body := map[string]any{
		"start_address": "Quinta das Palmeiras, Bloco E3-IV, Alporchinhos, 8400-450 Porches, Algarve, Portugal",
	}
	w := e.req(t, "PATCH", "/api/plan-parts/"+strconv.FormatInt(partID, 10), body, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH = %d, body %s", w.Code, w.Body.String())
	}

	got, _ := e.store.PlanPartByID(ctx, partID)
	if got.StartLat == nil || got.StartLon == nil {
		t.Fatalf("part not geocoded via fallback chain: %+v", got)
	}
	if *got.StartLat != 37.1 || *got.StartLon != -8.38 {
		t.Errorf("coords = (%v, %v), want (37.1, -8.38)", *got.StartLat, *got.StartLon)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/handlers/ -run TestEditAddressUsesFallbackChain -v`
Expected: FAIL — the current handler calls `a.Geocoder.Geocode` with the raw address only, which `resolves` doesn't contain, so coords stay nil.

- [ ] **Step 4: Switch the edit handler to the shared chain**

In `internal/handlers/handlers_plans.go`, replace the block at lines 557-569 with:

```go
	// A changed address is re-located via the shared geocode fallback chain
	// (address → name+country → tail backoff → airport label), the same path the
	// backfill uses. Explicit coordinates in the request, if any, still win.
	if a.Geocoder != nil {
		if cur, cerr := a.Store.PlanPartByID(r.Context(), id); cerr == nil {
			startLabel := cur.StartLabel
			if in.StartLabel != nil {
				startLabel = *in.StartLabel
			}
			endLabel := cur.EndLabel
			if in.EndLabel != nil {
				endLabel = *in.EndLabel
			}
			if in.StartAddress != nil && *in.StartAddress != "" && *in.StartAddress != cur.StartAddress && in.StartLat == nil {
				if lat, lon, ok := geocode.Endpoint(r.Context(), a.Geocoder, cur.Type, *in.StartAddress, startLabel); ok {
					in.StartLat, in.StartLon = &lat, &lon
				}
			}
			if in.EndAddress != nil && *in.EndAddress != "" && *in.EndAddress != cur.EndAddress && in.EndLat == nil {
				if lat, lon, ok := geocode.Endpoint(r.Context(), a.Geocoder, cur.Type, *in.EndAddress, endLabel); ok {
					in.EndLat, in.EndLon = &lat, &lon
				}
			}
		}
	}
```

Ensure `internal/handlers/handlers_plans.go` imports `"github.com/dpage/aerly/internal/geocode"` (add it to the import block if not already present).

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/handlers/ -run TestEditAddressUsesFallbackChain -v`
Expected: PASS.

- [ ] **Step 6: Run the full backend test suite**

Run: `go test ./...`
Expected: PASS (existing `TestBackfillPartCoordinates` etc. still pass — `fakeGeocoder` without `resolves` is unchanged).

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/handlers_plans.go internal/handlers/geocode_backfill_test.go internal/handlers/plan_part_geocode_edit_test.go
git commit -m "Route plan-part edit geocoding through shared fallback chain"
```

---

## Task 4: Frontend "unlocated" predicate helper

**Files:**
- Create: `web/src/lib/geo.ts`
- Test: `web/src/lib/geo.test.ts`

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/geo.test.ts`:

```ts
import { describe, it, expect } from 'vitest';
import type { PlanPart } from '../api/types';
import { startUnlocated, endUnlocated, isUnlocated, unlocatedCount } from './geo';

function part(over: Partial<PlanPart>): PlanPart {
  return {
    id: 1,
    plan_id: 1,
    type: 'hotel',
    seq: 0,
    status: 'planned',
    starts_at: '2026-06-08T15:00:00Z',
    start_tz: '',
    end_tz: '',
    start_label: '',
    start_address: '',
    end_label: '',
    end_address: '',
    ...over,
  } as PlanPart;
}

describe('unlocated predicates', () => {
  it('start address with no coord is unlocated', () => {
    expect(startUnlocated(part({ start_address: 'Some Hotel, Portugal' }))).toBe(true);
  });
  it('start address with a coord is located', () => {
    expect(startUnlocated(part({ start_address: 'X', start_lat: 1, start_lon: 2 }))).toBe(false);
  });
  it('no address is never unlocated', () => {
    expect(startUnlocated(part({ start_label: 'FAO' }))).toBe(false);
  });
  it('a flight leg (IATA labels, no addresses) is not flagged', () => {
    expect(isUnlocated(part({ type: 'flight', start_label: 'NQY', end_label: 'FAO' }))).toBe(false);
  });
  it('end address with no coord is unlocated', () => {
    expect(endUnlocated(part({ end_address: 'Pickup point' }))).toBe(true);
  });
  it('isUnlocated is the OR of both ends', () => {
    expect(isUnlocated(part({ end_address: 'Pickup point' }))).toBe(true);
  });
  it('unlocatedCount counts only unlocated, non-dismissed parts', () => {
    const parts = [
      part({ id: 1, start_address: 'A' }),                              // unlocated
      part({ id: 2, start_address: 'B', start_lat: 1, start_lon: 2 }),  // located
      part({ id: 3, start_address: 'C', dismissed_at: '2026-01-01T00:00:00Z' }), // dismissed
    ];
    expect(unlocatedCount(parts)).toBe(1);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/geo.test.ts`
Expected: FAIL — cannot resolve `./geo`.

- [ ] **Step 3: Implement the helper**

Create `web/src/lib/geo.ts`:

```ts
import type { PlanPart } from '../api/types';

// An endpoint is "unlocated" when the user gave an address but it didn't resolve
// to coordinates. This targets geocode failures only — flights carry IATA labels,
// not addresses, so resolver/quota gaps are NOT flagged here.
export function startUnlocated(p: PlanPart): boolean {
  return !!p.start_address && p.start_lat == null;
}

export function endUnlocated(p: PlanPart): boolean {
  return !!p.end_address && p.end_lat == null;
}

export function isUnlocated(p: PlanPart): boolean {
  return startUnlocated(p) || endUnlocated(p);
}

// unlocatedCount is the number of non-dismissed parts with an unresolved address —
// the figure shown in the map's "couldn't be placed" notice.
export function unlocatedCount(parts: PlanPart[]): number {
  return parts.filter((p) => !p.dismissed_at && isUnlocated(p)).length;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/lib/geo.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/geo.ts web/src/lib/geo.test.ts
git commit -m "Add frontend unlocated-address predicate helpers"
```

---

## Task 5: Edit dialog — inline warning + post-save notice

**Files:**
- Modify: `web/src/state/plansSlice.ts` (return the updated part)
- Modify: the plans-slice interface (return-type) — find with `grep -rn "updatePlanPart" web/src/state`
- Modify: `web/src/components/PlanEditDialog.tsx`
- Test: `web/src/components/PlanEditDialog.test.tsx` (create if absent)

- [ ] **Step 1: Make `updatePlanPart` return the updated part**

In `web/src/state/plansSlice.ts`, change the action (currently lines ~80-83) to:

```ts
  async updatePlanPart(partId, patch) {
    const updated = await api.updatePlanPart(partId, patch);
    await reloadCurrent(get);
    return updated;
  },
```

Update the slice's TypeScript interface so the method's signature returns
`Promise<PlanPart>` instead of `Promise<void>`. Find the declaration:

Run: `grep -rn "updatePlanPart(partId" web/src/state`

and change e.g. `updatePlanPart(partId: number, patch: UpdatePlanPartInput): Promise<void>;`
to `Promise<PlanPart>;`. Ensure `PlanPart` is imported in that file (it likely already imports from `../api/types`).

- [ ] **Step 2: Write the failing warning render test**

Create (or extend) `web/src/components/PlanEditDialog.test.tsx`. If the file
exists, add this test; otherwise create it with this content:

```tsx
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import PlanEditDialog from './PlanEditDialog';
import type { Plan, PlanPart } from '../api/types';

// The dialog reads from the store; stub the hooks it uses to no-ops/defaults.
vi.mock('../state/store', () => ({
  useStore: (sel: (s: unknown) => unknown) =>
    sel({
      updatePlan: vi.fn(),
      updatePlanPart: vi.fn(),
      splitPlanPart: vi.fn(),
      movePlan: vi.fn(),
      deletePlan: vi.fn(),
      setNotice: vi.fn(),
      setError: vi.fn(),
      trips: [],
      currentTripId: null,
    }),
}));

function hotelPlan(over: Partial<PlanPart>): Plan {
  const part = {
    id: 10,
    plan_id: 5,
    type: 'hotel',
    seq: 0,
    status: 'planned',
    starts_at: '2026-06-08T15:00:00Z',
    ends_at: '2026-06-12T11:00:00Z',
    start_tz: '',
    end_tz: '',
    start_label: 'Ukino Palmeiras Village',
    start_address: 'Quinta das Palmeiras, Porches, Portugal',
    end_label: 'Ukino Palmeiras Village',
    end_address: '',
    ...over,
  } as PlanPart;
  return {
    id: 5,
    trip_id: 1,
    type: 'hotel',
    title: 'Ukino Palmeiras Village',
    confirmation_ref: '',
    notes: '',
    supplier_name: '',
    contact_email: '',
    contact_phone: '',
    website: '',
    parts: [part],
  } as Plan;
}

describe('PlanEditDialog unlocated warning', () => {
  beforeEach(() => vi.clearAllMocks());

  it('warns when an addressed part has no coordinates', () => {
    render(<PlanEditDialog open plan={hotelPlan({})} onClose={() => {}} />);
    expect(screen.getByText(/couldn't be located on the map/i)).toBeInTheDocument();
  });

  it('no warning once the part has coordinates', () => {
    render(
      <PlanEditDialog
        open
        plan={hotelPlan({ start_lat: 37.1, start_lon: -8.38 })}
        onClose={() => {}}
      />,
    );
    expect(screen.queryByText(/couldn't be located on the map/i)).not.toBeInTheDocument();
  });
});
```

> Implementer note: match the mock to the real import path/shape used by
> `PlanEditDialog` (it does `useStore((s) => s.updatePlanPart)` etc.). If the
> component imports `useStore` from a different module path, adjust the
> `vi.mock` target accordingly. Add any store selectors the component reads.

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/PlanEditDialog.test.tsx`
Expected: FAIL — no "couldn't be located on the map" text yet.

- [ ] **Step 4: Add the inline warning to `EndFields`**

In `web/src/components/PlanEditDialog.tsx`:

(a) Add imports at the top:

```ts
import { startUnlocated, endUnlocated, isUnlocated } from '../lib/geo';
```

(b) Add an `unlocated` prop to `EndFields`. Change its props type (lines ~445-449) to include `unlocated?: boolean` and default it:

```tsx
function EndFields({
  heading,
  form,
  onChange,
  timeOnly = false,
  unlocated = false,
}: {
  heading: string;
  form: EndForm;
  onChange: (field: keyof EndForm, value: string) => void;
  timeOnly?: boolean;
  unlocated?: boolean;
}) {
```

(c) Under the Address `TextField` (the block at lines ~465-474), add the warning. Replace the Address `TextField`'s closing with a fragment that appends an Alert:

```tsx
      {!timeOnly && (
        <TextField
          label="Address"
          size="small"
          value={form.address}
          onChange={(e) => onChange('address', e.target.value)}
          helperText="Editing the address re-locates it on the map."
          fullWidth
        />
      )}
      {!timeOnly && unlocated && (
        <Alert severity="warning" sx={{ py: 0 }}>
          This address couldn&apos;t be located on the map. Try a simpler form —
          e.g. the property name and town.
        </Alert>
      )}
```

Ensure `Alert` is imported from `@mui/material` (add it to the existing MUI import).

(d) Pass `unlocated` from the two call sites (lines ~359-374):

```tsx
                <EndFields
                  heading={withEnd && isTransferType(part.type) ? 'From' : 'Where'}
                  form={form.start}
                  onChange={(f, v) => patchEnd(part.id, 'start', f, v)}
                  unlocated={startUnlocated(part)}
                />
                {withEnd && (
                  <Box sx={{ mt: 1.5 }}>
                    <EndFields
                      heading={isTransferType(part.type) ? 'To' : 'Until'}
                      form={form.end}
                      onChange={(f, v) => patchEnd(part.id, 'end', f, v)}
                      timeOnly={!isTransferType(part.type)}
                      unlocated={endUnlocated(part)}
                    />
                  </Box>
                )}
```

- [ ] **Step 5: Add the post-save notice in `handleSave`**

In `web/src/components/PlanEditDialog.tsx`:

(a) Add the store selector near the other `useStore` calls (around line 105):

```ts
  const setNotice = useStore((s) => s.setNotice);
```

(b) Replace the parts loop + `onClose()` in `handleSave` (lines 217-221) with:

```tsx
      const stranded: PlanPart[] = [];
      for (const part of editableParts) {
        const patch = buildPatch(part, forms[part.id], initial[part.id]);
        if (!patch) continue;
        const addrChanged =
          patch.start_address !== undefined || patch.end_address !== undefined;
        const updated = await updatePlanPart(part.id, patch);
        if (addrChanged && isUnlocated(updated)) stranded.push(updated);
      }
      onClose();
      if (stranded.length > 0) {
        const p = stranded[0];
        setNotice({
          severity: 'info',
          message: `Saved — couldn't place "${p.start_address || p.end_address}" on the map.`,
        });
      }
```

Ensure `PlanPart` is imported in `PlanEditDialog.tsx` (it already imports types like `UpdatePlanPartInput`; add `PlanPart` to that import if missing).

- [ ] **Step 6: Run the test to verify it passes**

Run: `cd web && npx vitest run src/components/PlanEditDialog.test.tsx`
Expected: PASS (both warning cases).

- [ ] **Step 7: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: no errors (confirms the `Promise<PlanPart>` return-type change is consistent).

- [ ] **Step 8: Commit**

```bash
git add web/src/state/plansSlice.ts web/src/components/PlanEditDialog.tsx web/src/components/PlanEditDialog.test.tsx web/src/state
git commit -m "Show unlocated-address warning and post-save notice in plan editor"
```

---

## Task 6: Timeline tile — "not on map" chip

**Files:**
- Modify: `web/src/pages/TripTimeline.tsx`
- Test: extend the nearest existing TripTimeline test, or assert via the `geo` helper (already covered in Task 4)

- [ ] **Step 1: Add the chip to the tile**

In `web/src/pages/TripTimeline.tsx`:

(a) Add the import:

```ts
import { isUnlocated } from '../lib/geo';
```

Ensure `Chip` and `Tooltip` are imported from `@mui/material` (add if missing), and an icon — add:

```ts
import LocationOffIcon from '@mui/icons-material/LocationOff';
```

(b) After the `places` block (lines 400-404), add:

```tsx
          {isUnlocated(part) && (
            <Tooltip title="Address couldn't be located — not shown on the map">
              <Chip
                size="small"
                color="warning"
                variant="outlined"
                icon={<LocationOffIcon sx={{ fontSize: 14 }} />}
                label="Not on map"
                sx={{ height: 18, fontSize: 10, mt: 0.25 }}
              />
            </Tooltip>
          )}
```

- [ ] **Step 2: Typecheck + build the frontend**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: no errors.

- [ ] **Step 3: Run the frontend test suite**

Run: `cd web && npx vitest run`
Expected: PASS (no regressions; the predicate itself is unit-tested in Task 4).

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/TripTimeline.tsx
git commit -m "Flag unlocated parts with a 'Not on map' chip on the timeline"
```

---

## Task 7: Map view — "couldn't be placed" notice

**Files:**
- Modify: `web/src/components/PlanMapView.tsx`

- [ ] **Step 1: Add the notice to the map sidebar**

In `web/src/components/PlanMapView.tsx`:

(a) Add the import:

```ts
import { unlocatedCount } from '../lib/geo';
```

Ensure `Alert` is imported from `@mui/material` (add if missing).

(b) Compute the count near the top of the component body (after the `ordered`
`useMemo`, around line 90):

```tsx
  const strandedCount = useMemo(() => unlocatedCount(parts), [parts]);
```

(c) In the sidebar `Box` (after `{controls && ...}` at line 315), add:

```tsx
        {strandedCount > 0 && (
          <Box sx={{ px: 2, pt: 1 }}>
            <Alert severity="warning" sx={{ py: 0 }}>
              {strandedCount} location{strandedCount === 1 ? '' : 's'} couldn&apos;t
              be placed on the map — open the item to fix its address.
            </Alert>
          </Box>
        )}
```

- [ ] **Step 2: Typecheck + build**

Run: `cd web && npx tsc --noEmit && npm run build`
Expected: no errors.

- [ ] **Step 3: Run the frontend test suite**

Run: `cd web && npx vitest run`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/components/PlanMapView.tsx
git commit -m "Show a map notice when some addresses couldn't be placed"
```

---

## Final verification

- [ ] **Backend:** `go test ./...` — all pass.
- [ ] **Frontend:** `cd web && npx vitest run && npx tsc --noEmit && npm run build` — all pass.
- [ ] **Manual smoke (optional, prod data):** the Ukino Palmeiras Village hotel
  (trip 34, part 123) should geocode on the next address re-save once deployed —
  the property-name + "Portugal" fallback resolves to ~37.10, -8.385.
