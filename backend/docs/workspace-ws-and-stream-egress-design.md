# Workspace WS And Submit Boundary Design

## Scope

This document closes two boundary problems that were still half-transitioned:

1. workspace realtime protocol and workspace UI state authority;
2. request prepare/materialize responsibilities in chat turn submit.

The design goal is to make the workspace runtime explicitly websocket-driven, keep
repository models from leaking directly into the UI protocol, and move media
prepare/materialize concerns behind a dedicated boundary.

## 1. Workspace Protocol Boundary

### Module ownership

- `internal/service/chat/workspace`
  - owns websocket protocol semantics
  - owns workspace-facing summary/timeline DTOs
  - owns conversation subscription rules
  - owns workspace command dispatch semantics
- `internal/http/handler/workspace_ws.go`
  - upgrades websocket
  - restores authenticated owner/actor from context
  - forwards parsed client frames to workspace service
  - does not implement workspace business rules
- `internal/service/chat/turn`
  - remains owner of turn mutation rules
  - does not define workspace protocol frames

### Authority rule

For the workspace UI:

- websocket is authoritative for conversation summaries;
- websocket is authoritative for selected conversation timeline;
- websocket is authoritative for output control commands (`delta`, `complete`,
  `abort`);
- HTTP endpoints remain for debug/admin/external callers, but the workspace UI
  does not use them to keep itself in sync.

This removes the previous three-way authority split across:

- HTTP response payloads,
- websocket incremental events,
- frontend local state patching.

## 2. Workspace Server DTOs

Repository `Conversation.Metadata` and raw timeline items must not be the
workspace protocol.

### Conversation summary DTO

The workspace sidebar receives `WorkspaceConversationSummary`.

It contains:

- stable conversation identity and timestamps;
- preview/title/count fields needed by the sidebar;
- explicit request/runtime fields:
  - `request_format`
  - `status`
  - `draft_text`

No frontend code should read `conversation.metadata.realtime_status`,
`realtime_draft_text`, or `request_format` directly.

Those stay internal persistence details.

### Runtime summary authority

The authoritative service-side projection is:

- `internal/service/chat/conversationstate`

It is the only service package allowed to interpret the persistence metadata
keys that currently back runtime state:

- `owner_id`
- `request_format`
- `model`
- `realtime_status`
- `realtime_draft_text`

Repository metadata may continue storing these values until a schema migration
promotes them to columns, but service/frontend-facing code must consume typed
projection fields instead of re-reading metadata keys.

The intended dependency direction is:

- repository returns persisted rows;
- `conversationstate` projects typed runtime state and workspace summaries;
- workspace, turn control, egress, streaming, resolve, and user control consume
  the typed projection.

This keeps persistence shape from becoming the implicit websocket protocol.

### Timeline entry DTO

The workspace timeline receives `WorkspaceTimelineItem`.

It contains:

- `message` entries;
- `system_event` entries.

For message entries, the workspace DTO includes parsed `content_parts`, so the
frontend no longer reparses `message.content` to rediscover images/thinking/text.

`message.content` remains available as raw debug/source content, but workspace UI
rendering prefers `content_parts`.

### Visible timeline rule

Workspace timeline is already a presentation-oriented protocol.

Therefore:

- workspace service projects raw chat messages/events into workspace-facing
  timeline entries;
- frontend does not own a second semantic layer that interprets raw message JSON
  into visible content.

The only client-side synthetic item that remains is the draft tail, which is a
projection of summary `draft_text`.

## 3. Workspace WS protocol

### Server -> client

- `workspace.snapshot`
  - sent immediately after connect
  - contains authoritative conversation summaries
- `workspace.connections`
  - current owner connection count
- `conversation.upsert`
  - summary upsert for one conversation
- `conversation.remove`
  - summary removal
- `timeline.reset`
  - authoritative timeline snapshot for one subscribed conversation
- `timeline.append`
  - incremental timeline append for one subscribed conversation
  - contains only a timeline item, not a conversation summary
- `workspace.command_ack`
  - command accepted and executed
- `workspace.command_error`
  - command rejected or failed

### Client -> server

- `workspace.ping`
- `timeline.subscribe`
- `timeline.unsubscribe`
- `workspace.command`

### Command model

`workspace.command` is the workspace control boundary.

It carries:

- `command_id`
- `command.kind`
  - `stream_delta`
  - `stream_complete`
  - `abort`
- `command.conversation_id`
- optional payload fields:
  - `text`
  - `mode`
  - `tool_name`
  - `tool_call_id`
  - `output`
  - `reasoning_stream_mode`
  - `error`

Execution rule:

1. frontend sends `workspace.command`;
2. backend validates and executes through turn service;
3. backend replies with `workspace.command_ack` or `workspace.command_error`;
4. actual state convergence still comes from `conversation.upsert` and
   `timeline.append`.

So acknowledgements are control-plane only, not state-plane payloads.

### Turn control authority

All turn control entry points must call:

- `internal/service/chat/control.Service.Execute`

Adapters may still exist for compatibility:

- workspace websocket command frames;
- `/api/chat/output/*` debug HTTP endpoints;
- user/admin abort endpoints.

Those adapters only map transport input/output. They do not perform owner
admission, pending-state interpretation, or protocol error mapping themselves.
The control service owns:

- command validation;
- actor/owner admission;
- dispatch to turn mutation service;
- transport-neutral result/error shape.

The turn mutation service remains responsible for the actual state mutation,
pending registry updates, egress bodies, and realtime publications.

## 4. Timeline publish rule

Realtime append and query/reset must use the same timeline-item builder.

That means:

- query-side reset does not rebuild timeline DTOs one way;
- realtime append does not rebuild them another way.

There is one builder for:

- raw message -> timeline item
- raw event -> timeline item
- timeline item -> workspace timeline DTO

## 5. Submit prepare/materialize boundary

`turn.Submitter` must not directly own all of:

- preprocess;
- media persistence;
- media rollback cleanup;
- request-body rebuilding.

Those concerns are moved behind a dedicated materialization boundary.

### Materializer responsibility

`RequestMaterializer` owns:

1. preprocess normalized request;
2. persist prepared media drafts;
3. rewrite request image references to public media ids/urls;
4. build final request debug body;
5. compensate persisted files on failure;
6. record file deletion failures if compensation fails.

### Submitter responsibility

`Submitter` then becomes smaller:

1. resolve ids;
2. call materializer;
3. persist pending turn;
4. register in-memory pending turn;
5. publish conversation/timeline realtime events.

## 6. Media public reference boundary

Workspace/user-visible media references must not encode owner partitioning in
the public path shape.

Public references are now:

- `/api/media/assets/{file_id}`

Authorization still validates asset owner from the authenticated principal and
repository lookup.

This keeps:

- storage partitioning;
- authorization;
- delivery URL shape

as separate concerns.

## 7. Expected outcomes

After this refactor:

- workspace protocol owns workspace summary/timeline/control semantics;
- frontend workspace runtime no longer mixes HTTP control responses with WS state;
- repository metadata stops acting as implicit UI protocol;
- timeline query and realtime append use one item builder;
- submit prepare/materialize complexity is moved out of `turn.Submitter`;
- media public URLs stop leaking owner partition shape.
