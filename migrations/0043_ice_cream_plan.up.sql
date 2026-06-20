-- Ice cream plan type (issue: score our ice cream finds).
--
-- A single-location visit to an ice cream parlour or café. Like dining and
-- excursion it's a single-venue (non-linkable) type, but it carries its own
-- satellite: a 0–5 star rating and a free-text note of what was ordered, so a
-- find can be scored after the fact and shown on the tracker map with a special
-- cone marker.

-- Widen the plans.type CHECK to admit 'ice_cream'. The original inline check
-- from 0010 is named plans_type_check by Postgres.
ALTER TABLE plans DROP CONSTRAINT plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (type IN
    ('flight','train','hotel','ground','dining','excursion','ice_cream'));

CREATE TABLE ice_cream_details (
    plan_part_id BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    rating       SMALLINT NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5),
    what_ordered TEXT     NOT NULL DEFAULT ''
);
