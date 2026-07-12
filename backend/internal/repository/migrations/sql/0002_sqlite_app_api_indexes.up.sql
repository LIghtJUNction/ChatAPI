CREATE INDEX IF NOT EXISTS idx_user_app_api_keys_user_id
ON user_app_api_keys(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_app_api_key_audit_logs_user_created
ON app_api_key_audit_logs(user_id, created_at DESC);
