CREATE INDEX IF NOT EXISTS idx_messages_response_id_nonempty
ON messages(response_id)
WHERE response_id IS NOT NULL AND response_id <> '';

CREATE INDEX IF NOT EXISTS idx_messages_request_debug_request_id
ON messages ((metadata_json->'request_debug'->>'request_id'))
WHERE metadata_json->'request_debug'->>'request_id' IS NOT NULL;
