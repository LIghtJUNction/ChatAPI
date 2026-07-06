package handlers

import "testing"

func TestNormalizedToolSchemas(t *testing.T) {
	items := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup_weather",
				"description": "Lookup weather",
				"parameters": map[string]any{
					"type": "object",
				},
			},
		},
		map[string]any{
			"name":        "search_docs",
			"description": "Search docs",
			"input_schema": map[string]any{
				"type": "object",
			},
		},
		map[string]any{
			"type": "function",
		},
	}

	normalized := normalizedToolSchemas(items)
	if len(normalized) != 2 {
		t.Fatalf("unexpected normalized tool schemas: %#v", normalized)
	}
	if normalized[0]["name"] != "lookup_weather" || normalized[0]["type"] != "function" {
		t.Fatalf("unexpected first normalized tool: %#v", normalized[0])
	}
	if normalized[1]["name"] != "search_docs" || normalized[1]["type"] != "function" {
		t.Fatalf("unexpected second normalized tool: %#v", normalized[1])
	}
	parameters, ok := normalized[1]["parameters"].(map[string]any)
	if !ok || parameters["type"] != "object" {
		t.Fatalf("unexpected normalized parameters: %#v", normalized[1])
	}
}
