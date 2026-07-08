package protocol

func extractResponsesInputParts(body map[string]any) []InputPart {
	if input, ok := body["input"].(string); ok && input != "" {
		return []InputPart{{Type: "text", Text: input}}
	}
	if input, ok := body["input"].([]any); ok {
		return extractResponsesTurnInputParts(input)
	}
	return nil
}

func extractResponsesRoleContent(body map[string]any, role string) string {
	if role == "system" {
		return ""
	}
	items := make([]string, 0)
	input, ok := body["input"].([]any)
	if !ok {
		return ""
	}
	for _, item := range input {
		record, ok := item.(map[string]any)
		if !ok || stringValue(record["role"], "") != role {
			continue
		}
		if content := flattenMessageContent(record["content"]); content != "" {
			items = append(items, content)
		}
	}
	return joinLines(items)
}

func extractResponsesTurnInputParts(input []any) []InputPart {
	parts := make([]InputPart, 0)
	for _, item := range input {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		part := extractInputPart(record)
		if part.Type != "" {
			parts = append(parts, part)
			continue
		}
		role := stringValue(record["role"], stringValue(record["type"], ""))
		if _, hasContent := record["content"]; hasContent && role == "" {
			parts = append(parts, extractPartsFromMessageContent(record["content"])...)
			continue
		}
		switch role {
		case "user":
			parts = append(parts, extractPartsFromMessageContent(record["content"])...)
		case "tool":
			parts = append(parts, extractToolResultParts(record["content"])...)
		}
	}
	return parts
}
