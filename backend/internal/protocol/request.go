package protocol

import "strings"

type Protocol string

const (
	ProtocolResponses         Protocol = "responses"
	ProtocolChatCompletions   Protocol = "chat_completions"
	ProtocolAnthropicMessages Protocol = "anthropic_messages"
)

type TurnRequest struct {
	Protocol         Protocol
	Model            string
	Stream           bool
	SystemContent    string
	DeveloperContent string
	AssistantContent string
	UserContent      string
	InputParts       []InputPart
	ToolSchemas      []ToolSchema
	ToolChoice       ToolChoice
	ResponseFormat   ResponseFormat
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

type NormalizedToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Type        string         `json:"type"`
}

type ToolSchema struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Raw         map[string]any `json:"raw,omitempty"`
}

type Usage struct {
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func ParseRequest(protocolValue string, body map[string]any) TurnRequest {
	proto := ParseProtocol(protocolValue)
	inputParts := extractRequestInputParts(proto, body)
	return TurnRequest{
		Protocol:         proto,
		Model:            stringValue(body["model"], "chatapi-lab"),
		Stream:           boolValue(body["stream"]),
		SystemContent:    extractRequestRoleContent(proto, body, "system"),
		DeveloperContent: extractRequestRoleContent(proto, body, "developer"),
		AssistantContent: extractRequestRoleContent(proto, body, "assistant"),
		UserContent:      joinInputPartText(inputParts),
		InputParts:       inputParts,
		ToolSchemas:      extractToolSchemas(body),
		ToolChoice:       extractToolChoice(body),
		ResponseFormat:   extractResponseFormat(body),
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

func extractRequestInputParts(proto Protocol, body map[string]any) []InputPart {
	switch proto {
	case ProtocolChatCompletions:
		return extractChatCompletionsInputParts(body)
	case ProtocolAnthropicMessages:
		return extractAnthropicInputParts(body)
	default:
		return extractResponsesInputParts(body)
	}
}

func extractRequestRoleContent(proto Protocol, body map[string]any, role string) string {
	switch proto {
	case ProtocolChatCompletions:
		return extractChatCompletionsRoleContent(body, role)
	case ProtocolAnthropicMessages:
		return extractAnthropicRoleContent(body, role)
	default:
		return extractResponsesRoleContent(body, role)
	}
}
