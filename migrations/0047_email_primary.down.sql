DROP INDEX IF EXISTS user_emails_one_primary;
ALTER TABLE user_emails DROP COLUMN is_primary;
