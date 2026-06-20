-- Reverse 0043: drop the ice_cream satellite and narrow the type CHECK back.
-- Any ice_cream plans must go first, otherwise the tightened CHECK can't be
-- re-added (the ON DELETE CASCADE from plans clears their parts/details).
DROP TABLE IF EXISTS ice_cream_details;

DELETE FROM plans WHERE type = 'ice_cream';

ALTER TABLE plans DROP CONSTRAINT plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (type IN
    ('flight','train','hotel','ground','dining','excursion'));
