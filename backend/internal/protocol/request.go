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
	Protocol    Protocol
	Model       string
	Stream      bool
	UserContent string
	ToolSchemas []any
}

type TurnResult struct {
	ResponseID string
	OutputText string
	Mode       string
	ToolName   string
	ToolCallID string
	ToolOutput string
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

func ParseRequest(protocolValue string, body map[string]any) TurnRequest {
	return TurnRequest{
		Protocol:    ParseProtocol(protocolValue),
		Model:       stringValue(body["model"], "chatapi-lab"),
		Stream:      boolValue(body["stream"]),
		UserContent: extractUserText(body),
		ToolSchemas: extractToolSchemas(body),
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
