-- Reverse 0017: drop the home address column.
ALTER TABLE users DROP COLUMN IF EXISTS home_address;
