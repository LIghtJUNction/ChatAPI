ALTER TABLE user_app_api_keys ADD COLUMN IF NOT EXISTS key_ciphertext TEXT NOT NULL DEFAULT '';
