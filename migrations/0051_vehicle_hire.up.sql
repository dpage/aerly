-- Migration 0051: add the 'vehicle_hire' plan type.
--
-- Covers any self-drive rental: cars, vans, campervans, motorbikes, bicycles,
-- e-scooters. The dividing line against 'ground' is who drives: a taxi,
-- shuttle or chauffeured transfer stays 'ground', whilst anything the
-- traveller collects and returns themselves is 'vehicle_hire'.
--
-- A hire is a ranged booking with two endpoints in time and often two
-- different addresses, structurally closer to a hotel stay than to a cab
-- ride. It needs no new plan_parts columns: ends_at, end_tz, end_label,
-- end_address and end_lat/end_lon already exist.
--
-- The plan type is stored on the plans table only (not plan_parts). Migration
-- 0046 last recreated this constraint; we drop and recreate it here.

ALTER TABLE plans DROP CONSTRAINT plans_type_check;
ALTER TABLE plans ADD CONSTRAINT plans_type_check CHECK (
  type IN ('flight','train','hotel','ground','dining','excursion',
           'ice_cream','meeting','event','vehicle_hire')
);

-- Satellite for 'vehicle_hire' parts. The excess and deposit amounts are
-- nullable so "not stated" stays distinct from a genuine zero excess, and
-- each carries its own currency because a hire's excess is not always quoted
-- in the same currency as the booking total (which lives on
-- plans.cost_amount).
CREATE TABLE vehicle_hire_details (
    plan_part_id     BIGINT PRIMARY KEY REFERENCES plan_parts(id) ON DELETE CASCADE,
    category         TEXT NOT NULL DEFAULT '',   -- e.g. "Standard SUV"
    vehicle          TEXT NOT NULL DEFAULT '',   -- e.g. "Kia Sportage or similar"
    transmission     TEXT NOT NULL DEFAULT '',   -- "Automatic" | "Manual" | ""
    fuel_policy      TEXT NOT NULL DEFAULT '',   -- e.g. "Same to same"
    mileage          TEXT NOT NULL DEFAULT '',   -- e.g. "Unlimited"
    excess_amount    NUMERIC(12,2),
    excess_currency  TEXT NOT NULL DEFAULT '',
    deposit_amount   NUMERIC(12,2),
    deposit_currency TEXT NOT NULL DEFAULT ''
);
