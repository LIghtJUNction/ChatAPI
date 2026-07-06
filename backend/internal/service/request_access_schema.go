package service

type RequestAccessSchema struct {
	Operations []RequestAccessOperationSchema `json:"operations"`
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
