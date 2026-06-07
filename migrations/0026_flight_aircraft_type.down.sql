-- Reverse 0026: drop the aircraft_type column.
ALTER TABLE flight_details DROP COLUMN IF EXISTS aircraft_type;
