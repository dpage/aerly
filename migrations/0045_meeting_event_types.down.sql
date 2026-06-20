-- Rollback migration 0045: remove 'meeting' and 'event' plan types.

DROP TABLE IF EXISTS event_details;
DROP TABLE IF EXISTS meeting_details;

-- Restore the narrow CHECK constraints (will fail if any rows use the new
-- types — that is intentional; run with caution after purging test data).
ALTER TABLE plan_parts DROP CONSTRAINT IF EXISTS plan_parts_type_check;
ALTER TABLE plan_parts ADD CONSTRAINT plan_parts_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion','ice_cream')
);

ALTER TABLE plans DROP CONSTRAINT IF EXISTS plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion','ice_cream')
);
