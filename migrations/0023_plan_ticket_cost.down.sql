-- Reverse 0023: drop the per-booking ticket number and cost columns.
ALTER TABLE plans DROP COLUMN IF EXISTS ticket_number;
ALTER TABLE plans DROP COLUMN IF EXISTS cost_amount;
ALTER TABLE plans DROP COLUMN IF EXISTS cost_currency;
