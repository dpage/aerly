-- Per-booking e-ticket number and cost (issue #22).
--
-- These sit on the plan (the booking), alongside confirmation_ref: a booking
-- has one ticket number and one total price regardless of how many legs
-- (parts) it spans. ticket_number holds the airline e-ticket / rail ticket
-- number when known; cost_amount + cost_currency hold the booking total as a
-- decimal amount plus an ISO 4217 currency code (e.g. 'GBP'). The email-ingest
-- extractor fills them when the source states them, the add/edit forms accept
-- them, and the plan tile shows them. Empty / NULL means "unknown".
ALTER TABLE plans ADD COLUMN ticket_number TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN cost_amount   NUMERIC(12,2);
ALTER TABLE plans ADD COLUMN cost_currency TEXT NOT NULL DEFAULT '';
