package user

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	appkeysvc "github.com/zyf/chatapi/internal/service/auth/appkey"
	modelkeysvc "github.com/zyf/chatapi/internal/service/auth/modelkey"
	"github.com/zyf/chatapi/internal/store"
)

type Service struct {
	store     store.Store
	appKeys   *appkeysvc.Service
	modelKeys *modelkeysvc.Service
}

func NewService(dataStore store.Store, appKeys *appkeysvc.Service, modelKeys *modelkeysvc.Service) *Service {
	return &Service{store: dataStore, appKeys: appKeys, modelKeys: modelKeys}
}

func (s *Service) GetUser(ctx context.Context, userID string) (store.User, error) {
	return s.store.GetUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	return s.store.ListUserIdentities(ctx, strings.TrimSpace(userID))
}

func (s *Service) ListAppKeys(ctx context.Context, userID string) ([]store.AppAPIKey, error) {
	return s.store.ListAppAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateAppKey(ctx context.Context, userID string, name string, scopes []string, resourceLimits map[string]any, expiresAt *time.Time) (store.AppAPIKey, string, error) {
	return s.appKeys.CreateKey(ctx, strings.TrimSpace(userID), strings.TrimSpace(name), scopes, resourceLimits, expiresAt)
}

func (s *Service) RevokeAppKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeAppAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) ListModelKeys(ctx context.Context, userID string) ([]store.ModelAPIKey, error) {
	return s.store.ListModelAPIKeysByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) CreateModelKey(ctx context.Context, userID string, name string, modelName string) (store.ModelAPIKey, string, error) {
	return s.modelKeys.CreateKey(ctx, strings.TrimSpace(userID), strings.TrimSpace(name), strings.TrimSpace(modelName))
}

func (s *Service) RevokeModelKey(ctx context.Context, userID string, keyID string) error {
	return s.store.RevokeModelAPIKey(ctx, strings.TrimSpace(keyID), strings.TrimSpace(userID))
}

func (s *Service) ListAutomationRules(ctx context.Context, userID string) ([]store.AutomationRule, error) {
	return s.store.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
}

func (s *Service) ReplaceAutomationRules(ctx context.Context, userID string, rules []store.UpsertAutomationRuleInput) ([]store.AutomationRule, error) {
	existing, err := s.store.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	replaceIDs := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		replaceIDs[strings.TrimSpace(item.ID)] = struct{}{}
	}
	return s.store.ReplaceAutomationRulesForUser(ctx, strings.TrimSpace(userID), replaceIDs, normalizeRuleInputs(rules))
}

func (s *Service) GetConfig(ctx context.Context, userID string, key string) (store.UserConfig, error) {
	return s.store.GetUserConfig(ctx, strings.TrimSpace(userID), strings.TrimSpace(key))
}

func (s *Service) SetConfig(ctx context.Context, userID string, key string, value map[string]any) (store.UserConfig, error) {
	return s.store.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: strings.TrimSpace(userID),
		Key:    strings.TrimSpace(key),
		Value:  cloneMap(value),
	})
}

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (store.DeleteConversationsResult, error) {
	return s.store.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
}

func (s *Service) DeleteConversations(ctx context.Context, conversationIDs []string) (store.DeleteConversationsResult, error) {
	return s.store.DeleteConversations(ctx, conversationIDs)
}

func normalizeRuleInputs(items []store.UpsertAutomationRuleInput) []store.UpsertAutomationRuleInput {
	result := make([]store.UpsertAutomationRuleInput, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = "rule_" + uuid.NewString()
		}
		result = append(result, store.UpsertAutomationRuleInput{
			ID:      id,
			UserID:  strings.TrimSpace(item.UserID),
			Enabled: item.Enabled,
			Payload: cloneMap(item.Payload),
		})
	}
	return result
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}
