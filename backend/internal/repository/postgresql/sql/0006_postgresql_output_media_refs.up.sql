CREATE TABLE IF NOT EXISTS media_assets (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL,
    file_id TEXT NOT NULL,
    path TEXT NOT NULL,
    media_type TEXT NOT NULL,
    bytes BIGINT NOT NULL,
    sha256 TEXT NOT NULL,
    width INTEGER NOT NULL DEFAULT 0,
    height INTEGER NOT NULL DEFAULT 0,
    source_kind TEXT NOT NULL DEFAULT '',
    original_name TEXT NOT NULL DEFAULT '',
    original_media_type TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_media_assets_file_id ON media_assets(file_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_assets_path ON media_assets(path);
CREATE INDEX IF NOT EXISTS idx_media_assets_owner_created ON media_assets(owner_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS media_asset_refs (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL REFERENCES media_assets(id) ON DELETE CASCADE,
    file_id TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    input_part_index INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_media_asset_refs_asset_id ON media_asset_refs(asset_id);
CREATE INDEX IF NOT EXISTS idx_media_asset_refs_conversation ON media_asset_refs(conversation_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_media_asset_refs_message ON media_asset_refs(message_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_media_asset_refs_owner ON media_asset_refs(owner_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS media_asset_staging (
    asset_id TEXT PRIMARY KEY REFERENCES media_assets(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL,
    conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    request_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_media_asset_staging_conversation
ON media_asset_staging(conversation_id, request_id, created_at DESC);

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
    created_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_media_asset_event_refs_asset ON media_asset_event_refs(asset_id);
CREATE INDEX IF NOT EXISTS idx_media_asset_event_refs_conversation ON media_asset_event_refs(conversation_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_media_asset_event_refs_event ON media_asset_event_refs(event_id, part_index, id);
