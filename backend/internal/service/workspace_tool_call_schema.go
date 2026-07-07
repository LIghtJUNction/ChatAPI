package service

type ToolCallAssistSchema struct {
	OutputJSONSchema      map[string]any      `json:"output_json_schema"`
	ConfidenceLevels      []string            `json:"confidence_levels"`
	ValidationRules       []string            `json:"validation_rules"`
	Notes                 []string            `json:"notes"`
	ErrorCodes            []UpstreamErrorCode `json:"error_codes,omitempty"`
	PromptContract        map[string]any      `json:"prompt_contract"`
	StructuredOutputModes []map[string]any    `json:"structured_output_modes"`
	OutputExamples        []map[string]any    `json:"output_examples,omitempty"`
}

func BuildToolCallAssistSchema() ToolCallAssistSchema {
	return ToolCallAssistSchema{
		OutputJSONSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"explanation": map[string]any{
					"type":        "string",
					"description": "Human-readable explanation shown above the draft tool call.",
				},
				"tool_call": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"name": map[string]any{
							"type":        "string",
							"description": "Tool name to prefill in the workspace form.",
						},
						"arguments": map[string]any{
							"type":                 "object",
							"description":          "JSON object used to prefill tool arguments.",
							"additionalProperties": true,
						},
					},
					"required":             []string{"name", "arguments"},
					"additionalProperties": false,
				},
				"confidence": map[string]any{
					"type": "string",
					"enum": []string{"low", "medium", "high"},
				},
				"warnings": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
				},
			},
			"required":             []string{"explanation", "tool_call"},
			"additionalProperties": false,
		},
		ConfidenceLevels: []string{"low", "medium", "high"},
		ValidationRules: []string{
			"tool_call.name must match one of the tools declared in parsed.tool_schemas",
			"tool_call.arguments must be a JSON object",
			"arguments should be validated against the selected tool schema before prefilling the form",
			"if validation fails, explanation may still be shown but the form must remain unsubmitted",
		},
		Notes: []string{
			"Render explanation for the user before showing the prefilled tool call draft.",
			"Do not auto-submit the draft tool call.",
			"If JSON parsing fails, keep the raw explanation text only.",
		},
		ErrorCodes: []UpstreamErrorCode{
			{Code: "assist.provider_required", Description: "The backend-side assist request is missing provider."},
			{Code: "assist.model_required", Description: "The backend-side assist request is missing model."},
			{Code: "assist.target_required", Description: "The backend-side assist request must include request_id or conversation_id."},
			{Code: "assist.provider_not_supported", Description: "The requested backend-side provider is not registered in this ChatAPI instance."},
			{Code: "assist.no_tools", Description: "The current target request does not declare any tools, so no tool call draft can be generated."},
			{Code: "assist.invalid_output", Description: "The upstream output could not be parsed into the expected explanation/tool_call JSON structure."},
			{Code: "provider_not_connected", Description: "The delegated provider requires user authorization but the current user has not connected it yet."},
			{Code: "provider_timeout", Description: "The delegated provider request timed out before ChatAPI received a usable response."},
			{Code: "provider_cancelled", Description: "The delegated provider request was cancelled before completion."},
			{Code: "provider_request_failed", Description: "The delegated provider request failed or returned an unreadable response."},
			{Code: "upstream_nil_response", Description: "The delegated provider returned no HTTP response body for the assist stream."},
			{Code: "upstream_stream_read_failed", Description: "The delegated provider stream terminated unexpectedly while ChatAPI was reading it."},
		},
		PromptContract: map[string]any{
			"required_goals": []string{
				"Explain which tool should be called and why before presenting the draft.",
				"Return exactly one tool_call draft that matches the current tool schema set.",
				"Call out ambiguity or inferred arguments in warnings instead of silently inventing facts.",
			},
			"required_output_order": []string{
				"explanation",
				"tool_call",
				"confidence",
				"warnings",
			},
			"recommended_instruction_lines": []string{
				"First explain your reasoning in the explanation field.",
				"Then output one JSON object that matches the provided schema exactly.",
				"Do not wrap the final JSON object in markdown unless the caller explicitly allows it.",
				"Do not auto-submit or pretend the tool has already been executed.",
			},
		},
		StructuredOutputModes: []map[string]any{
			{
				"name":        "native_json_schema",
				"description": "Preferred when the upstream supports JSON schema / structured output natively.",
				"requires":    []string{"response_format"},
			},
			{
				"name":        "prompted_json",
				"description": "Fallback when the upstream only supports plain text output; caller should parse and validate the final JSON object.",
				"requires":    []string{"prompt_contract", "assist_parse"},
			},
		},
		OutputExamples: []map[string]any{
			{
				"explanation": "The user explicitly asked for a weather lookup, so prefilling lookup_weather with the inferred city is the safest draft.",
				"tool_call": map[string]any{
					"name": "lookup_weather",
					"arguments": map[string]any{
						"city": "Beijing",
						"unit": "c",
					},
				},
				"confidence": "high",
				"warnings":   []string{"city inferred from recent user message"},
			},
		},
	}
}
