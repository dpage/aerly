-- Reverse 0034: drop the added indexes and restore the two that were changed.
DROP INDEX IF EXISTS positions_part_real_ts_idx;
DROP INDEX IF EXISTS trips_created_by_idx;
DROP INDEX IF EXISTS notifications_trip_idx;
DROP INDEX IF EXISTS notifications_plan_idx;

-- Restore the original insert-only audit index.
CREATE INDEX IF NOT EXISTS email_ingests_received_idx
    ON email_ingests (received_at DESC);

-- Restore the original default-collation btree on the tag label.
DROP INDEX IF EXISTS trip_tags_label_idx;
CREATE INDEX IF NOT EXISTS trip_tags_label_idx ON trip_tags (label_norm);
