# Flight gate display + in-app alert inbox

**Date:** 2026-06-03
**Status:** Approved for implementation

## Problem

Two related gaps in how flight changes reach the user:

1. **Gate is tracked but never shown.** The DB stores `origin_gate` / `dest_gate` /
   `origin_terminal` / `dest_terminal` on `flight_details` (migration 0014), the
   poller persists the live values each tick, and the gate-change *alert* fires —
   but the gate value is dropped at the API projection layer (`FlightDetailDTO`
   omits it), so no UI surface displays it.
2. **In-app alerts are published but never consumed.** The poller publishes a
   per-user `alert.created` SSE event (delayed / cancelled / diverted / gate),
   but the frontend has no listener and nothing is persisted (only a
   `last_alert_sig` dedupe column exists). Email alerts already work. So a user
   has no in-app way to see that a flight changed.

## Goals

- Show terminal + gate on the two flight surfaces, with an `Unknown` fallback.
- Make all four alert kinds viewable in-app: a transient toast when one arrives,
  plus a persistent history with an unread badge, surviving reload/offline.

## Non-goals

- Auto-pruning old alerts (the list query caps at 50; no background cleanup).
- Per-alert dismiss (mark-all-read only).
- Changing the email alert path (already works, including gate).
- Push/web-notification delivery.

---

## Part A — Gate display

### Data plumbing

The gate already reaches the `store.Flight` carrier via `flightPartColumns`
(`internal/store/tracker.go`), but the *display* path runs through the separate
`store.FlightDetail` satellite, which lacks the fields.

- `store.FlightDetail` (`internal/store/plans.go:65`): add `OriginGate`,
  `DestGate`, `OriginTerminal`, `DestTerminal string`.
- `FlightDetailFor` (`internal/store/plans.go:643`): extend the SELECT with
  `COALESCE(origin_gate,'')`, `COALESCE(dest_gate,'')`,
  `COALESCE(origin_terminal,'')`, `COALESCE(dest_terminal,'')`.
- `FlightDetailDTO` (`internal/api/dto.go:377`) + `ToFlightDetailDTO`: carry the
  four fields as JSON `origin_gate` / `dest_gate` / `origin_terminal` /
  `dest_terminal` (plain strings, no `omitempty` — the frontend renders the
  `Unknown` fallback from an empty value).
- Frontend `FlightDetail` type (`web/src/api/types.ts`): add the four optional
  strings.
- **No migration** — columns exist (0014).

### Frontend formatting helper

`fmtGate(terminal, gate)` (in `web/src/lib/`):
- both → `"Terminal 5 · Gate B32"`
- gate only → `"Gate B32"`
- terminal only → `"Terminal 5"`
- neither → `"Unknown"`

### Render locations

1. **Map — expanded `FlightDetailCard`** (`Route` section): two new rows —
   - `Departure` → `fmtGate(origin_terminal, origin_gate)`
   - `Arrival` → `fmtGate(dest_terminal, dest_gate)`
2. **Timeline tile — un-expanded face** (`TripTimeline` `PartCard`): a caption
   line on the always-visible tile face for flight parts, showing the
   **departure** terminal/gate (the actionable one at the airport), e.g.
   `Gate B32` or `Unknown`. (Departure-only on the face for compactness; the map
   card shows both.)

---

## Part B — Alert inbox (persist-per-recipient + toast, folded into avatar menu)

### Approach

Persist **one alert row per in-app recipient**. The poller already computes the
recipient set and a per-recipient `InApp` flag in `dispatchAlert`; persistence
hangs off that loop. (Rejected alternative: one shared alert row + a join table
for per-user read state — over-engineered for a simple per-user read flag.)

### Backend

- **Migration `0020_flight_alerts`:**
  ```
  flight_alerts(
    id BIGSERIAL PK,
    user_id BIGINT NOT NULL,
    plan_part_id BIGINT NOT NULL,
    plan_id BIGINT NOT NULL,
    trip_id BIGINT NOT NULL,
    ident TEXT NOT NULL,
    kind TEXT NOT NULL,         -- delayed|cancelled|diverted|gate
    status TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ NULL
  )
  ```
  Indexes: `(user_id, created_at DESC)`; partial `(user_id) WHERE read_at IS NULL`
  for the unread count.
- `poller.publishAlert` (`internal/poller/alerts.go`): **insert** a row for the
  recipient, then publish the existing `alert.created` SSE carrying the persisted
  alert (now including `id` + `created_at`). Insert failures are logged and never
  block the poll loop (same best-effort stance as the email path).
- `FlightAlertDTO` (`internal/api/dto.go:236`): add `id int64`,
  `created_at time.Time`, `read_at *time.Time`; update the stale `Kind` comment
  to include `gate`.
- `NotificationsDTO`: add `unread_alerts int`. The single avatar badge =
  `friend_requests_pending + unread_alerts`.
- **New endpoints:**
  - `GET /api/alerts` → the viewer's recent 50 alerts, newest first.
  - `POST /api/alerts/read` → mark all the viewer's alerts read (sets `read_at`).
- `GET /api/notifications` also computes `unread_alerts`.
- New store methods: `InsertFlightAlert`, `ListFlightAlerts`,
  `MarkFlightAlertsRead`, `CountUnreadFlightAlerts`.

### Frontend

- Extend `alertsSlice` (`web/src/state/alertsSlice.ts`): hold
  `alerts: FlightAlert[]`, derive `unreadAlerts`; actions `loadAlerts`,
  `applyIncomingAlert` (prepend, bump unread, surface a toast),
  `markAlertsRead`. Load alerts on `init` alongside notifications.
- `sse.ts`: add an `alert.created` listener → new `onAlert(alert)` handler.
- `App.tsx`: wire `onAlert` → `applyIncomingAlert`; render a **third Snackbar**
  (info severity) for the incoming-alert toast.
- `Layout.tsx`: the avatar `Badge` sums `friend_requests_pending + unread_alerts`;
  add an **Alerts** section in the account menu listing recent messages (each
  navigates to `/tracker?part=<plan_part_id>`); opening the menu (or the section)
  calls `markAlertsRead`. The existing Friends entry keeps its own count chip.
- `api/types.ts`: add `FlightAlert` type; `Notifications` gains `unread_alerts`.
- `api/client.ts`: `getAlerts`, `markAlertsRead`.

---

## Testing

- **store:** `FlightDetailFor` returns gate/terminal; `InsertFlightAlert` /
  `ListFlightAlerts` (ordering, 50 cap) / `MarkFlightAlertsRead` /
  `CountUnreadFlightAlerts`.
- **api:** `ToFlightDetailDTO` maps the four fields; `GET /api/alerts` and
  `POST /api/alerts/read` (auth-scoped to the viewer); `unread_alerts` in the
  notifications body.
- **poller:** a gate/delay/cancel change persists a row per in-app recipient and
  the `alert.created` SSE carries the row id.
- **frontend:** `FlightDetailCard` and the timeline tile render gate / `Unknown`;
  `fmtGate` cases; `alertsSlice` incoming-alert bumps unread and markRead clears
  it; the menu renders alerts and marks read on open; `sse.ts` routes
  `alert.created` to `onAlert`.

TDD throughout (test first, watch it fail, implement).

## Logistics

Work directly on `main` (other in-flight work is done). Commit the spec, then
implement in reviewable increments. Ask before pushing.
