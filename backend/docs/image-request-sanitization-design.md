# Chat Media Submit Refactor

## Scope

This document defines the chat image/media path after the refactor that fixes:

1. media side effects happening before admission / conversation resolve;
2. service-layer dependence on `localstore.StoredAsset`;
3. duplicate owner derivation across handler and turn;
4. duplicated request debug representations;
5. `turn` depending directly on preprocess-local DTOs;
6. chat-media route naming still exposing old `uploads` semantics.

`uploaded_images` remains a separate legacy storage path in this round. Chat request media uses `media_assets` only.

## Target chain

1. HTTP handler decodes JSON and normalizes protocol request.
2. `turn.Service` derives submit principal once from request context.
3. `turn.Service` performs admission and conversation resolve first.
4. `Submitter` calls `preprocess` to:
   - parse inline image input
   - validate policy
   - transcode to AVIF
   - assign stable `file_id`
   - rewrite request image parts to owner-scoped public asset URLs
   - produce in-memory media drafts only
5. `Submitter` persists media drafts to storage.
6. `Submitter` builds sanitized request body from the rewritten normalized request.
7. `Submitter` writes pending request / conversation / message / media asset refs to repository.
8. On repository failure, persisted media files are deleted synchronously.

## Authoritative data

### Principal / owner

Authoritative source during request handling:

- `actor` context restored by auth middleware

`turn.Service` is the only place that resolves submit principal from context for chat submit.

### Request debug representation

Authoritative debug / replay representation:

- `request_debug.request_body`

Other fields are metadata only:

- `request_keys`
- `request_method`
- `request_path`
- `request_query`
- `request_headers`
- `tool_schemas`
- `tool_choice`
- `response_format`

Not persisted anymore inside `request_debug`:

- `input_payload`
- duplicated `input_parts`

### User-visible rendered content

Authoritative timeline/message rendering payload for the user message:

- `message.content`

For user messages this is persisted as structured JSON text rebuilt from the normalized request after preprocess rewrite. That content contains only sanitized public image URLs, never inline base64.

## Boundaries

### `internal/service/chat/preprocess`

Responsibilities:

- parse inline/data-url/base64 image input
- enforce chat media policy
- transcode to AVIF
- allocate deterministic `file_id`
- rewrite request image URLs to public chat-media asset URLs
- return in-memory drafts

Non-responsibilities:

- disk persistence
- DB writes
- owner lookup from context

Output shape:

- rewritten `protocol.TurnRequest`
- `[]media.DraftAsset`

### `internal/platform/media`

Responsibilities:

- parse / sniff / decode image input
- AVIF encode
- define neutral chat media draft / stored asset DTOs
- build public chat media asset path

### `internal/platform/media/localstore`

Responsibilities:

- persist draft asset bytes to filesystem
- delete persisted asset by path

Input / output:

- accepts `media.DraftAsset`
- returns `media.StoredAsset`

It does not define the service-facing DTOs anymore.

### `internal/service/chat/turn`

Responsibilities:

- derive submit principal once
- perform admission
- resolve conversation target
- orchestrate preprocess -> asset persist -> repository write
- cleanup persisted files on submit failure

### repository

Responsibilities:

- persist pending request state
- persist `media_assets` and `media_asset_refs`
- persist only sanitized request debug body

Derived request input parts for query/read paths may be rebuilt from `request_body`.

## Public media route

Chat request media route:

- `GET /api/media/assets/{ownerID}/{fileID}`

Rules:

- authenticated session owner must match `ownerID`
- served asset must come from `media_assets`
- SVG is never served
- response uses restrictive headers

The public path encoded into sanitized requests is:

- `/api/media/assets/{ownerID}/{fileID}`

No file extension is required in the public URL contract.

## Failure model

### Preprocess failure

- no files written
- no DB writes

### Media persistence failure

- no DB writes

### Repository failure after media persistence

- synchronous file cleanup attempted
- no raw inline image payload persisted

## Testing requirements

1. inline base64 / data URL image is rewritten to `/api/media/assets/...`
2. sanitized request body contains no raw base64
3. preprocess no longer writes files
4. submit writes file only after admission / resolve path
5. repository stores sanitized request body only
6. authenticated owner can fetch media asset; non-owner cannot
7. frontend renders user message images from structured message content / sanitized request body
