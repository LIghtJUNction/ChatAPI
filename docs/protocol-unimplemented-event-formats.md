# Protocol Event Formats Not Yet Implemented

This file records protocol surfaces that are intentionally not generated until their format is verified through a real endpoint, an official SDK/docs reference, or a compatible implementation.

## Current Policy

- Do not invent stream events for client abort/disconnect.
- Responses abort/disconnect uses `response.failed`.
- Chat Completions abort/disconnect closes the stream without a synthetic chunk.
- Anthropic Messages abort/disconnect uses official `event: error`.
- Anthropic Messages local runtime does not generate official thinking blocks, because
  Anthropic `thinking` requires a provider-generated signature.
- Request fields that do not drive local behavior are preserved in request debug data and surfaced as option chips.

## Verified And Implemented

Verified with `tests/protocol_deep_probe.py` against the endpoint in `backend/docs/1.txt`:

- Responses text lifecycle:
  - `response.created`
  - `response.in_progress`
  - `response.output_item.added`
  - `response.content_part.added`
  - `response.output_text.delta`
  - `response.output_text.done`
  - `response.content_part.done`
  - `response.output_item.done`
  - `response.completed`
- Responses function call lifecycle:
  - `response.output_item.added` with `item.type=function_call`
  - `response.function_call_arguments.delta`
  - `response.function_call_arguments.done`
  - `response.output_item.done`
  - `response.completed`
- Responses failure:
  - `response.failed`
- Chat Completions text:
  - role chunk
  - `choices[].delta.content`
  - finish chunk
  - `[DONE]`
- Chat Completions tool call:
  - `choices[].delta.tool_calls[]`
  - `finish_reason=tool_calls`
  - `[DONE]`
- Anthropic Messages text:
  - `message_start`
  - optional `ping`
  - `content_block_start`
  - `content_block_delta` with `text_delta`
  - `content_block_stop`
  - `message_delta`
  - `message_stop`
- Anthropic Messages tool call:
  - `content_block_start` with `tool_use`
  - `content_block_delta` with `input_json_delta`
  - `content_block_stop`
  - `message_delta` with `stop_reason=tool_use`
  - `message_stop`
- Anthropic Messages upstream thinking shape:
  - `content_block_start` with `thinking`
  - `content_block_delta` with `thinking_delta`
  - `content_block_delta` with `signature_delta`
  - `content_block_stop`
  - subsequent text block

## Verified But Not Generated

### Anthropic `thinking_delta` / `signature_delta`

The official Anthropic SDK models final `thinking` content with a required `signature` field, and real streams emit `content_block_delta.delta.type=signature_delta` after `thinking_delta`. The value is provider-generated. ChatAPI cannot generate a valid signature locally.

For local manual output, ChatAPI does not generate Anthropic `thinking` blocks at all. Thinking text can still be represented in ChatAPI workspace timeline or in Responses reasoning events, but it is not exposed as Anthropic Messages `thinking_delta`.

If this backend later proxies an upstream Anthropic stream, it should forward the upstream `signature_delta` unchanged.

## Known But Not Yet Verified Through This Backend Probe

These event families are visible in SDK types or compatibility code, but are not implemented by the local manual runtime yet:

- Responses refusal:
  - `response.refusal.delta`
  - `response.refusal.done`
- Responses incomplete/cancelled:
  - `response.incomplete`
  - `response.cancelled` exists as a webhook event and constant in openai-go, but is not present in the Responses stream union in openai-go v1.12.0. The local abort policy remains `response.failed`.
- Responses annotations:
  - `response.output_text.annotation.added`
- Responses audio:
  - `response.audio.delta`
  - `response.audio.done`
  - `response.audio.transcript.delta`
  - `response.audio.transcript.done`
- Responses web search:
  - `response.web_search_call.in_progress`
  - `response.web_search_call.searching`
  - `response.web_search_call.completed`
- Responses file search:
  - `response.file_search_call.in_progress`
  - `response.file_search_call.searching`
  - `response.file_search_call.completed`
- Responses code interpreter:
  - `response.code_interpreter_call.in_progress`
  - `response.code_interpreter_call.interpreting`
  - `response.code_interpreter_call.completed`
  - `response.code_interpreter_call_code.delta`
  - `response.code_interpreter_call_code.done`
- Responses image generation:
  - `response.image_generation_call.in_progress`
  - `response.image_generation_call.generating`
  - `response.image_generation_call.partial_image`
  - `response.image_generation_call.completed`
- Responses MCP:
  - `response.mcp_call_arguments.delta`
  - `response.mcp_call_arguments.done`
  - `response.mcp_call.in_progress`
  - `response.mcp_call.completed`
  - `response.mcp_call.failed`
  - `response.mcp_list_tools.in_progress`
  - `response.mcp_list_tools.completed`
  - `response.mcp_list_tools.failed`

## Intentionally Out Of Scope For Local Runtime

These events have SDK-visible shapes, but ChatAPI currently has no corresponding local execution subsystem and should not fake them:

- Responses built-in web search events.
- Responses built-in file search events.
- Responses code interpreter events.
- Responses image generation events.
- Responses MCP events.
- Responses audio output / audio transcript events.

The request fields for these features should still be preserved and displayed as debug chips when present.

## Not Implemented Because There Is No Stable Official Local Output Shape

### Chat Completions reasoning stream

Some providers expose `reasoning_content` or similar fields in Chat Completions compatible streams, but this is not a stable official OpenAI Chat Completions event shape. The local runtime does not emit reasoning deltas for Chat Completions. Request fields such as `reasoning_effort` are preserved and surfaced as chips.

### Chat Completions abort event

No official SSE chunk shape was verified for local abort/disconnect. The local runtime closes the stream.

## Probe Command

Use this command to refresh current real-endpoint samples:

```bash
tests/.venv/bin/python tests/protocol_deep_probe.py \
  --case deep_responses_forced_tool \
  --case deep_responses_reasoning \
  --case deep_chat_forced_tool \
  --case deep_messages_forced_tool \
  --case deep_messages_thinking
```

Probe logs are local-only and ignored by git:

- `tests/.sse-probe-logs/`
- `tests/.protocol-stream-probe-results.json`
