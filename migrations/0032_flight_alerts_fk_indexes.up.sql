-- flight_alerts gained ON DELETE CASCADE foreign keys to trips, plans and
-- plan_parts in 0021, but no supporting indexes on those columns. Deleting a
-- trip / plan / plan_part therefore forces a sequential scan of flight_alerts
-- per cascade. Add the indexes so the cascade (and any alert lookup by
-- resource) uses an index instead. IF NOT EXISTS keeps this re-runnable.
CREATE INDEX IF NOT EXISTS flight_alerts_plan_part_idx ON flight_alerts (plan_part_id);
CREATE INDEX IF NOT EXISTS flight_alerts_plan_idx      ON flight_alerts (plan_id);
CREATE INDEX IF NOT EXISTS flight_alerts_trip_idx      ON flight_alerts (trip_id);
