# Workspace WS And Stream Egress Design

## Scope

This document closes two still-mixed boundaries:

1. Workspace realtime protocol between browser and backend.
2. Model protocol stream egress between HTTP handler and normalized chat turn flow.

The goal is to make both boundaries explicit, stable, and testable.

## 1. Workspace Realtime Protocol

### Module Boundary

- `internal/service/chat/workspace`
  - owns workspace websocket protocol semantics
  - owns owner-wide conversation summary events
  - owns conversation timeline subscription/reset/append semantics
  - owns per-connection subscription state
- `internal/http/handler/workspace_ws.go`
  - only upgrades websocket
  - restores authenticated owner from context
  - passes incoming client frames to workspace service
  - does not implement workspace business rules itself

### Protocol Shape

The workspace websocket is snapshot-first and increment-only afterward.

#### Server -> client events

- `workspace.snapshot`
  - sent once immediately after connect
  - contains conversation summaries only
- `workspace.connections`
  - owner-wide connection count update
- `conversation.upsert`
  - owner-wide conversation summary upsert
  - no timeline payload
- `conversation.remove`
  - owner-wide conversation removal
- `timeline.reset`
  - authoritative full timeline payload for one conversation
  - sent after `timeline.subscribe`
  - can also be used after reconnect/resync
- `timeline.append`
  - incremental append for one conversation
  - only sent to connections subscribed to that conversation

#### Client -> server events

- `workspace.ping`
  - keepalive/debug
- `timeline.subscribe`
  - select one conversation timeline for authoritative reset + future appends
- `timeline.unsubscribe`
  - stop receiving incremental timeline updates for that conversation

### Subscription Semantics

- Conversation summaries are owner-wide and always broadcast to all of the owner's connections.
- Timeline items are not owner-wide broadcast.
- A connection only receives `timeline.append` for conversations it explicitly subscribed to.
- The workspace frontend subscribes the currently selected conversation.

### Consistency Rule

`timeline.subscribe` must not race with `timeline.append` on the same connection.

The required behavior is:

1. serialize connection writes
2. while that serialization lock is held:
   - install subscription
   - load timeline snapshot
   - send `timeline.reset`
3. release the lock

This guarantees:

- no append interleaves ahead of the reset on the same socket
- no append is lost between subscribe and reset
- an append may be duplicated if it committed during subscribe/reset construction

Therefore the client must deduplicate `timeline.append` by timeline item id.

### HTTP Timeline API

`/api/conversations/{id}/timeline` remains as a query API for admin/debug/external use.

It is no longer part of the workspace UI runtime path.

The workspace UI must cold-load and stay in sync through websocket only.

## 2. Protocol Stream Egress Boundary

### Module Boundary

- `internal/service/chat/ingress`
  - parse protocol request into normalized request
  - submit normalized request into turn service
- `internal/service/chat/streaming`
  - owns protocol stream event mapping
  - owns SSE framing/writing
  - owns per-stream protocol state such as Anthropic block state
- `internal/service/chat/egress`
  - owns non-stream response/error body generation
- `internal/http/handler/chatapi.go`
  - parses HTTP input
  - delegates to ingress
  - delegates stream writing to streaming service
  - does not write SSE frames directly
  - does not import protocol stream frame types directly

### Stream Flow

For streaming requests:

1. handler parses JSON body
2. handler calls ingress parse
3. handler calls ingress submit stream
4. handler hands `(response writer, conversation, pending turn event channel)` to `streaming`
5. `streaming` writes:
   - protocol start frames
   - mapped pending delta/complete/abort frames
   - SSE wire format

### Responsibility Rule

The HTTP handler must not:

- build protocol stream payloads
- know Anthropic block lifecycle details
- write raw `event:` / `data:` SSE lines

Those all belong to `service/chat/streaming`.

## 3. Frontend Workspace Behavior

### Runtime Shape

- connect `/api/ws`
- wait for `workspace.snapshot`
- resolve selected conversation
- send `timeline.subscribe`
- use `timeline.reset` as authoritative selected timeline
- apply `timeline.append` incrementally
- apply `conversation.upsert` / `conversation.remove` to sidebar summaries

### Client Cache Rule

- conversation list cache and timeline cache are separate
- conversation list is driven by snapshot/upsert/remove
- selected conversation timeline is driven by reset/append
- no HTTP fallback fetch is used by workspace UI

## 4. Non-goals

This change does not introduce:

- server-side full list resend on every update
- multi-conversation timeline streaming by default
- transport-independent realtime abstraction outside websocket
- protocol normalization redesign

## 5. Expected Outcomes

After this change:

- workspace realtime semantics are explicit and stable
- conversation sidebar and selected timeline no longer rely on mixed HTTP + WS paths
- chat protocol handler becomes transport-thin again
- streaming wire details are isolated in one service
