package protocol

import "strings"

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func firstMap(values ...any) map[string]any {
	for _, value := range values {
		record, ok := value.(map[string]any)
		if ok {
			return record
		}
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	clone := make(map[string]any, len(input))
	for key, value := range input {
		clone[key] = value
	}
	return clone
}

func defaultString(value string, fallback string) string {
	value = stringValue(value, "")
	if value == "" {
		return fallback
	}
	return value
}

func extractPartsFromMessageContent(content any) []InputPart {
	switch typed := content.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []InputPart{{Type: "text", Text: strings.TrimSpace(typed)}}
	case []any:
		parts := make([]InputPart, 0, len(typed))
		for _, item := range typed {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			part := extractInputPart(record)
			if part.Type != "" {
				parts = append(parts, part)
			}
		}
		return parts
	default:
		return nil
	}
}

func flattenMessageContent(content any) string {
	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			part := extractInputPart(record)
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				parts = append(parts, strings.TrimSpace(part.Text))
				continue
			}
			text := firstNonEmptyText(
				record["text"],
				record["input_text"],
				nestedStringValue(record, "text", "value"),
			)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func extractInputPart(record map[string]any) InputPart {
	partType := stringValue(record["type"], "")
	text := firstNonEmptyText(
		record["text"],
		record["input_text"],
		nestedStringValue(record, "text", "value"),
	)
	switch partType {
	case "input_text", "output_text", "text":
		if text == "" {
			return InputPart{}
		}
		return InputPart{Type: "text", Text: text}
	case "input_image", "image", "image_url":
		return InputPart{
			Type:      "image",
			MediaType: firstNonEmptyText(record["media_type"], nestedStringValue(record, "source", "media_type")),
			URL:       firstNonEmptyText(record["image_url"], nestedStringValue(record, "image_url", "url"), nestedStringValue(record, "source", "data"), nestedStringValue(record, "source", "url")),
		}
	case "function_call_output", "tool_result":
		content := record["content"]
		if output, ok := record["output"]; ok {
			content = output
		}
		return extractToolResult(content, firstNonEmptyText(record["tool_call_id"], record["call_id"], record["tool_use_id"]))
	default:
		if text != "" {
			return InputPart{Type: "text", Text: text}
		}
		return InputPart{}
	}
}

func joinInputPartText(parts []InputPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, strings.TrimSpace(part.Text))
		} else if part.Type == "tool_result" && part.ToolResult != nil {
			for _, content := range part.ToolResult.Content {
				if content.Type == "text" && strings.TrimSpace(content.Text) != "" {
					texts = append(texts, strings.TrimSpace(content.Text))
				}
			}
		}
	}
	return strings.Join(texts, "\n")
}

func joinHumanText(parts []InputPart) string {
	texts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(texts, "\n")
}

func extractLastUserContent(proto Protocol, body map[string]any) string {
	if proto == ProtocolChatCompletions || proto == ProtocolAnthropicMessages {
		messages, _ := body["messages"].([]any)
		for index := len(messages) - 1; index >= 0; index-- {
			record, ok := messages[index].(map[string]any)
			if ok && stringValue(record["role"], "") == "user" {
				text := joinHumanText(extractPartsFromMessageContent(record["content"]))
				if text != "" {
					return text
				}
			}
		}
		return ""
	}
	if inputText, ok := body["input"].(string); ok {
		return strings.TrimSpace(inputText)
	}
	input, _ := body["input"].([]any)
	for index := len(input) - 1; index >= 0; index-- {
		record, ok := input[index].(map[string]any)
		if !ok {
			continue
		}
		role := stringValue(record["role"], "")
		if role == "user" {
			text := joinHumanText(extractPartsFromMessageContent(record["content"]))
			if text != "" {
				return text
			}
		}
		if role == "" {
			if part := extractInputPart(record); part.Type == "text" {
				return strings.TrimSpace(part.Text)
			}
			if _, hasContent := record["content"]; hasContent {
				if text := joinHumanText(extractPartsFromMessageContent(record["content"])); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func extractToolResult(content any, callID string) InputPart {
	result := &ToolResult{CallID: strings.TrimSpace(callID), Content: extractToolResultContent(content)}
	if len(result.Content) == 0 {
		return InputPart{}
	}
	return InputPart{Type: "tool_result", ToolResult: result}
}

func extractToolResultContent(content any) []ContentPart {
	if text, ok := content.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return nil
		}
		return []ContentPart{{Type: "text", Text: text}}
	}
	items, ok := content.([]any)
	if !ok {
		return nil
	}
	parts := make([]ContentPart, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		part := extractInputPart(record)
		switch part.Type {
		case "text":
			parts = append(parts, ContentPart{Type: "text", Text: part.Text})
		case "image":
			parts = append(parts, ContentPart{Type: "image", MediaType: part.MediaType, URL: part.URL})
		}
	}
	return parts
}

func flattenToolResultContent(content any) string {
	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			text := firstNonEmptyText(
				record["text"],
				record["output"],
				record["input_text"],
				nestedStringValue(record, "text", "value"),
			)
			if strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, "\n")
	case map[string]any:
		return strings.TrimSpace(firstNonEmptyText(
			typed["text"],
			typed["output"],
			typed["input_text"],
			nestedStringValue(typed, "text", "value"),
		))
	default:
		return ""
	}
}

func nestedStringValue(record map[string]any, keys ...string) string {
	current := any(record)
	for _, key := range keys {
		next, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = next[key]
	}
	return stringValue(current, "")
}

func firstNonEmptyText(values ...any) string {
	for _, value := range values {
		if text := stringValue(value, ""); text != "" {
			return text
		}
	}
	return ""
}
