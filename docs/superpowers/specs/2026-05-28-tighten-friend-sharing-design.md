# Tighten flight sharing to friends

## Summary

Today every accepted friend can see every flight a user creates, and the per-user passenger and share lists accept any user in the system. The "Share with everyone" toggle widens visibility to every authenticated account on the instance. This design tightens the model so that:

- A flight is invisible to other users by default. The creator only chooses to expose it via the per-user passenger list, the per-user share list, or the new broadcast-to-friends toggle.
- The broadcast toggle ("Share with all friends") reaches only the creator's accepted friends, not every authenticated user.
- Passenger and per-user share additions are refused when the target is not an accepted friend of the flight creator.
- The FlightList filter is inverted from opt-out ("Only my flights") to opt-in ("Show friends flights"), so the default view is just the user's own flights and they have to ask to see others'.

Friendships, already modelled as canonical-ordered `(user_low, user_high)` rows with `status IN ('pending','accepted')`, are the gate for both broadcast and per-user channels. Only `accepted` rows count.

## Backend changes

### Visibility rule

`internal/store/flights.go` defines the visibility predicate in three places:
`ListVisibleFlights`, `CanView`, and `VisibleUserIDs`. All three currently expand to:

```
created_by = viewer
  OR EXISTS flight_passengers(flight_id, viewer)
  OR EXISTS flight_shares(flight_id, viewer)
  OR is_public = TRUE
  OR EXISTS friendships(viewer, creator) WHERE status='accepted'
```

The standalone friend-of-creator branch is removed, and `is_public` is paired with the friendship check:

```
created_by = viewer
  OR EXISTS flight_passengers(flight_id, viewer)
  OR EXISTS flight_shares(flight_id, viewer)
  OR (is_public = TRUE
      AND EXISTS friendships(viewer, creator) WHERE status='accepted')
```

The superuser show-all branch in `ListVisibleFlights` and `CanView` is unchanged.

`VisibleUserIDs` is used by the SSE fan-out (`flightViewers`) and must match the read predicate exactly, otherwise event deliveries would leak to bystanders who can no longer load the flight. The query gains a `friendships` join in the public-viewers branch.

### Friendship check on passenger / share additions

`internal/handlers/flights.go` exposes three entry points that mutate the passenger or share list: `createFlight` (initial passenger/share lists), `addPassenger`, and `addShare`. Each will, before delegating to the store, verify that the target user has an `accepted` friendship with the flight's **creator** (not the actor — a superuser editing on someone else's behalf must still respect the creator's friend graph).

Concrete change: add a store helper

```go
func (s *Store) AreAcceptedFriends(ctx context.Context, a, b int64) (bool, error)
```

that resolves `(min, max)` and queries `friendships WHERE status='accepted'`. Wire it into:

- `createFlight`: inside the existing `for _, uid := range in.PassengerIDs` and `for _, uid := range in.SharedUserIDs` loops, call the helper with `(creator, uid)` and reject with `400 Bad Request` and body `{"error":"target is not a friend of the flight creator"}` if false. Self (creator == uid) is allowed.
- `addPassenger` and `addShare`: same check using the flight's `created_by`, fetched via the existing `FlightByID` call path (a small refactor adds the lookup; `requireEdit` already runs first).

The creator may always add themselves as a passenger (existing flows do this when the creator participates in the flight); we explicitly skip the friendship check when `target == creator`.

### Existing data

Per the design decision, no migration. Pre-existing `flight_passengers` and `flight_shares` rows that no longer satisfy the friendship rule remain valid via the explicit-passenger / explicit-share branches of the new visibility predicate — those branches do not consult `friendships`. Only new additions are gated.

### `is_public` semantics

Existing flights with `is_public = TRUE` are not migrated. They simply narrow from "visible to every authenticated user" to "visible to creator's accepted friends" the moment this ships. That is the intended tightening.

## Frontend changes

### FlightList toggle

`web/src/components/FlightList.tsx`:

- Label: "Only my flights" → "Show friends flights".
- Tooltip: "Also show flights from friends that they shared with you."
- The toggle is OFF by default (was effectively OFF before too, but with inverted semantics).

`web/src/state/store.ts`:

- Rename `showMineOnly` → `showFriends` and `setShowMineOnly` → `setShowFriends`.
- Storage key rename with one-shot migration: on load, if the new key is absent and the legacy key (`showMineOnly`) is present, the migration writes `showFriends := !showMineOnly` and removes the legacy key. Default for a fresh user remains OFF (= only mine).

`web/src/state/visibleFlights.ts`:

- The filter inverts. When `showFriends` is OFF: keep flights where `me === created_by || passenger_ids.includes(me)`. When ON: pass through all loaded flights (the server has already filtered to what the viewer is allowed to see). The `me == null` guard (skip filter while auth is loading) is preserved.

### FlightDialog

`web/src/components/FlightDialog.tsx`:

- The `VisibilityBlock` switch label: "Share with everyone" → "Share with all friends".
- Helper text on the share Autocomplete updates the "is public" branch:
  - From: `Flight is public — this list is ignored until you turn off "Share with everyone".`
  - To: `Flight is shared with all your friends — this list is ignored until you turn off "Share with all friends".`
- The "only the creator (or a superuser) can change sharing" tooltip is unchanged.
- The passenger Autocomplete and the share-list Autocomplete restrict their `options` to accepted friends of the current viewer. Already-attached non-friend users still render as `value` chips, so legacy state isn't yanked out from under the user; only new additions are restricted.

### Friends-only selector

A new selector `useFriendUsers()` (colocated with the existing store, e.g. in `web/src/state/friendUsers.ts`) returns the subset of `users` for whom an `accepted` friendship with the viewer exists. The FriendsDialog already fetches friendships from the API; this design promotes that fetch to live in the store (`friendships: Friendship[]` plus a `loadFriendships()` action loaded alongside the existing initial bootstrap) so the selector is cheap and globally available. The FriendsDialog migrates to consume the store value too, so we don't have two sources of truth.

## Tests

### Backend (Go)

`internal/store/flights_test.go` (extend existing tests):

- `ListVisibleFlights` and `CanView`:
  - Creator's own flight: visible.
  - Non-friend, not a passenger, not in share list, `is_public = false`: not visible.
  - Non-friend, not a passenger, not in share list, `is_public = true`: not visible (the change).
  - Accepted friend, `is_public = true`: visible.
  - Accepted friend, `is_public = false`, not in passenger/share lists: not visible (the change — was visible before).
  - Pending friendship is not enough.
  - Pre-existing share-list entry for a non-friend: visible via share-list branch (regression guard for "leave existing rows alone").

- `VisibleUserIDs`: includes the creator's accepted friends iff `is_public`; excludes non-friends; excludes pending.

`internal/handlers/flights_test.go`:

- `addPassenger` rejects a non-friend with 400; accepts a friend.
- `addShare` rejects a non-friend with 400; accepts a friend.
- `createFlight` with a non-friend in `passenger_ids` returns 400 and creates no row (rollback or pre-validate before insert — pre-validate is simpler and chosen here).
- `createFlight` with a non-friend in `shared_user_ids` returns 400.
- A superuser editing someone else's flight is gated by the **creator's** friend graph, not the superuser's.

### Frontend

`web/src/state/store.test.ts`:

- New default: `showFriends === false`.
- Persistence migration: legacy `showMineOnly = true` → new `showFriends = false`; legacy `showMineOnly = false` → new `showFriends = true`; absent legacy → fresh default `false`.

`web/src/state/visibleFlights.test.ts`:

- Rename existing tests. With `showFriends = false`, only creator/passenger flights pass. With `showFriends = true`, the full loaded list passes through.

`web/src/components/FlightList.test.tsx`:

- Toggle is labelled "Show friends flights".
- Clicking inverts the boolean as expected.

`web/src/components/FlightDialog.test.tsx`:

- Switch label is "Share with all friends".
- Passenger Autocomplete `options` is restricted to friends.
- A flight that already has a non-friend passenger renders that user as a chip in `value` (legacy state preserved).
- Helper text on the share Autocomplete updates when `is_public` is true.

`web/src/components/FriendsDialog.test.tsx`:

- Continues to pass after FriendsDialog migrates to the store-backed friendships.

## Out of scope

- No data migration of existing `flight_passengers` / `flight_shares` rows.
- No change to the superuser show-all branch.
- No change to the friend request / acceptance flow itself.
- No change to email-driven passenger ingestion (it already runs as the creator and so naturally adds them — friendship enforcement on that path is not part of this design and will be tracked separately if it turns out to be reachable for non-friends).

## Branch

`worktree-tighten-friend-sharing` (created via `EnterWorktree`).
