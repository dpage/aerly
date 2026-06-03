-- Schema hardening surfaced by code review.
--
--   1. flight_alerts (0020) was created with no foreign keys, so deleting a
--      user / trip / plan / part left orphaned alert rows. Add cascading FKs.
--   2. positions.plan_part_id has been the sole key since 0013 dropped the
--      legacy flight_id column, but stayed nullable. Tighten to NOT NULL.
--   3. plans.tripit_uid / trips.tripit_id re-import dedupe (0019) was a
--      read-then-write TOCTOU with no unique constraint. Add partial unique
--      indexes so ON CONFLICT can arbitrate concurrent imports.
--
-- Each step first removes any rows that would violate the new constraint, so
-- the migration is safe to apply to an existing database. The plans/trips
-- cascade is ON DELETE CASCADE all the way down (plan_parts, positions,
-- detail satellites), so deduping plans/trips cleans their children too.

-- (1) flight_alerts foreign keys ---------------------------------------------
DELETE FROM flight_alerts a WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = a.user_id);
DELETE FROM flight_alerts a WHERE NOT EXISTS (SELECT 1 FROM trips t WHERE t.id = a.trip_id);
DELETE FROM flight_alerts a WHERE NOT EXISTS (SELECT 1 FROM plans p WHERE p.id = a.plan_id);
DELETE FROM flight_alerts a WHERE NOT EXISTS (SELECT 1 FROM plan_parts pp WHERE pp.id = a.plan_part_id);

ALTER TABLE flight_alerts
    ADD CONSTRAINT flight_alerts_user_fk FOREIGN KEY (user_id)      REFERENCES users(id)      ON DELETE CASCADE,
    ADD CONSTRAINT flight_alerts_trip_fk FOREIGN KEY (trip_id)      REFERENCES trips(id)      ON DELETE CASCADE,
    ADD CONSTRAINT flight_alerts_plan_fk FOREIGN KEY (plan_id)      REFERENCES plans(id)      ON DELETE CASCADE,
    ADD CONSTRAINT flight_alerts_part_fk FOREIGN KEY (plan_part_id) REFERENCES plan_parts(id) ON DELETE CASCADE;

-- (2) positions.plan_part_id NOT NULL ----------------------------------------
DELETE FROM positions WHERE plan_part_id IS NULL;
ALTER TABLE positions ALTER COLUMN plan_part_id SET NOT NULL;

-- (3) tripit dedupe unique indexes -------------------------------------------
-- Keep the lowest plan id per (trip_id, tripit_uid) for non-empty uids.
DELETE FROM plans p
 USING plans q
 WHERE p.tripit_uid <> ''
   AND q.tripit_uid = p.tripit_uid
   AND q.trip_id    = p.trip_id
   AND q.id < p.id;
CREATE UNIQUE INDEX plans_trip_tripit_uid_uidx ON plans (trip_id, tripit_uid) WHERE tripit_uid <> '';

-- Keep the lowest trip id per (created_by, tripit_id) for non-empty ids.
DELETE FROM trips t
 USING trips u
 WHERE t.tripit_id <> ''
   AND u.tripit_id  = t.tripit_id
   AND u.created_by = t.created_by
   AND u.id < t.id;
CREATE UNIQUE INDEX trips_creator_tripit_id_uidx ON trips (created_by, tripit_id) WHERE tripit_id <> '';
