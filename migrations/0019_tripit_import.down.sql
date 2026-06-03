-- Reverse 0019: drop the TripIt import provenance columns.
ALTER TABLE plans DROP COLUMN IF EXISTS tripit_uid;
ALTER TABLE trips DROP COLUMN IF EXISTS tripit_id;
