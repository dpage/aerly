-- Reverse 0027: drop the dest_baggage_belt column.
ALTER TABLE flight_details DROP COLUMN IF EXISTS dest_baggage_belt;
