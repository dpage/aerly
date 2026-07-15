-- Pinned home location. home_address is a free-text field the LLM uses to
-- resolve "home" references, but geocoding that text can miss a rural address by
-- a few hundred metres. These optional coordinates let a user pin their home
-- exactly, so plans that resolve to home plot (and route) on the precise spot.
ALTER TABLE users
  ADD COLUMN home_lat double precision,
  ADD COLUMN home_lon double precision;
