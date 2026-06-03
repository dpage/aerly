-- Reverse 0021. Deleted orphan/duplicate rows are not restored.
DROP INDEX IF EXISTS trips_creator_tripit_id_uidx;
DROP INDEX IF EXISTS plans_trip_tripit_uid_uidx;

ALTER TABLE positions ALTER COLUMN plan_part_id DROP NOT NULL;

ALTER TABLE flight_alerts
    DROP CONSTRAINT IF EXISTS flight_alerts_part_fk,
    DROP CONSTRAINT IF EXISTS flight_alerts_plan_fk,
    DROP CONSTRAINT IF EXISTS flight_alerts_trip_fk,
    DROP CONSTRAINT IF EXISTS flight_alerts_user_fk;
