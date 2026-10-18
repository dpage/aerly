-- Migration 0055: explicit time zone sources (time zone redesign, PR 1).
-- start_tz = '' used to mean "the UTC digits are local wall-clock time". That
-- meaning now lives in the source column as 'floating', so '' can also mean a
-- true instant whose zone is simply unknown.
ALTER TABLE plan_parts
    ADD COLUMN start_tz_source TEXT NOT NULL DEFAULT 'unknown'
        CHECK (start_tz_source IN ('confirmed', 'inferred', 'unknown', 'floating')),
    ADD COLUMN end_tz_source TEXT NOT NULL DEFAULT 'unknown'
        CHECK (end_tz_source IN ('confirmed', 'inferred', 'unknown', 'floating'));

-- Existing zones all came from the item's own airport, coordinates or a TZID.
-- An empty zone keeps today's floating meaning, except on a provider-resolved
-- flight (real instants since #147) or an end that has no time at all.
UPDATE plan_parts part SET
    start_tz_source = CASE
        WHEN part.start_tz <> '' THEN 'confirmed'
        WHEN EXISTS (SELECT 1 FROM flight_details fd WHERE fd.plan_part_id = part.id AND fd.resolved) THEN 'unknown'
        ELSE 'floating' END,
    end_tz_source = CASE
        WHEN part.end_tz <> '' THEN 'confirmed'
        WHEN part.ends_at IS NULL THEN 'unknown'
        WHEN EXISTS (SELECT 1 FROM flight_details fd WHERE fd.plan_part_id = part.id AND fd.resolved) THEN 'unknown'
        ELSE 'floating' END;

ALTER TABLE plan_parts
    ADD CONSTRAINT plan_parts_start_tz_source_matches
        CHECK ((start_tz = '') = (start_tz_source IN ('unknown', 'floating'))),
    ADD CONSTRAINT plan_parts_end_tz_source_matches
        CHECK ((end_tz = '') = (end_tz_source IN ('unknown', 'floating')));

-- Feed events are resolved to true instants at fetch time.
ALTER TABLE trip_feed_events
    ADD COLUMN start_tz_source TEXT NOT NULL DEFAULT 'unknown'
        CHECK (start_tz_source IN ('confirmed', 'inferred', 'unknown'));
UPDATE trip_feed_events SET start_tz_source = 'inferred' WHERE start_tz <> '';

-- The viewer's home zone, for emails and push (PR 4). '' = not yet known.
ALTER TABLE users ADD COLUMN home_tz TEXT NOT NULL DEFAULT '';
