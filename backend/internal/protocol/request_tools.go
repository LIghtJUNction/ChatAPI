package protocol

func extractToolSchemas(body map[string]any) []ToolSchema {
	if tools, ok := body["tools"].([]any); ok {
		return normalizeToolSchemasDetailed(tools)
	}
	return nil
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
	record, ok := body["response_format"].(map[string]any)
	if !ok {
		return ResponseFormat{}
	}
	format := ResponseFormat{Type: stringValue(record["type"], "")}
	if schemaRecord, ok := record["json_schema"].(map[string]any); ok {
		format.Name = stringValue(schemaRecord["name"], "")
		if schema, ok := schemaRecord["schema"].(map[string]any); ok {
			format.Schema = schema
		}
	}
	return format
}
