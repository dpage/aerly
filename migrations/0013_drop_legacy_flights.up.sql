-- Wave 3: retire the legacy flight surface (spec §3.1, §7).
--
-- 0010 copied every legacy flights row into the plan model (plans +
-- plan_parts + flight_details, with positions re-pointed at plan_part_id and
-- passengers/shares migrated to plan_passengers / plan_visibility). The legacy
-- tables were intentionally left in place across Waves 1–2 so a pre-cut-over
-- rollback could still restore the old shape. Wave 3 drops them for good now
-- that the /api/flights CRUD surface and the flight_id-keyed store helpers are
-- gone.
--
-- Drop order respects the FKs: flight_passengers / flight_shares reference
-- flights(id). positions.flight_id still carries a column but its FK was
-- already dropped in 0010. Drop the dependent tables first, then the flights
-- table, then the now-orphaned positions column.

DROP TABLE IF EXISTS flight_passengers;
DROP TABLE IF EXISTS flight_shares;
DROP TABLE IF EXISTS flights;

-- positions_part_ts_idx was only RENAMED by 0010, never re-pointed: it is still
-- defined on (flight_id, ts DESC). Dropping the flight_id column would cascade
-- and silently drop it, leaving the part-keyed position lookups (which filter on
-- plan_part_id) with no supporting index. Re-create it on (plan_part_id, ts DESC)
-- so the poller/tracker queries stay indexed. Drop-then-recreate is simplest and
-- avoids relying on the cascade order.
DROP INDEX IF EXISTS positions_part_ts_idx;
ALTER TABLE positions DROP COLUMN IF EXISTS flight_id;
CREATE INDEX positions_part_ts_idx ON positions (plan_part_id, ts DESC);
