package protocol

import "strings"

type ParsedRequest struct {
	RequestFormat string
	Model         string
	Stream        bool
	UserContent   string
	ToolSchemas   []any
}

func ParseRequest(requestFormat string, body map[string]any) ParsedRequest {
	return ParsedRequest{
		RequestFormat: requestFormat,
		Model:         stringValue(body["model"], "chatapi-lab"),
		Stream:        boolValue(body["stream"]),
		UserContent:   extractUserText(body),
		ToolSchemas:   extractToolSchemas(body),
	}
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func extractUserText(body map[string]any) string {
	if input, ok := body["input"].(string); ok && strings.TrimSpace(input) != "" {
		return strings.TrimSpace(input)
	}
	if input, ok := body["input"].([]any); ok {
		parts := make([]string, 0)
		for _, item := range input {
			record, ok := item.(map[string]any)
			if !ok {
				continue
			}
			contentItems, ok := record["content"].([]any)
			if !ok {
				continue
			}
			for _, contentItem := range contentItems {
				contentRecord, ok := contentItem.(map[string]any)
				if !ok {
					continue
				}
				if text, ok := contentRecord["text"].(string); ok && strings.TrimSpace(text) != "" {
					parts = append(parts, strings.TrimSpace(text))
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	if messages, ok := body["messages"].([]any); ok {
		for i := len(messages) - 1; i >= 0; i-- {
			record, ok := messages[i].(map[string]any)
			if !ok {
				continue
			}
			if role, _ := record["role"].(string); role != "user" {
				continue
			}
			if content, ok := record["content"].(string); ok && strings.TrimSpace(content) != "" {
				return strings.TrimSpace(content)
			}
		}
	}
	return ""
}

func extractToolSchemas(body map[string]any) []any {
	if tools, ok := body["tools"].([]any); ok {
		return tools
	}
	return nil
}
