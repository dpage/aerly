# Flight gate display + in-app alert inbox — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show terminal+gate on the two flight surfaces (Unknown when absent), and make all flight alerts (delayed/cancelled/diverted/gate) viewable in-app as a persistent, badged inbox folded into the avatar menu, plus a toast when one arrives.

**Architecture:** Part A threads the already-stored `origin_gate`/`dest_gate`/`origin_terminal`/`dest_terminal` through the display path (`store.FlightDetail` → `FlightDetailDTO` → FE `FlightDetail`) and renders them. Part B adds a `flight_alerts` table written by the poller per in-app recipient, two new endpoints (`GET /api/alerts`, `POST /api/alerts/read`), an `unread_alerts` count on the notifications body, and an `alert.created` SSE consumer that updates an `alertsSlice` list + toast + avatar-menu inbox.

**Tech Stack:** Go (net/http `ServeMux`, pgx), Postgres (embedded SQL migrations via `migrations.FS`), React + TypeScript, Zustand, MUI, Vitest, Go testing with `testsupport.NewPool`.

**Spec:** `docs/superpowers/specs/2026-06-03-flight-gate-and-alert-inbox-design.md`

**Conventions observed:**
- Store tests: `s := newStore(t); if s == nil { return }`, then `mkUser(t, s)`, `mkTrip(t, s, owner)`, package-level `ctx`. Raw SQL via `s.Pool().Exec(ctx, ...)`.
- Handler tests: `e := setup(t, resolver, cfg)`, `uid := e.user(t, "name", false)`, `w := e.req(t, method, path, body, uid)`, `decodeBody[T](t, w)`.
- Poller tests: `p, s, hub, cap := alertPoller(t)`.
- Run Go tests: `go test ./internal/...`. Run FE tests: `cd web && npx vitest run <path>`.
- Migrations are auto-applied to every test DB by `testsupport.NewPool`.
- Commit messages end with the repo's Co-Authored-By trailer (see existing commits). Do NOT push.

---

## File Structure

**Part A (gate display)**
- Modify: `internal/store/plans.go` — `FlightDetail` struct + `FlightDetailFor`.
- Modify: `internal/api/dto.go` — `FlightDetailDTO` + `ToFlightDetailDTO`.
- Modify: `web/src/api/types.ts` — `FlightDetail`.
- Create: `web/src/lib/gate.ts` — `fmtGate` helper.
- Modify: `web/src/components/FlightDetailCard.tsx` — Route rows.
- Modify: `web/src/pages/TripTimeline.tsx` — tile-face gate line.

**Part B (alert inbox)**
- Create: `migrations/0020_flight_alerts.up.sql` / `.down.sql`.
- Modify: `internal/store/alerts.go` — `FlightAlert` type + 4 methods.
- Modify: `internal/api/dto.go` — `FlightAlertDTO` fields + `NotificationsDTO.UnreadAlerts` + `ToFlightAlertDTO`.
- Modify: `internal/handlers/notifications.go` — `buildNotificationsDTO` unread count.
- Create: `internal/handlers/handlers_alert_inbox.go` — `listAlerts` + `markAlertsRead`.
- Modify: `internal/handlers/handlers.go` — register 2 routes.
- Modify: `internal/poller/alerts.go` — `publishAlert` persists then publishes.
- Modify: `web/src/api/types.ts` — `FlightAlert` + `Notifications.unread_alerts`.
- Modify: `web/src/api/client.ts` — `getAlerts`, `markAlertsRead`.
- Modify: `web/src/state/alertsSlice.ts` — inbox state/actions.
- Modify: `web/src/state/coreSlice.ts` — load alerts on `init`.
- Modify: `web/src/sse.ts` — `alert.created` listener.
- Modify: `web/src/App.tsx` — wire `onAlert` + toast Snackbar.
- Modify: `web/src/components/Layout.tsx` — badge sum + Alerts menu section.

---

# PART A — Gate display

## Task 1: Thread gate/terminal through the store satellite

**Files:**
- Modify: `internal/store/plans.go:65` (`FlightDetail` struct), `internal/store/plans.go:643` (`FlightDetailFor`)
- Test: `internal/store/plans_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/store/plans_test.go`:

```go
func TestFlightDetailForReturnsGateAndTerminal(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	out := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "BA286",
		Parts: []CreatePlanPartPayload{{
			StartsAt: out, EndsAt: &in, StartLabel: "LHR", EndLabel: "SFO",
			Flight: &FlightDetail{
				Ident: "BA286", ScheduledOut: out, ScheduledIn: in,
				OriginIATA: "LHR", DestIATA: "SFO",
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, err := s.PartsByPlan(ctx, plan.ID)
	if err != nil || len(parts) != 1 {
		t.Fatalf("PartsByPlan = %d, %v", len(parts), err)
	}
	// The poller fills gate/terminal post-creation; simulate that.
	if _, err := s.Pool().Exec(ctx,
		`UPDATE flight_details SET origin_gate=$2, dest_gate=$3, origin_terminal=$4, dest_terminal=$5
		 WHERE plan_part_id=$1`,
		parts[0].ID, "B32", "", "5", ""); err != nil {
		t.Fatalf("set gate: %v", err)
	}

	fd, err := s.FlightDetailFor(ctx, parts[0].ID)
	if err != nil || fd == nil {
		t.Fatalf("FlightDetailFor = %v, %v", fd, err)
	}
	if fd.OriginGate != "B32" || fd.OriginTerminal != "5" {
		t.Errorf("origin gate/terminal = %q/%q, want B32/5", fd.OriginGate, fd.OriginTerminal)
	}
	if fd.DestGate != "" || fd.DestTerminal != "" {
		t.Errorf("dest gate/terminal = %q/%q, want empty", fd.DestGate, fd.DestTerminal)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestFlightDetailForReturnsGateAndTerminal -v`
Expected: compile error — `fd.OriginGate undefined`.

- [ ] **Step 3: Add the struct fields**

In `internal/store/plans.go`, add to the `FlightDetail` struct (after `LastResolvedAt *time.Time`):

```go
	OriginGate     string
	DestGate       string
	OriginTerminal string
	DestTerminal   string
```

- [ ] **Step 4: Extend `FlightDetailFor`**

Replace the query + Scan in `FlightDetailFor` (`internal/store/plans.go:643`) with:

```go
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, ident, icao24, callsign, scheduled_out, scheduled_in,
			estimated_out, estimated_in, actual_out, actual_in, origin_iata,
			dest_iata, flight_status, last_polled_at, last_resolved_at,
			COALESCE(origin_gate,''), COALESCE(dest_gate,''),
			COALESCE(origin_terminal,''), COALESCE(dest_terminal,'')
		FROM flight_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.Ident, &d.ICAO24, &d.Callsign, &d.ScheduledOut,
		&d.ScheduledIn, &d.EstimatedOut, &d.EstimatedIn, &d.ActualOut, &d.ActualIn,
		&d.OriginIATA, &d.DestIATA, &d.FlightStatus, &d.LastPolledAt, &d.LastResolvedAt,
		&d.OriginGate, &d.DestGate, &d.OriginTerminal, &d.DestTerminal)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestFlightDetailForReturnsGateAndTerminal -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/plans.go internal/store/plans_test.go
git commit -m "feat(store): carry gate/terminal on FlightDetail satellite

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Expose gate/terminal on the flight DTO

**Files:**
- Modify: `internal/api/dto.go:377` (`FlightDetailDTO`), `internal/api/dto.go:601` (`ToFlightDetailDTO`)
- Test: `internal/api/dto_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/api/dto_test.go`:

```go
func TestToFlightDetailDTOMapsGateAndTerminal(t *testing.T) {
	d := &store.FlightDetail{
		Ident: "BA286", OriginIATA: "LHR", DestIATA: "SFO",
		OriginGate: "B32", OriginTerminal: "5", DestGate: "", DestTerminal: "",
	}
	out := ToFlightDetailDTO(d, nil, nil)
	if out.OriginGate != "B32" || out.OriginTerminal != "5" {
		t.Errorf("origin gate/terminal = %q/%q, want B32/5", out.OriginGate, out.OriginTerminal)
	}
	if out.DestGate != "" || out.DestTerminal != "" {
		t.Errorf("dest gate/terminal = %q/%q, want empty", out.DestGate, out.DestTerminal)
	}
}
```

(If `store` isn't yet imported in `dto_test.go`, add `"github.com/dpage/aerly/internal/store"` to its imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestToFlightDetailDTOMapsGateAndTerminal -v`
Expected: compile error — `out.OriginGate undefined`.

- [ ] **Step 3: Add DTO fields**

In `internal/api/dto.go`, add to `FlightDetailDTO` (after `FlightStatus string \`json:"flight_status"\``):

```go
	OriginGate     string        `json:"origin_gate"`
	DestGate       string        `json:"dest_gate"`
	OriginTerminal string        `json:"origin_terminal"`
	DestTerminal   string        `json:"dest_terminal"`
```

- [ ] **Step 4: Map them in `ToFlightDetailDTO`**

In `ToFlightDetailDTO`, add to the `out := &FlightDetailDTO{...}` literal (after `FlightStatus: d.FlightStatus,`):

```go
		OriginGate:     d.OriginGate,
		DestGate:       d.DestGate,
		OriginTerminal: d.OriginTerminal,
		DestTerminal:   d.DestTerminal,
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestToFlightDetailDTOMapsGateAndTerminal -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto.go internal/api/dto_test.go
git commit -m "feat(api): expose gate/terminal on FlightDetailDTO

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Frontend type + `fmtGate` helper

**Files:**
- Modify: `web/src/api/types.ts:236` (`FlightDetail`)
- Create: `web/src/lib/gate.ts`
- Test: `web/src/lib/gate.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/gate.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { fmtGate } from './gate';

describe('fmtGate', () => {
  it('combines terminal and gate', () => {
    expect(fmtGate('5', 'B32')).toBe('Terminal 5 · Gate B32');
  });
  it('gate only', () => {
    expect(fmtGate('', 'B32')).toBe('Gate B32');
  });
  it('terminal only', () => {
    expect(fmtGate('5', '')).toBe('Terminal 5');
  });
  it('neither → Unknown', () => {
    expect(fmtGate('', '')).toBe('Unknown');
    expect(fmtGate(undefined, undefined)).toBe('Unknown');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/lib/gate.test.ts`
Expected: FAIL — cannot resolve `./gate`.

- [ ] **Step 3: Create the helper**

Create `web/src/lib/gate.ts`:

```ts
/** Human-readable "Terminal X · Gate Y" for a flight endpoint, with graceful
 * fallbacks: gate-only, terminal-only, or "Unknown" when neither is known. */
export function fmtGate(terminal?: string, gate?: string): string {
  const t = terminal?.trim();
  const g = gate?.trim();
  const parts: string[] = [];
  if (t) parts.push(`Terminal ${t}`);
  if (g) parts.push(`Gate ${g}`);
  return parts.length ? parts.join(' · ') : 'Unknown';
}
```

- [ ] **Step 4: Add the type fields**

In `web/src/api/types.ts`, add to `FlightDetail` (after `flight_status: string;`):

```ts
  origin_gate?: string;
  dest_gate?: string;
  origin_terminal?: string;
  dest_terminal?: string;
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/lib/gate.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/gate.ts web/src/lib/gate.test.ts web/src/api/types.ts
git commit -m "feat(web): fmtGate helper + gate/terminal on FlightDetail type

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Render gate in the map's `FlightDetailCard`

**Files:**
- Modify: `web/src/components/FlightDetailCard.tsx`
- Test: `web/src/components/FlightDetailCard.test.tsx` (create if absent)

- [ ] **Step 1: Write the failing test**

Create/append `web/src/components/FlightDetailCard.test.tsx`:

```tsx
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import FlightDetailCard from './FlightDetailCard';
import type { FlightDetail } from '../api/types';

const base: FlightDetail = {
  ident: 'BA286',
  callsign: '',
  scheduled_out: '2026-06-01T09:00:00Z',
  scheduled_in: '2026-06-01T12:00:00Z',
  origin_iata: 'LHR',
  dest_iata: 'SFO',
  flight_status: 'Scheduled',
};

describe('FlightDetailCard gate', () => {
  it('shows departure gate/terminal and Unknown arrival', () => {
    render(<FlightDetailCard flight={{ ...base, origin_terminal: '5', origin_gate: 'B32' }} />);
    expect(screen.getByText('Terminal 5 · Gate B32')).toBeInTheDocument();
    // Arrival gate unknown.
    expect(screen.getByText('Unknown')).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/FlightDetailCard.test.tsx`
Expected: FAIL — text not found.

- [ ] **Step 3: Add the rows**

In `web/src/components/FlightDetailCard.tsx`: add the import at the top (after the `fmtAgo` import):

```tsx
import { fmtGate } from '../lib/gate';
```

Then in the `Route` `Section`, after the `To` row, add:

```tsx
        <Row label="Departure" value={fmtGate(flight.origin_terminal, flight.origin_gate)} />
        <Row label="Arrival" value={fmtGate(flight.dest_terminal, flight.dest_gate)} />
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/FlightDetailCard.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/FlightDetailCard.tsx web/src/components/FlightDetailCard.test.tsx
git commit -m "feat(web): show departure/arrival gate in flight detail card

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Show departure gate on the timeline tile face

**Files:**
- Modify: `web/src/pages/TripTimeline.tsx`
- Test: `web/src/pages/TripTimeline.test.tsx` (append; or create a focused test)

The gate line goes on the **always-visible** tile face (not the expanded body), for flight parts only, showing the departure terminal/gate.

- [ ] **Step 1: Write the failing test**

Append to `web/src/pages/TripTimeline.test.tsx` (follow the file's existing render/store-seeding pattern — seed a trip whose plan has a flight part with `origin_gate`). Minimal assertion:

```tsx
it('shows the departure gate on a flight tile face', async () => {
  // Arrange: seed currentTrip with a flight plan part carrying origin_gate.
  // (Use the same store-seeding helper the other tests in this file use.)
  renderTimelineWithFlight({ origin_terminal: '5', origin_gate: 'B32' });
  expect(await screen.findByText('Terminal 5 · Gate B32')).toBeInTheDocument();
});
```

> Implementer note: reuse this file's existing harness for seeding `currentTrip`. If none exists, seed via `useStore.setState({ currentTrip: <trip with one flight plan/part> })` before `render(<TripTimeline />)` inside a `MemoryRouter`. The flight part needs `type:'flight'` and `flight: { ...required fields, origin_terminal:'5', origin_gate:'B32' }`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/pages/TripTimeline.test.tsx`
Expected: FAIL — text not found.

- [ ] **Step 3: Add the import + gate line**

In `web/src/pages/TripTimeline.tsx`, add the import (after the `trip-format` import block):

```tsx
import { fmtGate } from '../lib/gate';
```

In `PartCard`, inside the always-visible header `<Box sx={{ flexGrow: 1, minWidth: 0 }}>`, after the `confirmation_ref` block and before the `Track` link, add:

```tsx
          {part.type === 'flight' && part.flight && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {fmtGate(part.flight.origin_terminal, part.flight.origin_gate)}
            </Typography>
          )}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/pages/TripTimeline.test.tsx`
Expected: PASS.

- [ ] **Step 5: Run the broader FE suite to catch regressions**

Run: `cd web && npx vitest run src/pages/TripTimeline.test.tsx src/components/FlightDetailCard.test.tsx src/lib/gate.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/TripTimeline.tsx web/src/pages/TripTimeline.test.tsx
git commit -m "feat(web): show departure gate on timeline tile face

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

# PART B — In-app alert inbox

## Task 6: `flight_alerts` migration

**Files:**
- Create: `migrations/0020_flight_alerts.up.sql`, `migrations/0020_flight_alerts.down.sql`

- [ ] **Step 1: Write the up migration**

Create `migrations/0020_flight_alerts.up.sql`:

```sql
-- Persistent in-app flight alerts (spec: in-app alert inbox). The poller writes
-- one row per in-app recipient when a tracked flight meaningfully changes
-- (delayed | cancelled | diverted | gate). Rows back the avatar-menu inbox and
-- the unread badge; read_at NULL means unread. Email delivery is unchanged.
CREATE TABLE flight_alerts (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL,
    plan_part_id BIGINT NOT NULL,
    plan_id      BIGINT NOT NULL,
    trip_id      BIGINT NOT NULL,
    ident        TEXT NOT NULL,
    kind         TEXT NOT NULL,
    status       TEXT NOT NULL,
    message      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at      TIMESTAMPTZ
);

CREATE INDEX flight_alerts_user_created_idx ON flight_alerts (user_id, created_at DESC);
CREATE INDEX flight_alerts_user_unread_idx ON flight_alerts (user_id) WHERE read_at IS NULL;
```

- [ ] **Step 2: Write the down migration**

Create `migrations/0020_flight_alerts.down.sql`:

```sql
-- Reverse 0020.
DROP TABLE IF EXISTS flight_alerts;
```

- [ ] **Step 3: Verify migrations apply (a store test boots a migrated DB)**

Run: `go test ./internal/store/ -run TestFlightDetailForReturnsGateAndTerminal -v`
Expected: PASS (the fresh test DB now includes `flight_alerts`; a migration syntax error would fail here).

- [ ] **Step 4: Commit**

```bash
git add migrations/0020_flight_alerts.up.sql migrations/0020_flight_alerts.down.sql
git commit -m "feat(db): add flight_alerts table for in-app alert inbox

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Store methods for flight alerts

**Files:**
- Modify: `internal/store/alerts.go`
- Test: `internal/store/alerts_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Create/append `internal/store/alerts_test.go`:

```go
func TestFlightAlertInsertListMarkRead(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	trip := mkTrip(t, s, owner)
	plan, err := s.CreatePlan(ctx, CreatePlanPayload{
		TripID: trip, Type: "flight", Title: "BA286",
		Parts: []CreatePlanPartPayload{{
			StartsAt:   time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
			StartLabel: "LHR", EndLabel: "SFO",
			Flight: &FlightDetail{
				Ident: "BA286", OriginIATA: "LHR", DestIATA: "SFO",
				ScheduledOut: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
				ScheduledIn:  time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			},
		}},
	}, owner)
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	parts, _ := s.PartsByPlan(ctx, plan.ID)
	partID := parts[0].ID

	a := FlightAlert{
		UserID: owner, PlanPartID: partID, PlanID: plan.ID, TripID: trip,
		Ident: "BA286", Kind: "gate", Status: "Scheduled", Message: "BA286 now departs gate B32",
	}
	if _, err := s.InsertFlightAlert(ctx, a); err != nil {
		t.Fatalf("InsertFlightAlert: %v", err)
	}

	got, err := s.ListFlightAlerts(ctx, owner, 50)
	if err != nil || len(got) != 1 {
		t.Fatalf("ListFlightAlerts = %d, %v", len(got), err)
	}
	if got[0].ID == 0 || got[0].Message != a.Message || got[0].ReadAt != nil {
		t.Errorf("listed alert wrong: %+v", got[0])
	}

	n, err := s.CountUnreadFlightAlerts(ctx, owner)
	if err != nil || n != 1 {
		t.Fatalf("CountUnreadFlightAlerts = %d, %v", n, err)
	}

	if err := s.MarkFlightAlertsRead(ctx, owner); err != nil {
		t.Fatalf("MarkFlightAlertsRead: %v", err)
	}
	n, _ = s.CountUnreadFlightAlerts(ctx, owner)
	if n != 0 {
		t.Errorf("unread after mark-read = %d, want 0", n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestFlightAlertInsertListMarkRead -v`
Expected: compile error — `FlightAlert`/`InsertFlightAlert` undefined.

- [ ] **Step 3: Implement the type + methods**

Append to `internal/store/alerts.go`:

```go
// FlightAlert is one persisted in-app flight-change alert for a single user.
// ReadAt is nil while unread. ID/CreatedAt are set by the DB on insert.
type FlightAlert struct {
	ID         int64
	UserID     int64
	PlanPartID int64
	PlanID     int64
	TripID     int64
	Ident      string
	Kind       string // delayed|cancelled|diverted|gate
	Status     string
	Message    string
	CreatedAt  time.Time
	ReadAt     *time.Time
}

// InsertFlightAlert persists one alert row and returns it with ID/CreatedAt
// populated, so the caller can publish the persisted shape over SSE.
func (s *Store) InsertFlightAlert(ctx context.Context, a FlightAlert) (FlightAlert, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO flight_alerts
			(user_id, plan_part_id, plan_id, trip_id, ident, kind, status, message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`,
		a.UserID, a.PlanPartID, a.PlanID, a.TripID, a.Ident, a.Kind, a.Status, a.Message,
	).Scan(&a.ID, &a.CreatedAt)
	return a, err
}

// ListFlightAlerts returns a user's most recent alerts, newest first, capped at
// limit.
func (s *Store) ListFlightAlerts(ctx context.Context, userID int64, limit int) ([]FlightAlert, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, plan_part_id, plan_id, trip_id, ident, kind, status,
			message, created_at, read_at
		FROM flight_alerts WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FlightAlert
	for rows.Next() {
		var a FlightAlert
		if err := rows.Scan(&a.ID, &a.UserID, &a.PlanPartID, &a.PlanID, &a.TripID,
			&a.Ident, &a.Kind, &a.Status, &a.Message, &a.CreatedAt, &a.ReadAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkFlightAlertsRead stamps read_at on all of the user's unread alerts.
func (s *Store) MarkFlightAlertsRead(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE flight_alerts SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`,
		userID)
	return err
}

// CountUnreadFlightAlerts counts a user's unread alerts (for the badge).
func (s *Store) CountUnreadFlightAlerts(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM flight_alerts WHERE user_id = $1 AND read_at IS NULL`,
		userID).Scan(&n)
	return n, err
}
```

> Note: `internal/store/alerts.go` already imports `context` and `time`. If `alerts_test.go` is newly created, it needs `package store` and imports `"testing"` + `"time"` (`ctx` is a package-level var defined in the existing store test files).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestFlightAlertInsertListMarkRead -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/alerts.go internal/store/alerts_test.go
git commit -m "feat(store): flight_alerts insert/list/mark-read/count

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: DTO fields — alert id/timestamps + unread count

**Files:**
- Modify: `internal/api/dto.go` (`FlightAlertDTO`, `NotificationsDTO`, new `ToFlightAlertDTO`)
- Modify: `internal/handlers/notifications.go` (`buildNotificationsDTO`)
- Test: `internal/handlers/handlers_alert_inbox_test.go` (create) — covers the unread count via the notifications endpoint

- [ ] **Step 1: Write the failing test**

Create `internal/handlers/handlers_alert_inbox_test.go`:

```go
package handlers

import (
	"context"
	"net/http"
	"testing"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

// seedAlert inserts a flight_alert for a user via the store, returning nothing.
func seedAlert(t *testing.T, e *testEnv, userID int64, msg string) {
	t.Helper()
	if _, err := e.store.InsertFlightAlert(context.Background(), store.FlightAlert{
		UserID: userID, PlanPartID: 1, PlanID: 1, TripID: 1,
		Ident: "BA286", Kind: "gate", Status: "Scheduled", Message: msg,
	}); err != nil {
		t.Fatalf("seed alert: %v", err)
	}
}

func TestNotificationsIncludesUnreadAlerts(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "alice", false)
	seedAlert(t, e, uid, "BA286 now departs gate B32")
	seedAlert(t, e, uid, "BA286 cancelled")

	w := e.req(t, http.MethodGet, "/api/notifications", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeBody[api.NotificationsDTO](t, w)
	if got.UnreadAlerts != 2 {
		t.Errorf("unread_alerts = %d, want 2", got.UnreadAlerts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/ -run TestNotificationsIncludesUnreadAlerts -v`
Expected: compile error — `got.UnreadAlerts undefined`.

- [ ] **Step 3: Extend the DTOs**

In `internal/api/dto.go`, add to `NotificationsDTO` (after `FriendRequestsPending int`):

```go
	UnreadAlerts int `json:"unread_alerts"`
```

Replace `FlightAlertDTO` (update the comment to include `gate`, add id/timestamps):

```go
// FlightAlertDTO is a persisted in-app flight-change alert. It is both the
// element type of GET /api/alerts and the payload carried on the alert.created
// SSE event the poller publishes when a tracked flight changes (spec §9).
type FlightAlertDTO struct {
	ID         int64      `json:"id"`
	PlanPartID int64      `json:"plan_part_id"`
	PlanID     int64      `json:"plan_id"`
	TripID     int64      `json:"trip_id"`
	Ident      string     `json:"ident"`
	Kind       string     `json:"kind"` // delayed|cancelled|diverted|gate
	Status     string     `json:"status"`
	Message    string     `json:"message"`
	CreatedAt  time.Time  `json:"created_at"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
}

// ToFlightAlertDTO projects a stored alert onto the wire shape.
func ToFlightAlertDTO(a store.FlightAlert) FlightAlertDTO {
	return FlightAlertDTO{
		ID:         a.ID,
		PlanPartID: a.PlanPartID,
		PlanID:     a.PlanID,
		TripID:     a.TripID,
		Ident:      a.Ident,
		Kind:       a.Kind,
		Status:     a.Status,
		Message:    a.Message,
		CreatedAt:  a.CreatedAt,
		ReadAt:     a.ReadAt,
	}
}
```

> `internal/api/dto.go` already imports `store` and `time`.

- [ ] **Step 4: Compute unread count in `buildNotificationsDTO`**

In `internal/handlers/notifications.go`, replace `buildNotificationsDTO`:

```go
func (a *API) buildNotificationsDTO(ctx context.Context, userID int64) (api.NotificationsDTO, error) {
	n, err := a.Store.CountIncomingFriendRequests(ctx, userID)
	if err != nil {
		return api.NotificationsDTO{}, err
	}
	unread, err := a.Store.CountUnreadFlightAlerts(ctx, userID)
	if err != nil {
		return api.NotificationsDTO{}, err
	}
	return api.NotificationsDTO{FriendRequestsPending: n, UnreadAlerts: unread}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/handlers/ -run TestNotificationsIncludesUnreadAlerts -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/dto.go internal/handlers/notifications.go internal/handlers/handlers_alert_inbox_test.go
git commit -m "feat(api): alert DTO id/timestamps + unread_alerts in notifications

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: `GET /api/alerts` + `POST /api/alerts/read`

**Files:**
- Create: `internal/handlers/handlers_alert_inbox.go`
- Modify: `internal/handlers/handlers.go` (register routes; near the `/api/notifications` line ~80)
- Test: `internal/handlers/handlers_alert_inbox_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/handlers/handlers_alert_inbox_test.go`:

```go
func TestListAndMarkAlertsRead(t *testing.T) {
	e := setup(t, nil, nil)
	uid := e.user(t, "bob", false)
	other := e.user(t, "carol", false)
	seedAlert(t, e, uid, "BA286 now departs gate B32")
	seedAlert(t, e, other, "not yours")

	// List: only the viewer's alert.
	w := e.req(t, http.MethodGet, "/api/alerts", nil, uid)
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	list := decodeBody[[]api.FlightAlertDTO](t, w)
	if len(list) != 1 || list[0].Message != "BA286 now departs gate B32" {
		t.Fatalf("list = %+v", list)
	}

	// Mark read clears the unread count.
	w = e.req(t, http.MethodPost, "/api/alerts/read", nil, uid)
	if w.Code != http.StatusNoContent {
		t.Fatalf("mark-read status = %d", w.Code)
	}
	w = e.req(t, http.MethodGet, "/api/notifications", nil, uid)
	if decodeBody[api.NotificationsDTO](t, w).UnreadAlerts != 0 {
		t.Errorf("unread after mark-read != 0")
	}
}

func TestAlertsRequireAuth(t *testing.T) {
	e := setup(t, nil, nil)
	w := e.req(t, http.MethodGet, "/api/alerts", nil, 0)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("unauth list status = %d, want 401", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handlers/ -run 'TestListAndMarkAlertsRead|TestAlertsRequireAuth' -v`
Expected: FAIL — routes 404 (decode/`StatusNoContent` mismatch).

- [ ] **Step 3: Implement the handlers**

Create `internal/handlers/handlers_alert_inbox.go`:

```go
package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
)

// alertInboxLimit caps GET /api/alerts. The inbox is a recent-activity view,
// not full history (spec non-goal: no pagination/pruning yet).
const alertInboxLimit = 50

func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	rows, err := a.Store.ListFlightAlerts(r.Context(), me.ID, alertInboxLimit)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.FlightAlertDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, api.ToFlightAlertDTO(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) markAlertsRead(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	if err := a.Store.MarkFlightAlertsRead(r.Context(), me.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	// Recompute + push the badge so other tabs/devices clear too.
	a.publishNotifications(r.Context(), me.ID)
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Register the routes**

In `internal/handlers/handlers.go`, after the `/api/notifications` registration (line ~80):

```go
	mux.Handle("GET /api/alerts", req(http.HandlerFunc(a.listAlerts)))
	mux.Handle("POST /api/alerts/read", req(http.HandlerFunc(a.markAlertsRead)))
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/handlers/ -run 'TestListAndMarkAlertsRead|TestAlertsRequireAuth' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/handlers/handlers_alert_inbox.go internal/handlers/handlers.go internal/handlers/handlers_alert_inbox_test.go
git commit -m "feat(api): GET /api/alerts and POST /api/alerts/read

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Poller persists each in-app alert before publishing

**Files:**
- Modify: `internal/poller/alerts.go` (`publishAlert`)
- Test: `internal/poller/alerts_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/poller/alerts_test.go`. It uses this file's existing helpers
verbatim: `alertPoller(t)`, `seedUser(t, s)`, `mkPart(ctx, s, partSeed{...}, owner)`,
`setOriginGate(t, s, id, gate)`, and `p.maybeAlert(ctx, prev, id)` (see
`TestAlert_GateChangeAlwaysAlertsRegardlessOfThreshold` for the same shape):

```go
func TestAlert_GateChangePersistsInboxRow(t *testing.T) {
	p, s, _, _ := alertPoller(t)
	ctx := context.Background()
	owner := seedUser(t, s)
	// In-app on; crank the delay threshold so only the gate change can alert.
	if err := s.SetAlertPrefs(ctx, store.AlertPrefs{UserID: owner, InApp: true, Email: false, MinDelayMin: 600}); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
	now := time.Now()
	f, err := mkPart(ctx, s, partSeed{
		Ident: "BA286", ScheduledOut: now.Add(time.Hour), ScheduledIn: now.Add(3 * time.Hour),
		OriginIATA: "LHR", DestIATA: "JFK",
	}, owner)
	if err != nil {
		t.Fatalf("mkPart: %v", err)
	}

	prev := f // no gate yet
	setOriginGate(t, s, f.ID, "B32")
	p.maybeAlert(ctx, prev, f.ID)

	rows, err := s.ListFlightAlerts(ctx, owner, 50)
	if err != nil || len(rows) != 1 {
		t.Fatalf("ListFlightAlerts = %d, %v", len(rows), err)
	}
	if rows[0].Kind != "gate" || rows[0].PlanPartID != f.ID {
		t.Errorf("persisted alert wrong: %+v", rows[0])
	}
	if !strings.Contains(rows[0].Message, "B32") {
		t.Errorf("message missing gate: %q", rows[0].Message)
	}
}
```

(`strings` is already imported in `alerts_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/poller/ -run TestAlert_GateChangePersistsInboxRow -v`
Expected: FAIL — `ListFlightAlerts` returns 0 rows (poller doesn't persist yet).

- [ ] **Step 3: Persist in `publishAlert`**

In `internal/poller/alerts.go`, replace `publishAlert` so it inserts first, then publishes the persisted DTO:

```go
// publishAlert persists the alert for the recipient, then pushes a single-user,
// user-private alert.created SSE event carrying the stored row (with id +
// created_at). The payload reuses the open-shape NotificationsDTO with the Alert
// field set, so clients reading only friend_requests_pending ignore it safely.
// Persistence is best-effort: a failed insert is logged and we skip the push for
// that recipient (no orphan SSE without a backing row).
func (p *Poller) publishAlert(userID int64, tp *store.TrackerPart, ident, kind, msg string) {
	stored, err := p.Store.InsertFlightAlert(context.Background(), store.FlightAlert{
		UserID:     userID,
		PlanPartID: tp.PlanPartID,
		PlanID:     tp.PlanID,
		TripID:     tp.TripID,
		Ident:      ident,
		Kind:       kind,
		Status:     tp.Status,
		Message:    msg,
	})
	if err != nil {
		slog.Error("alert: persist inbox row", "user", userID, "err", err)
		return
	}
	dto := api.NotificationsDTO{Alert: ptrFlightAlertDTO(api.ToFlightAlertDTO(stored))}
	payload, err := json.Marshal(dto)
	if err != nil {
		slog.Error("alert: marshal", "err", err)
		return
	}
	p.Hub.Publish(sseAlertEvent(userID, payload))
}

func ptrFlightAlertDTO(d api.FlightAlertDTO) *api.FlightAlertDTO { return &d }
```

> The existing `publishAlert` body (building `api.NotificationsDTO{Alert: &api.FlightAlertDTO{...}}` by hand) is fully replaced. `context`, `encoding/json`, `log/slog`, `api`, and `store` are already imported in this file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/poller/ -run TestAlert_GateChangePersistsInboxRow -v`
Expected: PASS.

- [ ] **Step 5: Run the whole poller + backend suite (no regressions)**

Run: `go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/poller/alerts.go internal/poller/alerts_test.go
git commit -m "feat(poller): persist in-app alerts to flight_alerts before SSE

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Frontend API types + client methods

**Files:**
- Modify: `web/src/api/types.ts` (`FlightAlert`, `Notifications.unread_alerts`)
- Modify: `web/src/api/client.ts` (`getAlerts`, `markAlertsRead`)

- [ ] **Step 1: Add the types**

In `web/src/api/types.ts`, add `unread_alerts` to `Notifications`:

```ts
export interface Notifications {
  /** Count of friendship rows where the viewer is the recipient and
   *  status is still 'pending'. */
  friend_requests_pending: number;
  /** Count of the viewer's unread flight alerts (in-app inbox). */
  unread_alerts: number;
}
```

And add the `FlightAlert` type (near `AlertPrefs`):

```ts
/** A persisted in-app flight-change alert (inbox item / alert.created payload). */
export interface FlightAlert {
  id: number;
  plan_part_id: number;
  plan_id: number;
  trip_id: number;
  ident: string;
  kind: string; // delayed|cancelled|diverted|gate
  status: string;
  message: string;
  created_at: string;
  read_at?: string;
}
```

- [ ] **Step 2: Add client methods**

In `web/src/api/client.ts`, add inside the exported `api` object (near `getNotifications`):

```ts
  getAlerts: () => request<FlightAlert[]>('GET', '/api/alerts'),
  markAlertsRead: () => request<void>('POST', '/api/alerts/read'),
```

Ensure `FlightAlert` is imported in `client.ts`'s type import block (alongside `Notifications`).

- [ ] **Step 3: Typecheck**

Run: `cd web && npx tsc --noEmit`
Expected: no errors. (`unread_alerts` is now required on `Notifications`; the default in `coreSlice` is fixed in Task 12, so a transient error there is expected until then — if `tsc` flags only `coreSlice.ts` initial state, proceed; otherwise fix the flagged spot.)

- [ ] **Step 4: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts
git commit -m "feat(web): FlightAlert type + getAlerts/markAlertsRead client

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Alerts slice — inbox state, incoming, mark-read

**Files:**
- Modify: `web/src/state/alertsSlice.ts`
- Modify: `web/src/state/coreSlice.ts` (init load + default `unread_alerts`)
- Test: `web/src/state/alertsSlice.test.ts` (create) or append to `web/src/state/store.test.ts`

- [ ] **Step 1: Write the failing test**

Create `web/src/state/alertsSlice.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { useStore } from './store';
import type { FlightAlert } from '../api/types';
import { api } from '../api/client';

vi.mock('../api/client');

const mk = (id: number, msg: string): FlightAlert => ({
  id, plan_part_id: 1, plan_id: 1, trip_id: 1, ident: 'BA286',
  kind: 'gate', status: 'Scheduled', message: msg, created_at: '2026-06-01T00:00:00Z',
});

describe('alertsSlice inbox', () => {
  beforeEach(() => {
    useStore.setState({ alerts: [], unreadAlerts: 0 });
  });

  it('loadAlerts fills the list and unread count', async () => {
    vi.mocked(api.getAlerts).mockResolvedValue([mk(1, 'a'), mk(2, 'b')]);
    await useStore.getState().loadAlerts();
    expect(useStore.getState().alerts).toHaveLength(2);
    expect(useStore.getState().unreadAlerts).toBe(2);
  });

  it('applyIncomingAlert prepends and bumps unread', () => {
    useStore.setState({ alerts: [mk(1, 'a')], unreadAlerts: 1 });
    useStore.getState().applyIncomingAlert(mk(2, 'b'));
    expect(useStore.getState().alerts[0].id).toBe(2);
    expect(useStore.getState().unreadAlerts).toBe(2);
  });

  it('markAlertsRead clears unread and stamps read_at locally', async () => {
    vi.mocked(api.markAlertsRead).mockResolvedValue(undefined);
    useStore.setState({ alerts: [mk(1, 'a')], unreadAlerts: 1 });
    await useStore.getState().markAlertsRead();
    expect(useStore.getState().unreadAlerts).toBe(0);
    expect(useStore.getState().alerts[0].read_at).toBeTruthy();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/state/alertsSlice.test.ts`
Expected: FAIL — `alerts`/`loadAlerts` not on state.

- [ ] **Step 3: Extend the slice**

In `web/src/state/alertsSlice.ts`: import `FlightAlert` and add to the imports `import type { AlertPrefs, FlightAlert, UpdateAlertPrefsInput } from '../api/types';`. Extend the interface:

```ts
export interface AlertsSlice {
  alertPrefs: AlertPrefs | null;
  alerts: FlightAlert[];
  unreadAlerts: number;

  loadAlertPrefs: () => Promise<void>;
  updateAlertPrefs: (patch: UpdateAlertPrefsInput) => Promise<void>;
  optInPlanAlerts: (planId: number) => Promise<void>;
  optOutPlanAlerts: (planId: number) => Promise<void>;

  loadAlerts: () => Promise<void>;
  applyIncomingAlert: (alert: FlightAlert) => void;
  markAlertsRead: () => Promise<void>;
}
```

Add the initial state and actions inside `createAlertsSlice` (alongside the existing ones):

```ts
  alerts: [],
  unreadAlerts: 0,

  async loadAlerts() {
    try {
      const alerts = await api.getAlerts();
      set({ alerts, unreadAlerts: alerts.filter((a) => !a.read_at).length });
    } catch {
      // Non-fatal: SSE / next reload recovers the inbox.
    }
  },

  applyIncomingAlert(alert) {
    set((s) => ({
      alerts: [alert, ...s.alerts].slice(0, 50),
      unreadAlerts: s.unreadAlerts + 1,
    }));
  },

  async markAlertsRead() {
    const now = new Date().toISOString();
    set((s) => ({
      unreadAlerts: 0,
      alerts: s.alerts.map((a) => (a.read_at ? a : { ...a, read_at: now })),
    }));
    try {
      await api.markAlertsRead();
    } catch (err) {
      set({ error: errorMessage(err) });
    }
  },
```

- [ ] **Step 4: Load alerts on init + fix the default notifications shape**

In `web/src/state/coreSlice.ts`:
- Update the two `notifications:` initial/reset literals from `{ friend_requests_pending: 0 }` to `{ friend_requests_pending: 0, unread_alerts: 0 }` (lines ~81 and ~155).
- In `init`, add `loadAlerts` to the bootstrap fan-out:

```ts
      await Promise.all([get().refreshAll(), get().refreshNotifications(), get().loadAlerts()]);
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/state/alertsSlice.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/state/alertsSlice.ts web/src/state/coreSlice.ts web/src/state/alertsSlice.test.ts
git commit -m "feat(web): alerts inbox slice (load/incoming/mark-read)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 13: SSE consumer + toast

**Files:**
- Modify: `web/src/sse.ts` (`alert.created` listener, `onAlert` handler)
- Modify: `web/src/App.tsx` (wire `onAlert` → `applyIncomingAlert` + toast)
- Test: `web/src/sse.test.ts` (append; or create a focused test if absent)

- [ ] **Step 1: Write the failing test**

Append to `web/src/sse.test.ts` (use the file's existing `EventSource` mock harness). Assert that an `alert.created` event whose payload is a `NotificationsDTO`-shaped object with `alert` set routes to `onAlert`:

```ts
it('routes alert.created to onAlert', () => {
  const onAlert = vi.fn();
  connectSSE({ onNotifications: vi.fn(), onPlanPart: vi.fn(), onAlert }, {});
  // Fire the mocked EventSource's 'alert.created' listener with a payload.
  emit('alert.created', JSON.stringify({ alert: { id: 7, message: 'BA286 cancelled' } }));
  expect(onAlert).toHaveBeenCalledWith(expect.objectContaining({ id: 7 }));
});
```

> Implementer note: match the existing mock's API in `sse.test.ts` (how it registers listeners and `emit`s events). If the file uses a helper to grab the registered handler, reuse it. The behaviour under test: the `alert.created` listener parses `data`, and calls `onAlert(parsed.alert)` only when `parsed.alert` is present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/sse.test.ts`
Expected: FAIL — `onAlert` not called (no listener yet).

- [ ] **Step 3: Add the SSE handler type + listener**

In `web/src/sse.ts`: import `FlightAlert` alongside the existing type import:

```ts
import type { FlightAlert, Notifications, TrackerPart } from './api/types';
```

Add to `SSEHandlers`:

```ts
  /** A flight-change alert arrived for the viewer. The poller publishes
   * alert.created (user-private) carrying a NotificationsDTO with `alert` set. */
  onAlert?: (alert: FlightAlert) => void;
```

Add the listener inside `open()` (after the `notifications.updated` listener):

```ts
    es.addEventListener('alert.created', (ev) => {
      try {
        const { alert } = JSON.parse((ev as MessageEvent).data) as { alert?: FlightAlert };
        if (alert) handlers.onAlert?.(alert);
      } catch (err) {
        console.error('bad SSE payload', err);
      }
    });
```

- [ ] **Step 4: Wire it + toast in App.tsx**

In `web/src/App.tsx`:
- Pull the action and a toast setter from the store. Add near the other `useStore` selectors:

```tsx
  const applyIncomingAlert = useStore((s) => s.applyIncomingAlert);
```

- Add `onAlert` to the `connectSSE` handlers object:

```tsx
        onAlert: (alert) => {
          applyIncomingAlert(alert);
          setNotice({ message: alert.message, severity: 'info' });
        },
```

- Add `applyIncomingAlert` to the effect's dependency array (alongside the other handlers).

> This reuses the existing `notice` Snackbar (info severity) as the toast — no third Snackbar needed; `setNotice` is already a store action used by App.tsx. (Simpler than the spec's "third Snackbar"; same UX.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/sse.test.ts`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/sse.ts web/src/App.tsx web/src/sse.test.ts
git commit -m "feat(web): consume alert.created SSE — inbox + toast

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 14: Avatar menu — combined badge + alerts section

**Files:**
- Modify: `web/src/components/Layout.tsx`
- Test: `web/src/components/Layout.test.tsx` (append)

The avatar `Badge` shows `friend_requests_pending + unread_alerts`. A new **Alerts** section in the menu lists recent messages; opening the menu calls `markAlertsRead`. Each alert navigates to `/tracker?part=<plan_part_id>`.

- [ ] **Step 1: Write the failing test**

Append to `web/src/components/Layout.test.tsx` (reuse the file's existing render harness — it already renders `<Layout/>` within a router and seeds the store):

```tsx
it('shows alerts in the account menu and marks them read on open', async () => {
  const markAlertsRead = vi.fn().mockResolvedValue(undefined);
  useStore.setState({
    notifications: { friend_requests_pending: 0, unread_alerts: 1 },
    alerts: [{
      id: 1, plan_part_id: 9, plan_id: 1, trip_id: 1, ident: 'BA286',
      kind: 'gate', status: 'Scheduled', message: 'BA286 now departs gate B32',
      created_at: '2026-06-01T00:00:00Z',
    }],
    markAlertsRead,
  });
  renderLayout(); // existing helper in this test file
  await userEvent.click(screen.getByLabelText('Account menu'));
  expect(screen.getByText('BA286 now departs gate B32')).toBeInTheDocument();
  expect(markAlertsRead).toHaveBeenCalled();
});
```

> Implementer note: match the file's existing setup (how it provides the store + router). If it renders via a local `renderLayout`/`setup`, reuse it; otherwise wrap `<Layout/>` in `<MemoryRouter>` and set store state before clicking.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/components/Layout.test.tsx`
Expected: FAIL — alert message not rendered.

- [ ] **Step 3: Implement the menu section + badge sum**

In `web/src/components/Layout.tsx`:

- Add store selectors near the existing `pendingRequests`:

```tsx
  const unreadAlerts = useStore((s) => s.notifications.unread_alerts);
  const alerts = useStore((s) => s.alerts);
  const markAlertsRead = useStore((s) => s.markAlertsRead);
  const navigate = useNavigate();
```

(Import `useNavigate` from `react-router-dom` if not already imported.)

- Change the avatar `Badge` count to the sum:

```tsx
            badgeContent={pendingRequests + unreadAlerts}
            ...
            invisible={pendingRequests + unreadAlerts === 0}
```

- When the menu opens, mark alerts read. Find where `setMenuAnchor(e.currentTarget)` is called (the avatar `IconButton onClick`) and replace with:

```tsx
                onClick={(e) => {
                  setMenuAnchor(e.currentTarget);
                  if (unreadAlerts > 0) void markAlertsRead();
                }}
```

- Add an **Alerts** section at the top of the `<Menu>` (after the "Signed in as" item + its `<Divider/>`, before the Friends item):

```tsx
            {alerts.length > 0 && (
              <Box>
                <MenuItem disabled sx={{ opacity: '1 !important' }}>
                  <Typography variant="caption" color="text.secondary">
                    Flight alerts
                  </Typography>
                </MenuItem>
                {alerts.slice(0, 6).map((al) => (
                  <MenuItem
                    key={al.id}
                    onClick={() => {
                      closeMenu();
                      navigate(`/tracker?part=${al.plan_part_id}`);
                    }}
                  >
                    <ListItemIcon>
                      <NotificationsIcon fontSize="small" />
                    </ListItemIcon>
                    <Typography variant="body2" noWrap sx={{ maxWidth: 260 }}>
                      {al.message}
                    </Typography>
                  </MenuItem>
                ))}
                <Divider />
              </Box>
            )}
```

(`Box`, `MenuItem`, `ListItemIcon`, `Typography`, `Divider`, `NotificationsIcon` are already imported in this file.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/components/Layout.test.tsx`
Expected: PASS.

- [ ] **Step 5: Full FE suite + typecheck + lint**

Run: `cd web && npx tsc --noEmit && npx vitest run`
Expected: PASS. Then `cd web && npm run lint` (if defined) — expect clean.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/Layout.tsx web/src/components/Layout.test.tsx
git commit -m "feat(web): combined alert/friend badge + alerts in avatar menu

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Final verification

- [ ] **Backend:** `go test ./...` — all PASS.
- [ ] **Frontend:** `cd web && npx tsc --noEmit && npx vitest run` — all PASS.
- [ ] **Manual smoke (optional, via the run/verify skill):** dev-login, open a trip with a flight, confirm the gate row shows (Unknown when absent); simulate a gate change (UPDATE flight_details.origin_gate on a tracked part) and confirm a toast + the avatar badge + the menu entry appear, and that opening the menu clears the badge.
- [ ] Report results to the user. Do NOT push without asking.

---

## Notes for the executor

- **TDD discipline:** every code change is preceded by a failing test in the same task. Don't batch.
- **DRY:** `fmtGate` is the single source for gate formatting (both surfaces use it). `ToFlightAlertDTO` is the single projection (handler list + poller SSE both use it).
- **YAGNI:** no pagination, no per-alert dismiss, no auto-prune — list caps at 50 in both the query and the slice.
- **Naming consistency:** store `FlightAlert` ↔ DTO `FlightAlertDTO` ↔ FE `FlightAlert`; methods `InsertFlightAlert`/`ListFlightAlerts`/`MarkFlightAlertsRead`/`CountUnreadFlightAlerts`; slice actions `loadAlerts`/`applyIncomingAlert`/`markAlertsRead`.
- **Email path untouched** — gate emails already work via `BuildFlightAlertEmail`.
```
