DROP FUNCTION IF EXISTS plan_visible_to_member(BIGINT, BIGINT);
ALTER TABLE plan_passengers DROP COLUMN IF EXISTS via_trip;
DROP TABLE IF EXISTS trip_passengers;
