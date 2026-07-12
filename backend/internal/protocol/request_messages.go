package protocol

import "strings"

func extractMessagesRoleContent(body map[string]any, role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return ""
	}
	items := make([]string, 0)
	if messages, ok := body["messages"].([]any); ok {
		for _, item := range messages {
			record, ok := item.(map[string]any)
			if !ok || stringValue(record["role"], "") != role {
				continue
			}
			if content := flattenMessageContent(record["content"]); content != "" {
				items = append(items, content)
			}
		}
	}
	return joinLines(items)
}

func joinLines(items []string) string {
	if len(items) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item) == "" {
			continue
		}
		filtered = append(filtered, strings.TrimSpace(item))
	}
	return strings.Join(filtered, "\n")
}
