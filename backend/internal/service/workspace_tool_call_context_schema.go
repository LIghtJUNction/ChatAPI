package service

type WorkspaceToolCallContextSchema struct {
	Operations []WorkspaceToolCallContextOperationSchema `json:"operations"`
}

type WorkspaceToolCallContextOperationSchema struct {
	Name             string              `json:"name"`
	Method           string              `json:"method"`
	Path             string              `json:"path"`
	Description      string              `json:"description"`
	Fields           []ConfigFieldSchema `json:"fields,omitempty"`
	RequiresActor    string              `json:"requires_actor,omitempty"`
	ResponseSections []string            `json:"response_sections,omitempty"`
	Notes            []string            `json:"notes,omitempty"`
}

func BuildWorkspaceToolCallContextSchema() WorkspaceToolCallContextSchema {
	return WorkspaceToolCallContextSchema{
		Operations: []WorkspaceToolCallContextOperationSchema{
			{
				Name:          "assist_context",
				Method:        "GET",
				Path:          "/api/workspace/tool-call/assist-context",
				Description:   "Read the current request/conversation context required by the Tool Call assistant workspace.",
				RequiresActor: "interactive_user",
				Fields: []ConfigFieldSchema{
					{Key: "request_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional request id target. Required when conversation_id is omitted."},
					{Key: "conversation_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional conversation id target. Required when request_id is omitted."},
					{Key: "candidate_base_url", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional browser-side upstream base URL candidate used only for recursion-risk hints."},
				},
				ResponseSections: []string{
					"request",
					"parsed",
					"conversation",
					"messages",
					"draft",
					"assist_schema",
					"upstream_assistant_schema",
					"upstream_protocol_templates",
					"upstream_hints",
					"upstream_input_hints",
				},
				Notes: []string{
					"At least one of request_id or conversation_id must be provided.",
					"parsed.normalized_tool_schemas is the stable tool schema projection intended for frontend rendering.",
					"The endpoint does not accept or persist upstream API keys and does not call upstream models.",
					"candidate_base_url only affects upstream_hints and is not stored.",
				},
			},
		},
	}
}
