-- ICAO radio callsign (e.g. "DLH493" for Lufthansa LH493) returned by the
-- resolver. Stored so the poller can match OpenSky state vectors by the
-- broadcasting aircraft's callsign as well as its icao24, defending
-- against same-airframe-different-sector mismatches when an airline reuses
-- a hex on multiple flights in a day. Nullable: not every resolver knows
-- the callsign, and far-future schedules don't have one assigned yet.
ALTER TABLE flights ADD COLUMN callsign TEXT;

-- Timestamp of the most recent resolver call for this flight, regardless
-- of outcome. Drives the poller's "late refresh" decision: AeroDataBox
-- only attaches a concrete airframe (modeS / reg) within ~24h of
-- departure, so we keep poking it as departure approaches even when the
-- row already has *some* metadata. NULL means "never resolved" (manual
-- entries fall in this bucket until first refresh).
ALTER TABLE flights ADD COLUMN last_resolved_at TIMESTAMPTZ;
