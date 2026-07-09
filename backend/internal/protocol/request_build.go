package protocol

func BuildRequestBody(request TurnRequest) map[string]any {
	switch request.Protocol {
	case ProtocolChatCompletions:
		return buildChatCompletionsRequestBody(request)
	case ProtocolAnthropicMessages:
		return buildAnthropicRequestBody(request)
	default:
		return buildResponsesRequestBody(request)
	}
}

func buildResponsesRequestBody(request TurnRequest) map[string]any {
	body := map[string]any{
		"model": request.Model,
		"input": buildResponsesInput(request.InputParts),
	}
	if request.Stream {
		body["stream"] = true
	}
	if request.ConversationID != "" {
		body["conversation_id"] = request.ConversationID
	}
	if len(request.ToolSchemas) > 0 {
		body["tools"] = RawToolSchemas(request.ToolSchemas)
	}
	if toolChoice := buildToolChoiceBody(request.ToolChoice); toolChoice != nil {
		body["tool_choice"] = toolChoice
	}
	if responseFormat := buildResponseFormatBody(request.ResponseFormat); responseFormat != nil {
		body["response_format"] = responseFormat
	}
	return body
}

func buildChatCompletionsRequestBody(request TurnRequest) map[string]any {
	body := map[string]any{
		"model":    request.Model,
		"messages": buildChatCompletionsMessages(request),
	}
	if request.Stream {
		body["stream"] = true
	}
	if request.ConversationID != "" {
		body["conversation_id"] = request.ConversationID
	}
	if len(request.ToolSchemas) > 0 {
		body["tools"] = RawToolSchemas(request.ToolSchemas)
	}
	if toolChoice := buildToolChoiceBody(request.ToolChoice); toolChoice != nil {
		body["tool_choice"] = toolChoice
	}
	if responseFormat := buildResponseFormatBody(request.ResponseFormat); responseFormat != nil {
		body["response_format"] = responseFormat
	}
	return body
}

func buildAnthropicRequestBody(request TurnRequest) map[string]any {
	body := map[string]any{
		"model":    request.Model,
		"messages": buildAnthropicMessages(request),
	}
	if request.Stream {
		body["stream"] = true
	}
	if request.ConversationID != "" {
		body["conversation_id"] = request.ConversationID
	}
	if request.SystemContent != "" {
		body["system"] = []any{map[string]any{"type": "text", "text": request.SystemContent}}
	}
	if len(request.ToolSchemas) > 0 {
		body["tools"] = buildAnthropicTools(request.ToolSchemas)
	}
	return body
}

func buildResponsesInput(parts []InputPart) []any {
	items := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			items = append(items, map[string]any{"type": "input_text", "text": part.Text})
		case "image":
			item := map[string]any{"type": "input_image", "image_url": part.URL}
			if part.MediaType != "" {
				item["media_type"] = part.MediaType
			}
			items = append(items, item)
		case "tool_result":
			item := map[string]any{"type": "function_call_output", "output": part.Text}
			if part.ToolCallID != "" {
				item["call_id"] = part.ToolCallID
			}
			items = append(items, item)
		}
	}
	return items
}

func buildChatCompletionsMessages(request TurnRequest) []any {
	items := make([]any, 0, 4)
	if request.SystemContent != "" {
		items = append(items, map[string]any{"role": "system", "content": request.SystemContent})
	}
	if request.DeveloperContent != "" {
		items = append(items, map[string]any{"role": "developer", "content": request.DeveloperContent})
	}
	if request.AssistantContent != "" {
		items = append(items, map[string]any{"role": "assistant", "content": request.AssistantContent})
	}
	if payload := buildChatMessageContent(request.InputParts); len(payload) > 0 {
		items = append(items, map[string]any{"role": "user", "content": payload})
	}
	return items
}

func buildAnthropicMessages(request TurnRequest) []any {
	items := make([]any, 0, 2)
	if request.AssistantContent != "" {
		items = append(items, map[string]any{"role": "assistant", "content": request.AssistantContent})
	}
	if payload := buildAnthropicMessageContent(request.InputParts); len(payload) > 0 {
		items = append(items, map[string]any{"role": "user", "content": payload})
	}
	return items
}

func buildChatMessageContent(parts []InputPart) []any {
	items := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			items = append(items, map[string]any{"type": "text", "text": part.Text})
		case "image":
			items = append(items, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": part.URL},
			})
		}
	}
	return items
}

func buildAnthropicMessageContent(parts []InputPart) []any {
	items := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			items = append(items, map[string]any{"type": "text", "text": part.Text})
		case "image":
			items = append(items, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "url",
					"url":        part.URL,
					"media_type": part.MediaType,
				},
			})
		case "tool_result":
			item := map[string]any{
				"type":    "tool_result",
				"content": []any{map[string]any{"type": "text", "text": part.Text}},
			}
			if part.ToolCallID != "" {
				item["tool_use_id"] = part.ToolCallID
			}
			items = append(items, item)
		}
	}
	return items
}

func buildToolChoiceBody(choice ToolChoice) any {
	if choice.Type == "" {
		return nil
	}
	if choice.Name == "" {
		return choice.Type
	}
	return map[string]any{
		"type": choice.Type,
		"function": map[string]any{
			"name": choice.Name,
		},
	}
}

func buildResponseFormatBody(format ResponseFormat) map[string]any {
	if format.Type == "" {
		return nil
	}
	body := map[string]any{"type": format.Type}
	if format.Type == "json_schema" {
		body["json_schema"] = map[string]any{
			"name":   format.Name,
			"schema": cloneMap(format.Schema),
		}
	}
	return body
}

func buildAnthropicTools(items []ToolSchema) []any {
	tools := make([]any, 0, len(items))
	for _, item := range items {
		tools = append(tools, map[string]any{
			"name":         item.Name,
			"description":  item.Description,
			"input_schema": cloneMap(item.Parameters),
		})
	}
	return tools
}
