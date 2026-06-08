# Share with all friends — design

**Date:** 2026-06-08
**Status:** Approved, pending implementation plan

## Problem

Sharing a trip or plan today requires adding friends one at a time as trip
members, trip passengers, or plan passengers. There is no way to share with all
of your friends in one action, and the legacy per-flight "Share with all
friends" toggle was removed in Wave 3 when flights became trips/plans. Users
want that convenience back, plus the ability to pre-share with people they have
only just invited.

## Goals

1. **Share with all friends** as a *persistent* setting that also covers friends
   you make in the future — at both the trip level and the individual plan level.
2. A selectable access level for the all-friends share (viewer / editor), with
   **per-person overrides** layered on top (all = viewer, Magnus = editor).
3. **Plan-scoped visibility**: sharing a single plan reveals only that plan to
   the recipient, not the rest of the trip — they still see the trip tile.
4. **Notify on close**: optionally email + in-app notify everyone who newly
   gained access during a share-dialog session.
5. **Share to invited users**: share with people who have a pending friend
   request *and* with people invited only by email who have no account yet. The
   share lights up automatically once they accept and sign in.

## Non-goals (v1)

- Per-friend *exclusion* from an all-friends share (blocking one friend). The
  override model only supports promoting/demoting an explicit individual, not
  removing them from the all-friends set.
- Merging `flight_alerts` and the new `notifications` table into one unified
  inbox table. The new table sits alongside the existing one; full merge is a
  possible later cleanup.

## Architecture decision: compute access at read time

The all-friends membership is **derived live in the visibility predicate**, not
materialized into rows. Only two new flag columns are stored. This gives
"persistent, includes future friends" for free: a newly-accepted friend matches
the flag immediately, unfriending instantly revokes, and per-person overrides
compose as plain rows on top. The cost — more complex read predicates — is
contained because the predicate is already centralized
(`canViewPlanPredicate` and its inlined twin in `ListVisiblePlanParts`,
`internal/store/plans.go`).

## Access model

Two grant types, both computed:

- **Trip grant (full access)** — sees all plans in the trip except ones
  explicitly hidden from them. Sources: trip owner; an explicit `trip_members`
  row; or (new) the trip's `share_all_friends_role` is set and the viewer is an
  accepted friend of the owner.
- **Plan grant (scoped access)** — sees only that one plan. Sources: a plan
  passenger; a plan `only_visible_to` membership; or (new) the plan's
  `share_all_friends` flag is on and the viewer is an accepted friend of the
  owner.
- **Tile visibility** — the viewer sees the trip tile if they have *any* grant,
  trip or plan. This is what lets a plan-scoped viewer (e.g. Claire) see the
  trip tile but only the one flight shared with her.

### Effective role (edit vs view)

`owner > explicit trip_members.role > all-friends default role > none`. An
explicit member row always wins, so "all = viewer, Magnus = editor" is just the
trip flag at `viewer` plus an explicit editor row for Magnus. Downgrades work
the same way (explicit `viewer` beats an all-friends `editor` default).
Plan-scoped viewers have no edit rights.

### Friend-gated reads (unifying rule)

Every friend-derived grant — the all-friends flags *and* explicit shares to
friends — is **active only while an accepted friendship exists** between owner
and recipient. Consequences, all desired:

- Pending shares stay dormant and light up automatically on acceptance.
- Unfriending instantly revokes shared access (a deliberate change; today it
  does not).
- The all-friends flag automatically covers anyone who later accepts.

Aerly already requires friendship to share, so gating reads on friendship is
consistent with the existing model.

### Behavior change: drop the passenger → trip-viewer trigger

Today a DB trigger auto-promotes any plan passenger to a trip *viewer*, which is
why adding one passenger currently leaks the whole trip. Under this design that
trigger is removed: plan passengers (and other plan grants) become plan-scoped,
and the trip tile shows via the "any grant" rule. "Everyone on the trip" default
visibility now means *everyone with a trip grant* (full members), not anyone who
merely holds a plan grant.

## Schema changes

- `trips.share_all_friends_role TEXT` — `NULL` = off; `'viewer'` / `'editor'` =
  on, granting that default role to all the owner's accepted friends.
- `plans.share_all_friends BOOLEAN NOT NULL DEFAULT false` — on = all the
  owner's accepted friends get a plan grant to this plan.
- New table `pending_shares` — pre-shares to email-only invitees, keyed by
  `(email_lower, kind, target_id)`:
  - `email_lower TEXT`, `kind TEXT` (`'trip'`|`'plan'`), `target_id BIGINT`,
    `role TEXT` (trip role for trip shares; ignored/`NULL` for plan grants),
    `inviter_id BIGINT`, `created_at TIMESTAMPTZ`.
- New table `notifications` — non-flight in-app inbox items:
  - `id BIGSERIAL PK`, `user_id BIGINT`, `kind TEXT` (e.g. `'share'`),
    `actor_id BIGINT`, `trip_id BIGINT NULL`, `plan_id BIGINT NULL`,
    `message TEXT`, `created_at TIMESTAMPTZ DEFAULT NOW()`, `read_at TIMESTAMPTZ`.
  - Indexes mirroring `flight_alerts`: `(user_id, created_at DESC)` and a
    partial unread index `(user_id) WHERE read_at IS NULL`.
- **Drop** the plan-passenger → trip-viewer trigger.
- **Legacy cleanup**: delete `trip_members` viewer rows that exist *only* because
  of a `plan_passenger` (so those people correctly become plan-scoped). The exact
  cleanup query is validated against prod row counts before running.

## Visibility predicate rewrite

The predicate (shared by `CanViewPlan` and `ListVisiblePlanParts`) becomes:

A viewer V sees plan P in trip T when **either**:

1. V has a **trip grant** on T — V is the owner, OR an explicit `trip_members`
   row exists for V, OR `T.share_all_friends_role IS NOT NULL` and V is an
   accepted friend of the owner — **and** P is not hidden from V (P has no
   `plan_visibility` row, or mode `hidden_from` without V, or mode
   `only_visible_to` including V); **or**
2. V has a **plan grant** on P — V is a passenger of P, OR V is in P's
   `only_visible_to` members, OR `P.share_all_friends` is true and V is an
   accepted friend of the owner.

Trip-tile visibility = "V can see at least one plan in T, or has a trip grant on
T." Superuser bypass and trip-owner always-visible are preserved.

## API surface

- `PUT /api/trips/{id}/share-all-friends` → `{role: "viewer"|"editor"|null}`.
- `PUT /api/plans/{id}/share-all-friends` → `{enabled: bool}`.
- Relax `requireFriendTarget` from "accepted" to "any friendship edge exists" so
  `addTripMember` / `addPlanPassenger` / `setPlanVisibility` accept a pending
  friend's `user_id`; the read-time friend-gate keeps it dormant until accepted.
- Member/passenger add accepts `{email}` for a not-yet-registered person →
  `inviteFriendByEmail` + a `pending_shares` row.
- `POST /api/trips/{id}/notify-shares` and `POST /api/plans/{id}/notify-shares`
  → `{user_ids:[], emails:[]}`; sends email + in-app notification to exactly that
  newly-granted set.
- Trip DTO gains `share_all_friends_role`; Plan DTO gains `share_all_friends`.
- Extend `consumePendingInvitesTx` (runs at first verified login) to convert
  matching `pending_shares` rows into real grant rows under the new `user_id`,
  then delete them.

## Notifications

Reuse the existing inbox machinery (`flight_alerts` pattern: unread badge,
`read_at`, `notifications.updated` SSE, avatar-menu list). The new
`notifications` table carries `'share'` items. `buildNotificationsDTO` gains a
`CountUnreadShareNotifications` fan-out alongside `FriendRequestsPending` and
`UnreadAlerts`; the avatar inbox merges flight alerts and share notifications by
time; mark-read and the SSE ping behave identically. Each share notification
reads "<Actor> shared *<Trip/Plan name>* with you" and links to the target.

Email uses the existing `internal/mailer` (`Send` + `HTMLShell`): "<Actor>
shared *<name>* with you" plus a link.

## Frontend

- **TripMembersDialog** (`web/src/components/TripMembersDialog.tsx`): add a
  "Share with all friends" control (Off / Viewer / Editor). The member table
  shows explicit per-person overrides. The friend picker lists pending friends
  (labelled "pending") and accepts an email to invite-and-share. On close, if
  anyone gained access this session (individuals added, plus everyone newly
  covered when the all-friends toggle flips on), show a "Notify the people you
  just added" checkbox (default on) that drives the notify call.
- **PlanPrivacyDialog** (`web/src/components/PlanPrivacyDialog.tsx`): add a
  "Share with all friends" toggle and the same notify-on-close behavior.
- Picker source (`web/src/state/friendUsers.ts`) extended to surface pending
  friends and support email entry.

## Testing

- **Store**: visibility-predicate matrix — full member vs plan-scoped, trip
  all-friends, plan all-friends, per-person overrides, `hidden_from` /
  `only_visible_to` interactions, unfriend revocation, pending-friend dormancy,
  tile visibility for plan-scoped viewers.
- **Handlers**: new share-all-friends endpoints; relaxed friend gating on add;
  share-by-email creating invite + `pending_shares`; `pending_shares` conversion
  at first login; notify-shares email + in-app fan-out.
- **Frontend (Playwright)**: both dialogs — all-friends toggle and role select,
  per-person override, email-invite share, pending-friend share, and
  notify-on-close.
