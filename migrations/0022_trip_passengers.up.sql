-- Trip-level passengers (issue #20). A trip passenger is someone travelling on
-- the whole trip — e.g. a partner on a shared holiday — added once at the trip
-- level rather than per plan. The application materialises each trip passenger
-- into plan_passengers for the plans they're allowed to see (existing and
-- future), so on those plans they're a real passenger (on the "on board" list
-- and alert recipients) and the trip lands under their "My trips" (issue #19) —
-- while plans hidden from them (plan_visibility) stay hidden everywhere.
CREATE TABLE trip_passengers (
    trip_id  BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (trip_id, user_id)
);

CREATE INDEX trip_passengers_user_idx ON trip_passengers (user_id);

-- via_trip marks a plan_passengers row that was materialised from a trip-level
-- passenger (vs an explicit per-plan add). It lets us reconcile/remove only the
-- trip-derived rows without disturbing manual passenger assignments, and lets a
-- manual add "win" (an explicit passenger is never auto-removed when a plan is
-- hidden from them, mirroring the "a passenger always sees their plan" rule).
ALTER TABLE plan_passengers ADD COLUMN via_trip BOOLEAN NOT NULL DEFAULT false;

-- plan_visible_to_member(plan, user) is the §4 visibility rule for a plain trip
-- member (no creator/explicit-passenger override): a plan with no visibility row
-- is visible; a hidden_from plan is visible unless it names the user; an
-- only_visible_to plan is visible only when it names them. Used to decide which
-- plans a trip passenger may be materialised onto.
CREATE FUNCTION plan_visible_to_member(p_plan BIGINT, p_user BIGINT) RETURNS boolean AS $$
    SELECT NOT EXISTS (SELECT 1 FROM plan_visibility v WHERE v.plan_id = p_plan)
        OR EXISTS (
            SELECT 1 FROM plan_visibility v
            WHERE v.plan_id = p_plan AND v.mode = 'hidden_from'
              AND NOT EXISTS (
                SELECT 1 FROM plan_visibility_members m
                WHERE m.plan_id = p_plan AND m.user_id = p_user))
        OR EXISTS (
            SELECT 1 FROM plan_visibility v
            JOIN plan_visibility_members m ON m.plan_id = v.plan_id
            WHERE v.plan_id = p_plan AND v.mode = 'only_visible_to' AND m.user_id = p_user);
$$ LANGUAGE sql STABLE;
