-- Migration 0056: departure summaries on by default.
--
-- 0054 shipped the departure summary opt-in, and in its first ten days nobody
-- found the setting, so it now defaults on for new rows and is switched on for
-- every existing one. No row had it on when this was written, so no user
-- loses a choice they made; users without a row pick up the matching default
-- in dueFlightNotices and defaultAlertPrefs.
ALTER TABLE alert_prefs ALTER COLUMN departure_summary SET DEFAULT TRUE;

UPDATE alert_prefs SET departure_summary = TRUE WHERE NOT departure_summary;
