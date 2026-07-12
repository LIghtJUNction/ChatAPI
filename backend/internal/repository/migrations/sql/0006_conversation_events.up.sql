CREATE TABLE IF NOT EXISTS conversation_events (
	id TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	owner_id TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL,
	level TEXT NOT NULL DEFAULT 'info',
	title TEXT NOT NULL,
	detail TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT '',
	metadata_json TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversation_events_conversation_created
ON conversation_events(conversation_id, created_at ASC, id ASC);

CREATE INDEX IF NOT EXISTS idx_conversation_events_owner_created
ON conversation_events(owner_id, created_at DESC, id DESC);
