-- Remove legacy trip_members 'viewer' rows that the dropped passenger→viewer
-- trigger (migration 0030) created. Such a row is identifiable as: role
-- 'viewer', the user is NOT a trip-level passenger (those keep their viewer
-- row intentionally), but IS a plan passenger somewhere in the trip — i.e. the
-- membership only ever existed because adding the passenger fired the trigger.
-- Now that passengers are plan-scoped, these rows are an over-share (the user
-- sees the whole trip instead of just the plans they're a passenger of).
--
-- This was held back from 0030 pending validation that it wouldn't delete a
-- deliberately-added viewer. Validated against production: the predicate
-- matched exactly one row, conclusively trigger-derived (membership added_at
-- == the user's plan_passenger added_at to the microsecond; user was a
-- passenger on every plan in the trip). Safe to apply. On fresh test/CI
-- databases the predicate matches nothing, so this is a no-op there.
DELETE FROM trip_members tm
 WHERE tm.role = 'viewer'
   AND NOT EXISTS (SELECT 1 FROM trip_passengers tp
                   WHERE tp.trip_id = tm.trip_id AND tp.user_id = tm.user_id)
   AND EXISTS (SELECT 1 FROM plan_passengers pp
               JOIN plans pl ON pl.id = pp.plan_id
               WHERE pl.trip_id = tm.trip_id AND pp.user_id = tm.user_id);
