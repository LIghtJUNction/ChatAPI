package service

type RequestAccessSchema struct {
	Operations         []RequestAccessOperationSchema `json:"operations"`
	ParsedItemFields   []ConfigFieldSchema            `json:"parsed_item_fields,omitempty"`
	ParsedDetailFields []ConfigFieldSchema            `json:"parsed_detail_fields,omitempty"`
	ReplayFields       []ConfigFieldSchema            `json:"replay_fields,omitempty"`
}

type RequestAccessOperationSchema struct {
	Name           string              `json:"name"`
	Method         string              `json:"method"`
	Path           string              `json:"path"`
	Description    string              `json:"description"`
	RequiredScopes []string            `json:"required_scopes,omitempty"`
	Fields         []ConfigFieldSchema `json:"fields,omitempty"`
	Notes          []string            `json:"notes,omitempty"`
}

type ConversationAccessSchema struct {
	Operations []ConversationAccessOperationSchema `json:"operations"`
}

type ConversationAccessOperationSchema struct {
	Name           string   `json:"name"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	Description    string   `json:"description"`
	RequiredScopes []string `json:"required_scopes,omitempty"`
	Notes          []string `json:"notes,omitempty"`
}

func BuildLabRequestsSchema() RequestAccessSchema {
	return buildRequestAccessSchema("/lab/requests", "/lab/requests/{request_id}", "/lab/requests/{request_id}")
}

func BuildAppRequestsSchema() RequestAccessSchema {
	return buildRequestAccessSchema("/api/app/requests", "/api/app/requests/{request_id}", "/api/app/requests/{request_id}")
}

func buildRequestAccessSchema(listPath string, detailPath string, controlBasePath string) RequestAccessSchema {
	controlFields := []ConfigFieldSchema{
		{Key: "response_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional response id override for the completion payload."},
		{Key: "text", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Assistant output text or draft delta text."},
		{Key: "mode", ValueType: "string", DefaultValue: "assistant_message", Public: false, AdminWriteOnly: true, Description: "Completion mode.", Validation: map[string]any{"enum": []string{"assistant_message", "thinking", "tool_call", "tool_result"}}},
		{Key: "tool_name", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Required when mode=tool_call."},
		{Key: "tool_call_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional tool call correlation id."},
		{Key: "output", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Tool result output. Falls back to text when omitted."},
		{Key: "reasoning_stream_mode", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional reasoning stream mode forwarded to the protocol encoder."},
	}
	return RequestAccessSchema{
		ParsedItemFields: []ConfigFieldSchema{
			{Key: "request_id", ValueType: "string", Public: true, Description: "Stable request id for request-level control and lookup."},
			{Key: "request_format", ValueType: "string", Public: true, Description: "Normalized protocol name such as responses, chat_completions, or anthropic_messages."},
			{Key: "model", ValueType: "string", Public: true, Description: "Virtual model name captured from the original request."},
			{Key: "system_text", ValueType: "string", Public: true, Description: "Flattened system prompt text extracted from the request."},
			{Key: "developer_text", ValueType: "string", Public: true, Description: "Flattened developer message text extracted from the request."},
			{Key: "assistant_text", ValueType: "string", Public: true, Description: "Flattened assistant context text extracted from the request."},
			{Key: "user_text", ValueType: "string", Public: true, Description: "Flattened latest user-visible input text."},
			{Key: "input_part_types", ValueType: "array", Public: true, Description: "Ordered list of normalized input part types used by the request."},
			{Key: "tool_choice", ValueType: "object", Public: true, Description: "Normalized tool_choice projection."},
			{Key: "response_format", ValueType: "object", Public: true, Description: "Normalized response_format projection."},
			{Key: "normalized_tool_schemas", ValueType: "array", Public: true, Description: "Stable tool schema projection for list/debug UIs."},
			{Key: "request_body_keys", ValueType: "array", Public: true, Description: "Sorted top-level keys observed in the original request body."},
		},
		ParsedDetailFields: []ConfigFieldSchema{
			{Key: "request_format", ValueType: "string", Public: true, Description: "Normalized protocol name such as responses, chat_completions, or anthropic_messages."},
			{Key: "model", ValueType: "string", Public: true, Description: "Virtual model name captured from the original request."},
			{Key: "system_text", ValueType: "string", Public: true, Description: "Flattened system prompt text extracted from the request."},
			{Key: "developer_text", ValueType: "string", Public: true, Description: "Flattened developer message text extracted from the request."},
			{Key: "assistant_text", ValueType: "string", Public: true, Description: "Flattened assistant context text extracted from the request."},
			{Key: "user_text", ValueType: "string", Public: true, Description: "Flattened latest user-visible input text."},
			{Key: "input_parts", ValueType: "array", Public: true, Description: "Normalized structured input parts extracted from the request."},
			{Key: "tool_choice", ValueType: "object", Public: true, Description: "Normalized tool_choice projection."},
			{Key: "tool_schemas", ValueType: "array", Public: true, Description: "Raw tool schema payload as captured from the original request."},
			{Key: "normalized_tool_schemas", ValueType: "array", Public: true, Description: "Stable tool schema projection for frontend rendering and validation."},
			{Key: "response_format", ValueType: "object", Public: true, Description: "Normalized response_format projection."},
			{Key: "request_method", ValueType: "string", Public: true, Description: "Original HTTP method used by the client request."},
			{Key: "request_path", ValueType: "string", Public: true, Description: "Original HTTP path used by the client request."},
			{Key: "request_query", ValueType: "object", Public: true, Description: "Captured request query parameters after removing Lab secrets."},
			{Key: "request_headers", ValueType: "object", Public: true, Description: "Captured request headers after filtering sensitive keys."},
			{Key: "request_body_keys", ValueType: "array", Public: true, Description: "Sorted top-level keys observed in the original request body."},
			{Key: "replay", ValueType: "object", Public: true, Description: "Replay/debug projection derived from the captured request snapshot."},
		},
		ReplayFields: []ConfigFieldSchema{
			{Key: "method", ValueType: "string", Public: true, Description: "Replay HTTP method."},
			{Key: "path", ValueType: "string", Public: true, Description: "Replay HTTP path without base URL."},
			{Key: "query", ValueType: "object", Public: true, Description: "Replay query parameters."},
			{Key: "headers", ValueType: "object", Public: true, Description: "Replay headers after filtering Authorization, Cookie, and key-bearing headers."},
			{Key: "body", ValueType: "object", Public: true, Description: "Replay request body snapshot."},
			{Key: "curl", ValueType: "string", Public: true, Description: "Replay curl command built against the current instance base URL."},
		},
		Operations: []RequestAccessOperationSchema{
			{
				Name:           "list_requests",
				Method:         "GET",
				Path:           listPath,
				Description:    "List requests visible from the current surface together with parsed summary items.",
				RequiredScopes: []string{"requests:read"},
				Notes: []string{
					"The response shape is {ok, items, parsed_items}.",
					"parsed_items is a lightweight summary projection for list UIs.",
				},
			},
			{
				Name:           "get_request",
				Method:         "GET",
				Path:           detailPath,
				Description:    "Read one request together with the parsed detail view.",
				RequiredScopes: []string{"requests:read"},
				Notes: []string{
					"The response shape is {ok, request, parsed}.",
					"parsed now includes request_method, request_path, request_query, request_headers, and replay.curl for debugging/replay flows.",
				},
			},
			{
				Name:           "copy_request_curl",
				Method:         "POST",
				Path:           detailPath + "/copy-curl",
				Description:    "Build a replayable curl command for one captured request.",
				RequiredScopes: []string{"requests:read"},
				Notes: []string{
					"The response shape is {ok, request_id, curl}.",
					"Sensitive headers such as Authorization, Cookie, and X-ChatAPI-App-Key are excluded from the generated curl command.",
				},
			},
			{
				Name:           "request_delta",
				Method:         "POST",
				Path:           controlBasePath + "/delta",
				Description:    "Update the draft output of a pending request without completing it.",
				RequiredScopes: []string{"requests:respond"},
				Fields: []ConfigFieldSchema{
					{Key: "text", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Draft delta text to persist for the request's conversation."},
				},
			},
			{
				Name:           "request_complete",
				Method:         "POST",
				Path:           controlBasePath + "/complete",
				Description:    "Complete a pending request.",
				RequiredScopes: []string{"requests:respond"},
				Fields:         controlFields,
			},
			{
				Name:           "request_abort",
				Method:         "POST",
				Path:           controlBasePath + "/abort",
				Description:    "Abort a pending request.",
				RequiredScopes: []string{"requests:respond"},
				Fields: []ConfigFieldSchema{
					{Key: "error", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Abort reason returned to the waiting client."},
				},
				Notes: []string{
					"error is required for abort requests.",
				},
			},
		},
	}
}

func BuildAppConversationsSchema() ConversationAccessSchema {
	return ConversationAccessSchema{
		Operations: []ConversationAccessOperationSchema{
			{
				Name:           "list_conversations",
				Method:         "GET",
				Path:           "/api/app/conversations",
				Description:    "List conversations visible to the app API key owner after resource-limit filtering.",
				RequiredScopes: []string{"conversations:read"},
				Notes: []string{
					"The response shape is {ok, items}.",
				},
			},
			{
				Name:           "list_conversation_messages",
				Method:         "GET",
				Path:           "/api/app/conversations/{conversation_id}/messages",
				Description:    "List messages of a conversation visible to the app API key owner.",
				RequiredScopes: []string{"conversations:read"},
				Notes: []string{
					"Conversation resource limits may still return 403 even when the scope is present.",
					"The response shape is {ok, items}.",
				},
			},
		},
	}
}
