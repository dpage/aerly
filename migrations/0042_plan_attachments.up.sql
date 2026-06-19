-- Per-plan file attachments (issue #91).
--
-- A plan (the booking) can carry uploaded files — a PDF ticket, a booking
-- confirmation, a scanned voucher. The file bytes live out-of-band in a
-- configured object store (an absolute filesystem path, or an S3 bucket); this
-- table holds only the metadata plus the opaque storage_key the store hands
-- back. The feature is gated entirely by server config (ATTACHMENTS_STORE): with
-- no store configured the upload endpoints 503 and the UI hides the affordance,
-- but the table is harmless when empty.
--
-- uploaded_by is SET NULL on user delete so an attachment outlives the account
-- that added it (the plan, and the booking it documents, are still relevant to
-- the rest of the trip). ON DELETE CASCADE from plans removes the rows when a
-- plan is deleted; the blob bytes are swept by the application before the row
-- goes (the DB can't reach the object store).
CREATE TABLE plan_attachments (
    id           BIGSERIAL PRIMARY KEY,
    plan_id      BIGINT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    uploaded_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    filename     TEXT   NOT NULL,
    content_type TEXT   NOT NULL DEFAULT '',
    size_bytes   BIGINT NOT NULL,
    storage_key  TEXT   NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX plan_attachments_plan_idx ON plan_attachments (plan_id);
