-- Reverse of 0051. Any existing vehicle_hire plans are reclassified to
-- 'ground' (where they would have landed before this migration) so the
-- narrowed constraint can be applied without deleting user data. The
-- satellite table is dropped first so no orphan detail rows are left
-- attached to plans that are about to be reclassified.

DROP TABLE IF EXISTS vehicle_hire_details;

UPDATE plans SET type = 'ground' WHERE type = 'vehicle_hire';

ALTER TABLE plans DROP CONSTRAINT plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion',
           'ice_cream','meeting','event')
);
