CREATE TABLE IF NOT EXISTS db_meta (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL,
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS schema_migrations (
	version TEXT PRIMARY KEY,
	name TEXT NOT NULL DEFAULT '',
	applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	checksum TEXT NOT NULL DEFAULT '',
	dirty INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS conversations (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	last_message_at TEXT NOT NULL,
	message_count INTEGER NOT NULL DEFAULT 0,
	last_message_preview TEXT NOT NULL DEFAULT '',
	last_user_text TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS messages (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	role TEXT NOT NULL,
	content TEXT NOT NULL,
	created_at TEXT NOT NULL,
	status TEXT,
	response_id TEXT,
	metadata_json TEXT NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_created_at
ON messages(conversation_id, created_at, id);

CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	username TEXT NOT NULL DEFAULT '',
	email TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'user',
	is_active INTEGER NOT NULL DEFAULT 1,
	local_admin INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	last_login_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username
ON users(username)
WHERE username <> '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email
ON users(email)
WHERE email <> '';

CREATE TABLE IF NOT EXISTS user_configs (
	user_id TEXT NOT NULL,
	key TEXT NOT NULL,
	value_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	PRIMARY KEY(user_id, key)
);

CREATE TABLE IF NOT EXISTS config (
	key TEXT PRIMARY KEY,
	value_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE TABLE IF NOT EXISTS user_api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL,
	key_ciphertext TEXT NOT NULL,
	key_prefix TEXT NOT NULL,
	model TEXT NOT NULL DEFAULT '',
	last_used_at TEXT,
	created_at TEXT NOT NULL,
	revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_api_keys_key_prefix
ON user_api_keys(key_prefix);

CREATE INDEX IF NOT EXISTS idx_user_api_keys_user_id
ON user_api_keys(user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS user_identities (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	provider TEXT NOT NULL,
	subject TEXT NOT NULL,
	email TEXT NOT NULL DEFAULT '',
	email_verified INTEGER NOT NULL DEFAULT 0,
	profile_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
	last_login_at TEXT,
	UNIQUE(provider, subject)
);

CREATE INDEX IF NOT EXISTS idx_user_identities_user_provider
ON user_identities(user_id, provider);

CREATE TABLE IF NOT EXISTS automation_rules (
	id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 1,
	rule_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	PRIMARY KEY(user_id, id)
);

CREATE INDEX IF NOT EXISTS idx_automation_rules_user_updated
ON automation_rules(user_id, updated_at DESC, id);

CREATE TABLE IF NOT EXISTS user_app_api_keys (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	name TEXT NOT NULL,
	key_hash TEXT NOT NULL,
	key_prefix TEXT NOT NULL,
	scopes_json TEXT NOT NULL DEFAULT '[]',
	resource_limits_json TEXT NOT NULL DEFAULT '{}',
	expires_at TEXT,
	last_used_at TEXT,
	created_at TEXT NOT NULL,
	revoked_at TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_app_api_keys_key_prefix
ON user_app_api_keys(key_prefix);

CREATE TABLE IF NOT EXISTS app_api_key_audit_logs (
	id TEXT PRIMARY KEY,
	app_api_key_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	route TEXT NOT NULL,
	status_code INTEGER NOT NULL,
	error_code TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
	id TEXT PRIMARY KEY,
	actor_user_id TEXT NOT NULL DEFAULT '',
	actor_role TEXT NOT NULL DEFAULT '',
	actor_source TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL,
	resource_type TEXT NOT NULL DEFAULT '',
	resource_id TEXT NOT NULL DEFAULT '',
	action TEXT NOT NULL,
	outcome TEXT NOT NULL,
	ip_address TEXT NOT NULL DEFAULT '',
	user_agent TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created
ON audit_logs(created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created
ON audit_logs(actor_user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_audit_logs_event_created
ON audit_logs(event_type, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS uploaded_images (
	id TEXT PRIMARY KEY,
	owner_id TEXT NOT NULL,
	filename TEXT NOT NULL,
	original_filename TEXT NOT NULL DEFAULT '',
	content_type TEXT NOT NULL,
	bytes INTEGER NOT NULL,
	url TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_uploaded_images_filename
ON uploaded_images(filename);

CREATE INDEX IF NOT EXISTS idx_uploaded_images_owner_created
ON uploaded_images(owner_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS storage_user_quotas (
	owner_id TEXT PRIMARY KEY,
	quota_bytes INTEGER NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS storage_file_deletion_failures (
	path TEXT PRIMARY KEY,
	filename TEXT NOT NULL DEFAULT '',
	owner_id TEXT NOT NULL DEFAULT '',
	bytes INTEGER NOT NULL DEFAULT 0,
	last_error TEXT NOT NULL DEFAULT '',
	attempts INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_storage_file_deletion_failures_updated
ON storage_file_deletion_failures(updated_at ASC, path ASC);
