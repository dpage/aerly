-- Reverse 0016: drop the per-part address columns.
ALTER TABLE plan_parts DROP COLUMN IF EXISTS start_address;
ALTER TABLE plan_parts DROP COLUMN IF EXISTS end_address;
