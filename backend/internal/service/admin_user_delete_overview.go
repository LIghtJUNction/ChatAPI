package service

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

type AdminUserDeleteOverview struct {
	User                       store.User                   `json:"user"`
	Preview                    store.UserDeletionPreview    `json:"preview"`
	OwnershipItems             store.UserOwnershipSelection `json:"ownership_items"`
	OwnershipConversationCount int                          `json:"ownership_conversation_count"`
	OwnershipUploadCount       int                          `json:"ownership_upload_count"`
	RecommendedNextActions     []string                     `json:"recommended_next_actions"`
}

type AdminUserDeleteOverviewService struct {
	deletion  *AdminUserDeletionService
	ownership *AdminUserOwnershipService
}

func NewAdminUserDeleteOverviewService(dataStore store.Store) *AdminUserDeleteOverviewService {
	return &AdminUserDeleteOverviewService{
		deletion:  NewAdminUserDeletionService(dataStore),
		ownership: NewAdminUserOwnershipService(dataStore),
	}
}

func (s *AdminUserDeleteOverviewService) Get(ctx context.Context, userID string) (AdminUserDeleteOverview, error) {
	if s == nil || s.deletion == nil || s.ownership == nil {
		return AdminUserDeleteOverview{}, ErrInvalidUserInput
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return AdminUserDeleteOverview{}, ErrInvalidUserInput
	}
	preview, err := s.deletion.Preview(ctx, userID)
	if err != nil {
		return AdminUserDeleteOverview{}, err
	}
	items, err := s.ownership.Items(ctx, userID)
	if err != nil {
		return AdminUserDeleteOverview{}, err
	}
	result := AdminUserDeleteOverview{
		User:                       preview.User,
		Preview:                    preview,
		OwnershipItems:             items,
		OwnershipConversationCount: len(items.Conversations),
		OwnershipUploadCount:       len(items.Uploads),
		RecommendedNextActions:     buildDeleteOverviewRecommendations(preview, items),
	}
	return result, nil
}

func buildDeleteOverviewRecommendations(preview store.UserDeletionPreview, items store.UserOwnershipSelection) []string {
	actions := make([]string, 0, 4)
	if preview.CanDelete {
		return append(actions, "purge_user")
	}
	if preview.Counts.OwnedConversations > 0 {
		if len(items.Conversations) > 0 {
			actions = append(actions, "review_ownership_items")
			actions = append(actions, "transfer_or_cleanup_conversations")
		}
	}
	if preview.Counts.OwnedUploadedImages > 0 {
		if len(items.Uploads) > 0 {
			actions = append(actions, "review_ownership_items")
			actions = append(actions, "transfer_or_cleanup_uploads")
		}
	}
	if preview.Counts.Identities > 0 {
		actions = append(actions, "review_identities")
	}
	if len(actions) == 0 {
		actions = append(actions, "review_delete_preview")
	}
	return dedupeStrings(actions)
}

func dedupeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
