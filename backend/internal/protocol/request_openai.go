package protocol

func extractChatCompletionsInputParts(body map[string]any) []InputPart {
	messages, ok := body["messages"].([]any)
	if !ok {
		return nil
	}
	for i := len(messages) - 1; i >= 0; i-- {
		record, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		switch stringValue(record["role"], "") {
		case "user":
			return extractPartsFromMessageContent(record["content"])
		case "tool":
			parts := extractToolResultParts(record["content"])
			for idx := range parts {
				if parts[idx].Type == "tool_result" {
					parts[idx].ToolCallID = stringValue(record["tool_call_id"], "")
				}
			}
			return parts
		}
	}
	return nil
}

func extractChatCompletionsRoleContent(body map[string]any, role string) string {
	return extractMessagesRoleContent(body, role)
}
