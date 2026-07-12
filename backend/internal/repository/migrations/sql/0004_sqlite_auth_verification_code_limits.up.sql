ALTER TABLE auth_verification_codes
ADD COLUMN failed_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE auth_verification_codes
ADD COLUMN last_sent_at TEXT NOT NULL DEFAULT '';
