package config

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/repository/chatrepo"
	"github.com/zyf/chatapi/internal/repository/configrepo"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

type Deps struct {
	Configs configrepo.Store
	Chat    chatrepo.Store
	Logger *zap.Logger
}

type Service struct {
	configs configrepo.Store
	chat    chatrepo.Store
	logger *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{configs: deps.Configs, chat: deps.Chat, logger: deps.Logger}
}

func (s *Service) GetUserConfig(ctx context.Context, userID string) (store.UserConfig, error) {
	item, err := s.configs.GetUserConfig(ctx, strings.TrimSpace(userID), "settings")
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("config.key", "settings")).Debug("usercontrol config fetched user config")
	}
	return item, err
}

func (s *Service) UpdateUserConfig(ctx context.Context, userID string, value map[string]any) (store.UserConfig, error) {
	item, err := s.configs.SetUserConfig(ctx, store.SetUserConfigInput{
		UserID: strings.TrimSpace(userID),
		Key:    "settings",
		Value:  cloneMap(value),
	})
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("config.key", "settings")).Info("usercontrol config updated user config")
	}
	return item, err
}

func (s *Service) ListAutomationRules(ctx context.Context, userID string) ([]store.AutomationRule, error) {
	items, err := s.configs.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.Int("rules.count", len(items))).Debug("usercontrol config listed automation rules")
	}
	return items, err
}

func (s *Service) ReplaceAutomationRules(ctx context.Context, userID string, rules []map[string]any) ([]store.AutomationRule, error) {
	inputs := make([]store.UpsertAutomationRuleInput, 0, len(rules))
	for _, item := range rules {
		payload := cloneMap(item)
		id := stringValue(payload["id"], "")
		enabled, _ := payload["enabled"].(bool)
		delete(payload, "id")
		delete(payload, "enabled")
		if strings.TrimSpace(id) == "" {
			id = "rule_" + uuid.NewString()
		}
		inputs = append(inputs, store.UpsertAutomationRuleInput{
			ID:      id,
			UserID:  userID,
			Enabled: enabled,
			Payload: payload,
		})
	}
	existing, err := s.configs.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	replaceIDs := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		replaceIDs[strings.TrimSpace(item.ID)] = struct{}{}
	}
	items, err := s.configs.ReplaceAutomationRulesForUser(ctx, strings.TrimSpace(userID), replaceIDs, inputs)
	if err == nil {
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", strings.TrimSpace(userID)),
			zap.Int("rules.input_count", len(rules)),
			zap.Int("rules.existing_count", len(existing)),
			zap.Int("rules.output_count", len(items)),
		).Info("usercontrol config replaced automation rules")
	}
	return items, err
}

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (store.DeleteConversationsResult, error) {
	return s.chat.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
}

func (s *Service) DeleteConversations(ctx context.Context, conversationIDs []string) (store.DeleteConversationsResult, error) {
	return s.chat.DeleteConversations(ctx, conversationIDs)
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
