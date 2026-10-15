-- Migration 0054: departure summary (issue #130).
--
-- An opt-in summary sent four hours before each flight leg departs, so the
-- traveller gets the essentials (route, times, terminal and gate if known) in
-- one place shortly before they need them.
--
-- The preference lives on alert_prefs beside the check-in reminder setting
-- (0052), for the same reasons: per user, flight-specific, and delivered down
-- the same in_app / email channels that table already governs. It defaults to
-- FALSE so existing users are opted out until they ask for it.
ALTER TABLE alert_prefs ADD COLUMN departure_summary BOOLEAN NOT NULL DEFAULT FALSE;

-- Dedupe: one summary per flight part per user, ever. Same shape as
-- flight_checkin_sent (0052); the poller marks the pair only after it has tried
-- to deliver, so a crash mid-send re-sends rather than silently dropping.
CREATE TABLE flight_departure_summary_sent (
    plan_part_id BIGINT NOT NULL REFERENCES plan_parts(id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    sent_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (plan_part_id, user_id)
);
