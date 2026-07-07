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
					"backend_assistant_providers",
					"upstream_assistant_schema",
					"upstream_protocol_templates",
					"upstream_hints",
					"upstream_input_hints",
				},
				Notes: []string{
					"At least one of request_id or conversation_id must be provided.",
					"parsed.normalized_tool_schemas is the stable tool schema projection intended for frontend rendering.",
					"backend_assistant_providers declares which delegated upstream providers are currently wired into the backend-side assist path.",
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
					{Key: "provider", ValueType: "string", DefaultValue: "kirari", Public: false, AdminWriteOnly: true, Description: "Backend-side upstream provider identifier. See assist-context.backend_assistant_providers for currently available values."},
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
					"The set of backend-side provider values is runtime-defined by backend_assistant_providers.",
					"The endpoint returns a draft only and never auto-submits a tool call.",
					"Browser-direct upstream assistant remains the default path for arbitrary user-configured upstream models.",
				},
			},
			{
				Name:          "assist_stream",
				Method:        "POST",
				Path:          "/api/workspace/tool-call/assist/stream",
				Description:   "Request a backend-side Tool Call draft assistant as a normalized SSE stream without submitting the draft.",
				RequiresActor: "interactive_user",
				Fields: []ConfigFieldSchema{
					{Key: "provider", ValueType: "string", DefaultValue: "kirari", Public: false, AdminWriteOnly: true, Description: "Backend-side upstream provider identifier. See assist-context.backend_assistant_providers for currently available values."},
					{Key: "model", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Provider model id used for the assist request."},
					{Key: "request_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional request id target. Required when conversation_id is omitted."},
					{Key: "conversation_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional conversation id target. Required when request_id is omitted."},
				},
				ResponseSections: []string{
					"event: assist.started",
					"event: assist.delta",
					"event: assist.completed",
					"event: assist.failed",
				},
				Notes: []string{
					"At least one of request_id or conversation_id must be provided.",
					"The set of backend-side provider values is runtime-defined by backend_assistant_providers.",
					"The assist.completed payload matches the non-stream assist result shape.",
					"Browser-direct upstream assistant remains the default path for arbitrary user-configured upstream models.",
				},
			},
			{
				Name:          "assist_parse",
				Method:        "POST",
				Path:          "/api/workspace/tool-call/assist/parse",
				Description:   "Parse and validate browser-side upstream assistant output against the current workspace target without submitting the draft.",
				RequiresActor: "interactive_user",
				Fields: []ConfigFieldSchema{
					{Key: "provider", ValueType: "string", DefaultValue: "browser_upstream", Public: false, AdminWriteOnly: true, Description: "Optional upstream provider identifier used only for metadata echo and audit."},
					{Key: "model", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional upstream model id used only for metadata echo and audit."},
					{Key: "request_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional request id target. Required when conversation_id is omitted."},
					{Key: "conversation_id", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Optional conversation id target. Required when request_id is omitted."},
					{Key: "raw_output", ValueType: "string", DefaultValue: "", Public: false, AdminWriteOnly: true, Description: "Raw upstream model output captured by the browser-side assistant path."},
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
					"The endpoint does not contact any upstream model and never accepts upstream API keys.",
					"This route is intended for browser-direct upstream assistants so the frontend can reuse backend parsing and validation logic before the user manually submits a tool call.",
				},
			},
		},
	}
}
