CREATE TABLE IF NOT EXISTS media_asset_event_refs (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL,
    url TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    event_id TEXT NOT NULL REFERENCES conversation_events(id) ON DELETE CASCADE,
    purpose TEXT NOT NULL DEFAULT 'image_generation_result',
    part_index INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_asset_staging (
    asset_id TEXT PRIMARY KEY REFERENCES media_assets(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    request_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_media_asset_staging_conversation
ON media_asset_staging(conversation_id, request_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_media_asset_event_refs_asset
ON media_asset_event_refs(asset_id);

CREATE INDEX IF NOT EXISTS idx_media_asset_event_refs_conversation
ON media_asset_event_refs(conversation_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_media_asset_event_refs_event
ON media_asset_event_refs(event_id, part_index, id);
