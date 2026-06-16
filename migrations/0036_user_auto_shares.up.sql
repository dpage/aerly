-- Per-user "Always share with" defaults. Each row says: whenever user_id
-- creates a new trip, automatically grant share_with_id the given role on it.
--
-- Use case: someone who always wants their partner to see their trips (as a
-- viewer) and their PA to manage them (as an editor) sets this once instead of
-- sharing every trip by hand. 'passenger' grants a trip-level passenger
-- (a traveller on every plan, issue #20) rather than a plain member role.
--
-- This only affects trips created AFTER the default is set; existing trips are
-- left untouched. The grant is applied at trip-creation time (CreateTrip) as a
-- real trip_members / trip_passengers row, so changing or removing a default
-- later does not retroactively revoke access already granted.
CREATE TABLE user_auto_shares (
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    share_with_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role          TEXT   NOT NULL CHECK (role IN ('viewer', 'editor', 'passenger')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, share_with_id),
    -- Sharing a trip with yourself is meaningless (you're already the owner).
    CHECK (user_id <> share_with_id)
);

CREATE INDEX user_auto_shares_user_idx ON user_auto_shares (user_id);
-- Index the FK target column so deleting/updating a user doesn't force a full
-- scan of this table for the referential-integrity check.
CREATE INDEX user_auto_shares_share_with_idx ON user_auto_shares (share_with_id);
