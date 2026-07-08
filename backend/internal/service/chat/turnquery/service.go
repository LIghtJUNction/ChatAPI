package turnquery

import (
	"context"
	"errors"

	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"go.uber.org/zap"
)

var ErrForbidden = errors.New("forbidden")

type Service struct {
	Store  chat.Store
	Logger *zap.Logger
}

func (s *Service) ListMessages(ctx context.Context, conversationID string) ([]common.Message, error) {
	items, err := s.Store.ListMessages(ctx, conversationID)
	if err != nil {
		s.logger().Warn("list messages failed", zap.String("conversation.id", conversationID), zap.Error(err))
		return nil, err
	}
	s.logger().Debug("listed messages", zap.String("conversation.id", conversationID), zap.Int("messages.count", len(items)))
	return items, nil
}

func (s *Service) ListMessagesForOwner(ctx context.Context, conversationID string, ownerID string) ([]common.Message, error) {
	conversation, err := s.Store.GetConversation(ctx, conversationID)
	if err != nil {
		s.logger().Warn("owner message lookup failed at conversation fetch", zap.String("conversation.id", conversationID), zap.String("owner.id", ownerID), zap.Error(err))
		return nil, err
	}
	if ownerID != "" && stringValue(conversation.Metadata["owner_id"], "") != ownerID {
		s.logger().Warn("owner message lookup forbidden", zap.String("conversation.id", conversationID), zap.String("owner.id", ownerID))
		return nil, ErrForbidden
	}
	items, err := s.Store.ListMessages(ctx, conversationID)
	if err != nil {
		s.logger().Warn("owner message lookup list failed", zap.String("conversation.id", conversationID), zap.String("owner.id", ownerID), zap.Error(err))
		return nil, err
	}
	s.logger().Debug("listed messages for owner", zap.String("conversation.id", conversationID), zap.String("owner.id", ownerID), zap.Int("messages.count", len(items)))
	return items, nil
}

func (s *Service) ListConversationsForOwner(ctx context.Context, ownerID string) ([]common.Conversation, error) {
	items, err := s.Store.ListConversations(ctx)
	if err != nil {
		s.logger().Warn("list conversations failed", zap.String("owner.id", ownerID), zap.Error(err))
		return nil, err
	}
	filtered := make([]common.Conversation, 0, len(items))
	for _, item := range items {
		if ownerID == "" || stringValue(item.Metadata["owner_id"], "") == ownerID {
			filtered = append(filtered, item)
		}
	}
	s.logger().Debug("listed conversations for owner", zap.String("owner.id", ownerID), zap.Int("conversations.count", len(filtered)))
	return filtered, nil
}

func (s *Service) ListRequests(ctx context.Context) ([]common.Request, error) {
	items, err := s.Store.ListRequests(ctx)
	if err != nil {
		s.logger().Warn("list requests failed", zap.Error(err))
		return nil, err
	}
	s.logger().Debug("listed requests", zap.Int("requests.count", len(items)))
	return items, nil
}

func (s *Service) ListRequestsForOwner(ctx context.Context, ownerID string) ([]common.Request, error) {
	items, err := s.Store.ListRequests(ctx)
	if err != nil {
		s.logger().Warn("list requests for owner failed", zap.String("owner.id", ownerID), zap.Error(err))
		return nil, err
	}
	filtered := make([]common.Request, 0, len(items))
	for _, item := range items {
		if ownerID == "" || item.OwnerID == ownerID {
			filtered = append(filtered, item)
		}
	}
	s.logger().Debug("listed requests for owner", zap.String("owner.id", ownerID), zap.Int("requests.count", len(filtered)))
	return filtered, nil
}

func (s *Service) GetRequest(ctx context.Context, requestID string) (common.Request, error) {
	item, err := s.Store.GetRequest(ctx, requestID)
	if err != nil {
		s.logger().Warn("get request failed", zap.String("request.id", requestID), zap.Error(err))
		return common.Request{}, err
	}
	s.logger().Debug("fetched request", zap.String("request.id", requestID), zap.String("owner.id", item.OwnerID))
	return item, nil
}

func (s *Service) GetRequestForOwner(ctx context.Context, requestID string, ownerID string) (common.Request, error) {
	item, err := s.Store.GetRequest(ctx, requestID)
	if err != nil {
		s.logger().Warn("get request for owner failed", zap.String("request.id", requestID), zap.String("owner.id", ownerID), zap.Error(err))
		return common.Request{}, err
	}
	if ownerID != "" && item.OwnerID != ownerID {
		s.logger().Warn("get request for owner forbidden", zap.String("request.id", requestID), zap.String("owner.id", ownerID), zap.String("request.owner_id", item.OwnerID))
		return common.Request{}, ErrForbidden
	}
	s.logger().Debug("fetched request for owner", zap.String("request.id", requestID), zap.String("owner.id", ownerID))
	return item, nil
}

func (s *Service) logger() *zap.Logger {
	if s == nil || s.Logger == nil {
		return zap.NewNop()
	}
	return s.Logger
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && raw != "" {
		return raw
	}
	return fallback
}
