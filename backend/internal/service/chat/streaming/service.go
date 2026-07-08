package streaming

import (
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) BuildStartEvents(conversation common.Conversation) []protocol.StreamEvent {
	meta := conversationMeta(conversation, "")
	return meta.BuildStreamStart()
}

func (s *Service) BuildPendingEvents(conversation common.Conversation, event turnsvc.PendingEvent, anthropicBlockStarted bool) ([]protocol.StreamEvent, bool) {
	meta := conversationMeta(conversation, "")
	return meta.BuildPendingStreamEvents(protocol.PendingStreamEvent{
		Type:      event.Type,
		DeltaText: event.DeltaText,
		ErrorBody: event.ErrorBody,
		Result: protocol.TurnResult{
			ResponseID: stringValue(conversation.ResponseID, ""),
			OutputText: event.OutputText,
			Mode:       event.Mode,
			ToolName:   event.ToolName,
			ToolCallID: event.ToolCallID,
			ToolOutput: event.ToolOutput,
		},
	}, anthropicBlockStarted)
}

func conversationMeta(conversation common.Conversation, fallbackModel string) protocol.ConversationMeta {
	if fallbackModel == "" {
		fallbackModel = "chatapi-lab"
	}
	return protocol.ConversationMeta{
		Protocol:   protocol.ParseProtocol(stringValue(conversation.Metadata["request_format"], string(protocol.ProtocolResponses))),
		Model:      stringValue(conversation.Metadata["model"], fallbackModel),
		ResponseID: stringValue(conversation.ResponseID, ""),
	}
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && raw != "" {
		return raw
	}
	return fallback
}
