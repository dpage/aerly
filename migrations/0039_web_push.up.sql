-- Web Push (PWA push notifications). Push is modelled as a delivery channel
-- that complements the in-app/SSE and email channels: it reaches a user's
-- device when Aerly is closed. The whole feature is dormant unless VAPID keys
-- are configured (see config.WebPushEnabled); these tables simply hold the
-- per-device subscriptions and the per-kind opt-out state.

-- One row per browser/device subscription. endpoint is the push-service URL the
-- browser handed us; it uniquely identifies a subscription, so it carries the
-- UNIQUE constraint and an upsert reassigns it (e.g. a different user signing in
-- on the same browser). p256dh/auth are the client's encryption keys. The
-- last_*/failure_count columns drive pruning: a subscription that returns 404/410
-- is deleted immediately, and one that keeps failing transiently is pruned once
-- failure_count crosses the package threshold.
CREATE TABLE webpush_subscriptions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint        TEXT   NOT NULL UNIQUE,
    p256dh          TEXT   NOT NULL,
    auth            TEXT   NOT NULL,
    user_agent      TEXT   NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_success_at TIMESTAMPTZ,
    last_failure_at TIMESTAMPTZ,
    failure_count   INTEGER NOT NULL DEFAULT 0
);
-- Index the FK target so per-user lookup (the send path) and user deletion's
-- referential-integrity check don't scan the whole table.
CREATE INDEX webpush_subscriptions_user_idx ON webpush_subscriptions (user_id);

-- Per-kind push opt-out. ABSENCE of a row means enabled, so granting permission
-- opts a user into every kind and they toggle individual kinds off. kinds today:
-- 'alert' (flight changes) and 'share' (a trip/plan shared with you).
CREATE TABLE push_kind_prefs (
    user_id BIGINT  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind    TEXT    NOT NULL,
    enabled BOOLEAN NOT NULL,
    PRIMARY KEY (user_id, kind)
);
