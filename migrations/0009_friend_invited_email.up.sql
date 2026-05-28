-- Outgoing pending friendships in the list endpoint must not confirm whether
-- the typed email belongs to a registered user. Storing the inviter-typed
-- email on the row lets the list endpoint render the row by email rather
-- than by friend_id (name/gravatar leaks via the users index).
--
-- New pending rows always get invited_email set by inviteFriendByUserID.
-- For legacy pending rows, backfill from the recipient's oldest verified
-- email — what the inviter would have seen rendered today. If a pending
-- row's recipient has no verified email (shouldn't happen, since the invite
-- path required one at creation time), drop the row outright; the inviter
-- can re-invite.

ALTER TABLE friendships
    ADD COLUMN invited_email TEXT;

UPDATE friendships f
SET invited_email = (
    SELECT ue.address
    FROM user_emails ue
    WHERE ue.user_id = CASE
        WHEN f.requested_by = f.user_low THEN f.user_high
        ELSE f.user_low
    END
      AND ue.verified
    ORDER BY ue.verified_at ASC NULLS LAST, ue.created_at ASC
    LIMIT 1
)
WHERE f.status = 'pending'
  AND f.invited_email IS NULL;

DELETE FROM friendships
WHERE status = 'pending' AND invited_email IS NULL;

ALTER TABLE friendships
    ADD CONSTRAINT friendships_pending_has_invited_email
    CHECK (status <> 'pending' OR invited_email IS NOT NULL);
