-- Rollback migration 0046: remove 'meeting' and 'event' plan types.
--
-- Restore the CHECK constraint FIRST so that if any rows still use the new
-- types the constraint re-add fails before the satellite data is destroyed.
-- Restores to the state left by migration 0043 (ice_cream included).

ALTER TABLE plans DROP CONSTRAINT plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion','ice_cream')
);

-- Only drop satellite tables once the constraint check above has passed.
DROP TABLE IF EXISTS event_details;
DROP TABLE IF EXISTS meeting_details;
