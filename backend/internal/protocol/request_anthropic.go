package protocol

func extractAnthropicInputParts(body map[string]any) []InputPart {
	messages, ok := body["messages"].([]any)
	if !ok {
		return nil
	}
	for i := len(messages) - 1; i >= 0; i-- {
		record, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if stringValue(record["role"], "") == "user" {
			return extractPartsFromMessageContent(record["content"])
		}
	}
	return nil
}

func extractAnthropicRoleContent(body map[string]any, role string) string {
	if role == "system" {
		return flattenMessageContent(body["system"])
	}
	return extractMessagesRoleContent(body, role)
}
