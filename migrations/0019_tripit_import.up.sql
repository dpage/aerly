-- TripIt import provenance. Storing the source TripIt trip id (on trips) and
-- the source event UID (on plans) makes re-importing the same .ics idempotent:
-- an existing trip is reused rather than duplicated, and plans whose UID is
-- already present are skipped. Empty string means "not from a TripIt import".
ALTER TABLE trips ADD COLUMN tripit_id  TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN tripit_uid TEXT NOT NULL DEFAULT '';
