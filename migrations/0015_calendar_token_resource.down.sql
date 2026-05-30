-- Reverse 0015: collapse back to the (user_id, scope) primary key. Any extra
-- per-resource rows beyond one-per-scope would violate the old PK, so keep only
-- the most recent token per (user_id, scope) before restoring the constraint.

DELETE FROM calendar_tokens a
    USING calendar_tokens b
    WHERE a.user_id = b.user_id
      AND a.scope = b.scope
      AND (a.created_at, a.resource_id) < (b.created_at, b.resource_id);

ALTER TABLE calendar_tokens DROP CONSTRAINT calendar_tokens_pkey;
ALTER TABLE calendar_tokens
    ADD CONSTRAINT calendar_tokens_pkey PRIMARY KEY (user_id, scope);
ALTER TABLE calendar_tokens DROP COLUMN resource_id;
