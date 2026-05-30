-- Reverse 0013: recreate the legacy flights / flight_passengers / flight_shares
-- table + column STRUCTURE so a rollback chain can proceed (0012-down, 0011-down,
-- 0010-down expect these objects to exist again).
--
-- IMPORTANT: this restores STRUCTURE ONLY. The pre-migration row DATA is NOT
-- recovered here — 0010 already copied every legacy flights row into the plan
-- model (plans / plan_parts / flight_details / plan_passengers /
-- plan_visibility), and 0013-up dropped the originals. The recreated tables
-- come back empty. The flights table is rebuilt with the full column set it had
-- by 0012 (original 0001 columns plus icao24 (0002), is_public (0004),
-- callsign + last_resolved_at (0006); aeroapi_id stays dropped per 0003).

CREATE TABLE flights (
    id              BIGSERIAL PRIMARY KEY,
    ident           TEXT NOT NULL,
    scheduled_out   TIMESTAMPTZ NOT NULL,
    scheduled_in    TIMESTAMPTZ NOT NULL,
    estimated_out   TIMESTAMPTZ,
    estimated_in    TIMESTAMPTZ,
    actual_out      TIMESTAMPTZ,
    actual_in       TIMESTAMPTZ,
    origin_iata     TEXT NOT NULL DEFAULT '',
    origin_lat      DOUBLE PRECISION,
    origin_lon      DOUBLE PRECISION,
    dest_iata       TEXT NOT NULL DEFAULT '',
    dest_lat        DOUBLE PRECISION,
    dest_lon        DOUBLE PRECISION,
    status          TEXT NOT NULL DEFAULT 'Scheduled',
    icao24          TEXT,
    callsign        TEXT,
    last_polled_at  TIMESTAMPTZ,
    last_resolved_at TIMESTAMPTZ,
    created_by      BIGINT REFERENCES users(id) ON DELETE SET NULL,
    notes           TEXT NOT NULL DEFAULT '',
    is_public       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX flights_scheduled_out_idx ON flights (scheduled_out);
CREATE INDEX flights_active_idx ON flights (scheduled_in)
    WHERE status NOT IN ('Arrived', 'Cancelled');

CREATE TABLE flight_passengers (
    flight_id   BIGINT NOT NULL REFERENCES flights(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (flight_id, user_id)
);

CREATE INDEX flight_passengers_user_idx ON flight_passengers (user_id);

CREATE TABLE flight_shares (
    flight_id   BIGINT NOT NULL REFERENCES flights(id) ON DELETE CASCADE,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (flight_id, user_id)
);

CREATE INDEX flight_shares_user_idx ON flight_shares (user_id);

-- Restore positions.flight_id to its just-before-0013 shape: nullable, NO FK
-- (dropped in 0010). The pre-0013 positions_part_ts_idx was (mis)defined on
-- (flight_id, ts DESC) — 0010 only renamed it — so restore it to that exact
-- shape: drop the plan_part_id-keyed index this migration's up created, re-add
-- flight_id, and rebuild positions_part_ts_idx on (flight_id, ts DESC). A
-- continued rollback through 0011-down / 0010-down then re-adds the NOT NULL
-- constraint, the flights FK, and renames the index back to
-- positions_flight_ts_idx.
DROP INDEX IF EXISTS positions_part_ts_idx;
ALTER TABLE positions ADD COLUMN flight_id BIGINT;
CREATE INDEX positions_part_ts_idx ON positions (flight_id, ts DESC);
