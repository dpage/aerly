# Upcoming-plan email reminders — design

**Issue:** [#11](https://github.com/dpage/aerly/issues/11) — Add email alerts for upcoming plans
**Date:** 2026-06-04

## Problem

Users want to be reminded ahead of an upcoming plan they can see. They should be
able to opt in at the **trip** level (reminders for every plan in the trip) and
override that per **plan** — including opting *out* of a single plan while
opted in for the trip. Lead time (how many hours ahead) is configurable at both
levels.

This is distinct from the existing flight-status-change alert system
(`alert_prefs`, `plan_alert_optin`, `flight_alerts`, `poller/alerts.go`), which
fires on delay / gate / cancellation. **This feature must not change that
behaviour** — status-change alerts keep firing whenever the API gives us the
info, regardless of reminder opt-ins.

## Scope (agreed)

- **Granularity:** a reminder fires per **plan-part** (each leg / timeline
  entry), since that is what carries `starts_at`. Opt-in remains per-plan and
  per-trip.
- **Default lead time:** 24 hours, overridable at trip and plan level.
- **Channels:** email **and** in-app inbox entry (reusing the existing inbox).
- **Plan types:** all (`flight`, `train`, `hotel`, `ground`, `dining`,
  `excursion`) — anything with an upcoming `plan_parts.starts_at`.

## Data model — migration `0023_plan_reminders`

```sql
-- Trip-level reminder opt-in: presence of a row = opted in for the whole trip.
CREATE TABLE trip_reminder_optin (
    trip_id    BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lead_hours INT NOT NULL DEFAULT 24 CHECK (lead_hours > 0),
    PRIMARY KEY (trip_id, user_id)
);

-- Per-plan override. enabled = TRUE → opt in; enabled = FALSE → explicit
-- opt-out (beats a trip-level opt-in). Absence of a row = inherit the trip.
CREATE TABLE plan_reminder_optin (
    plan_id    BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL,
    lead_hours INT NOT NULL DEFAULT 24 CHECK (lead_hours > 0),
    PRIMARY KEY (plan_id, user_id)
);

-- Dedupe: one reminder per part per user, ever.
CREATE TABLE plan_reminder_sent (
    plan_part_id BIGINT NOT NULL REFERENCES plan_parts(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_part_id, user_id)
);
```

A matching `0023_plan_reminders.down.sql` drops the three tables.

### Effective opt-in for `(user, plan)`

1. If a `plan_reminder_optin` row exists → use it (`enabled` + its `lead_hours`).
   This overrides the trip in **both** directions (force on / force off).
2. Else if a `trip_reminder_optin` row exists → opted in with the trip's
   `lead_hours`.
3. Else → not opted in.

## Scheduler — `internal/poller/reminders.go`

A new pass added to `Poller.tick()`, alongside the existing active-flight and
metadata passes. It reuses the poller's existing lifecycle, mail config
(`MailFromAddress`, `SendmailPath`, `PublicURL`, `SendAlertEmail`) and SSE hub.
No in-memory timers — each tick is a fresh DB query, so it is restart-safe.

`remindUpcoming(ctx, now)`:

1. `due := Store.DueReminders(ctx, now)` returns candidate
   `(plan_part, user, email, lead_hours, trip/plan context)` rows where:
   - the part is active (`status <> 'cancelled'`, `dismissed_at IS NULL`),
   - `starts_at > now` (never remind for something already started),
   - `now >= starts_at - lead_hours` (inside the lead window),
   - no `plan_reminder_sent` row exists for `(part, user)`,
   - the user's **effective opt-in** resolves to *on* (the plan-override /
     trip resolution above, expressed in SQL).
2. For each candidate plan, filter its users through the existing
   `Store.VisiblePlanUserIDs(planID)` set, so a trip-level opt-in never leaks a
   plan hidden from that viewer by `plan_visibility`. (Reuses tested visibility
   logic instead of duplicating it in the `DueReminders` SQL.)
3. For each survivor, dispatch and then `MarkReminderSent(part, user)`:
   - **Email:** when `MailFromAddress` is set and the user has a verified
     address — `BuildPlanReminderEmail` via `SendAlertEmail`.
   - **In-app:** always — persist a `flight_alerts` row with `kind = "reminder"`
     and publish the `alert.created` SSE event (same path as
     `poller.publishAlert`).
   - `MarkReminderSent` is stamped **after** dispatch, so a crash mid-send
     re-sends on the next tick rather than silently dropping (mirrors the
     existing `SetFlightPartAlertSig` ordering).

Reminders fire on both channels purely from the opt-in. They do **not** consult
`alert_prefs` (that governs status-change alerts only).

## Email — `internal/mailer/plan_reminder.go`

`BuildPlanReminderEmail(PlanReminderInput)` reusing `AssembleRFC822` /
`HTMLShell` / `BrandColor`, generic across plan types:

- **Subject:** `Upcoming: <label>` where `<label>` is the flight ident + route
  for flights, the plan title when set, or a type-derived fallback
  ("Hotel check-in", "Dinner reservation", "Train", …).
- **Lead line:** `<label> starts <local time> (<tz>)`, followed by an
  "Open Aerly" button linking to the trip timeline (`<PublicURL>/trips/<tripID>`).

`PlanReminderSubject(label)` is exposed so the in-app message can reuse it.

## API

Per-viewer fields folded into existing DTOs (mirrors `PlanDTO.alert_opted_in`):

- **`TripDTO`**: `reminder_opted_in bool`, `reminder_lead_hours int`.
- **`PlanDTO`**: `reminder_override string` (`"inherit" | "on" | "off"`),
  `reminder_lead_hours int`.

New endpoints (mirroring the existing plan-alert-optin route style and authz):

| Method & path | Body | Effect |
|---|---|---|
| `PUT /api/trips/{id}/reminder` | `{lead_hours}` | Opt in / update trip lead. |
| `DELETE /api/trips/{id}/reminder` | — | Opt out at trip level (remove row). |
| `PUT /api/plans/{id}/reminder` | `{enabled, lead_hours}` | Set per-plan override. |
| `DELETE /api/plans/{id}/reminder` | — | Clear override (inherit trip). |

Authz: the requester must be a trip member (trip endpoints) / able to see the
plan (plan endpoints), reusing the existing access checks used by the trip and
plan handlers. `lead_hours` is validated `> 0` (and clamped to a sane max, e.g.
≤ 8760).

## Frontend

- **`TripDetail`**: an "Email reminders" switch + an "hours before" number
  field → `PUT` / `DELETE` the trip reminder.
- **`PlanEditDialog`**: a reminder override control — a select
  (`Use trip setting` / `Remind me` / `Don't remind me`) with a lead-hours field
  shown when `Remind me` → `PUT` / `DELETE` the plan reminder.
- **Inbox (`Layout.tsx`)**: relabel the section header from "Flight alerts" to
  "Alerts"; branch navigation by `kind` — `reminder` → `/trips/:tripId`
  timeline, other kinds → `/tracker?part=` as today.
- Zustand store actions, `api/client.ts` methods, and `api/types.ts` additions
  for all four endpoints + the new DTO fields.

## Testing

- **store**: opt-in CRUD; `DueReminders` resolution — plan-override-beats-trip
  (both `on→off` and `off→on`), dedupe via `plan_reminder_sent`, visibility
  exclusion, lead-window boundaries, and exclusion of past / cancelled /
  dismissed parts.
- **poller**: `remindUpcoming` captures the email, persists the in-app row,
  marks sent, and does not double-send on a subsequent tick.
- **mailer**: rendered output for a flight label and a non-flight label.
- **handlers**: the four endpoints incl. authz failure cases.
- **web**: component tests for the two controls and the inbox nav branch.

## Out of scope / non-goals

- No per-channel preference for reminders (the opt-in is the toggle); YAGNI.
- No change to status-change alerts, `alert_prefs`, `plan_alert_optin`, or the
  existing `poller/alerts.go` logic.
- No re-reminding when a part's time shifts after a reminder was sent — the
  status-change alert system already covers material delays.
```
