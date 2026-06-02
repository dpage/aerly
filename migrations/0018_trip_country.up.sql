-- The trip's main country as a lowercase ISO 3166-1 alpha-2 code (e.g. 'es'),
-- derived by geocoding the destination and used to show a flag on the trip
-- card. Empty means "not yet derived"; the sentinel 'zz' means "looked up, no
-- country found" so we don't re-query forever.
ALTER TABLE trips ADD COLUMN country_code TEXT NOT NULL DEFAULT '';
