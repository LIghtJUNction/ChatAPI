package service

type ToolCallAssistSchema struct {
	OutputJSONSchema map[string]any `json:"output_json_schema"`
	ConfidenceLevels []string       `json:"confidence_levels"`
	ValidationRules  []string       `json:"validation_rules"`
	Notes            []string       `json:"notes"`
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
	}
}
