package egress

import (
	"strings"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) InvalidJSONBody(requestFormat string) map[string]any {
	return protocol.InvalidJSONError(requestFormat)
}

func (s *Service) ErrorStatus(err error) int {
	return protocol.HTTPStatus(err)
}

func (s *Service) ErrorBody(requestFormat string, err error) map[string]any {
	return protocol.BuildErrorBody(requestFormat, err)
}

func (s *Service) InternalErrorBody(requestFormat string, err error) map[string]any {
	if err == nil {
		return protocol.BuildErrorBody(requestFormat, protocol.InternalError("internal server error"))
	}
	return protocol.BuildErrorBody(requestFormat, protocol.InternalError(err.Error()))
}

func (s *Service) AbortBody(conversation common.Conversation, reason string) map[string]any {
	return protocol.AbortError(requestFormatOfConversation(conversation), reason)
}

func (s *Service) CompleteBody(conversation common.Conversation, input common.CompletePendingInput, message common.Message) map[string]any {
	return protocol.BuildResponseForMeta(protocol.ConversationMeta{
		Protocol:   protocol.ParseProtocol(requestFormatOfConversation(conversation)),
		Model:      stringValue(conversation.Metadata["model"], "chatapi-lab"),
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
	}, protocol.TurnResult{
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
		OutputText: message.Content,
		Mode:       input.Mode,
		ToolName:   input.ToolName,
		ToolCallID: input.ToolCallID,
		ToolOutput: stringValue(input.ToolOutput, message.Content),
	})
}

func requestFormatOfConversation(conversation common.Conversation) string {
	return stringValue(conversation.Metadata["request_format"], string(protocol.ProtocolResponses))
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
