-- Drop the SPF audit column. SPF support was removed from the ingest path:
-- forwarded mail arrives with a different envelope sender from the booking's
-- original domain, so SPF alignment against the From header never holds for
-- legitimate forwards. DKIM (which survives forwarding because the signing
-- domain aligns with From) remains the sole sender-authentication check.
ALTER TABLE email_ingests DROP COLUMN IF EXISTS spf_pass;
