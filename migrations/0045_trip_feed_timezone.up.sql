-- Optional per-feed display timezone. Some iCal feeds emit event times in UTC
-- with no VTIMEZONE / X-WR-TIMEZONE, so we can't tell their real local zone.
-- This lets the user pick one (an IANA name) when adding the feed; it's used as
-- the fallback display zone for events that carry no zone of their own.
ALTER TABLE trip_feeds ADD COLUMN timezone TEXT NOT NULL DEFAULT '';
