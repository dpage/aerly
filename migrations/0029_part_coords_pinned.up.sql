-- Mark a plan part's coordinates as manually pinned, so a user-set location
-- (e.g. a Google Maps pin for a venue whose postal address geocodes to the
-- wrong nearby spot) is never silently overwritten by the geocoder.
--
-- Geocoded coordinates are best-effort and get re-derived whenever the address
-- changes (and by the startup backfill). A pinned endpoint opts out of all of
-- that: the edit handler and the backfill both skip geocoding it, keeping the
-- exact lat/lon the user entered. Clearing the override unpins it, which lets
-- the geocoder take over again. Existing rows default to false (geocoded).
ALTER TABLE plan_parts ADD COLUMN start_coords_pinned BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE plan_parts ADD COLUMN end_coords_pinned   BOOLEAN NOT NULL DEFAULT false;
