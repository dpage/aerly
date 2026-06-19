-- Re-add the SPF audit column. The historical per-message SPF results cannot
-- be recovered, so existing rows backfill to false (matching how 0035 itself
-- backfilled rows that predated the column).
ALTER TABLE email_ingests ADD COLUMN IF NOT EXISTS spf_pass BOOLEAN NOT NULL DEFAULT FALSE;
