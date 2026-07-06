package service

type ConversationControlSchema struct {
	Operations []ConversationControlOperationSchema `json:"operations"`
}

type ConversationControlOperationSchema struct {
	Name          string              `json:"name"`
	Method        string              `json:"method"`
	Path          string              `json:"path"`
	Description   string              `json:"description"`
	Fields        []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresActor string              `json:"requires_actor,omitempty"`
	Notes         []string            `json:"notes,omitempty"`
}

func BuildConversationControlSchema() ConversationControlSchema {
	completeFields := []ConfigFieldSchema{
		{Key: "response_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional response id override for the completion payload."},
		{Key: "text", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Assistant output text or reasoning text depending on mode."},
		{Key: "mode", ValueType: "string", DefaultValue: "assistant_message", Public: false, AdminWriteOnly: true, Description: "Completion mode.", Validation: map[string]any{"enum": []string{"assistant_message", "thinking", "tool_call", "tool_result"}}},
		{Key: "tool_name", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Required when mode=tool_call."},
		{Key: "tool_call_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional tool call correlation id."},
		{Key: "output", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Tool result output. Falls back to text when omitted."},
		{Key: "reasoning_stream_mode", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional reasoning stream mode forwarded to the protocol encoder."},
	}
	return ConversationControlSchema{
		Operations: []ConversationControlOperationSchema{
			{
				Name:          "list_conversation_messages",
				Method:        "GET",
				Path:          "/api/conversations/{conversation_id}/messages",
				Description:   "List persisted messages for a conversation.",
				RequiresActor: "interactive_or_lab",
				Notes: []string{
					"The response shape is {ok, items}.",
				},
			},
			{
				Name:          "respond_conversation",
				Method:        "POST",
				Path:          "/api/conversations/{conversation_id}/respond",
				Description:   "Complete the pending turn in one shot using the path-based conversation control API.",
				RequiresActor: "interactive_or_lab",
				Fields:        completeFields,
				Notes: []string{
					"This is the preferred non-streaming manual reply endpoint for the future WebUI.",
				},
			},
			{
				Name:          "stream_delta_conversation",
				Method:        "POST",
				Path:          "/api/conversations/{conversation_id}/stream/delta",
				Description:   "Update the draft output for a pending conversation without completing it.",
				RequiresActor: "interactive_or_lab",
				Fields: []ConfigFieldSchema{
					{Key: "text", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Draft delta text to persist for the pending conversation."},
				},
				Notes: []string{
					"The response shape includes the updated draft and current conversation status.",
				},
			},
			{
				Name:          "stream_complete_conversation",
				Method:        "POST",
				Path:          "/api/conversations/{conversation_id}/stream/complete",
				Description:   "Complete a streamed manual reply using the path-based conversation control API.",
				RequiresActor: "interactive_or_lab",
				Fields:        completeFields,
			},
			{
				Name:          "abort_conversation",
				Method:        "POST",
				Path:          "/api/conversations/{conversation_id}/abort",
				Description:   "Abort the pending turn for a conversation.",
				RequiresActor: "interactive_or_lab",
				Fields: []ConfigFieldSchema{
					{Key: "error", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Abort reason returned to the waiting client."},
				},
				Notes: []string{
					"error is required for abort requests.",
				},
			},
			{
				Name:          "legacy_output_delta",
				Method:        "POST",
				Path:          "/api/chat/output/delta",
				Description:   "Legacy compatibility alias for streamed draft updates.",
				RequiresActor: "interactive_or_lab",
				Fields: []ConfigFieldSchema{
					{Key: "conversation_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Conversation id to update when using the legacy body-addressed endpoint."},
					{Key: "text", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Draft delta text to persist for the pending conversation."},
				},
				Notes: []string{
					"Prefer /api/conversations/{conversation_id}/stream/delta for new clients.",
				},
			},
			{
				Name:          "legacy_output_complete",
				Method:        "POST",
				Path:          "/api/chat/output/complete",
				Description:   "Legacy compatibility alias for one-shot or streamed completion.",
				RequiresActor: "interactive_or_lab",
				Fields: append([]ConfigFieldSchema{
					{Key: "conversation_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Conversation id to complete when using the legacy body-addressed endpoint."},
				}, completeFields...),
				Notes: []string{
					"Prefer /api/conversations/{conversation_id}/respond or /stream/complete for new clients.",
				},
			},
		},
	}
}
