-- Generic per-part departure/arrival postal addresses (free text).
--
-- Parts already carry short place labels (start_label/end_label) and
-- coordinates (start_lat/lon, end_lat/lon). Addresses sit alongside: the LLM
-- fills them on ingest (inferring well-known ones such as airport terminals),
-- the manual add form accepts them, and the geocoder turns a free-text address
-- into start/end coordinates for map markers. Empty means "unknown".
ALTER TABLE plan_parts ADD COLUMN start_address TEXT NOT NULL DEFAULT '';
ALTER TABLE plan_parts ADD COLUMN end_address   TEXT NOT NULL DEFAULT '';
