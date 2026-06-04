# Link / split multi-part bookings (issue #12)

## Problem

A "booking" in Aerly is a `plan`; its legs (outbound / return / connections) are
`plan_parts`. A plan with more than one live part shows a **multi-part** chip on
the timeline.

Whether legs end up grouped into one multi-part plan or scattered across several
single-part plans is decided at capture time and is often wrong:

- The **LLM capture path** (`planops.Propose` via the email/paste extractor) is
  *told* to emit "one booking = one plan", but it sometimes splits a round-trip
  into separate plans, or lumps unrelated flights together.
- The **TripIt `.ics` path** (`tripitics`) always emits **one plan per leg** — it
  has no booking-grouping signal at all, so every multi-leg itinerary imports as
  separate plans.

Today there is no way to fix this after the fact. Users need to **link** plans
that *are* the same booking, and **split** a plan whose parts *aren't*.

## Scope

In scope:

1. **Link** two or more same-type plans into one multi-part plan.
2. **Split** one leg out of a multi-part plan into its own plan.
3. **Auto-group** proposed flight/train plans that share a confirmation reference
   at capture time, plus a sharper LLM prompt so grouping is more often right up
   front.

Link/split applies only to plan types that legitimately have multi-leg bookings:
**`flight` and `train`**. Hotels, ground, dining and excursion plans are excluded
(they are single-venue or already paired by the importer).

Out of scope: schema changes, a standalone "manage bookings" screen, reordering
legs by hand (sequence is always derived from start time).

## Key model facts (why the design is shaped this way)

- A `plan_part` has **no type column** — a part's type is read from its plan's
  `plans.type` (`planPartColumns` selects `pl.type`). Therefore **every part of a
  plan shares one type**, so a link may only combine plans of the *same* type, and
  the result is naturally a valid single-type multi-part plan.
- `flight_details` / `train_details` and `positions` key on **`plan_part_id`**, so
  per-type details and live tracking travel with a part automatically when it is
  re-parented. No extra work for those.
- `plan_passengers` and `plan_visibility` / `plan_visibility_members` key on
  **`plan_id`**. They do **not** travel with a part. A split must therefore
  **copy** the parent's passengers and visibility to the new plan; otherwise an
  `only_visible_to` booking would silently become trip-wide when a leg is split
  out. A link keeps the primary plan's passengers/visibility and discards the
  absorbed plans' (the "keep one as primary" rule).
- `flight_alerts` **denormalises** `plan_id` and `trip_id` with no FK. When a part
  moves between plans, its alert rows' `plan_id` must be repointed — and this is
  load-bearing for link, where the emptied absorbed plans are deleted, leaving any
  un-repointed alert `plan_id` dangling.

## Design

### Store

Two new transactional methods on `*store.Store`, both all-or-nothing:

**`LinkPlans(ctx, primaryID int64, absorbIDs []int64) error`**

1. Load the primary and every absorbed plan. Validate: all exist; all are in the
   **same trip** as the primary; all have the **same `type`**, and that type is
   `flight` or `train`; `absorbIDs` is non-empty and excludes the primary.
2. Re-parent every part of each absorbed plan to `primaryID`
   (`UPDATE plan_parts SET plan_id = primary WHERE plan_id = ANY(absorbed)`),
   including dismissed/superseded parts so history is preserved.
3. Repoint moved parts' alerts:
   `UPDATE flight_alerts SET plan_id = primary WHERE plan_id = ANY(absorbed)`.
4. Re-sequence the primary's live parts by `starts_at` (`seq = 0..n`).
5. Delete the now-empty absorbed plans (`plan_passengers`, `plan_visibility*`
   cascade away). The primary's plan-level fields and passengers/visibility are
   untouched — primary wins.

**`SplitPlanPart(ctx, partID int64) (newPlanID int64, parentPlanID int64, err error)`**

1. Load the part, its plan, and the plan's live-part count. Reject with a sentinel
   (`ErrNotSplittable`) when the plan has **one or zero** live parts (nothing to
   split) or the type isn't `flight`/`train`.
2. Insert a new plan in the same trip, same `type` and `source`, copying the
   parent's `title`, `confirmation_ref` and `notes` (a copy — the user edits the
   new plan afterward; confirmed: copy, don't clear).
3. Copy the parent's passengers (`plan_passengers`) and visibility
   (`plan_visibility` + members) to the new plan.
4. Move the part to the new plan and repoint its `flight_alerts.plan_id`.
5. Re-sequence both plans' live parts by `starts_at`.

Both methods run inside `pgx` transactions following the existing store pattern.

### HTTP

- `POST /api/plans/{id}/link` — body `{ "plan_ids": [<absorbed ids>] }`; `{id}` is
  the primary. Requires editor on the plan's trip (`requirePlanEdit`), and each
  absorbed plan is validated by the store to be in that same trip. Returns the
  primary plan DTO. Publishes `plan.updated` for the primary and `trip.updated`
  for the trip (absorbed plans vanished from the timeline).
- `POST /api/plan-parts/{id}/split` — splits part `{id}` out. Requires
  `requirePartEdit`. Returns the new plan DTO (and the parent is reloaded by the
  client via the trip refresh / SSE). Publishes `plan.updated` for both plans and
  `trip.updated`.

`ErrNotSplittable` / validation failures map to `400`; unknown ids to `404`;
cross-trip or cross-type to `400` with a clear message.

### Capture-time auto-grouping

A pure post-pass in `planops.Propose`, applied to the proposed plans before
return: group `flight` and `train` proposals that share a **non-empty,
case-insensitive** `confirmation_ref` into a single multi-part proposal, with
parts ordered by `StartsAt`. Plans with an empty ref are never merged. This runs
after extraction and rebooking matching; it only merges plans of the same type.

The LLM `plansSystemPrompt` is sharpened to stress that all legs sharing one
booking reference (PNR) — including multi-segment one-ways and connections, not
just round trips — must be emitted as one plan with multiple parts.

The `.ics` path is unchanged (TripIt carries no PNR); those imports are corrected
with the manual link UI.

### Web UI

- **Timeline (`TripTimeline.tsx`): link.** A "Link bookings" affordance puts the
  timeline into a selection mode where the user ticks 2+ plans of the *same*
  linkable type (flight or train); a confirm action calls `link` with the first
  selected (or an explicitly chosen) plan as primary. On success the absorbed
  plans fold into the primary and the multi-part chip appears. Selection is
  constrained to one type at a time; mixed/ineligible selections disable confirm.
- **`PlanEditDialog.tsx`: split.** For a multi-part flight/train plan, each leg row
  gains a **"Split out"** button (hidden when the plan has a single live part, and
  for non-linkable types). It calls `split` for that part and refreshes.
- Zustand store gains `linkPlans` and `splitPlanPart` actions mirroring the
  existing `movePlan` / `updatePlanPart` action shape; API client gains the two
  calls; `api/types` unchanged (responses are existing `Plan` DTOs).

### Testing

- **Store:** link merges parts + repoints `flight_alerts` + deletes emptied plans;
  link rejects cross-trip, cross-type, non-linkable type, empty/self absorb; split
  copies passengers + visibility to the new plan, re-sequences both, repoints
  alerts; split rejects single-part and non-linkable plans.
- **Handlers:** authz (non-editor 403), payload validation, 404s, SSE publishes.
- **planops:** confirmation_ref auto-grouping merges same-ref same-type flight
  plans, leaves empty-ref and differing-type plans alone, orders parts by start.
- **Web:** timeline link flow (select → confirm → folded) and the split button
  (visible only for multi-part linkable plans; calls action).

## Deliberate decisions

- **Split copies the parent's confirmation_ref** (user edits afterward) rather than
  clearing it — no data loss. (Confirmed.)
- **Link allows any same-type plans the user selects**, regardless of
  confirmation_ref — the capture paths sometimes drop/garble the PNR, so manual
  judgement is trusted. (Confirmed.)
- **Primary wins** on link: absorbed plans' passengers/visibility/notes/title are
  discarded. (Confirmed: "keep one as primary".)
- **Sequence is always derived from `starts_at`**, never hand-ordered.
