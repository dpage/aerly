-- Index tuning from an audit of the store package's queries against the live
-- schema. Plain (non-CONCURRENT) statements: each migration file runs in one
-- transaction, and every table here is small, so a brief lock is harmless.

-- positions is the fastest-growing table. LatestRealPosition and
-- lastRealPositionBefore want the newest *real* fix for a part
-- (is_estimated = false ORDER BY ts DESC LIMIT 1). The existing
-- positions_part_ts_idx walks newest-first but must skip the runs of
-- dead-reckoned (estimated) points that pile up at a track's tail after
-- signal loss. A partial index over real fixes only makes that lookup O(1).
CREATE INDEX IF NOT EXISTS positions_part_real_ts_idx
    ON positions (plan_part_id, ts DESC)
    WHERE is_estimated = false;

-- ListTrips (the per-request trip list) leads with t.created_by = $1, but
-- nothing indexed it: trips_creator_tripit_id_uidx is partial (only rows with
-- a non-empty tripit_id). This also backs the trips.created_by -> users FK, so
-- deleting a user no longer sequentially scans trips.
CREATE INDEX IF NOT EXISTS trips_created_by_idx ON trips (created_by);

-- notifications.trip_id and .plan_id are ON DELETE CASCADE foreign keys with
-- no supporting index. The table accumulates a row per social event, so as it
-- grows, deleting a trip or plan would force a sequential scan per cascade.
CREATE INDEX IF NOT EXISTS notifications_trip_idx ON notifications (trip_id);
CREATE INDEX IF NOT EXISTS notifications_plan_idx ON notifications (plan_id);

-- email_ingests is insert-only: nothing in the app ever reads it back, so an
-- index on received_at alone backs no query and no constraint. (The sibling
-- email_ingests_user_idx is kept: it covers the user_id FK for user deletion.)
DROP INDEX IF EXISTS email_ingests_received_idx;

-- SuggestTags does `label_norm LIKE $2 || '%'`, but the database collation is
-- en_US.UTF-8, so a default-collation btree cannot serve a prefix LIKE. Rebuild
-- the index with text_pattern_ops so the autocomplete prefix scan can use it.
-- Equality lookups on label_norm always arrive with trip_id and continue to use
-- the (trip_id, label_norm) primary key.
DROP INDEX IF EXISTS trip_tags_label_idx;
CREATE INDEX IF NOT EXISTS trip_tags_label_idx
    ON trip_tags (label_norm text_pattern_ops);
