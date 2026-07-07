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
		text = firstNonEmptyText(record["output"], text, flattenToolResultContent(record["content"]))
		if text == "" {
			return InputPart{}
		}
		return InputPart{Type: "tool_result", Text: text}
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
		if (part.Type == "text" || part.Type == "tool_result") && strings.TrimSpace(part.Text) != "" {
			texts = append(texts, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(texts, "\n")
}

func extractToolResultParts(content any) []InputPart {
	text := flattenToolResultContent(content)
	if text == "" {
		return nil
	}
	return []InputPart{{Type: "tool_result", Text: text}}
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
