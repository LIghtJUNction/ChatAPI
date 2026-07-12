# Browser Tool Call Assist

## Boundary

Tool Call Assist is a workspace-only draft helper. It sends the selected function
schema and the operator's temporary instruction directly from the browser to an
operator-configured OpenAI-compatible upstream. ChatAPI's backend never receives
the upstream URL, API key, assist prompt, or model response.

The feature may update only the local Tool Call form state. It must not publish a
workspace command, append a timeline item, mutate a pending turn, create an
automation recording step, or persist an assist result. The existing explicit
"output Tool Call" action remains the only submission boundary.

## Modules

- `features/tool-call-assist/storage.ts` owns the browser-local configuration.
- `features/tool-call-assist/client.ts` builds upstream requests, handles timeout
  and cancellation, parses protocol responses, and validates the returned object
  against the selected schema.
- `features/tool-call-assist/ToolCallAssistPopover.tsx` owns the temporary prompt,
  request lifecycle, and form-fill interaction.
- `components/settings/BrowserAssistSettingsPanel.tsx` edits the local profile and
  explicitly communicates that configuration never reaches ChatAPI's server.

`ChatPane` supplies the currently selected schema and applies validated fields.
It does not know upstream protocol response shapes.

## Configuration

One browser profile per ChatAPI user ID is stored under
`chatapi.browser-tool-call-assist.v1.<user-id>`:

- enabled protocol: OpenAI Responses or Chat Completions;
- base URL, such as `https://api.openai.com/v1`;
- model;
- API key.

The key is plain browser local storage. Profiles are isolated between ChatAPI user
IDs on the same origin and can be explicitly deleted. This is appropriate only on
a trusted device. Clearing site data removes it. The server cannot synchronize or
recover it. Browser CORS policy still applies to the configured upstream.

The client accepts only absolute HTTP(S) endpoints, rejects the browser's current
ChatAPI origin, and rejects redirects. Alternate domains or ports that route to the
same deployment cannot be discovered without server participation, so the user is
responsible for selecting an external upstream. The configured endpoint is the
user's explicit data recipient.

## Request And Result Semantics

The selected function's parameter schema becomes a strict JSON Schema response
format when supported. The instruction asks for one JSON object and forbids prose.
The operator's temporary prompt supplies task-specific context.

The complete returned object is validated with AJV draft-07 against the selected
Tool JSON Schema. Object and array values are serialized into the JSON text
representation expected by the existing complex-field editor. The codec is shared
with final manual Tool Call encoding, but strict AJV validation is limited to the
assist boundary so this feature does not narrow existing protocol compatibility.
An invalid response is shown in the popover and never changes the form.

Successful generation must be a complete schema-valid object and replaces only
fields declared in that result. Closing the popover clears its temporary prompt and
aborts any in-flight browser request.
Each request is bound to the current conversation, request, and selected tool.
Changing any of them aborts the request and prevents a stale result from changing
the new draft.
