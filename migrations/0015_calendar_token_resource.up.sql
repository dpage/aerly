-- Per-resource calendar token granularity. Previously calendar_tokens was keyed
-- (user_id, scope), so regenerating e.g. a "trip" token revoked EVERY trip feed
-- for that user at once. Re-key by (user_id, scope, resource_id) so each
-- trip/plan feed has its own independently-revocable token. resource_id is 0 for
-- the "me" scope (which has no resource).

ALTER TABLE calendar_tokens
    ADD COLUMN resource_id BIGINT NOT NULL DEFAULT 0;

ALTER TABLE calendar_tokens DROP CONSTRAINT calendar_tokens_pkey;
ALTER TABLE calendar_tokens
    ADD CONSTRAINT calendar_tokens_pkey PRIMARY KEY (user_id, scope, resource_id);
