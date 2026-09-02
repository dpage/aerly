-- Reverse of 0052. Dropping the column loses every user's check-in preference;
-- dropping the table loses the record of which reminders were sent, so a
-- re-applied 0052 would re-send for any flight still inside its window.
DROP TABLE IF EXISTS flight_checkin_sent;

ALTER TABLE alert_prefs DROP COLUMN IF EXISTS checkin;
