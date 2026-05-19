-- Drop the unused aeroapi_id column. The original schema reserved it to
-- carry a FlightAware fa_flight_id from a planned AeroAPI integration; that
-- integration never landed and we now identify airframes via icao24 instead.
ALTER TABLE flights DROP COLUMN IF EXISTS aeroapi_id;
