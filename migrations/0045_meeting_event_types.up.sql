-- Migration 0045: add 'meeting' and 'event' plan types.
--
-- Meeting: useful for volunteer stand-ups, org meetings, committee sessions —
--   any group event at a fixed venue with an optional organiser contact.
-- Event: a general ticketed/attended happening — conference talks, concerts,
--   cinema, theatre, sports matches, etc. Replaces the narrower 'talk' concept.
--
-- We drop and recreate the CHECK constraint on plans.type (PostgreSQL does not
-- support ALTER CHECK); both the plans and plan_parts tables share the same
-- allowed set via the constraint defined on plans.type.

-- 1. Widen the CHECK constraint on plans.type.
ALTER TABLE plans DROP CONSTRAINT IF EXISTS plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion','ice_cream','meeting','event')
);

-- 2. Widen the CHECK constraint on plan_parts.type.
ALTER TABLE plan_parts DROP CONSTRAINT IF EXISTS plan_parts_type_check;
ALTER TABLE plan_parts ADD CONSTRAINT plan_parts_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion','ice_cream','meeting','event')
);

-- 3. Satellite table for 'meeting' parts.
--    location = room / building / virtual link; organiser = person running it.
CREATE TABLE meeting_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    location      TEXT NOT NULL DEFAULT '',
    organiser     TEXT NOT NULL DEFAULT '',
    platform      TEXT NOT NULL DEFAULT ''   -- e.g. "Zoom", "Google Meet", ""
);

-- 4. Satellite table for 'event' parts.
--    performer / speaker / act; venue within venue (stage / room / track).
CREATE TABLE event_details (
    plan_part_id  BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    performer     TEXT NOT NULL DEFAULT '',
    category      TEXT NOT NULL DEFAULT '',   -- e.g. "Concert", "Talk", "Cinema"
    venue_area    TEXT NOT NULL DEFAULT '',   -- stage, screen, track, room…
    url           TEXT NOT NULL DEFAULT ''
);
