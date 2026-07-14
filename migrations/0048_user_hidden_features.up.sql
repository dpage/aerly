-- Per-user feature-hiding preferences: let a user hide features they don't use
-- (the Explore feature, and the map features) so the UI isn't cluttered with
-- affordances they never touch. Both default to FALSE, i.e. everything is shown
-- unless the user opts to hide it.
ALTER TABLE users
  ADD COLUMN hide_explore BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN hide_maps BOOLEAN NOT NULL DEFAULT FALSE;
