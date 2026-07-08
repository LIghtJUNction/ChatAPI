package conversationresolve

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type PendingLookup interface {
	FindConversationIDByToolCallID(ownerID string, toolCallID string) (string, bool)
}

type Service struct {
	Store   chat.Reader
	Pending PendingLookup
}

type ResolveInput struct {
	OwnerID string
	Request protocol.TurnRequest
	RawBody map[string]any
}

type Target struct {
	ConversationID string
	Reuse          bool
	Source         string
}

func New(store chat.Reader, pending PendingLookup) *Service {
	return &Service{Store: store, Pending: pending}
}

func (s *Service) Resolve(ctx context.Context, input ResolveInput) (Target, error) {
	ownerID := strings.TrimSpace(input.OwnerID)
	if conversationID := explicitConversationID(input.RawBody); conversationID != "" {
		conversation, err := s.Store.GetConversation(ctx, conversationID)
		if err != nil {
			return Target{}, err
		}
		if ownerID != "" && ownerID != ownerIDOfConversation(conversation) {
			return Target{}, common.ErrNotFound
		}
		if !protocolCompatible(conversation, input.Request.Protocol.String()) {
			return Target{}, common.ErrTurnConflict
		}
		return Target{ConversationID: conversationID, Reuse: true, Source: "explicit_id"}, nil
	}
	for _, toolCallID := range extractToolCallIDs(input.RawBody) {
		if s.Pending != nil {
			if conversationID, ok := s.Pending.FindConversationIDByToolCallID(ownerID, toolCallID); ok {
				return Target{ConversationID: conversationID, Reuse: true, Source: "pending_tool_call_id"}, nil
			}
		}
		if conversation, err := s.Store.FindConversationByToolCallID(ctx, ownerID, toolCallID); err == nil {
			return Target{ConversationID: conversation.ID, Reuse: true, Source: "stored_tool_call_id"}, nil
		} else if err != nil && err != common.ErrNotFound {
			return Target{}, err
		}
	}
	return Target{}, nil
}

func explicitConversationID(body map[string]any) string {
	return strings.TrimSpace(stringValue(body["conversation_id"], ""))
}

func extractToolCallIDs(body map[string]any) []string {
	seen := map[string]struct{}{}
	var ids []string
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if id := strings.TrimSpace(stringValue(typed["tool_call_id"], "")); id != "" {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					ids = append(ids, id)
				}
			}
			if id := strings.TrimSpace(stringValue(typed["call_id"], "")); id != "" {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					ids = append(ids, id)
				}
			}
			for _, item := range typed {
				visit(item)
			}
		case []any:
			for _, item := range typed {
				visit(item)
			}
		}
	}
	visit(body)
	return ids
}

func protocolCompatible(conversation common.Conversation, requestFormat string) bool {
	locked := strings.TrimSpace(stringValue(conversation.Metadata["request_format"], ""))
	if locked == "" {
		return true
	}
	return locked == strings.TrimSpace(requestFormat)
}

func ownerIDOfConversation(conversation common.Conversation) string {
	return strings.TrimSpace(stringValue(conversation.Metadata["owner_id"], ""))
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
