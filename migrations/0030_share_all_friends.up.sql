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
