-- Give user_emails an explicit "primary" address, i.e. the one login/account
-- email that user-facing notifications (share alerts, friend requests) should
-- be sent to. Previously there was no such concept: the notification code just
-- took the newest verified address, so adding and verifying a secondary email
-- silently redirected notifications away from the user's login email.
ALTER TABLE user_emails ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT FALSE;

-- Backfill: for each user, treat the OLDEST verified address as primary. That
-- is the login email in the common case (it's written first, on first sign-in);
-- secondary addresses are added later and so have newer created_at.
UPDATE user_emails ue
SET is_primary = TRUE
FROM (
    SELECT DISTINCT ON (user_id) id
    FROM user_emails
    WHERE verified
    ORDER BY user_id, created_at ASC, id ASC
) first
WHERE ue.id = first.id;

-- At most one primary per user.
CREATE UNIQUE INDEX user_emails_one_primary
    ON user_emails (user_id) WHERE is_primary;
