package handlers

import (
	"sort"

	"github.com/zyf/chatapi/internal/store"
)

func requestParsedSummary(item store.Request) map[string]any {
	return map[string]any{
		"request_id":        item.RequestID,
		"request_format":    item.RequestFormat,
		"model":             item.Model,
		"system_text":       item.SystemText,
		"developer_text":    item.DeveloperText,
		"assistant_text":    item.AssistantText,
		"user_text":         item.InputText,
		"input_part_types":  inputPartTypes(item.InputParts),
		"tool_choice":       item.ToolChoice,
		"response_format":   item.ResponseFormat,
		"request_body_keys": keysOf(item.RequestBody),
	}
}

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

func inputPartTypes(parts []store.RequestInputPart) []string {
	if len(parts) == 0 {
		return nil
	}
	types := make([]string, 0, len(parts))
	for _, part := range parts {
		if part.Type == "" {
			continue
		}
		types = append(types, part.Type)
	}
	if len(types) == 0 {
		return nil
	}
	return types
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
