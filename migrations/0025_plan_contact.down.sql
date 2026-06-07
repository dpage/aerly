-- Reverse 0025: drop the per-booking supplier contact columns.
ALTER TABLE plans DROP COLUMN IF EXISTS supplier_name;
ALTER TABLE plans DROP COLUMN IF EXISTS contact_email;
ALTER TABLE plans DROP COLUMN IF EXISTS contact_phone;
ALTER TABLE plans DROP COLUMN IF EXISTS website;
