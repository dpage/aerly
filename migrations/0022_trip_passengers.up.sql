-- Trip-level passengers (issue #20). A trip passenger is someone travelling on
-- the whole trip — e.g. a partner on a shared holiday — added once at the trip
-- level rather than per plan. The application materialises each trip passenger
-- into plan_passengers for every plan in the trip (existing and future), so
-- they're a passenger on all of them: they see all non-hidden plans, appear in
-- the per-plan "on board" lists and alert recipients, and the trip lands under
-- their "My trips" (issue #19). This table is the source of truth, used to
-- back-fill newly created plans and to remove the materialised rows cleanly.
CREATE TABLE trip_passengers (
    trip_id  BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id  BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (trip_id, user_id)
);

CREATE INDEX trip_passengers_user_idx ON trip_passengers (user_id);
