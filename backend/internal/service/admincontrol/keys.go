package admincontrol

import (
	"context"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Service) ListAppKeys(ctx context.Context, userID string) ([]common.AppAPIKey, error) {
	items, err := s.keyStore.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	active := make([]common.AppAPIKey, 0, len(items))
	for _, item := range items {
		if item.RevokedAt == nil {
			active = append(active, item)
		}
	}
	return active, nil
}

func (s *Service) RevokeAppKey(ctx context.Context, userID string, keyID string) error {
	return s.keyStore.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]common.ModelAPIKey, error) {
	items, err := s.keyStore.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	active := make([]common.ModelAPIKey, 0, len(items))
	for _, item := range items {
		if item.RevokedAt == nil {
			active = append(active, item)
		}
	}
	return active, nil
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	return s.keyStore.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}
