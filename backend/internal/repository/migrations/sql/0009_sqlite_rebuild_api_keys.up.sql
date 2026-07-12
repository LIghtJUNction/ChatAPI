DELETE FROM user_app_api_keys;
DELETE FROM user_api_keys;

ALTER TABLE user_app_api_keys ADD COLUMN key_ciphertext TEXT NOT NULL DEFAULT '';
