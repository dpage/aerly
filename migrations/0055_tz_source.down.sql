-- Reverting loses the zone sources and users' home zones; '' reverts to
-- meaning floating for every empty zone.
ALTER TABLE users DROP COLUMN IF EXISTS home_tz;
ALTER TABLE trip_feed_events DROP COLUMN IF EXISTS start_tz_source;
ALTER TABLE plan_parts
    DROP CONSTRAINT IF EXISTS plan_parts_end_tz_source_matches,
    DROP CONSTRAINT IF EXISTS plan_parts_start_tz_source_matches,
    DROP COLUMN IF EXISTS end_tz_source,
    DROP COLUMN IF EXISTS start_tz_source;
