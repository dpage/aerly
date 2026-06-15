-- Record the SPF authentication result alongside DKIM for each processed
-- inbound message. The ingest path now evaluates (and can require) SPF as well
-- as DKIM, so the audit trail carries both. Existing rows predate the check and
-- backfill to false — we have no SPF record for them.
ALTER TABLE email_ingests ADD COLUMN spf_pass BOOLEAN NOT NULL DEFAULT FALSE;
