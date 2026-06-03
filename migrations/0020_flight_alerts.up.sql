-- Persistent in-app flight alerts (spec: in-app alert inbox). The poller writes
-- one row per in-app recipient when a tracked flight meaningfully changes
-- (delayed | cancelled | diverted | gate). Rows back the avatar-menu inbox and
-- the unread badge; read_at NULL means unread. Email delivery is unchanged.
CREATE TABLE flight_alerts (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL,
    plan_part_id BIGINT NOT NULL,
    plan_id      BIGINT NOT NULL,
    trip_id      BIGINT NOT NULL,
    ident        TEXT NOT NULL,
    kind         TEXT NOT NULL,
    status       TEXT NOT NULL,
    message      TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    read_at      TIMESTAMPTZ
);

CREATE INDEX flight_alerts_user_created_idx ON flight_alerts (user_id, created_at DESC);
CREATE INDEX flight_alerts_user_unread_idx ON flight_alerts (user_id) WHERE read_at IS NULL;
