-- Surface the aircraft type on flight tiles (flight-tiles-aircraft-gate).
--
-- AeroDataBox already returns the aircraft model (e.g. "Boeing 777-300ER") and
-- the resolver fetched it, but it was only ever folded into the owning plan's
-- free-text notes ("Airline · Model") — never modelled on flight_details, so
-- the expanded flight tile on the maps could not show it. Add a nullable column
-- the resolver backfill fills only-fill-empty, like terminal and the other
-- airframe metadata. NULL means "unknown / not yet resolved".
ALTER TABLE flight_details ADD COLUMN aircraft_type TEXT;
