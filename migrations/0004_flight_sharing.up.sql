-- Flight sharing: visibility model extension.
-- A flight is visible to a user when any of: they are the creator, they
-- are a passenger, they are in flight_shares for the flight, the flight
-- is_public, or the viewer is a superuser viewing with show_all.

ALTER TABLE flights ADD COLUMN is_public BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE flight_shares (
    flight_id BIGINT NOT NULL REFERENCES flights(id) ON DELETE CASCADE,
    user_id   BIGINT NOT NULL REFERENCES users(id)   ON DELETE CASCADE,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (flight_id, user_id)
);

CREATE INDEX flight_shares_user_idx ON flight_shares (user_id);
