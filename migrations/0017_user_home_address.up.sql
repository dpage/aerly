-- A traveller's home address, used as context for ingest (so a confirmation
-- like "taxi from home to LHR T5" resolves "home" to a real address) and as a
-- default origin/marker. Empty means "not set".
ALTER TABLE users ADD COLUMN home_address TEXT NOT NULL DEFAULT '';
