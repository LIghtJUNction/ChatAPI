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
			{
				Name:          "assist_execute",
				Method:        "POST",
				Path:          "/api/workspace/tool-call/assist",
				Description:   "Request a backend-side Tool Call draft assistant for the current workspace target without submitting the draft.",
				RequiresActor: "interactive_user",
				Fields: []ConfigFieldSchema{
					{Key: "provider", ValueType: "string", DefaultValue: "kirari", Public: false, AdminWriteOnly: true, Description: "Backend-side upstream provider identifier. Current supported value: kirari."},
					{Key: "model", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Provider model id used for the assist request."},
					{Key: "request_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional request id target. Required when conversation_id is omitted."},
					{Key: "conversation_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional conversation id target. Required when request_id is omitted."},
				},
				ResponseSections: []string{
					"assist.provider",
					"assist.model",
					"assist.explanation",
					"assist.tool_call",
					"assist.confidence",
					"assist.warnings",
					"assist.validation_errors",
					"assist.valid_draft",
					"assist.raw_output",
					"assist.request",
				},
				Notes: []string{
					"At least one of request_id or conversation_id must be provided.",
					"Current backend-side assist support is limited to provider=kirari.",
					"The endpoint returns a draft only and never auto-submits a tool call.",
					"Browser-direct upstream assistant remains the default path for arbitrary user-configured upstream models.",
				},
			},
		},
	}
}
