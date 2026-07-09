package protocol

import "strings"

func extractToolSchemas(body map[string]any) []ToolSchema {
	if tools, ok := body["tools"].([]any); ok {
		return normalizeToolSchemasDetailed(tools)
	}
	return nil
}

func extractBuiltinTools(proto Protocol, body map[string]any) []BuiltinTool {
	if proto != ProtocolResponses {
		return nil
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) == 0 {
		return nil
	}
	out := make([]BuiltinTool, 0, len(tools))
	for _, raw := range tools {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		toolType := strings.TrimSpace(stringValue(record["type"], ""))
		kind, label := builtinToolKind(toolType)
		if kind == "" {
			continue
		}
		out = append(out, BuiltinTool{
			Kind:  kind,
			Type:  toolType,
			Label: label,
			Raw:   cloneMap(record),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func builtinToolKind(toolType string) (string, string) {
	switch strings.TrimSpace(toolType) {
	case "web_search", "web_search_preview", "web_search_preview_2025_03_11":
		return "web_search", "Web Search"
	case "image_generation":
		return "image_generation", "Image Generation"
	default:
		return "", ""
	}
}

func NormalizeToolSchemas(items []any) []NormalizedToolSchema {
	return normalizeToolSchemaViews(normalizeToolSchemasDetailed(items))
}

func RawToolSchemas(items []ToolSchema) []any {
	if len(items) == 0 {
		return nil
	}
	raw := make([]any, 0, len(items))
	for _, item := range items {
		if len(item.Raw) == 0 {
			continue
		}
		raw = append(raw, cloneMap(item.Raw))
	}
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func RawBuiltinTools(items []BuiltinTool) []any {
	if len(items) == 0 {
		return nil
	}
	raw := make([]any, 0, len(items))
	for _, item := range items {
		if len(item.Raw) != 0 {
			raw = append(raw, cloneMap(item.Raw))
			continue
		}
		if item.Type != "" {
			raw = append(raw, map[string]any{"type": item.Type})
		}
	}
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func normalizeToolSchemaViews(items []ToolSchema) []NormalizedToolSchema {
	if len(items) == 0 {
		return nil
	}
	normalized := make([]NormalizedToolSchema, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, NormalizedToolSchema{
			Name:        item.Name,
			Description: item.Description,
			Parameters:  cloneMap(item.Parameters),
			Type:        item.Type,
		})
	}
	return normalized
}

func normalizeToolSchemasDetailed(items []any) []ToolSchema {
	if len(items) == 0 {
		return nil
	}
	normalized := make([]ToolSchema, 0, len(items))
	for _, raw := range items {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		toolType := defaultString(stringValue(record["type"], ""), "function")
		if kind, _ := builtinToolKind(toolType); kind != "" {
			continue
		}
		if function, ok := record["function"].(map[string]any); ok {
			name := stringValue(function["name"], "")
			if name == "" {
				continue
			}
			normalized = append(normalized, ToolSchema{
				Name:        name,
				Description: stringValue(function["description"], ""),
				Parameters:  cloneMap(firstMap(function["parameters"], function["input_schema"])),
				Type:        toolType,
				Raw:         cloneMap(record),
			})
			continue
		}

		name := stringValue(record["name"], "")
		if name == "" {
			continue
		}
		normalized = append(normalized, ToolSchema{
			Name:        name,
			Description: stringValue(record["description"], ""),
			Parameters:  cloneMap(firstMap(record["parameters"], record["input_schema"])),
			Type:        toolType,
			Raw:         cloneMap(record),
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func extractToolChoice(body map[string]any) ToolChoice {
	switch typed := body["tool_choice"].(type) {
	case string:
		return ToolChoice{Type: stringValue(typed, "")}
	case map[string]any:
		choice := ToolChoice{Type: stringValue(typed["type"], "")}
		choice.Name = firstNonEmptyText(typed["name"], nestedStringValue(typed, "function", "name"))
		return choice
	default:
		return ToolChoice{}
	}
}

func extractResponseFormat(body map[string]any) ResponseFormat {
	if textRecord, ok := body["text"].(map[string]any); ok {
		if formatRecord, ok := textRecord["format"].(map[string]any); ok {
			return responseFormatFromRecord(formatRecord)
		}
	}
	record, ok := body["response_format"].(map[string]any)
	if !ok {
		return ResponseFormat{}
	}
	return responseFormatFromRecord(record)
}

func responseFormatFromRecord(record map[string]any) ResponseFormat {
	format := ResponseFormat{Type: stringValue(record["type"], "")}
	if schemaRecord, ok := record["json_schema"].(map[string]any); ok {
		format.Name = stringValue(schemaRecord["name"], "")
		if schema, ok := schemaRecord["schema"].(map[string]any); ok {
			format.Schema = schema
		}
		return format
	}
	if format.Type == "json_schema" {
		format.Name = stringValue(record["name"], "")
		if schema, ok := record["schema"].(map[string]any); ok {
			format.Schema = schema
		}
	}
	return format
}
