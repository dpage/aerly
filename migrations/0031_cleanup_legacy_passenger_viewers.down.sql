-- Irreversible data cleanup: the removed trip_members rows were legacy trigger
-- artifacts and are not restored on rollback (recreating them would re-introduce
-- the over-share this migration fixed). No-op.
SELECT 1;
