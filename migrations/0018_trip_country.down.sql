-- Reverse 0018: drop the derived country code column.
ALTER TABLE trips DROP COLUMN IF EXISTS country_code;
