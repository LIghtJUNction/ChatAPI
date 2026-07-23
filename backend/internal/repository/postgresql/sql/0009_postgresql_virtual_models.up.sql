CREATE TABLE IF NOT EXISTS user_virtual_models (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE(user_id, name)
);
CREATE INDEX IF NOT EXISTS idx_user_virtual_models_user_id ON user_virtual_models(user_id, created_at DESC);

INSERT INTO user_virtual_models(id, user_id, name, created_at)
SELECT 'vmodel_' || md5(random()::text || clock_timestamp()::text || user_id || btrim(model)), user_id, btrim(model), MIN(created_at)
FROM user_api_keys
WHERE btrim(model) <> ''
GROUP BY user_id, btrim(model)
ON CONFLICT(user_id, name) DO NOTHING;

ALTER TABLE user_api_keys ADD COLUMN IF NOT EXISTS key_hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_user_api_keys_key_hash ON user_api_keys(key_hash) WHERE key_hash <> '';
