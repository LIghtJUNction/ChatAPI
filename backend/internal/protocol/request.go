package protocol

import (
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type Protocol string

const (
	ProtocolResponses         Protocol = "responses"
	ProtocolChatCompletions   Protocol = "chat_completions"
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
)

type TurnRequest struct {
	Protocol       Protocol
	Model          string
	Stream         bool
	UserContent    string
	InputParts     []InputPart
	ToolSchemas    []any
	ToolChoice     ToolChoice
	ResponseFormat ResponseFormat
}

type TurnResult struct {
	ResponseID string
	OutputText string
	Mode       string
	ToolName   string
	ToolCallID string
	ToolOutput string
	Usage      Usage
}

type ConversationMeta struct {
	Protocol   Protocol
	Model      string
	ResponseID string
}

type PendingStreamEvent struct {
	Type      string
	DeltaText string
	ErrorBody map[string]any
	Result    TurnResult
}

type InputPart struct {
	Type      string
	Text      string
	MediaType string
	URL       string
}

type ToolChoice struct {
	Type string
	Name string
}

type ResponseFormat struct {
	Type   string
	Name   string
	Schema map[string]any
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func ParseRequest(protocolValue string, body map[string]any) TurnRequest {
	inputParts := extractInputParts(body)
	return TurnRequest{
		Protocol:       ParseProtocol(protocolValue),
		Model:          stringValue(body["model"], "chatapi-lab"),
		Stream:         boolValue(body["stream"]),
		UserContent:    joinInputPartText(inputParts),
		InputParts:     inputParts,
		ToolSchemas:    extractToolSchemas(body),
		ToolChoice:     extractToolChoice(body),
		ResponseFormat: extractResponseFormat(body),
	}
}

func ParseProtocol(value string) Protocol {
	switch strings.TrimSpace(value) {
	case string(ProtocolChatCompletions):
		return ProtocolChatCompletions
	case string(ProtocolAnthropicMessages):
		return ProtocolAnthropicMessages
	default:
		return ProtocolResponses
	}
}

func (p Protocol) String() string {
	return string(p)
}

func (p Protocol) IsAnthropicMessages() bool {
	return p == ProtocolAnthropicMessages
}

func ConversationMetaFromConversation(conversation store.Conversation) ConversationMeta {
	return ConversationMeta{
		Protocol:   ParseProtocol(stringValue(conversation.Metadata["request_format"], string(ProtocolResponses))),
		Model:      stringValue(conversation.Metadata["model"], "chatapi-lab"),
		ResponseID: stringValue(conversation.ResponseID, ""),
	}
}

func (meta ConversationMeta) BuildStreamStart() []StreamEvent {
	return BuildStreamStart(meta)
}

func (meta ConversationMeta) BuildStreamDelta(deltaText string) []StreamEvent {
	return BuildStreamDelta(meta, deltaText)
}

func (meta ConversationMeta) BuildStreamComplete(result TurnResult) []StreamEvent {
	return BuildStreamComplete(meta, result)
}

func (meta ConversationMeta) BuildStreamAbort(body map[string]any) []StreamEvent {
	return BuildStreamAbort(meta, body)
}

func (meta ConversationMeta) BuildPendingStreamEvents(event PendingStreamEvent, anthropicBlockStarted bool) ([]StreamEvent, bool) {
	if meta.Protocol.IsAnthropicMessages() {
		switch event.Type {
		case "delta":
			if !anthropicBlockStarted {
				return append([]StreamEvent{BuildAnthropicContentBlockStart(event.Result)}, meta.BuildStreamDelta(event.DeltaText)...), true
			}
			return meta.BuildStreamDelta(event.DeltaText), true
		case "complete":
			streamEvents := make([]StreamEvent, 0, 4)
			if !anthropicBlockStarted {
				streamEvents = append(streamEvents, BuildAnthropicContentBlockStart(event.Result))
			}
			streamEvents = append(streamEvents, meta.BuildStreamComplete(event.Result)...)
			return streamEvents, true
		case "abort":
			return meta.BuildStreamAbort(event.ErrorBody), anthropicBlockStarted
		default:
			return nil, anthropicBlockStarted
		}
	}

	switch event.Type {
	case "delta":
		return meta.BuildStreamDelta(event.DeltaText), anthropicBlockStarted
	case "complete":
		return meta.BuildStreamComplete(event.Result), anthropicBlockStarted
	case "abort":
		return meta.BuildStreamAbort(event.ErrorBody), anthropicBlockStarted
	default:
		return nil, anthropicBlockStarted
	}
}

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

func extractToolSchemas(body map[string]any) []any {
	if tools, ok := body["tools"].([]any); ok {
		return tools
	}
	return nil
}

func extractInputParts(body map[string]any) []InputPart {
	if input, ok := body["input"].(string); ok && strings.TrimSpace(input) != "" {
		return []InputPart{{Type: "text", Text: strings.TrimSpace(input)}}
	}
	if input, ok := body["input"].([]any); ok {
		return extractPartsFromTurnInput(input)
	}
	if messages, ok := body["messages"].([]any); ok {
		for i := len(messages) - 1; i >= 0; i-- {
			record, ok := messages[i].(map[string]any)
			if !ok {
				continue
			}
			role := stringValue(record["role"], "")
			switch role {
			case "user":
				return extractPartsFromMessageContent(record["content"])
			case "tool":
				return extractToolResultParts(record["content"])
			}
		}
	}
	return nil
}

func extractPartsFromTurnInput(input []any) []InputPart {
	parts := make([]InputPart, 0)
	for _, item := range input {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		part := extractInputPart(record)
		if part.Type != "" {
			parts = append(parts, part)
			continue
		}
		role := stringValue(record["role"], stringValue(record["type"], ""))
		switch role {
		case "user":
			parts = append(parts, extractPartsFromMessageContent(record["content"])...)
		case "tool":
			parts = append(parts, extractToolResultParts(record["content"])...)
		default:
			if stringValue(record["type"], "") == "message" {
				parts = append(parts, extractPartsFromMessageContent(record["content"])...)
			}
		}
	}
	return parts
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

func extractToolChoice(body map[string]any) ToolChoice {
	switch typed := body["tool_choice"].(type) {
	case string:
		return ToolChoice{Type: strings.TrimSpace(typed)}
	case map[string]any:
		choice := ToolChoice{
			Type: stringValue(typed["type"], ""),
		}
		choice.Name = firstNonEmptyText(typed["name"], nestedStringValue(typed, "function", "name"))
		return choice
	default:
		return ToolChoice{}
	}
}

func extractResponseFormat(body map[string]any) ResponseFormat {
	record, ok := body["response_format"].(map[string]any)
	if !ok {
		return ResponseFormat{}
	}
	format := ResponseFormat{
		Type: stringValue(record["type"], ""),
	}
	if schemaRecord, ok := record["json_schema"].(map[string]any); ok {
		format.Name = stringValue(schemaRecord["name"], "")
		if schema, ok := schemaRecord["schema"].(map[string]any); ok {
			format.Schema = schema
		}
	}
	return format
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
