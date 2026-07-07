package service

type ToolCallAssistSchema struct {
	OutputJSONSchema      map[string]any   `json:"output_json_schema"`
	ConfidenceLevels      []string         `json:"confidence_levels"`
	ValidationRules       []string         `json:"validation_rules"`
	Notes                 []string         `json:"notes"`
	PromptContract        map[string]any   `json:"prompt_contract"`
	StructuredOutputModes []map[string]any `json:"structured_output_modes"`
	OutputExamples        []map[string]any `json:"output_examples,omitempty"`
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
