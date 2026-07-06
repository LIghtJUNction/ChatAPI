CREATE TABLE IF NOT EXISTS db_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	checksum TEXT NOT NULL DEFAULT '',
	dirty BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL DEFAULT '',
	email TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'user',
	is_active BOOLEAN NOT NULL DEFAULT true,
	local_admin BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_login_at TIMESTAMPTZ NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username_nonempty ON users(username) WHERE username <> '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_nonempty ON users(email) WHERE email <> '';

CREATE TABLE IF NOT EXISTS user_identities (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	provider TEXT NOT NULL,
	subject TEXT NOT NULL,
	email TEXT NOT NULL DEFAULT '',
	email_verified BOOLEAN NOT NULL DEFAULT false,
	profile_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_login_at TIMESTAMPTZ NULL,
	UNIQUE(provider, subject)
);
CREATE INDEX IF NOT EXISTS idx_user_identities_user_id ON user_identities(user_id);

CREATE TABLE IF NOT EXISTS config (
	key TEXT PRIMARY KEY,
	value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS conversations (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_message_at TIMESTAMPTZ NOT NULL,
	message_count INTEGER NOT NULL DEFAULT 0,
	last_message_preview TEXT NOT NULL DEFAULT '',
	last_user_text TEXT NOT NULL DEFAULT '',
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	status TEXT NULL,
	response_id TEXT NULL,
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX IF NOT EXISTS idx_messages_conversation_created_at ON messages(conversation_id, created_at, id);

CREATE TABLE IF NOT EXISTS user_configs (
	user_id TEXT NOT NULL,
	key TEXT NOT NULL,
	value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY(user_id, key)
);

CREATE TABLE IF NOT EXISTS automation_rules (
	id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	enabled BOOLEAN NOT NULL DEFAULT true,
	rule_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY(user_id, id)
);
CREATE INDEX IF NOT EXISTS idx_automation_rules_user_updated ON automation_rules(user_id, updated_at DESC, id);

CREATE TABLE IF NOT EXISTS user_app_api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	key_hash TEXT NOT NULL,
	key_prefix TEXT NOT NULL UNIQUE,
	scopes_json JSONB NOT NULL DEFAULT '[]'::jsonb,
	resource_limits_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	expires_at TIMESTAMPTZ NULL,
	last_used_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL,
	revoked_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_user_app_api_keys_user_id ON user_app_api_keys(user_id);

CREATE TABLE IF NOT EXISTS app_api_key_audit_logs (
	id TEXT PRIMARY KEY,
	app_api_key_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	route TEXT NOT NULL DEFAULT '',
	status_code INTEGER NOT NULL,
	error_code TEXT NOT NULL DEFAULT '',
	created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_app_api_key_audit_logs_user_created ON app_api_key_audit_logs(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS audit_logs (
	id TEXT PRIMARY KEY,
	actor_user_id TEXT NOT NULL DEFAULT '',
	actor_role TEXT NOT NULL DEFAULT '',
	actor_source TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL,
	resource_type TEXT NOT NULL DEFAULT '',
	resource_id TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	outcome TEXT NOT NULL DEFAULT 'success',
	ip_address TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created ON audit_logs(created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created ON audit_logs(actor_user_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event_created ON audit_logs(event_type, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS user_api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	key_ciphertext TEXT NOT NULL,
	key_prefix TEXT NOT NULL UNIQUE,
	model TEXT NOT NULL DEFAULT '',
	last_used_at TIMESTAMPTZ NULL,
	created_at TIMESTAMPTZ NOT NULL,
	revoked_at TIMESTAMPTZ NULL
);
CREATE INDEX IF NOT EXISTS idx_user_api_keys_user_id ON user_api_keys(user_id);

CREATE TABLE IF NOT EXISTS uploaded_images (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL,
	filename TEXT NOT NULL,
	original_filename TEXT NOT NULL DEFAULT '',
	content_type TEXT NOT NULL,
	bytes BIGINT NOT NULL,
	url TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_uploaded_images_filename ON uploaded_images(filename);
CREATE INDEX IF NOT EXISTS idx_uploaded_images_owner_created ON uploaded_images(owner_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS storage_user_quotas (
	owner_id TEXT PRIMARY KEY,
	quota_bytes BIGINT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS storage_file_deletion_failures (
	path TEXT PRIMARY KEY,
	filename TEXT NOT NULL DEFAULT '',
	owner_id TEXT NOT NULL DEFAULT '',
	bytes BIGINT NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_storage_file_deletion_failures_updated ON storage_file_deletion_failures(updated_at ASC, path ASC);
