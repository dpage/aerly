-- Reverse of 0056. Only the column default goes back to FALSE: the UP switched
-- every row on and cannot tell which a user has since chosen, so stored
-- preferences are left as they are.
ALTER TABLE alert_prefs ALTER COLUMN departure_summary SET DEFAULT FALSE;
