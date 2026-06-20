-- Migration 0046: add 'meeting' and 'event' plan types.
--
-- Meeting: useful for volunteer stand-ups, org meetings, committee sessions —
--   any group event at a fixed venue with an optional organiser contact.
-- Event: a general ticketed/attended happening — conference talks, concerts,
--   cinema, theatre, sports matches, etc.
--
-- The plan type is stored on the plans table only (not plan_parts). Migration
-- 0043 already renamed the constraint to plans_type_check and added ice_cream;
-- we drop and recreate it here to add meeting and event.

-- 1. Widen the CHECK constraint on plans.type.
ALTER TABLE plans DROP CONSTRAINT plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion','ice_cream','meeting','event')
);

-- 2. Satellite table for 'meeting' parts.
--    location = room / building / virtual link; organiser = person running it.
CREATE TABLE meeting_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    location      TEXT NOT NULL DEFAULT '',
    organiser     TEXT NOT NULL DEFAULT '',
    platform      TEXT NOT NULL DEFAULT ''   -- e.g. "Zoom", "Google Meet", ""
);

-- 3. Satellite table for 'event' parts.
--    performer / speaker / act; venue within venue (stage / room / track).
CREATE TABLE event_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    performer     TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT '',   -- e.g. "Concert", "Talk", "Cinema"
    venue_area    TEXT NOT NULL DEFAULT '',   -- stage, screen, track, room…
    url           TEXT NOT NULL DEFAULT ''
);
