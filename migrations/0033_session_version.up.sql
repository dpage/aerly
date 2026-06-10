-- session_version is the user's session epoch, embedded in every session cookie
-- issued for them. Bumping it invalidates all previously-issued sessions for
-- that user (the auth middleware rejects a cookie whose embedded version no
-- longer matches), giving stateless "sign out everywhere" / forced logout with
-- no server-side session store. Existing v1 cookies carry an implicit version 0,
-- which matches this default, so the column is backward-compatible.
ALTER TABLE users ADD COLUMN session_version INTEGER NOT NULL DEFAULT 0;
