package streaming

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) StreamPendingTurn(ctx context.Context, w http.ResponseWriter, conversation common.Conversation, events <-chan turnsvc.PendingEvent) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return http.ErrNotSupported
	}

	anthropicBlockStarted := false
	for _, event := range s.buildStartEvents(conversation) {
		if err := writeSSEEvent(w, event); err != nil {
			return err
		}
		flusher.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-events:
			if !ok {
				return nil
			}
			streamEvents, nextAnthropicBlockStarted := s.buildPendingEvents(conversation, event, anthropicBlockStarted)
			anthropicBlockStarted = nextAnthropicBlockStarted
			for _, streamEvent := range streamEvents {
				if err := writeSSEEvent(w, streamEvent); err != nil {
					return err
				}
				flusher.Flush()
			}
		}
	}
}

func (s *Service) buildStartEvents(conversation common.Conversation) []protocol.StreamEvent {
	meta := conversationMeta(conversation, "")
	return meta.BuildStreamStart()
}

func (s *Service) buildPendingEvents(conversation common.Conversation, event turnsvc.PendingEvent, anthropicBlockStarted bool) ([]protocol.StreamEvent, bool) {
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

func writeSSEEvent(w http.ResponseWriter, event protocol.StreamEvent) error {
	if event.Event != "" {
		if _, err := w.Write([]byte("event: " + event.Event + "\n")); err != nil {
			return err
		}
	}
	switch data := event.Data.(type) {
	case string:
		_, err := w.Write([]byte("data: " + data + "\n\n"))
		return err
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("data: " + string(raw) + "\n\n"))
		return err
	}
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
