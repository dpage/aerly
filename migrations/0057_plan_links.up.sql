-- Per-plan links (issue #109).
--
-- A plan can carry any number of title/URL pairs pointing at things kept
-- elsewhere: an e-ticket, a boarding pass, a booking page. Aerly stores only the
-- link, never the target. position keeps the order the user entered them in; the
-- set is replaced wholesale whenever the plan is saved, so there is no need for
-- the positions to stay dense between saves.
CREATE TABLE plan_links (
    id         BIGSERIAL PRIMARY KEY,
    plan_id    BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    position   INT    NOT NULL,
    title      TEXT   NOT NULL,
    url        TEXT   NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX plan_links_plan_idx ON plan_links (plan_id, position);
