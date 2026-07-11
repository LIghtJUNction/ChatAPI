CREATE INDEX IF NOT EXISTS idx_conversations_owner_updated
ON conversations(
  COALESCE(json_extract(metadata_json, '$.owner_id'), ''),
  updated_at DESC,
  id DESC
);
