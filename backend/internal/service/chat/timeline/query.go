package timeline

import (
	"context"
	"strings"

	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

func (s *Service) ListTimelineForOwner(ctx context.Context, conversationID string, ownerID string) ([]Item, error) {
	conversationID = strings.TrimSpace(conversationID)
	ownerID = strings.TrimSpace(ownerID)
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if ownerID != "" && strings.TrimSpace(stringValue(conversation.Metadata["owner_id"], "")) != ownerID {
		return nil, turnquerysvc.ErrForbidden
	}
	return s.ListTimeline(ctx, conversationID)
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
