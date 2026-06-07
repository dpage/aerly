-- Per-booking supplier contact details, consistent across every plan type.
--
-- These sit on the plan (the booking), alongside confirmation_ref / ticket_number:
-- a booking is made with one supplier whose contact details apply regardless of
-- the plan type or how many legs (parts) it spans. supplier_name is the airline,
-- hotel, train operator, car-hire firm, restaurant, or tour operator the booking
-- was made with; contact_email / contact_phone are how to reach them about this
-- booking; website is their booking / management URL (shown as an open-in-new-tab
-- link in the UI). The email-ingest extractor fills them when the source states
-- them, the add/edit forms accept them, and the plan tile shows them. Empty means
-- "unknown". They complement — and never replace — the per-type detail fields
-- (e.g. a hotel's property phone or a train's operator), which describe the
-- specific service rather than who the booking is with.
ALTER TABLE plans ADD COLUMN supplier_name TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN contact_email TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN contact_phone TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN website       TEXT NOT NULL DEFAULT '';
