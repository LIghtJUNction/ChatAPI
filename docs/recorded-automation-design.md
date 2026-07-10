# Recorded Automation Design

## Scope

Recorded automation is a user-owned extension of the chat turn runtime. It records successful
workspace control actions, matches a newly waiting turn by its last user text, and replays those
actions through the same `chat/control` application service used by a person.

It does not construct provider protocol events, mutate pending state directly, or write chat
records directly.

## Ownership

- `internal/service/automation` owns rule validation, CRUD use cases, recording sessions,
  matching, scheduling, cancellation, and execution state.
- `internal/repository/automation` owns the narrow persistence contract for rule aggregates.
- `internal/service/chat/control` remains the sole application entry point for output actions.
- `internal/service/chat/events` publishes an immutable `turn.waiting` fact after the pending turn
  has been committed and registered.
- `internal/service/automation` publishes typed recording/execution state events.
- `internal/service/chat/workspace` adapts recording commands and projects automation state events
  onto the owner's WebSocket connections.
- `internal/service/usercontrol` exposes automation management without absorbing runtime logic.

## Rule aggregate

Each rule is independent and is updated atomically.

```text
Rule
  id, owner_id, name, enabled, priority
  match.target = last_user_text
  match.pattern = Go RE2 regular expression
  playback.mode = recorded | fixed
  playback.initial_delay_ms
  playback.fixed_interval_ms
	playback.loop
	playback.loop_interval_ms
  steps[]
    id
    delay_before_ms
    action
```

The action is the transport-independent output action already accepted by `chat/control`.
Rules with an empty pattern or no steps cannot be enabled. Multiple matching rules are sorted by
priority descending, updated time descending, and ID ascending; only the first match runs.

## Recording

1. The workspace sends `automation.record.start` for an active conversation.
2. The service binds the recording to the current owner, conversation ID, and request ID.
3. Successful user/workspace or app-API control commands are observed after execution.
4. Each accepted command becomes a step with the elapsed time since recording start or the
   previous accepted command.
5. Failed commands, automation-originated commands, and commands for another request are ignored.
6. `automation.record.stop` returns and persists a disabled draft rule. Terminal actions also stop
   recording after capturing themselves.
7. `automation.record.cancel` discards the in-memory recording.

Recording registration holds the pending turn mutation lock while it revalidates and binds the
request identity. A successful terminal control is recorded before stop-and-save; an unmanaged
terminal event such as network disconnect marks the recording and automatically persists the
captured steps as a disabled draft.

Explicit stop/cancel passes through the same per-conversation `chat/control` serialization barrier
as output commands. The resulting draft therefore contains every successful command before the
barrier, including a command whose repository mutation finished while the stop request was in
flight.

Recording sessions are process-local. A reconnect can query `automation.record.state`; a server
restart discards an unfinished recording because the associated pending request is disconnected.

## Playback

The automation service subscribes to `turn.waiting`. The event carries the authoritative owner,
request, response, conversation, protocol, model, and last user text; matching never scans pending
turns or message history.

For recorded timing, each step uses its captured `delay_before_ms`. For fixed timing, the first
step uses `initial_delay_ms` and subsequent steps use `fixed_interval_ms`.

Playback may enable `loop` with a `loop_interval_ms`. The first cycle keeps the normal recorded or
fixed initial delay. On later cycles, `loop_interval_ms` is the complete delay from the previous
cycle's last action to the next cycle's first action; the first step's captured/initial delay is not
added again. Looping is valid only for non-terminal step lists. It continues while the same request
is pending and stops through the normal cancellation authorities.

Before every step the runner verifies that the same request is still pending. It then invokes
`chat/control` with source `automation` and an expected request ID. The turn service verifies that
identity while holding the mutation lock, closing the check/mutation race. Completion, abort,
disconnect, replacement by a newer
request, rule disable/delete, or a successful manual command cancels remaining steps.

Execution admission is guarded by two process-local authorities:

- a manual-takeover tombstone keyed by `(conversation_id, request_id)` prevents a rule match that
  was already loading from registering after successful manual output;
- an owner-scoped rule generation invalidates rule snapshots loaded before a save, disable, or
  delete operation.

Registration checks both authorities while holding the automation service mutex. Existing
executions are canceled under the same authority, so cancellation applies both before and after an
execution becomes visible in the execution registry.

Before replacing the conversation's execution entry, admission also holds the pending turn
mutation mutex and revalidates owner plus request ID. A delayed match from an older request can
therefore neither register nor cancel the newer request's execution.

`chat/control` serializes commands per conversation. A successful manual command notifies the
automation observer and cancels queued playback before the control queue is released; failed
manual commands do not cancel playback. A queued automation command observes cancellation before
entering turn mutation. An already executing atomic mutation is allowed to finish.

Execution is best-effort and process-local. This matches the pending runtime lifetime: startup
already disconnects recovered pending requests, so durable job recovery would create invalid
responses.

## WebSocket contract

Client commands:

- `workspace.command { command_id, conversation_id, request_id, ...action }`
- `automation.record.start { command_id, conversation_id }`
- `automation.record.stop { command_id }`
- `automation.record.cancel { command_id }`
- `automation.record.get { command_id }`

Server messages:

- `automation.record.ack { command_id, state }`
- `automation.record.error { command_id, code, message }`
- `automation.record.state { state }`
- `automation.execution.state { execution }`

Only `start` requires a conversation because recording binds to an active request; the remaining
commands address the owner's single recording session. Every WebSocket connection requests `get`
after opening, even when no conversation is selected.

`automation.record.ack` also carries an authoritative `executions` snapshot for reconnect. Running
and terminal execution states are indexed by conversation and terminal states remain queryable for
ten minutes. Expiration publishes a revisioned `removed` execution state so connected clients
delete the terminal indicator without waiting for reconnect.

Recording and execution mutations share an owner-scoped monotonic revision. Realtime states carry
their mutation revision, and snapshot acknowledgements carry the revision of the atomic recording
plus executions snapshot. The frontend merges recording state and each conversation's execution
state by their own latest revisions; the snapshot revision is the barrier for removing entries that
no longer exist. This avoids both delayed-snapshot rollback and cross-domain event loss.

Recording state is owner-scoped and may be broadcast to multiple tabs. Ordinary output remains a
`workspace.command`; there is no second output protocol for automation.

Every user-facing output command carries the request ID observed by the client. WS acknowledgements
and errors echo it, and generic HTTP output endpoints require `request_id`. The turn mutation lock
compares that value with the active pending request, preventing delayed commands for request A from
being applied after the conversation has advanced to request B.

Execution identity is `(owner_id, conversation_id, request_id, rule_id)`. Rule disable/delete only
cancels executions belonging to the same owner and rule. Frontend execution state is indexed by
conversation rather than stored as one owner-wide value.

## Safety limits

- maximum 128 steps per rule
- maximum 512 characters per regular expression
- maximum 24 hours captured or configured delay per step
- maximum 24 hours total playback duration
- terminal actions must be the last step
- a rule may not contain unsupported control kinds
- request-bound `image_generation` assets are not recordable; supporting them requires a separate
  automation-owned output asset lifecycle

The rule editor covers every action accepted by `Recordable()`: delta, complete, one-shot respond,
tool-call completion, built-in web search, and abort. Image generation is intentionally absent from
the editor because its asset is bound to the original request. Tool-call arguments use the action's
`text`/`OutputText` field; `output` remains reserved for tool-result output. The editor keeps the
single terminal action at the end of the ordered step list.
