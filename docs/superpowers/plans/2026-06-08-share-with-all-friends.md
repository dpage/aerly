# Share with all friends — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user share a whole trip or a single plan with *all* their friends (current and future) in one action, with a selectable access level and per-person overrides, plus pre-sharing to invited-but-not-yet-accepted people and an optional notification on dialog close.

**Architecture:** Access is computed at read time from two new flag columns (`trips.share_all_friends_role`, `plans.share_all_friends`) layered over the existing explicit grant rows. The visibility predicate becomes two-tier — a *trip grant* (sees all non-hidden plans) vs a *plan grant* (sees only that plan) — and is **friend-gated**: every non-owner viewer must have an accepted friendship with the trip's owner for any grant to be live. This single gate gives pending-share activation, unfriend revocation, and future-friend inclusion for free. The plan-passenger→trip-viewer DB trigger is dropped so passengers become plan-scoped.

**Tech Stack:** Go (pgx, `net/http`, custom store), PostgreSQL migrations (`migrations/NNNN_*.up.sql`/`.down.sql`), React + TypeScript + Zustand + MUI, Vitest + Testing Library, Playwright. Email via `internal/mailer` (`Send` + `HTMLShell`), live updates via `internal/sse`.

---

## Canonical access model (reference for all SQL tasks)

For a viewer `V` looking at plan `P` in trip `T` owned by `O = T.created_by`:

```
sees(V, P) :=
    V is superuser-opting-in            -- existing showAll bypass, unchanged
 OR V = O                               -- owner always
 OR (
      accepted_friend(O, V)             -- FRIEND GATE
      AND ( plan_grant(V, P) OR trip_default(V, P) )
    )

plan_grant(V, P) :=                     -- "sees only this plan"
      V = P.created_by
   OR EXISTS plan_passengers(P, V)
   OR P.share_all_friends = true
   OR EXISTS (plan_visibility pv mode 'only_visible_to' with member V)

trip_grant(V, T) :=                     -- "full member"
      EXISTS trip_members(T, V)
   OR T.share_all_friends_role IS NOT NULL

trip_default(V, P) :=                   -- full member sees a non-restricted plan
      trip_grant(V, T)
   AND (
         NOT EXISTS plan_visibility(P)                              -- default everyone
      OR (pv.mode = 'hidden_from' AND V NOT IN pv members)          -- not hidden from V
       )
```

Tile visibility (trip list):

```
sees_trip(V, T) :=
    V = O
 OR ( accepted_friend(O, V)
      AND ( trip_grant(V, T) OR EXISTS (plan P in T : plan_grant(V, P)) ) )
```

Note `only_visible_to` membership is folded into `plan_grant` (it both restricts full members and grants the named people). `hidden_from` only matters under `trip_default`. Passengers and plan creators are granted before `hidden_from` is consulted (preserves existing behaviour).

**Why this satisfies the requirements:**
- *Pending Aerly user* — we write the explicit grant row immediately, but `accepted_friend(O,V)` is false until they accept, so it stays dormant, then lights up on accept. No hook.
- *Unfriend* — `accepted_friend(O,V)` flips false, revoking every grant. No hook.
- *Future friends* — `share_all_friends_role`/`share_all_friends` match any accepted friend, including ones made later. No hook.
- *Email invitee* — no `user_id` yet, so a `pending_shares` row is stored and converted to a real grant row at first verified login (the one hook).

---

## File map

**Backend — create:**
- `migrations/0030_share_all_friends.up.sql` / `.down.sql` — flag columns, `pending_shares`, `notifications`, drop trigger, legacy cleanup.
- `migrations/migrations_0030_test.go` — migration up/down assertions.
- `internal/store/sharing.go` — `SetTripShareAllFriends`, `SetPlanShareAllFriends`, `pending_shares` CRUD, `consumePendingSharesTx`.
- `internal/store/notifications_store.go` — `Notification` struct + `InsertNotification`, `ListNotifications`, `MarkNotificationsRead`, `CountUnreadNotifications`.
- `internal/handlers/sharing.go` — share-all-friends + notify-shares handlers.

**Backend — modify:**
- `internal/store/plans.go` — rewrite `canViewPlanPredicate`, `ListVisiblePlanParts` inline predicate, `VisiblePlanUserIDs`; add `share_all_friends` to `Plan`/`planColumns`/`scanPlan`.
- `internal/store/trips.go` — rewrite `ListTrips`/tile predicate; add `ShareAllFriendsRole` to `Trip`/`tripColumns`/scan.
- `internal/store/friends.go` — call `consumePendingSharesTx` from `consumePendingInvitesTx`; add `AnyFriendshipEdge`.
- `internal/handlers/handlers.go` — register new routes.
- `internal/handlers/handlers_trips.go` — relax `requireFriendTarget` (pending edge ok, gate on owner), add email-share branch to member/passenger adds.
- `internal/handlers/handlers_plans.go` — drop "trigger" comment, add email-share branch.
- `internal/handlers/notifications.go` — add `CountUnreadNotifications` to `buildNotificationsDTO`.
- `internal/handlers/handlers_alert_inbox.go` — merge `notifications` into `listAlerts`.
- `internal/api/dto.go` — `TripDTO.ShareAllFriendsRole`, `PlanDTO.ShareAllFriends`, generic `NotificationItemDTO`, wire into `ToTripDTO`/plan DTO.

**Frontend — modify:**
- `web/src/api/types.ts` — add fields + input types.
- `web/src/api/client.ts` — `setTripShareAllFriends`, `setPlanShareAllFriends`, `notifyTripShares`, `notifyPlanShares`, share-by-email inputs.
- `web/src/state/friendUsers.ts` — add `useFriendCandidates` (accepted + pending).
- `web/src/state/tripsSlice.ts` / `plansSlice.ts` — new actions.
- `web/src/components/TripMembersDialog.tsx` — all-friends control + email invite + notify-on-close.
- `web/src/components/PlanPrivacyDialog.tsx` — all-friends toggle + notify-on-close.
- `web/src/components/AlertInbox` (the avatar inbox) — render share notifications.

**Tests** accompany every task (locations given inline).

---

## Phase 0 — Schema & migration

### Task 1: Migration 0030 — flags, tables, drop trigger, cleanup

**Files:**
- Create: `migrations/0030_share_all_friends.up.sql`
- Create: `migrations/0030_share_all_friends.down.sql`
- Test: `migrations/migrations_0030_test.go`

- [ ] **Step 1: Write the up migration**

Create `migrations/0030_share_all_friends.up.sql`:

```sql
-- Share-with-all-friends: persistent, friend-gated, computed-at-read-time.
--
-- Two flag columns are the only new sharing *state*; the all-friends grant is
-- derived live in the visibility predicate from these plus the friendships
-- table, so future friends are included automatically and unfriending revokes.
--
-- NULL share_all_friends_role = off; 'viewer'/'editor' = every accepted friend
-- of the trip owner gets that default trip role (explicit trip_members rows
-- override it per person).
ALTER TABLE trips ADD COLUMN share_all_friends_role TEXT
    CHECK (share_all_friends_role IN ('viewer', 'editor'));

-- Per-plan all-friends grant: every accepted friend of the trip owner gets a
-- plan-scoped grant (sees this plan only, plus the trip tile).
ALTER TABLE plans ADD COLUMN share_all_friends BOOLEAN NOT NULL DEFAULT false;

-- Pre-shares to people invited only by email (no account yet). Converted into
-- real grant rows at the invitee's first verified login (consumePendingShares).
-- kind 'trip' uses role; kind 'plan' ignores it (plan grants are role-less).
CREATE TABLE pending_shares (
    id          BIGSERIAL PRIMARY KEY,
    email_lower TEXT   NOT NULL,
    kind        TEXT   NOT NULL CHECK (kind IN ('trip', 'plan')),
    target_id   BIGINT NOT NULL,
    role        TEXT   CHECK (role IN ('viewer', 'editor')),
    inviter_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (email_lower, kind, target_id)
);
CREATE INDEX pending_shares_email_idx ON pending_shares (email_lower);

-- Generic in-app notification inbox (non-flight). Mirrors flight_alerts so the
-- avatar inbox + unread badge machinery extends naturally. kind 'share' today.
CREATE TABLE notifications (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT   NOT NULL,
    actor_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    trip_id    BIGINT,
    plan_id    BIGINT,
    message    TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at    TIMESTAMPTZ
);
CREATE INDEX notifications_user_created_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_user_unread_idx ON notifications (user_id) WHERE read_at IS NULL;

-- Passengers become plan-scoped: drop the trigger that auto-promoted a plan
-- passenger to a trip viewer (which leaked the whole trip). Trip-tile and plan
-- visibility are now computed from grants, so the membership row is unneeded.
DROP TRIGGER IF EXISTS plan_passengers_ensure_member ON plan_passengers;
DROP FUNCTION IF EXISTS plan_passenger_ensure_member();

-- Legacy cleanup: remove viewer trip_members rows whose only justification was
-- the dropped trigger — i.e. the user is a plan_passenger somewhere in the trip
-- and is a plain 'viewer' and is NOT a trip-level passenger (trip passengers
-- legitimately keep their viewer row). After this they are correctly
-- plan-scoped via their plan_passengers rows. Owners/editors untouched.
DELETE FROM trip_members tm
 WHERE tm.role = 'viewer'
   AND NOT EXISTS (SELECT 1 FROM trip_passengers tp
                   WHERE tp.trip_id = tm.trip_id AND tp.user_id = tm.user_id)
   AND EXISTS (SELECT 1 FROM plan_passengers pp
               JOIN plans pl ON pl.id = pp.plan_id
               WHERE pl.trip_id = tm.trip_id AND pp.user_id = tm.user_id);
```

- [ ] **Step 2: Write the down migration**

Create `migrations/0030_share_all_friends.down.sql`:

```sql
-- Recreate the passenger⇒viewer trigger (mirror of 0010_trip_core).
CREATE FUNCTION plan_passenger_ensure_member() RETURNS trigger AS $$
BEGIN
    INSERT INTO trip_members (trip_id, user_id, role)
    SELECT p.trip_id, NEW.user_id, 'viewer'
    FROM plans p
    WHERE p.id = NEW.plan_id
    ON CONFLICT (trip_id, user_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER plan_passengers_ensure_member
    AFTER INSERT ON plan_passengers
    FOR EACH ROW
    EXECUTE FUNCTION plan_passenger_ensure_member();

DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS pending_shares;
ALTER TABLE plans DROP COLUMN IF EXISTS share_all_friends;
ALTER TABLE trips DROP COLUMN IF EXISTS share_all_friends_role;
```

- [ ] **Step 3: Write the migration test**

Create `migrations/migrations_0030_test.go` (follow the `migrations_0010_test.go` harness — `testsupport.NewPool` applies all up migrations):

```go
package migrations_test

import (
	"context"
	"testing"

	"github.com/dpage/aerly/internal/testsupport"
)

func TestMigration0030(t *testing.T) {
	pool := testsupport.NewPool(t)
	if pool == nil {
		return
	}
	ctx := context.Background()

	for _, tbl := range []string{"pending_shares", "notifications"} {
		if !tableExists(t, pool, tbl) {
			t.Errorf("expected table %q after up", tbl)
		}
	}
	if !columnExists(t, pool, "trips", "share_all_friends_role") {
		t.Error("trips.share_all_friends_role missing")
	}
	if !columnExists(t, pool, "plans", "share_all_friends") {
		t.Error("plans.share_all_friends missing")
	}

	// The passenger⇒viewer trigger must be GONE: inserting a plan_passenger no
	// longer creates a trip_members row.
	uid := testsupport.InsertUser(t, pool, "m30owner", false, true)
	pax := testsupport.InsertUser(t, pool, "m30pax", false, true)
	var tripID, planID int64
	if err := pool.QueryRow(ctx,
		`INSERT INTO trips (name, created_by) VALUES ('T', $1) RETURNING id`, uid).Scan(&tripID); err != nil {
		t.Fatalf("insert trip: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO plans (trip_id, type) VALUES ($1, 'flight') RETURNING id`, tripID).Scan(&planID); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id) VALUES ($1, $2)`, planID, pax); err != nil {
		t.Fatalf("insert plan_passenger: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, pax).Scan(&n); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if n != 0 {
		t.Errorf("trigger should be dropped: got %d trip_members rows, want 0", n)
	}
}
```

- [ ] **Step 4: Run the migration test**

Run: `go test ./migrations/ -run TestMigration0030 -v`
Expected: PASS (skips with no DB unless `AERLY_REQUIRE_DB=1`).

- [ ] **Step 5: Commit**

```bash
git add migrations/0030_share_all_friends.up.sql migrations/0030_share_all_friends.down.sql migrations/migrations_0030_test.go
git commit -m "feat(db): share-all-friends schema; drop passenger→viewer trigger"
```

> **Note for executor:** the legacy `DELETE FROM trip_members` cleanup is validated against prod counts before the production migration runs (read-only `sudo -u aerly psql` per project memory). It is safe in tests (empty/seeded DBs).

---

## Phase 1 — Store: visibility model

### Task 2: Add `share_all_friends` to Plan and `share_all_friends_role` to Trip structs

**Files:**
- Modify: `internal/store/plans.go` (Plan struct ~14-38, `planColumns` ~276, `scanPlan` ~278-289)
- Modify: `internal/store/trips.go` (Trip struct ~11-33, `tripColumns`, scan)
- Test: `internal/store/sharing_test.go` (new)

- [ ] **Step 1: Write a failing test for the flag round-trip**

Create `internal/store/sharing_test.go`:

```go
package store

import "testing"

func TestShareAllFriendsFlagsRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	tripID := mkTrip(t, s, owner)
	planID := mkPlan(t, s, tripID, owner)

	if err := s.SetTripShareAllFriends(ctx, tripID, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}
	tr, err := s.TripByID(ctx, tripID)
	if err != nil || tr.ShareAllFriendsRole != "viewer" {
		t.Fatalf("trip role = %q, %v; want viewer", tr.ShareAllFriendsRole, err)
	}

	if err := s.SetPlanShareAllFriends(ctx, planID, true); err != nil {
		t.Fatalf("SetPlanShareAllFriends: %v", err)
	}
	pl, err := s.PlanByID(ctx, planID)
	if err != nil || !pl.ShareAllFriends {
		t.Fatalf("plan flag = %v, %v; want true", pl.ShareAllFriends, err)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails to compile**

Run: `go test ./internal/store/ -run TestShareAllFriendsFlagsRoundTrip`
Expected: FAIL — `tr.ShareAllFriendsRole undefined`, `SetTripShareAllFriends undefined`, etc.

- [ ] **Step 3: Add the struct fields and scan columns**

In `internal/store/plans.go`, add to the `Plan` struct (after `Website string`):

```go
	// ShareAllFriends, when true, grants every accepted friend of the trip
	// owner a plan-scoped view of this plan (computed at read time).
	ShareAllFriends bool
```

Update `planColumns` to append `, share_all_friends` and add `&p.ShareAllFriends` to `scanPlan`'s `row.Scan(...)` (last argument).

In `internal/store/trips.go`, add to `Trip` struct (after `CountryCode`):

```go
	// ShareAllFriendsRole is "" (off), "viewer", or "editor": the default trip
	// role granted to every accepted friend of the owner (read-time computed).
	ShareAllFriendsRole string
```

Update `tripColumns` to include `share_all_friends_role` and add `&t.ShareAllFriendsRole` to the trip scan. Because the column is nullable, scan into a `sql.NullString` or use `COALESCE(share_all_friends_role, '')` in the select — prefer the `COALESCE` form in `tripColumns`/`listTripsSelect` so the scan target stays a plain `string`. (Find every `SELECT ... FROM trips` scan that uses `tripColumns` and ensure they all get the new column; `TripByID` and `ListTrips` both use it.)

- [ ] **Step 4: Implement the setters (in new `internal/store/sharing.go`)**

Create `internal/store/sharing.go`:

```go
package store

import "context"

// SetTripShareAllFriends sets (or clears with "") the trip-level all-friends
// default role. "viewer"/"editor" enable the grant; "" disables it.
func (s *Store) SetTripShareAllFriends(ctx context.Context, tripID int64, role string) error {
	var arg any
	if role == "" {
		arg = nil
	} else {
		arg = role
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE trips SET share_all_friends_role = $2, updated_at = NOW() WHERE id = $1`,
		tripID, arg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPlanShareAllFriends toggles the per-plan all-friends grant.
func (s *Store) SetPlanShareAllFriends(ctx context.Context, planID int64, enabled bool) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE plans SET share_all_friends = $2, updated_at = NOW() WHERE id = $1`,
		planID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 5: Run the test to confirm it passes**

Run: `go test ./internal/store/ -run TestShareAllFriendsFlagsRoundTrip -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/plans.go internal/store/trips.go internal/store/sharing.go internal/store/sharing_test.go
git commit -m "feat(store): share-all-friends flag columns and setters"
```

---

### Task 3: Rewrite the plan-visibility predicate (two-tier + friend gate)

**Files:**
- Modify: `internal/store/plans.go` — `canViewPlanPredicate` (~1539), `ListVisiblePlanParts` inline `visible` (~1613), `VisiblePlanUserIDs` (~1693)
- Test: `internal/store/plans_visibility_test.go` (extend), `internal/store/sharing_test.go`

- [ ] **Step 1: Write failing tests for the new semantics**

Add to `internal/store/sharing_test.go`. These encode the four behaviours. Helpers `mkTrip`, `addMember`, `mkPlan`, `addPlanPassenger` exist in `plans_visibility_test.go`; add a `befriend` store-test helper if absent:

```go
func befriendStore(t *testing.T, s *Store, a, b int64) {
	t.Helper()
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil {
		t.Fatalf("request friendship: %v", err)
	}
	if _, err := s.AcceptFriendship(ctx, b, a); err != nil {
		t.Fatalf("accept friendship: %v", err)
	}
}

func canView(t *testing.T, s *Store, planID, viewer int64) bool {
	t.Helper()
	ok, err := s.CanViewPlan(ctx, planID, viewer, false)
	if err != nil {
		t.Fatalf("CanViewPlan: %v", err)
	}
	return ok
}

// Plan-scoped: a passenger on one plan sees ONLY that plan, not the trip's
// other default-visibility plans (the leak the trigger used to cause).
func TestPlanGrantIsScoped(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	claire := mkUser(t, s)
	befriendStore(t, s, owner, claire)
	tripID := mkTrip(t, s, owner)
	flight := mkPlan(t, s, tripID, owner)
	hotel := mkPlan(t, s, tripID, owner)

	// Share only the flight with Claire (as a passenger). She is NOT a member.
	if err := s.AddPlanPassenger(ctx, flight, claire); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if !canView(t, s, flight, claire) {
		t.Error("claire should see the shared flight")
	}
	if canView(t, s, hotel, claire) {
		t.Error("claire must NOT see the unshared hotel")
	}
}

// Full member sees all non-hidden plans.
func TestTripMemberSeesAllNonHidden(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	bob := mkUser(t, s)
	befriendStore(t, s, owner, bob)
	tripID := mkTrip(t, s, owner)
	addMember(t, s, tripID, bob, "viewer")
	p1 := mkPlan(t, s, tripID, owner)
	p2 := mkPlan(t, s, tripID, owner)
	if !canView(t, s, p1, bob) || !canView(t, s, p2, bob) {
		t.Error("full member should see all default-visibility plans")
	}
	// Hide p2 from bob.
	if err := s.SetPlanVisibility(ctx, p2, "hidden_from", []int64{bob}); err != nil {
		t.Fatalf("SetPlanVisibility: %v", err)
	}
	if canView(t, s, p2, bob) {
		t.Error("bob must not see a plan hidden from him")
	}
}

// Trip all-friends flag: any accepted friend is a full member, no explicit row.
func TestTripShareAllFriendsGrantsFullAccess(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	fran := mkUser(t, s)
	befriendStore(t, s, owner, fran)
	tripID := mkTrip(t, s, owner)
	p := mkPlan(t, s, tripID, owner)
	if canView(t, s, p, fran) {
		t.Fatal("precondition: fran not yet granted")
	}
	if err := s.SetTripShareAllFriends(ctx, tripID, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}
	if !canView(t, s, p, fran) {
		t.Error("accepted friend should see plan via trip all-friends flag")
	}
	// A non-friend stranger must not.
	stranger := mkUser(t, s)
	if canView(t, s, p, stranger) {
		t.Error("non-friend must not see all-friends trip")
	}
}

// Plan all-friends flag: scoped grant to every friend.
func TestPlanShareAllFriendsScoped(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	gus := mkUser(t, s)
	befriendStore(t, s, owner, gus)
	tripID := mkTrip(t, s, owner)
	shared := mkPlan(t, s, tripID, owner)
	other := mkPlan(t, s, tripID, owner)
	if err := s.SetPlanShareAllFriends(ctx, shared, true); err != nil {
		t.Fatalf("SetPlanShareAllFriends: %v", err)
	}
	if !canView(t, s, shared, gus) {
		t.Error("friend should see plan-all-friends plan")
	}
	if canView(t, s, other, gus) {
		t.Error("friend must not see the other plan (plan grant is scoped)")
	}
}

// Friend gate: unfriending revokes; pending stays dormant.
func TestFriendGateActivationAndRevocation(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	pat := mkUser(t, s)
	tripID := mkTrip(t, s, owner)
	addMember(t, s, tripID, pat, "viewer") // explicit grant row written now
	p := mkPlan(t, s, tripID, owner)

	// Pending only (request, no accept): dormant.
	if _, err := s.RequestFriendship(ctx, owner, pat, ""); err != nil {
		t.Fatalf("request: %v", err)
	}
	if canView(t, s, p, pat) {
		t.Error("explicit share to a pending friend must be dormant")
	}
	// Accept: lights up.
	if _, err := s.AcceptFriendship(ctx, pat, owner); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !canView(t, s, p, pat) {
		t.Error("share should activate once friendship accepted")
	}
	// Unfriend: revoked.
	if err := s.RemoveFriendship(ctx, owner, pat); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if canView(t, s, p, pat) {
		t.Error("unfriending must revoke shared access")
	}
}
```

- [ ] **Step 2: Run to confirm failures**

Run: `go test ./internal/store/ -run 'TestPlanGrant|TestTripMemberSees|TestTripShareAllFriends|TestPlanShareAllFriends|TestFriendGate' -v`
Expected: FAIL (old predicate ignores the flags and the friend gate; `TestPlanGrantIsScoped` fails because today a passenger sees everything; `TestFriendGate...` fails because explicit rows aren't gated).

- [ ] **Step 3: Replace `canViewPlanPredicate`**

In `internal/store/plans.go`, replace the `canViewPlanPredicate` const with the two-tier, friend-gated form (`$1 = planID`, `$2 = viewerID`):

```go
const canViewPlanPredicate = `
	EXISTS (
		SELECT 1 FROM plans p
		JOIN trips t ON t.id = p.trip_id
		WHERE p.id = $1
		  AND (
		       t.created_by = $2
		    OR (
		         -- FRIEND GATE: viewer must be an accepted friend of the owner.
		         EXISTS (SELECT 1 FROM friendships f
		                 WHERE f.status = 'accepted'
		                   AND (f.user_low = LEAST(t.created_by, $2)
		                        AND f.user_high = GREATEST(t.created_by, $2)))
		         AND (
		              -- PLAN GRANT (scoped: this plan only)
		              p.created_by = $2
		           OR EXISTS (SELECT 1 FROM plan_passengers pp
		                      WHERE pp.plan_id = p.id AND pp.user_id = $2)
		           OR p.share_all_friends
		           OR EXISTS (SELECT 1 FROM plan_visibility pv
		                      JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                      WHERE pv.plan_id = p.id
		                        AND pv.mode = 'only_visible_to'
		                        AND m.user_id = $2)
		              -- TRIP GRANT (full: all non-hidden plans)
		           OR (
		                ( EXISTS (SELECT 1 FROM trip_members tm
		                          WHERE tm.trip_id = p.trip_id AND tm.user_id = $2)
		                  OR t.share_all_friends_role IS NOT NULL )
		                AND (
		                     NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = p.id)
		                  OR EXISTS (SELECT 1 FROM plan_visibility pv
		                             WHERE pv.plan_id = p.id AND pv.mode = 'hidden_from'
		                               AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                               WHERE m.plan_id = p.id AND m.user_id = $2))
		                    )
		              )
		         )
		       )
		  )
	)`
```

- [ ] **Step 4: Replace the inline predicate in `ListVisiblePlanParts`**

Replace the `visible` string (keys on `$1 = viewerID`, correlates `pl`/`t`) with the same logic adapted (`p.` → `pl.`, `$2` → `$1`, `t.created_by` stays):

```go
	visible := `(
		t.created_by = $1
	 OR (
		  EXISTS (SELECT 1 FROM friendships f
		          WHERE f.status = 'accepted'
		            AND f.user_low = LEAST(t.created_by, $1)
		            AND f.user_high = GREATEST(t.created_by, $1))
		  AND (
		       pl.created_by = $1
		    OR EXISTS (SELECT 1 FROM plan_passengers pp
		               WHERE pp.plan_id = pl.id AND pp.user_id = $1)
		    OR pl.share_all_friends
		    OR EXISTS (SELECT 1 FROM plan_visibility pv
		               JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		               WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
		                 AND m.user_id = $1)
		    OR (
		         ( EXISTS (SELECT 1 FROM trip_members tm
		                   WHERE tm.trip_id = pl.trip_id AND tm.user_id = $1)
		           OR t.share_all_friends_role IS NOT NULL )
		         AND (
		              NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = pl.id)
		           OR EXISTS (SELECT 1 FROM plan_visibility pv
		                      WHERE pv.plan_id = pl.id AND pv.mode = 'hidden_from'
		                        AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                        WHERE m.plan_id = pl.id AND m.user_id = $1))
		             )
		       )
		  )
		)
	)`
```

(The `SELECT` clause in `ListVisiblePlanParts` is unchanged. The `JOIN plans pl` already aliases the plan, so `pl.share_all_friends` resolves once Task 2 added the column to `plans`.)

- [ ] **Step 5: Update `VisiblePlanUserIDs` to match**

Replace its `WHERE` body with the friend-gated two-tier form (`p` is the plan via `JOIN plans p ON p.id = $1`, `u.id` is the candidate viewer):

```go
	rows, err := s.pool.Query(ctx, `
		SELECT u.id FROM users u
		JOIN plans p ON p.id = $1
		JOIN trips t ON t.id = p.trip_id
		WHERE
		     u.id = t.created_by
		  OR (
		       EXISTS (SELECT 1 FROM friendships f
		               WHERE f.status = 'accepted'
		                 AND f.user_low = LEAST(t.created_by, u.id)
		                 AND f.user_high = GREATEST(t.created_by, u.id))
		       AND (
		            u.id = p.created_by
		         OR EXISTS (SELECT 1 FROM plan_passengers pp WHERE pp.plan_id = p.id AND pp.user_id = u.id)
		         OR p.share_all_friends
		         OR EXISTS (SELECT 1 FROM plan_visibility pv
		                    JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                    WHERE pv.plan_id = p.id AND pv.mode = 'only_visible_to' AND m.user_id = u.id)
		         OR (
		              ( EXISTS (SELECT 1 FROM trip_members tm WHERE tm.trip_id = p.trip_id AND tm.user_id = u.id)
		                OR t.share_all_friends_role IS NOT NULL )
		              AND (
		                   NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = p.id)
		                OR EXISTS (SELECT 1 FROM plan_visibility pv
		                           WHERE pv.plan_id = p.id AND pv.mode = 'hidden_from'
		                             AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                             WHERE m.plan_id = p.id AND m.user_id = u.id))
		                  )
		            )
		       )
		     )`, planID)
```

- [ ] **Step 6: Run the new + existing visibility tests**

Run: `go test ./internal/store/ -run 'Visib|TestPlanGrant|TestTripMemberSees|TestTripShareAllFriends|TestPlanShareAllFriends|TestFriendGate' -v`
Expected: PASS. Investigate any existing `plans_visibility_test.go` case that assumed a passenger could see default-visibility sibling plans or that a member with no friendship could see — those encode the *old* leak/ungated behaviour and must be updated to add a `befriendStore(...)` and to expect plan-scoping. Update them to the new model (do not weaken the new assertions).

- [ ] **Step 7: Commit**

```bash
git add internal/store/plans.go internal/store/sharing_test.go internal/store/plans_visibility_test.go
git commit -m "feat(store): two-tier friend-gated plan visibility predicate"
```

---

### Task 4: Trip-tile visibility (`ListTrips`)

**Files:**
- Modify: `internal/store/trips.go` — `ListTrips` (~109-121)
- Test: `internal/store/sharing_test.go`

- [ ] **Step 1: Write failing tests for tile visibility**

Add to `internal/store/sharing_test.go`:

```go
func tripVisible(t *testing.T, s *Store, viewer, tripID int64) bool {
	t.Helper()
	trips, err := s.ListTrips(ctx, viewer)
	if err != nil {
		t.Fatalf("ListTrips: %v", err)
	}
	for _, tr := range trips {
		if tr.ID == tripID {
			return true
		}
	}
	return false
}

func TestTileVisibleForPlanScopedViewer(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	claire := mkUser(t, s)
	befriendStore(t, s, owner, claire)
	tripID := mkTrip(t, s, owner)
	flight := mkPlan(t, s, tripID, owner)
	if err := s.AddPlanPassenger(ctx, flight, claire); err != nil {
		t.Fatalf("AddPlanPassenger: %v", err)
	}
	if !tripVisible(t, s, claire, tripID) {
		t.Error("plan-scoped viewer should see the trip tile")
	}
	// A friend with no grant anywhere must not see the tile.
	stranger := mkUser(t, s)
	befriendStore(t, s, owner, stranger)
	if tripVisible(t, s, stranger, tripID) {
		t.Error("friend with no grant must not see the tile")
	}
}

func TestTileVisibleForTripAllFriends(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	fran := mkUser(t, s)
	befriendStore(t, s, owner, fran)
	tripID := mkTrip(t, s, owner)
	_ = mkPlan(t, s, tripID, owner)
	if err := s.SetTripShareAllFriends(ctx, tripID, "viewer"); err != nil {
		t.Fatalf("SetTripShareAllFriends: %v", err)
	}
	if !tripVisible(t, s, fran, tripID) {
		t.Error("all-friends trip should be visible to a friend")
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/store/ -run TestTileVisible -v`
Expected: FAIL — current `ListTrips` only checks `created_by` / `trip_members`.

- [ ] **Step 3: Rewrite `ListTrips`**

Replace the `WHERE` clause in `ListTrips`:

```go
func (s *Store) ListTrips(ctx context.Context, viewerID int64) ([]*Trip, error) {
	rows, err := s.pool.Query(ctx, listTripsSelect+`
		WHERE t.created_by = $1
		   OR (
		        EXISTS (SELECT 1 FROM friendships f
		                WHERE f.status = 'accepted'
		                  AND f.user_low = LEAST(t.created_by, $1)
		                  AND f.user_high = GREATEST(t.created_by, $1))
		        AND (
		             EXISTS (SELECT 1 FROM trip_members tm
		                     WHERE tm.trip_id = t.id AND tm.user_id = $1)
		          OR t.share_all_friends_role IS NOT NULL
		          OR EXISTS (
		               SELECT 1 FROM plans pl
		               WHERE pl.trip_id = t.id
		                 AND (
		                      pl.created_by = $1
		                   OR pl.share_all_friends
		                   OR EXISTS (SELECT 1 FROM plan_passengers pp
		                              WHERE pp.plan_id = pl.id AND pp.user_id = $1)
		                   OR EXISTS (SELECT 1 FROM plan_visibility pv
		                              JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                              WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
		                                AND m.user_id = $1)
		                     )
		             )
		           )
		      )
		ORDER BY t.updated_at DESC, t.id DESC`, viewerID)
	if err != nil {
		return nil, err
	}
	return s.scanTripList(rows)
}
```

- [ ] **Step 4: Run the tile tests + the full store suite**

Run: `go test ./internal/store/ -run TestTileVisible -v` then `go test ./internal/store/`
Expected: PASS. Fix any existing `ListTrips` test that assumed an ungated member sees the tile (add `befriendStore`).

- [ ] **Step 5: Commit**

```bash
git add internal/store/trips.go internal/store/sharing_test.go
git commit -m "feat(store): trip-tile visibility from any grant + all-friends"
```

---

## Phase 2 — Store: pending shares, conversion, notifications

### Task 5: `pending_shares` CRUD + conversion at first login

**Files:**
- Modify: `internal/store/sharing.go` (add funcs)
- Modify: `internal/store/friends.go` — call `consumePendingSharesTx` inside `consumePendingInvitesTx` (after the friendship-accept loop, before `return inviters`)
- Test: `internal/store/sharing_test.go`, `internal/store/friends_test.go`

- [ ] **Step 1: Write a failing test for conversion at login**

Add to `internal/store/sharing_test.go` (mirrors `TestLinkLoginConsumesPendingInvites` in `friends_test.go` — use `testsupport.InsertUser` + the email/login helpers used there; consult `friends_test.go:172` for the exact link-login call):

```go
func TestPendingTripShareConvertsOnLogin(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	owner := mkUser(t, s)
	tripID := mkTrip(t, s, owner)

	// Owner invited joiner@example.com to the trip before they had an account.
	if err := s.InsertPendingShare(ctx, PendingShare{
		EmailLower: "joiner@example.com", Kind: "trip", TargetID: tripID,
		Role: "viewer", InviterID: owner,
	}); err != nil {
		t.Fatalf("InsertPendingShare: %v", err)
	}
	// Also queue the friend invite so consume accepts the friendship.
	if _, err := s.UpsertPendingFriendInvite(ctx, owner, "joiner@example.com", ""); err != nil {
		t.Fatalf("UpsertPendingFriendInvite: %v", err)
	}

	// Joiner signs in for the first time with that verified email. Use the same
	// link-login entrypoint TestLinkLoginConsumesPendingInvites uses.
	joiner := linkLoginWithEmail(t, s, "joinerlogin", "joiner@example.com")

	// The pending share became a real trip_members row, and the friendship is
	// accepted, so the trip is now visible to the joiner.
	if !tripVisible(t, s, joiner, tripID) {
		t.Error("pending trip share should convert to a live grant on first login")
	}
	var n int
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM pending_shares WHERE email_lower='joiner@example.com'`).Scan(&n); err != nil {
		t.Fatalf("count pending_shares: %v", err)
	}
	if n != 0 {
		t.Errorf("pending_shares not consumed: %d rows left", n)
	}
}
```

> Implement `linkLoginWithEmail` as a small helper in the test file copying the link-login setup from `friends_test.go:172` (`TestLinkLoginConsumesPendingInvites`) — it calls the same store entrypoint that triggers `consumePendingInvitesTx`. If that test uses `s.LinkLogin(...)` / `s.UpsertUserFromOAuth(...)`, reuse that exact call.

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/store/ -run TestPendingTripShareConverts -v`
Expected: FAIL — `PendingShare`/`InsertPendingShare` undefined.

- [ ] **Step 3: Implement `pending_shares` store API**

Add to `internal/store/sharing.go`:

```go
import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PendingShare is a pre-share to an email address with no account yet.
type PendingShare struct {
	EmailLower string
	Kind       string // "trip" | "plan"
	TargetID   int64
	Role       string // "viewer"|"editor" for trip; "" for plan
	InviterID  int64
}

// InsertPendingShare records a pre-share, idempotent on (email, kind, target).
func (s *Store) InsertPendingShare(ctx context.Context, ps PendingShare) error {
	var role any
	if ps.Role == "" {
		role = nil
	} else {
		role = ps.Role
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO pending_shares (email_lower, kind, target_id, role, inviter_id)
		VALUES (lower($1), $2, $3, $4, $5)
		ON CONFLICT (email_lower, kind, target_id) DO UPDATE SET role = EXCLUDED.role`,
		ps.EmailLower, ps.Kind, ps.TargetID, role, ps.InviterID)
	return err
}

// consumePendingSharesTx converts pending_shares addressed to any of userID's
// verified emails into real grant rows (trip_members for kind 'trip', a
// plan_passenger for kind 'plan'), then deletes them. Runs inside the same tx
// as consumePendingInvitesTx so a new user's pre-shares and friendships land
// atomically. The friend gate then makes them visible (the invite was just
// accepted in the same tx).
func consumePendingSharesTx(ctx context.Context, tx pgx.Tx, userID int64) error {
	rows, err := tx.Query(ctx, `
		DELETE FROM pending_shares
		 WHERE email_lower IN (
		   SELECT lower(address) FROM user_emails
		   WHERE user_id = $1 AND verified = TRUE)
		RETURNING kind, target_id, role`, userID)
	if err != nil {
		return err
	}
	type ps struct {
		kind   string
		target int64
		role   *string
	}
	var claimed []ps
	for rows.Next() {
		var p ps
		if err := rows.Scan(&p.kind, &p.target, &p.role); err != nil {
			rows.Close()
			return err
		}
		claimed = append(claimed, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range claimed {
		switch p.kind {
		case "trip":
			role := "viewer"
			if p.role != nil && *p.role != "" {
				role = *p.role
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO trip_members (trip_id, user_id, role) VALUES ($1, $2, $3)
				ON CONFLICT (trip_id, user_id) DO NOTHING`, p.target, userID, role); err != nil {
				return err
			}
		case "plan":
			if _, err := tx.Exec(ctx, `
				INSERT INTO plan_passengers (plan_id, user_id, via_trip) VALUES ($1, $2, false)
				ON CONFLICT (plan_id, user_id) DO NOTHING`, p.target, userID); err != nil {
				return err
			}
		}
	}
	return nil
}
```

- [ ] **Step 4: Call it from `consumePendingInvitesTx`**

In `internal/store/friends.go`, just before `return inviters, nil` at the end of `consumePendingInvitesTx`, add:

```go
	if err := consumePendingSharesTx(ctx, tx, userID); err != nil {
		return nil, err
	}
```

- [ ] **Step 5: Run the conversion test**

Run: `go test ./internal/store/ -run TestPendingTripShareConverts -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/sharing.go internal/store/friends.go internal/store/sharing_test.go
git commit -m "feat(store): pending_shares with conversion at first login"
```

---

### Task 6: `notifications` store API

**Files:**
- Create: `internal/store/notifications_store.go`
- Test: `internal/store/notifications_store_test.go`

- [ ] **Step 1: Write a failing test**

Create `internal/store/notifications_store_test.go`:

```go
package store

import "testing"

func TestNotificationsInboxRoundTrip(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	actor := mkUser(t, s)
	recipient := mkUser(t, s)
	tripID := mkTrip(t, s, actor)

	got, err := s.InsertNotification(ctx, Notification{
		UserID: recipient, Kind: "share", ActorID: &actor,
		TripID: &tripID, Message: "Alice shared Paris 2026 with you",
	})
	if err != nil || got.ID == 0 {
		t.Fatalf("InsertNotification: %+v, %v", got, err)
	}
	if n, _ := s.CountUnreadNotifications(ctx, recipient); n != 1 {
		t.Errorf("unread = %d, want 1", n)
	}
	list, err := s.ListNotifications(ctx, recipient, 50)
	if err != nil || len(list) != 1 || list[0].Message == "" {
		t.Fatalf("ListNotifications = %+v, %v", list, err)
	}
	if err := s.MarkNotificationsRead(ctx, recipient); err != nil {
		t.Fatalf("MarkNotificationsRead: %v", err)
	}
	if n, _ := s.CountUnreadNotifications(ctx, recipient); n != 0 {
		t.Errorf("unread after read = %d, want 0", n)
	}
}
```

- [ ] **Step 2: Run to confirm failure**

Run: `go test ./internal/store/ -run TestNotificationsInbox -v`
Expected: FAIL — undefined `Notification` / methods.

- [ ] **Step 3: Implement (mirror `internal/store/alerts.go`)**

Create `internal/store/notifications_store.go`:

```go
package store

import (
	"context"
	"time"
)

// Notification is one generic in-app inbox item (kind "share" today).
type Notification struct {
	ID        int64
	UserID    int64
	Kind      string
	ActorID   *int64
	TripID    *int64
	PlanID    *int64
	Message   string
	CreatedAt time.Time
	ReadAt    *time.Time
}

func (s *Store) InsertNotification(ctx context.Context, n Notification) (Notification, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, kind, actor_id, trip_id, plan_id, message)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at`,
		n.UserID, n.Kind, n.ActorID, n.TripID, n.PlanID, n.Message,
	).Scan(&n.ID, &n.CreatedAt)
	return n, err
}

func (s *Store) ListNotifications(ctx context.Context, userID int64, limit int) ([]Notification, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, kind, actor_id, trip_id, plan_id, message, created_at, read_at
		FROM notifications WHERE user_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.ActorID, &n.TripID,
			&n.PlanID, &n.Message, &n.CreatedAt, &n.ReadAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) MarkNotificationsRead(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}

func (s *Store) CountUnreadNotifications(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&n)
	return n, err
}
```

- [ ] **Step 4: Run the test**

Run: `go test ./internal/store/ -run TestNotificationsInbox -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/notifications_store.go internal/store/notifications_store_test.go
git commit -m "feat(store): generic in-app notifications inbox"
```

---

## Phase 3 — API / handlers

### Task 7: Relax `requireFriendTarget`; gate on owner-friendship; add `AnyFriendshipEdge`

**Files:**
- Modify: `internal/store/friends.go` — add `AnyFriendshipEdge`
- Modify: `internal/handlers/handlers_trips.go` — `requireFriendTarget` (~549-567)
- Test: `internal/store/friends_test.go`, `internal/handlers/handlers_trips_test.go` (or wherever member-add tests live)

- [ ] **Step 1: Write a failing test (store)**

Add to `internal/store/friends_test.go`:

```go
func TestAnyFriendshipEdge(t *testing.T) {
	s := newStore(t)
	if s == nil {
		return
	}
	a, b, c := mkUser(t, s), mkUser(t, s), mkUser(t, s)
	if _, err := s.RequestFriendship(ctx, a, b, ""); err != nil { // pending a→b
		t.Fatalf("request: %v", err)
	}
	if ok, _ := s.AnyFriendshipEdge(ctx, a, b); !ok {
		t.Error("pending edge should count as an edge")
	}
	if ok, _ := s.AnyFriendshipEdge(ctx, a, c); ok {
		t.Error("no edge between a and c")
	}
}
```

- [ ] **Step 2: Run to confirm failure** — `go test ./internal/store/ -run TestAnyFriendshipEdge` → FAIL (undefined).

- [ ] **Step 3: Implement `AnyFriendshipEdge`** in `internal/store/friends.go` (next to `AreAcceptedFriends`):

```go
// AnyFriendshipEdge reports whether a and b have any friendship row (pending or
// accepted). Used to allow pre-sharing to a pending friend; the read-time gate
// keeps the share dormant until the edge is accepted.
func (s *Store) AnyFriendshipEdge(ctx context.Context, a, b int64) (bool, error) {
	if a == b {
		return false, nil
	}
	low, high := pairOrder(a, b)
	var ok bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM friendships WHERE user_low=$1 AND user_high=$2)`,
		low, high).Scan(&ok)
	return ok, err
}
```

- [ ] **Step 4: Relax `requireFriendTarget`**

In `internal/handlers/handlers_trips.go`, change the body so it accepts a *pending* edge too (the read-gate enforces acceptance), keeping the superuser/self bypass:

```go
func (a *API) requireFriendTarget(ctx context.Context, actor *store.User, target int64, w http.ResponseWriter) error {
	if actor == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return errors.New("unauthorized")
	}
	if actor.IsSuperuser || actor.ID == target {
		return nil
	}
	// Accept any friendship edge (pending or accepted). A pending share stays
	// dormant until the target accepts — see the read-time friend gate.
	ok, err := a.Store.AnyFriendshipEdge(ctx, actor.ID, target)
	if err != nil {
		serverError(w, err)
		return err
	}
	if !ok {
		writeError(w, http.StatusForbidden, "user must be a friend (or invited)")
		return errors.New("not a friend")
	}
	return nil
}
```

- [ ] **Step 5: Run store + handler tests**

Run: `go test ./internal/store/ -run TestAnyFriendshipEdge -v && go test ./internal/handlers/`
Expected: PASS. If an existing handler test asserted a 403 for adding a *pending* friend, update it to expect success (the new contract).

- [ ] **Step 6: Commit**

```bash
git add internal/store/friends.go internal/store/friends_test.go internal/handlers/handlers_trips.go
git commit -m "feat: allow pre-sharing to invited (pending) friends"
```

---

### Task 8: Share-all-friends endpoints + DTO fields

**Files:**
- Create: `internal/handlers/sharing.go`
- Modify: `internal/handlers/handlers.go` (routes), `internal/api/dto.go` (DTO fields + mappers)
- Modify: `internal/handlers/handlers_trips.go` (tripDTO wiring), `internal/handlers/handlers_plans.go` (planDTO wiring)
- Test: `internal/handlers/sharing_test.go`

- [ ] **Step 1: Write a failing handler test**

Create `internal/handlers/sharing_test.go`:

```go
package handlers

import (
	"net/http"
	"testing"
)

func TestSetTripShareAllFriends(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner", false)
	tr := mkTripHandler(t, e, owner) // helper: POST /api/trips

	w := e.req(t, "PUT", "/api/trips/"+itoa(tr)+"/share-all-friends",
		map[string]any{"role": "viewer"}, owner)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	// DTO reflects it.
	got := e.req(t, "GET", "/api/trips/"+itoa(tr), nil, owner)
	if !containsJSON(got.Body.String(), `"share_all_friends_role":"viewer"`) {
		t.Errorf("trip DTO missing flag: %s", got.Body.String())
	}
}
```

> `mkTripHandler`, `itoa`, `containsJSON` — add small local helpers if not present (`itoa` = `strconv.FormatInt`; `containsJSON` = `strings.Contains`). Check `handlers_trips_test.go` for an existing trip-create helper and reuse it.

- [ ] **Step 2: Run to confirm failure** — `go test ./internal/handlers/ -run TestSetTripShareAllFriends` → FAIL (404, route missing).

- [ ] **Step 3: Add DTO fields**

In `internal/api/dto.go`: add `ShareAllFriendsRole string \`json:"share_all_friends_role,omitempty"\`` to `TripDTO`, and set it in `ToTripDTO` from `t.ShareAllFriendsRole`. Add `ShareAllFriends bool \`json:"share_all_friends"\`` to `PlanDTO` and set it wherever the plan DTO is assembled (find the plan→DTO mapper; it scans from `store.Plan`, so set `dto.ShareAllFriends = p.ShareAllFriends`).

- [ ] **Step 4: Implement the handlers**

Create `internal/handlers/sharing.go`:

```go
package handlers

import (
	"net/http"

	"github.com/dpage/aerly/internal/auth"
)

type shareAllFriendsTripReq struct {
	Role string `json:"role"` // "viewer"|"editor"|"" (clear)
}

func (a *API) setTripShareAllFriends(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripOwner(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareAllFriendsTripReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.Role != "" && in.Role != "viewer" && in.Role != "editor" {
		writeError(w, http.StatusBadRequest, "role must be viewer, editor, or empty")
		return
	}
	if err := a.Store.SetTripShareAllFriends(r.Context(), id, in.Role); err != nil {
		handleStoreErr(w, err)
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.tripDTO(r, t, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishTripUpdated(r.Context(), id)
	writeJSON(w, http.StatusOK, dto)
}

type shareAllFriendsPlanReq struct {
	Enabled bool `json:"enabled"`
}

func (a *API) setPlanShareAllFriends(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in shareAllFriendsPlanReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.Store.SetPlanShareAllFriends(r.Context(), id, in.Enabled); err != nil {
		handleStoreErr(w, err)
		return
	}
	dto, err := a.planDTO(r.Context(), id, me.ID)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.publishPlanUpdated(r.Context(), dto.TripID, id)
	writeJSON(w, http.StatusOK, dto)
}
```

- [ ] **Step 5: Register routes**

In `internal/handlers/handlers.go`, in `Register`, add:

```go
	mux.Handle("PUT /api/trips/{id}/share-all-friends", req(http.HandlerFunc(a.setTripShareAllFriends)))
	mux.Handle("PUT /api/plans/{id}/share-all-friends", req(http.HandlerFunc(a.setPlanShareAllFriends)))
```

- [ ] **Step 6: Run the test + the suite**

Run: `go test ./internal/handlers/ -run TestSetTripShareAllFriends -v && go test ./internal/handlers/`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/sharing.go internal/handlers/handlers.go internal/api/dto.go internal/handlers/handlers_trips.go internal/handlers/handlers_plans.go internal/handlers/sharing_test.go
git commit -m "feat(api): share-all-friends endpoints + DTO fields"
```

---

### Task 9: Notify-shares endpoints (email + in-app)

**Files:**
- Modify: `internal/handlers/sharing.go` (add handlers + email builder)
- Create: `internal/handlers/share_emails.go` (email composer; mirror `friend_emails.go`)
- Modify: `internal/handlers/handlers.go` (routes), `internal/handlers/notifications.go` (`buildNotificationsDTO`)
- Test: `internal/handlers/sharing_test.go`

- [ ] **Step 1: Write a failing test**

Add to `internal/handlers/sharing_test.go`:

```go
func TestNotifyTripShares(t *testing.T) {
	e := setup(t, nil, nil)
	owner := e.user(t, "owner2", false)
	bob := e.user(t, "bob2", false)
	e.befriend(t, owner, bob)
	tr := mkTripHandler(t, e, owner)
	// bob is a member
	e.req(t, "POST", "/api/trips/"+itoa(tr)+"/members",
		map[string]any{"user_id": bob, "role": "viewer"}, owner)

	w := e.req(t, "POST", "/api/trips/"+itoa(tr)+"/notify-shares",
		map[string]any{"user_ids": []int64{bob}, "emails": []string{}}, owner)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	// bob has an unread in-app notification.
	if n, _ := e.store.CountUnreadNotifications(ctx(), bob); n != 1 {
		t.Errorf("bob unread notifications = %d, want 1", n)
	}
}
```

> `ctx()` — use `context.Background()`; add a tiny local helper or inline it.

- [ ] **Step 2: Run to confirm failure** — FAIL (route missing).

- [ ] **Step 3: Implement the email composer**

Create `internal/handlers/share_emails.go` (mirror `buildFriendRequestEmail` in `friend_emails.go`, using `mailer.AssembleRFC822` + `mailer.HTMLShell` + `mailer.HTMLEscape`):

```go
package handlers

import (
	"fmt"
	"strings"

	"github.com/dpage/aerly/internal/mailer"
)

type shareEmailInput struct {
	FromAddr  string
	ToAddr    string
	PublicURL string
	ActorName string
	ItemName  string // trip or plan title
	Path      string // e.g. "/trips/42"
}

func buildShareEmail(in shareEmailInput) string {
	site := strings.TrimRight(in.PublicURL, "/")
	link := site + in.Path
	subject := fmt.Sprintf("%s shared %q with you on Aerly", in.ActorName, in.ItemName)
	plain := fmt.Sprintf("%s shared %q with you on Aerly.\r\n\r\nOpen it: %s\r\n\r\n— Aerly\r\n",
		in.ActorName, in.ItemName, link)
	htmlBody := fmt.Sprintf(
		`<p style="margin:0 0 16px;font-size:15px;"><strong>%s</strong> shared <strong>%s</strong> with you on Aerly.</p>`+
			`<p style="margin:0;"><a href="%s" style="display:inline-block;padding:10px 18px;border-radius:6px;background:%s;color:#ffffff;font-weight:600;text-decoration:none;">Open it</a></p>`,
		mailer.HTMLEscape(in.ActorName), mailer.HTMLEscape(in.ItemName),
		mailer.HTMLEscape(link), mailer.BrandColor)
	return mailer.AssembleRFC822(in.FromAddr, in.ToAddr, subject, plain,
		mailer.HTMLShell(subject, htmlBody, in.PublicURL))
}
```

- [ ] **Step 4: Implement the notify handlers**

Add to `internal/handlers/sharing.go`. Factor a shared core that takes the item name, path, recipient user-ids, and emails:

```go
type notifySharesReq struct {
	UserIDs []int64  `json:"user_ids"`
	Emails  []string `json:"emails"`
}

// notifyShares sends an in-app notification + email to each user id, and an
// email to each pre-shared address. Best-effort: per-recipient failures are
// logged, never fatal. Owner action only (callers gate before invoking).
func (a *API) notifyShares(r *http.Request, in notifySharesReq, kind string, tripID, planID int64, itemName, path string) {
	me := auth.UserFrom(r.Context())
	ctx := r.Context()
	var tp, pp *int64
	if tripID != 0 {
		tp = &tripID
	}
	if planID != 0 {
		pp = &planID
	}
	for _, uid := range in.UserIDs {
		msg := fmt.Sprintf("%s shared %q with you", actorLabel(me), itemName)
		if _, err := a.Store.InsertNotification(ctx, store.Notification{
			UserID: uid, Kind: "share", ActorID: &me.ID, TripID: tp, PlanID: pp, Message: msg,
		}); err != nil {
			slog.Error("notifyShares: insert notification", "err", err, "to", uid)
			continue
		}
		a.publishNotifications(ctx, uid)
		a.emailUser(ctx, uid, itemName, path) // helper below
	}
	for _, addr := range in.Emails {
		a.sendShareEmailTo(ctx, addr, itemName, path)
	}
}
```

Add small helpers in the same file: `actorLabel(*store.User) string` (name or username), `emailUser` (load the user's verified email via `a.Store.EmailsByUser`, then `sendShareEmailTo`), and `sendShareEmailTo` (guard `a.Config.MailFromAddress`, build with `buildShareEmail`, `mailer.Send`). Mirror `sendFriendRequestNotification` for the email-loading + send + timeout pattern. Then the two route handlers:

```go
func (a *API) notifyTripShares(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in notifySharesReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t, err := a.Store.TripByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	a.notifyShares(r, in, "trip", id, 0, t.Name, fmt.Sprintf("/trips/%d", id))
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) notifyPlanShares(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requirePlanEdit(r.Context(), id, me, w); err != nil {
		return
	}
	var in notifySharesReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pl, err := a.Store.PlanByID(r.Context(), id)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	name := pl.Title
	if name == "" {
		name = pl.Type
	}
	a.notifyShares(r, in, "plan", pl.TripID, id, name, fmt.Sprintf("/trips/%d", pl.TripID))
	w.WriteHeader(http.StatusNoContent)
}
```

Add imports (`fmt`, `log/slog`, `github.com/dpage/aerly/internal/store`) to `sharing.go`.

- [ ] **Step 5: Register routes**

In `internal/handlers/handlers.go`:

```go
	mux.Handle("POST /api/trips/{id}/notify-shares", req(http.HandlerFunc(a.notifyTripShares)))
	mux.Handle("POST /api/plans/{id}/notify-shares", req(http.HandlerFunc(a.notifyPlanShares)))
```

- [ ] **Step 6: Run the test + suite**

Run: `go test ./internal/handlers/ -run TestNotifyTripShares -v && go test ./internal/handlers/`
Expected: PASS (email is a no-op when `MailFromAddress` unset in tests — the in-app insert still happens).

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/sharing.go internal/handlers/share_emails.go internal/handlers/handlers.go internal/handlers/sharing_test.go
git commit -m "feat(api): notify-shares endpoints (email + in-app)"
```

---

### Task 10: Surface `notifications` in the inbox + badge

**Files:**
- Modify: `internal/handlers/notifications.go` — `buildNotificationsDTO`
- Modify: `internal/handlers/handlers_alert_inbox.go` — `listAlerts` merges notifications; `markAlertsRead` also marks notifications read
- Modify: `internal/api/dto.go` — add a generic `NotificationItemDTO` + mapper; widen `NotificationsDTO` with `UnreadShares int`
- Test: `internal/handlers/handlers_alert_inbox_test.go`

- [ ] **Step 1: Write a failing test**

Add to `internal/handlers/handlers_alert_inbox_test.go`:

```go
func TestInboxIncludesShareNotifications(t *testing.T) {
	e := setup(t, nil, nil)
	actor := e.user(t, "actor", false)
	uid := e.user(t, "inboxuser", false)
	tripID := mkTripHandler(t, e, actor)
	if _, err := e.store.InsertNotification(context.Background(), store.Notification{
		UserID: uid, Kind: "share", ActorID: &actor, TripID: &tripID,
		Message: "actor shared T with you",
	}); err != nil {
		t.Fatalf("InsertNotification: %v", err)
	}
	w := e.req(t, "GET", "/api/notifications", nil, uid)
	if !containsJSON(w.Body.String(), `"unread_shares":1`) {
		t.Errorf("notifications DTO missing unread_shares: %s", w.Body.String())
	}
	w = e.req(t, "GET", "/api/alerts", nil, uid)
	if !containsJSON(w.Body.String(), "shared T with you") {
		t.Errorf("alert inbox missing share item: %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run to confirm failure** — FAIL.

- [ ] **Step 3: Widen `NotificationsDTO` + add item DTO**

In `internal/api/dto.go`, add `UnreadShares int \`json:"unread_shares"\`` to `NotificationsDTO`, and a generic inbox item:

```go
type NotificationItemDTO struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	ActorID   *int64     `json:"actor_id,omitempty"`
	TripID    *int64     `json:"trip_id,omitempty"`
	PlanID    *int64     `json:"plan_id,omitempty"`
	Message   string     `json:"message"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
}

func ToNotificationItemDTO(n store.Notification) NotificationItemDTO {
	return NotificationItemDTO{
		ID: n.ID, Kind: n.Kind, ActorID: n.ActorID, TripID: n.TripID,
		PlanID: n.PlanID, Message: n.Message, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt,
	}
}
```

- [ ] **Step 4: Add the count to `buildNotificationsDTO`**

In `internal/handlers/notifications.go`, after the `UnreadAlerts` fetch:

```go
	unreadShares, err := a.Store.CountUnreadNotifications(ctx, userID)
	if err != nil {
		return api.NotificationsDTO{}, err
	}
	return api.NotificationsDTO{
		FriendRequestsPending: n,
		UnreadAlerts:          unread,
		UnreadShares:          unreadShares,
	}, nil
```

- [ ] **Step 5: Merge into the inbox**

In `internal/handlers/handlers_alert_inbox.go`, decide on a merged shape. Simplest, additive, non-breaking: keep `listAlerts` returning flight alerts but also include share items in the same array via a shared wrapper. Since the existing endpoint returns `[]FlightAlertDTO`, add the generic items under a new field by switching the response to an object — but to avoid breaking the existing FE contract, instead add a **separate** array appended after mapping both into a unified `[]NotificationItemDTO` is cleanest. Change `listAlerts` to return a merged, time-sorted `[]NotificationItemDTO`, mapping each `FlightAlertDTO` into a `NotificationItemDTO` (kind = the flight `Kind`, message = alert `Message`, trip/plan ids carried). Update `markAlertsRead` to also call `MarkNotificationsRead`:

```go
func (a *API) listAlerts(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFrom(r.Context())
	alerts, err := a.Store.ListFlightAlerts(r.Context(), me.ID, alertInboxLimit)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	notes, err := a.Store.ListNotifications(r.Context(), me.ID, alertInboxLimit)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	out := make([]api.NotificationItemDTO, 0, len(alerts)+len(notes))
	for _, al := range alerts {
		out = append(out, api.NotificationItemDTO{
			ID: al.ID, Kind: al.Kind, TripID: &al.TripID, PlanID: &al.PlanID,
			Message: al.Message, CreatedAt: al.CreatedAt, ReadAt: al.ReadAt,
		})
	}
	for _, n := range notes {
		out = append(out, api.ToNotificationItemDTO(n))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	writeJSON(w, http.StatusOK, out)
}
```

Add `"sort"` import. In `markAlertsRead`, add `_ = a.Store.MarkNotificationsRead(r.Context(), me.ID)` before `publishNotifications`.

> **FE contract note:** this changes `/api/alerts` items from `FlightAlertDTO` to `NotificationItemDTO`. The alert-inbox FE component (Task 15) must be updated in the same release. Existing alert-inbox FE/tests that read `plan_part_id`/`ident` lose those fields — acceptable since the inbox only renders `message` + a link; verify in Task 15.

- [ ] **Step 6: Run the test + suite**

Run: `go test ./internal/handlers/ -run TestInboxIncludesShare -v && go test ./internal/handlers/`
Expected: PASS. Fix any existing alert-inbox handler test asserting the old `FlightAlertDTO` field names.

- [ ] **Step 7: Commit**

```bash
git add internal/handlers/notifications.go internal/handlers/handlers_alert_inbox.go internal/api/dto.go internal/handlers/handlers_alert_inbox_test.go
git commit -m "feat(api): merge share notifications into the inbox + badge"
```

---

## Phase 4 — Frontend

### Task 11: Types + API client methods

**Files:**
- Modify: `web/src/api/types.ts`, `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts` (extend)

- [ ] **Step 1: Add types**

In `web/src/api/types.ts`:
- Add `share_all_friends_role?: 'viewer' | 'editor';` to `Trip`.
- Add `share_all_friends: boolean;` to `Plan`.
- Add a notifications field if not present: extend the `Notifications` interface with `unread_shares: number;`.
- Add a unified inbox item type:

```typescript
export interface NotificationItem {
  id: number;
  kind: string;
  actor_id?: number;
  trip_id?: number;
  plan_id?: number;
  message: string;
  created_at: string;
  read_at?: string;
}

export interface NotifySharesInput {
  user_ids: number[];
  emails: string[];
}
```

- [ ] **Step 2: Add client methods**

In `web/src/api/client.ts`, alongside the trip/plan methods:

```typescript
setTripShareAllFriends: (tripId: number, role: 'viewer' | 'editor' | null) =>
  request<Trip>('PUT', `/api/trips/${tripId}/share-all-friends`, { role: role ?? '' }),
setPlanShareAllFriends: (planId: number, enabled: boolean) =>
  request<Plan>('PUT', `/api/plans/${planId}/share-all-friends`, { enabled }),
notifyTripShares: (tripId: number, input: NotifySharesInput) =>
  request<void>('POST', `/api/trips/${tripId}/notify-shares`, input).then(() => undefined),
notifyPlanShares: (planId: number, input: NotifySharesInput) =>
  request<void>('POST', `/api/plans/${planId}/notify-shares`, input).then(() => undefined),
```

If `/api/alerts` is consumed via a typed method, update its return type to `NotificationItem[]`.

- [ ] **Step 3: Extend the client test**

In `web/src/api/client.test.ts`, add a case asserting `setTripShareAllFriends(7, 'viewer')` issues `PUT /api/trips/7/share-all-friends` with body `{ role: 'viewer' }` (follow the existing fetch-mock pattern in that file).

- [ ] **Step 4: Run**

Run: `cd web && npx vitest run src/api/client.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/types.ts web/src/api/client.ts web/src/api/client.test.ts
git commit -m "feat(web): types + client for share-all-friends and notify"
```

---

### Task 12: Friend candidates (accepted + pending)

**Files:**
- Modify: `web/src/state/friendUsers.ts`
- Test: `web/src/state/friendUsers.test.ts` (create if absent)

- [ ] **Step 1: Write a failing test**

Create/extend `web/src/state/friendUsers.test.ts` asserting `useFriendCandidates` returns accepted friends with `pending: false` and pending (non-outgoing-email) friends with `pending: true`. Use `renderHook` from `@testing-library/react` and the store-mock pattern from the dialog tests.

- [ ] **Step 2: Implement `useFriendCandidates`**

Append to `web/src/state/friendUsers.ts` (keep `useFriendUsers` unchanged):

```typescript
export interface FriendCandidate {
  user: User;
  pending: boolean;
}

/**
 * Friends pickable for sharing: accepted friends plus pending ones (so you can
 * pre-share to an invited person). Outgoing email-only invites have no
 * friend_id/user and are excluded — share them by typing the email instead.
 */
export function useFriendCandidates(): FriendCandidate[] {
  const meId = useStore((s) => s.me?.id);
  const users = useStore((s) => s.users);
  const friendships = useStore((s) => s.friendships);

  return useMemo(() => {
    if (meId == null) return [];
    const byId = new Map<number, User>(users.map((u) => [u.id, u]));
    const out: FriendCandidate[] = [];
    for (const f of friendships) {
      if (f.friend_id == null) continue;
      const u = byId.get(f.friend_id);
      if (!u) continue;
      if (f.status === 'accepted') out.push({ user: u, pending: false });
      else if (f.status === 'pending') out.push({ user: u, pending: true });
    }
    return out;
  }, [meId, users, friendships]);
}
```

- [ ] **Step 3: Run** — `cd web && npx vitest run src/state/friendUsers.test.ts` → PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/state/friendUsers.ts web/src/state/friendUsers.test.ts
git commit -m "feat(web): friend candidates incl. pending for pre-sharing"
```

---

### Task 13: Store slice actions

**Files:**
- Modify: `web/src/state/tripsSlice.ts`, `web/src/state/plansSlice.ts`
- Test: existing slice tests (`store.test.ts`) — extend minimally

- [ ] **Step 1: Add trip actions**

In `web/src/state/tripsSlice.ts`, add to the slice interface + implementation (follow the `addTripMember` pattern — the endpoint returns the updated `Trip`):

```typescript
async setTripShareAllFriends(tripId, role) {
  const updated = await api.setTripShareAllFriends(tripId, role);
  set((s) => ({ trips: s.trips.map((t) => (t.id === tripId ? updated : t)) }));
},
async notifyTripShares(tripId, input) {
  await api.notifyTripShares(tripId, input);
},
```

- [ ] **Step 2: Add plan actions**

In `web/src/state/plansSlice.ts` (follow `setPlanVisibility` → `reloadCurrent`):

```typescript
async setPlanShareAllFriends(planId, enabled) {
  await api.setPlanShareAllFriends(planId, enabled);
  await reloadCurrent(get);
},
async notifyPlanShares(planId, input) {
  await api.notifyPlanShares(planId, input);
},
```

Add the matching method signatures to each slice's TypeScript interface.

- [ ] **Step 3: Typecheck + run store tests**

Run: `cd web && npx tsc --noEmit && npx vitest run src/state/store.test.ts`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add web/src/state/tripsSlice.ts web/src/state/plansSlice.ts
git commit -m "feat(web): store actions for share-all-friends + notify"
```

---

### Task 14: TripMembersDialog — all-friends control, email invite, notify-on-close

**Files:**
- Modify: `web/src/components/TripMembersDialog.tsx`
- Test: `web/src/components/TripMembersDialog.test.tsx`

- [ ] **Step 1: Write failing tests**

Add cases to `TripMembersDialog.test.tsx` (mock `setTripShareAllFriends`, `notifyTripShares` in the hoisted store mock):
1. Selecting "All friends → Viewer" calls `setTripShareAllFriends(7, 'viewer')`.
2. With the trip's `share_all_friends_role` already `'viewer'`, the control shows Viewer selected.
3. After adding a friend then closing with the "Notify" box checked, `notifyTripShares(7, { user_ids: [<id>], emails: [] })` is called with the session-added id.

- [ ] **Step 2: Run to confirm failure** — `cd web && npx vitest run src/components/TripMembersDialog.test.tsx` → FAIL.

- [ ] **Step 3: Implement the UI**

In `TripMembersDialog.tsx`:
- Pull `setTripShareAllFriends`, `notifyTripShares` from the store; accept the trip's current `share_all_friends_role` via props (the dialog already receives `tripId`/`members`; add a `shareAllFriendsRole?: 'viewer'|'editor'` prop, threaded from the trip object by the parent) — or read the trip from the store by `tripId`. Follow the existing prop style; if `members` is passed in, add `shareAllFriendsRole` the same way.
- Add a control above the member table (only when `canManage`): a select with options Off / Viewer / Editor bound to `share_all_friends_role`. `onChange` → `await setTripShareAllFriends(tripId, value === 'off' ? null : value)`.
- Track session-added recipients: keep a `addedThisSession` `Set<number>` state; in `handleAdd`, after a successful add, add the picked id. When the all-friends toggle is switched **on**, add every accepted friend's id (from `useFriendCandidates().filter(c => !c.pending)`) to the set (they newly gained access).
- Email invite: add a small "or invite by email" text field; on submit call `api.inviteFriend({ email })` then, to pre-share, call a new path — since the member-add endpoint is by `user_id`, email pre-shares go through `pending_shares` server-side; add a client method `inviteAndShareTrip`? **Simpler for v1:** the email field calls `api.inviteFriend` and adds the email to a `emailsThisSession` set used only for notify. (Server-side `pending_shares` creation for email-shares is wired by extending the invite path — if out of scope for the dialog, the all-friends flag covers them on accept; see note.) Capture typed emails into `emailsThisSession`.
- Notify-on-close: render a checkbox "Notify the people I just added" (default checked) when `addedThisSession.size + emailsThisSession.size > 0`. Wrap `onClose` so that if checked, it first calls `notifyTripShares(tripId, { user_ids: [...addedThisSession], emails: [...emailsThisSession] })`.

> **Scope note for executor:** wiring `pending_shares` creation for *email* shares from the trip dialog requires a dedicated endpoint (`POST /api/trips/{id}/share-by-email` doing `inviteFriendByEmail` + `InsertPendingShare`). If you implement it, add it as a sibling in Task 8/9; otherwise email-invitees still receive access via the all-friends flag once they accept, and the email field is invite-only. Pick one and make the dialog behaviour match; do not leave a dead control.

- [ ] **Step 4: Run** — `cd web && npx vitest run src/components/TripMembersDialog.test.tsx` → PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/TripMembersDialog.tsx web/src/components/TripMembersDialog.test.tsx
git commit -m "feat(web): trip dialog — share with all friends + notify on close"
```

---

### Task 15: PlanPrivacyDialog — all-friends toggle + notify-on-close; inbox rendering

**Files:**
- Modify: `web/src/components/PlanPrivacyDialog.tsx` + test
- Modify: the avatar alert-inbox component (the one consuming `/api/alerts`) + its test

- [ ] **Step 1: Write failing tests (PlanPrivacyDialog)**

Add to `PlanPrivacyDialog.test.tsx`: toggling "Share with all friends" on calls `setPlanShareAllFriends(plan.id, true)`; reflects `plan.share_all_friends` initial state; notify-on-close fires `notifyPlanShares` with newly-added passengers.

- [ ] **Step 2: Implement (PlanPrivacyDialog)**

Add a `Switch`/checkbox "Share with all friends" bound to `plan.share_all_friends`, calling `setPlanShareAllFriends(plan.id, checked)`. Track passengers added this session (in `handleAddPax`) and wrap `onClose` with the same notify checkbox → `notifyPlanShares(plan.id, { user_ids, emails: [] })` as in Task 14.

- [ ] **Step 3: Update the alert-inbox component**

Find the component rendering `/api/alerts` (consumes `FlightAlert[]` today via `loadAlerts` in the store). Update its item type to `NotificationItem` and render `message` + a link derived from `trip_id`/`plan_id` (navigate to `/trips/${trip_id}`). Update `web/src/sse.ts` `onAlert`/`notifications.updated` handling only if the payload shape changed — the badge already reads counts from `notifications.updated`; add `unread_shares` into the badge total in the store's notifications handler. Update affected tests.

- [ ] **Step 4: Run the web suite**

Run: `cd web && npx vitest run && npx tsc --noEmit`
Expected: PASS. Fix any alert-inbox test referencing removed `FlightAlert` fields.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/PlanPrivacyDialog.tsx web/src/components/PlanPrivacyDialog.test.tsx web/src/components web/src/sse.ts web/src/state
git commit -m "feat(web): plan dialog all-friends toggle; generic inbox items"
```

---

## Phase 5 — End-to-end

### Task 16: Playwright coverage

**Files:**
- Create: `web/e2e/share-all-friends.spec.ts` (follow the existing Playwright config + the dev-login pattern: `/auth/dev-login`, Vite proxy to `:8080`, `setMatchMedia` for responsive — see project memory `aerly-playwright-dev-login`)

- [ ] **Step 1: Write the e2e spec**

Cover, using two dev-login users who are accepted friends:
1. Owner sets a trip to "all friends → viewer"; the friend sees the trip tile and all plans.
2. Owner shares a single plan with all friends; the friend sees the trip tile but only that plan.
3. Owner adds a friend and, on closing the dialog with Notify checked, the friend's in-app badge increments and the inbox shows the share item.
4. Owner promotes one friend to editor (override) while all-friends stays viewer; that friend can edit, others cannot.

- [ ] **Step 2: Run**

Run: `cd web && npx playwright test share-all-friends.spec.ts`
Expected: PASS (requires the dev server + DB; see `aerly-playwright-dev-login`).

- [ ] **Step 3: Commit**

```bash
git add web/e2e/share-all-friends.spec.ts
git commit -m "test(e2e): share-with-all-friends flows"
```

---

## Final verification

- [ ] `go build ./... && go test ./...` (set `AERLY_REQUIRE_DB=1` locally to exercise DB tests)
- [ ] `cd web && npx tsc --noEmit && npx vitest run`
- [ ] Manually validate the legacy `DELETE FROM trip_members` cleanup against prod row counts (read-only `sudo -u aerly psql`) before deploying the migration.
- [ ] Confirm the per-file coverage gate passes (the repo enforces it — recent commit `0037258`).
