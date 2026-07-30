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
	body := cloneAnyMap(request.Options.ProviderExtras)
	if body == nil {
		body = map[string]any{}
	}
	body["model"] = request.Model
	body["input"] = buildResponsesInput(request.InputParts)
	if request.Options.Instructions != "" {
		body["instructions"] = request.Options.Instructions
	}
	if request.Options.PreviousResponseID != "" {
		body["previous_response_id"] = request.Options.PreviousResponseID
	}
	if request.Options.Store != nil {
		body["store"] = *request.Options.Store
	}
	if len(request.Options.Metadata) > 0 {
		body["metadata"] = cloneAnyMap(request.Options.Metadata)
	}
	if len(request.Options.Include) > 0 {
		body["include"] = append([]string(nil), request.Options.Include...)
	}
	if request.Options.MaxOutputTokens != nil {
		body["max_output_tokens"] = *request.Options.MaxOutputTokens
	}
	if request.Options.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *request.Options.ParallelToolCalls
	}
	if len(request.Options.Reasoning) > 0 {
		body["reasoning"] = cloneAnyMap(request.Options.Reasoning)
	}
	if request.Options.ServiceTier != "" {
		body["service_tier"] = request.Options.ServiceTier
	}
	if len(request.Options.StreamOptions) > 0 {
		body["stream_options"] = cloneAnyMap(request.Options.StreamOptions)
	}
	if request.Options.Temperature != nil {
		body["temperature"] = *request.Options.Temperature
	}
	if request.Options.TopP != nil {
		body["top_p"] = *request.Options.TopP
	}
	if len(request.Options.Text) > 0 {
		body["text"] = cloneAnyMap(request.Options.Text)
	}
	if request.Options.Truncation != "" {
		body["truncation"] = request.Options.Truncation
	}
	if request.Options.User != "" {
		body["user"] = request.Options.User
	}
	if responseFormat := buildResponsesTextFormatBody(request.ResponseFormat); responseFormat != nil {
		text, _ := body["text"].(map[string]any)
		if text == nil {
			text = map[string]any{}
		}
		text["format"] = responseFormat
		body["text"] = text
	}
	if request.Stream {
		body["stream"] = true
	}
	if request.ConversationID != "" {
		body["conversation_id"] = request.ConversationID
	}
	if tools := buildResponsesTools(request); len(tools) > 0 {
		body["tools"] = tools
	}
	if toolChoice := buildToolChoiceBody(request.ToolChoice); toolChoice != nil {
		body["tool_choice"] = toolChoice
	}
	return body
}

func buildResponsesTools(request TurnRequest) []any {
	tools := make([]any, 0, len(request.ToolSchemas)+len(request.BuiltinTools))
	tools = append(tools, RawToolSchemas(request.ToolSchemas)...)
	tools = append(tools, RawBuiltinTools(request.BuiltinTools)...)
	if len(tools) == 0 {
		return nil
	}
	return tools
}

func buildChatCompletionsRequestBody(request TurnRequest) map[string]any {
	body := cloneAnyMap(request.Options.ProviderExtras)
	if body == nil {
		body = map[string]any{}
	}
	body["model"] = request.Model
	body["messages"] = buildChatCompletionsMessages(request)
	applyOpenAICommonOptions(body, request.Options)
	if request.Options.MaxTokens != nil {
		body["max_tokens"] = *request.Options.MaxTokens
	}
	if request.Options.MaxCompletionTokens != nil {
		body["max_completion_tokens"] = *request.Options.MaxCompletionTokens
	}
	if len(request.Options.Stop) > 0 {
		body["stop"] = append([]string(nil), request.Options.Stop...)
	}
	if request.Options.N != nil {
		body["n"] = *request.Options.N
	}
	if request.Options.PresencePenalty != nil {
		body["presence_penalty"] = *request.Options.PresencePenalty
	}
	if request.Options.FrequencyPenalty != nil {
		body["frequency_penalty"] = *request.Options.FrequencyPenalty
	}
	if request.Options.Seed != nil {
		body["seed"] = *request.Options.Seed
	}
	if request.Options.ReasoningEffort != "" {
		body["reasoning_effort"] = request.Options.ReasoningEffort
	}
	if len(request.Options.Modalities) > 0 {
		body["modalities"] = append([]string(nil), request.Options.Modalities...)
	}
	if len(request.Options.Audio) > 0 {
		body["audio"] = cloneAnyMap(request.Options.Audio)
	}
	if len(request.Options.Prediction) > 0 {
		body["prediction"] = cloneAnyMap(request.Options.Prediction)
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
	body := cloneAnyMap(request.Options.ProviderExtras)
	if body == nil {
		body = map[string]any{}
	}
	body["model"] = request.Model
	body["messages"] = buildAnthropicMessages(request)
	if request.Options.MaxTokens != nil {
		body["max_tokens"] = *request.Options.MaxTokens
	}
	if request.Options.Temperature != nil {
		body["temperature"] = *request.Options.Temperature
	}
	if request.Options.TopP != nil {
		body["top_p"] = *request.Options.TopP
	}
	if request.Options.TopK != nil {
		body["top_k"] = *request.Options.TopK
	}
	if len(request.Options.Stop) > 0 {
		body["stop_sequences"] = append([]string(nil), request.Options.Stop...)
	}
	if len(request.Options.Metadata) > 0 {
		body["metadata"] = cloneAnyMap(request.Options.Metadata)
	}
	if len(request.Options.Thinking) > 0 {
		body["thinking"] = cloneAnyMap(request.Options.Thinking)
	}
	if request.Options.ServiceTier != "" {
		body["service_tier"] = request.Options.ServiceTier
	}
	if len(request.Options.MCPServers) > 0 {
		body["mcp_servers"] = cloneMapList(request.Options.MCPServers)
	}
	if len(request.Options.ContextManagement) > 0 {
		body["context_management"] = cloneAnyMap(request.Options.ContextManagement)
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

func applyOpenAICommonOptions(body map[string]any, options TurnOptions) {
	if len(options.Metadata) > 0 {
		body["metadata"] = cloneAnyMap(options.Metadata)
	}
	if options.Temperature != nil {
		body["temperature"] = *options.Temperature
	}
	if options.TopP != nil {
		body["top_p"] = *options.TopP
	}
	if options.User != "" {
		body["user"] = options.User
	}
	if len(options.StreamOptions) > 0 {
		body["stream_options"] = cloneAnyMap(options.StreamOptions)
	}
	if options.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *options.ParallelToolCalls
	}
	if options.ServiceTier != "" {
		body["service_tier"] = options.ServiceTier
	}
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
			if part.ToolResult == nil {
				continue
			}
			item := map[string]any{"type": "function_call_output", "output": buildResponsesToolResultContent(part.ToolResult.Content)}
			if part.ToolResult.CallID != "" {
				item["call_id"] = part.ToolResult.CallID
			}
			items = append(items, item)
		}
	}
	return items
}

func buildResponsesToolResultContent(parts []ContentPart) any {
	if len(parts) == 1 && parts[0].Type == "text" {
		return parts[0].Text
	}
	content := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			content = append(content, map[string]any{"type": "input_text", "text": part.Text})
		case "image":
			image := map[string]any{"type": "input_image", "image_url": part.URL}
			if part.MediaType != "" {
				image["media_type"] = part.MediaType
			}
			content = append(content, image)
		}
	}
	return content
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
	for _, message := range buildChatInputMessages(request.InputParts) {
		items = append(items, message)
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
			if part.ToolResult == nil {
				continue
			}
			item := map[string]any{
				"type":    "tool_result",
				"content": buildAnthropicToolResultContent(part.ToolResult.Content),
			}
			if part.ToolResult.CallID != "" {
				item["tool_use_id"] = part.ToolResult.CallID
			}
			items = append(items, item)
		}
	}
	return items
}

func buildChatInputMessages(parts []InputPart) []any {
	messages := make([]any, 0, len(parts))
	userContent := make([]any, 0, len(parts))
	flushUser := func() {
		if len(userContent) == 0 {
			return
		}
		messages = append(messages, map[string]any{"role": "user", "content": userContent})
		userContent = nil
	}
	for _, part := range parts {
		if part.Type != "tool_result" {
			userContent = append(userContent, buildChatMessageContent([]InputPart{part})...)
			continue
		}
		if part.ToolResult == nil {
			continue
		}
		flushUser()
		message := map[string]any{"role": "tool", "content": buildChatToolResultContent(part.ToolResult.Content)}
		if part.ToolResult.CallID != "" {
			message["tool_call_id"] = part.ToolResult.CallID
		}
		messages = append(messages, message)
	}
	flushUser()
	return messages
}

func buildChatToolResultContent(parts []ContentPart) any {
	if len(parts) == 1 && parts[0].Type == "text" {
		return parts[0].Text
	}
	content := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			content = append(content, map[string]any{"type": "text", "text": part.Text})
		case "image":
			content = append(content, map[string]any{"type": "image_url", "image_url": map[string]any{"url": part.URL}})
		}
	}
	return content
}

func buildAnthropicToolResultContent(parts []ContentPart) []any {
	content := make([]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "text":
			content = append(content, map[string]any{"type": "text", "text": part.Text})
		case "image":
			content = append(content, map[string]any{"type": "image", "source": map[string]any{
				"type": "url", "url": part.URL, "media_type": part.MediaType,
			}})
		}
	}
	return content
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

func buildResponsesTextFormatBody(format ResponseFormat) map[string]any {
	if format.Type == "" {
		return nil
	}
	if format.Type != "json_schema" {
		return map[string]any{"type": format.Type}
	}
	return map[string]any{
		"type":   "json_schema",
		"name":   format.Name,
		"schema": cloneMap(format.Schema),
	}
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
