-- Reverse of 0054. Dropping the column loses every user's departure-summary
-- preference; dropping the table loses the record of which summaries were sent,
-- so a re-applied 0054 would re-send for any flight still inside its window.
DROP TABLE IF EXISTS flight_departure_summary_sent;

ALTER TABLE alert_prefs DROP COLUMN IF EXISTS departure_summary;
