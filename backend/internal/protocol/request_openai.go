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
			part := extractToolResult(record["content"], stringValue(record["tool_call_id"], ""))
			if part.Type != "" {
				return []InputPart{part}
			}
			return nil
		}
	}
	return nil
}

func extractChatCompletionsRoleContent(body map[string]any, role string) string {
	return extractMessagesRoleContent(body, role)
}
