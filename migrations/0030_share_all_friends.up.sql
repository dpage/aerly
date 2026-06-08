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
    UNIQUE (email_lower, kind, target_id),
    CHECK (kind = 'plan' OR role IN ('viewer', 'editor'))
);
CREATE INDEX pending_shares_email_idx ON pending_shares (email_lower);

-- Generic in-app notification inbox (non-flight). Mirrors flight_alerts so the
-- avatar inbox + unread badge machinery extends naturally. kind 'share' today.
CREATE TABLE notifications (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       TEXT   NOT NULL,
    actor_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    trip_id    BIGINT REFERENCES trips(id) ON DELETE CASCADE,
    plan_id    BIGINT REFERENCES plans(id) ON DELETE CASCADE,
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

-- NOTE: legacy trip_members 'viewer' rows that were auto-created by the dropped
-- trigger for plan passengers are intentionally NOT cleaned up here. trip_members
-- has no provenance column, so a blanket DELETE could not distinguish those from
-- viewer memberships an owner added on purpose. The cleanup is performed as a
-- separate, manually-validated step against production (see the implementation
-- plan). New passenger adds no longer create a viewer row, so they are correctly
-- plan-scoped going forward.
