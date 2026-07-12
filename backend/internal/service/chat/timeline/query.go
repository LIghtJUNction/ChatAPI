package timeline

import (
	"context"
	"strings"

	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

func (s *Service) ListTimelineForOwner(ctx context.Context, conversationID string, ownerID string) ([]Item, error) {
	conversationID = strings.TrimSpace(conversationID)
	ownerID = strings.TrimSpace(ownerID)
	conversation, err := s.store.GetConversation(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if ownerID != "" && conversationstate.OwnerID(conversation) != ownerID {
		return nil, turnquerysvc.ErrForbidden
	}
	return s.ListTimeline(ctx, conversationID)
}
