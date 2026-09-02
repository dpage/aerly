-- Migration 0052: check-in reminders (issue #119).
--
-- Most airlines open online check-in 24 hours before departure, and the ask
-- was for a nudge five minutes before that window opens rather than after it,
-- so the traveller is at the keyboard when the seats are released.
--
-- The preference lives on alert_prefs beside the flight-change alert settings,
-- because it is the same shape of thing: per user, flight-specific, and
-- delivered down the same in_app / email channels that table already governs.
-- It defaults to FALSE so existing users are opted out until they ask for it,
-- unlike in_app / email which default on.
ALTER TABLE alert_prefs ADD COLUMN checkin BOOLEAN NOT NULL DEFAULT FALSE;

-- Dedupe: one check-in reminder per flight part per user, ever. Mirrors
-- plan_reminder_sent (0024); the poller marks the pair only after it has tried
-- to deliver, so a crash mid-send re-sends rather than silently dropping.
CREATE TABLE flight_checkin_sent (
    plan_part_id BIGINT NOT NULL REFERENCES plan_parts(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_part_id, user_id)
);
