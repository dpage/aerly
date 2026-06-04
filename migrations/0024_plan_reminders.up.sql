-- Upcoming-plan reminders (issue #11). Separate from the flight-status-change
-- alert tables (alert_prefs / plan_alert_optin): these drive scheduled "your
-- plan starts soon" emails + in-app notices, fired per plan_part by the poller.

-- Trip-level opt-in: presence of a row = opted in for the whole trip.
CREATE TABLE trip_reminder_optin (
    trip_id    BIGINT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lead_hours INT NOT NULL DEFAULT 24 CHECK (lead_hours > 0),
    PRIMARY KEY (trip_id, user_id)
);

-- Per-plan override. enabled = TRUE → opt in; enabled = FALSE → explicit
-- opt-out (beats a trip-level opt-in). Absence of a row = inherit the trip.
CREATE TABLE plan_reminder_optin (
    plan_id    BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL,
    lead_hours INT NOT NULL DEFAULT 24 CHECK (lead_hours > 0),
    PRIMARY KEY (plan_id, user_id)
);

-- Dedupe: one reminder per part per user, ever.
CREATE TABLE plan_reminder_sent (
    plan_part_id BIGINT NOT NULL REFERENCES plan_parts(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_part_id, user_id)
);
