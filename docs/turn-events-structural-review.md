# Chat Event Subscription Boundary

This document records the current structure of the chat event subscription refactor and the remaining boundary that is intentionally not solved in this round.

## Goal

Chat mutation services should publish domain facts. They should not know whether a fact is consumed by the workspace websocket UI, a future CLI listener, ntfy, webhook delivery, or another projection.

The target shape is:

- mutation service persists domain state
- mutation result contains the facts needed by the event source
- `internal/service/chat/events` carries domain payloads
- subscribers project those payloads into their own read model or transport format

## Current Boundary

### Event Contract

`internal/service/chat/events` defines the in-process event contract:

- `conversation.upserted`
- `conversation.deleted`
- `message.appended`
- `conversation_event.appended`

The public event payload is made of repository domain objects:

- `common.Conversation`
- `common.Message`
- `common.ConversationEvent`
- route fields such as `OwnerID` and `ConversationID`

It does not carry workspace websocket frames or `timeline.Item`. Timeline projection is a subscriber concern.

### Turn Service

`turn.Service` is the event source for turn lifecycle mutations.

`Submitter` only creates a pending turn and returns the mutation artifacts needed by the lifecycle service. It no longer owns realtime publishing or lifecycle event helpers.

`turn.Service` publishes:

- conversation upsert after submit, draft, complete, abort, and disconnect state changes
- message append after submit and complete
- conversation event append after abort and disconnect system events

Abort and disconnect use repository mutation results directly. They do not re-query the latest event or infer the just-written timeline item.

### Conversation Deletion

Conversation deletion events are driven from delete mutation results.

`common.DeleteConversationsResult` includes `DeletedConversationItems`, each with:

- `ID`
- `OwnerID`

SQLite and PostgreSQL implementations read the actual deleted conversation identities in the same transaction before deleting. This gives service layers a concrete mutation result instead of forcing them to guess owner routing information.

The following service entry points publish deletion events through `chatevents.PublishDeletedConversations`:

- user conversation delete/prune
- user config conversation delete helpers
- admin conversation delete

The router only wires the event publisher. It does not construct conversation deletion events.

### Workspace Realtime

Workspace realtime is a chat event subscriber.

`workspace.RealtimePublisher` implements `HandleChatEvent`, then projects domain events into websocket frames:

- `conversation.upsert`
- `conversation.remove`
- `timeline.append`

The workspace hub broadcast methods are package-private implementation details. External services do not call workspace realtime directly.

Timeline item identity rules are centralized in `internal/service/chat/timeline`:

- `ItemFromMessage`
- `ItemFromConversationEvent`

Both timeline reset/query and realtime append use the same projection rules.

### Owner Routing

Event routing uses explicit `OwnerID` on the event.

For turn lifecycle events, `turn.Service` builds a small route envelope from the authoritative source for that path:

- submit: pending turn created from the authenticated principal
- abort/disconnect: pending or stored turn identity
- draft/complete: request context with conversation fallback

Subscribers consume `event.OwnerID`; they do not re-derive owner from conversation metadata.

## Remaining Known Gap

The dispatcher is still synchronous in-process fan-out.

That is acceptable for the current workspace websocket projection, but it is not sufficient for external notification subscribers such as ntfy, webhook delivery, or a durable listener API.

Before adding external delivery subscribers, split subscriber semantics into:

- synchronous in-process projections for cheap read-model updates
- asynchronous delivery for external side effects, with queue/outbox, timeout, retry, and failure isolation

This remaining gap is intentionally not fixed in this round.

