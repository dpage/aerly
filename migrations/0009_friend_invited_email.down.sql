ALTER TABLE friendships
    DROP CONSTRAINT IF EXISTS friendships_pending_has_invited_email;

ALTER TABLE friendships
    DROP COLUMN IF EXISTS invited_email;
