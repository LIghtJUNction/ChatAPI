# Workspace conversation pagination

## Boundary

Conversation history pagination belongs to the workspace read model. The repository performs owner-scoped ordered queries; workspace owns opaque cursors and WS frames; the frontend only merges pages and requests the next cursor.

Realtime `conversation.upsert` and `conversation.remove` remain authoritative incremental mutations. Pagination only controls historical discovery and never replaces realtime updates.

Each new connection has an initialization barrier. Realtime frames produced after registration are queued until `workspace.snapshot` has been sent through the same serialized connection writer, then replayed in order. A historical page may add an unseen ID, but it must never overwrite an already loaded summary or restore an ID removed by realtime on that connection.

## Ordering and cursor

Rows are ordered by `(updated_at DESC, id DESC)`. A cursor encodes the last row's two ordering values. Queries use a strict keyset boundary, so realtime inserts and updates do not produce the offset drift associated with page numbers.

The cursor is opaque outside the workspace service. Invalid or mismatched cursors are rejected.

## WS frames

Initial server frame:

```json
{
  "type": "workspace.snapshot",
  "conversations": [],
  "has_more": true,
  "next_cursor": "opaque"
}
```

Client request:

```json
{
  "type": "conversation.page.get",
  "command_id": "page_1",
  "cursor": "opaque"
}
```

Server response:

```json
{
  "type": "conversation.page",
  "command_id": "page_1",
  "conversations": [],
  "has_more": false,
  "next_cursor": ""
}
```

The initial and subsequent page size is 30. The frontend deduplicates by conversation ID, then reapplies workspace ordering. Only one page request may be in flight per connection. Page requests time out after 15 seconds and become explicitly retryable.

## Failure and reconnect

Malformed cursors produce a workspace client-message error and do not alter loaded state. A reconnect replaces local discovery state with a new first page; later realtime upserts are merged normally. The selected conversation is chosen from the loaded first page, with the persisted selection used only when it is present there.

## Verification

- Repository queries return only one owner's rows in stable keyset order.
- Snapshot never exceeds 30 conversations.
- Consecutive pages contain no duplicates and expose all rows.
- Invalid cursors are rejected.
- Frontend requests one page near the list bottom and stops when `has_more=false`.
- Realtime upsert/remove remains correct while pages are loading.
