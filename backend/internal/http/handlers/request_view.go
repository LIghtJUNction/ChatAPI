package handlers

import (
	"sort"

	"github.com/zyf/chatapi/internal/store"
)

func requestParsedView(item store.Request) map[string]any {
	return map[string]any{
		"request_format":    item.RequestFormat,
		"model":             item.Model,
		"system_text":       item.SystemText,
		"developer_text":    item.DeveloperText,
		"assistant_text":    item.AssistantText,
		"user_text":         item.InputText,
		"input_parts":       item.InputParts,
		"tool_choice":       item.ToolChoice,
		"tool_schemas":      item.ToolSchemas,
		"response_format":   item.ResponseFormat,
		"request_body_keys": keysOf(item.RequestBody),
	}
}

func keysOf(value map[string]any) []string {
	if len(value) == 0 {
		return nil
	}
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
