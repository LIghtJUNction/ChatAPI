package config

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	"go.uber.org/zap"
)

type Deps struct {
	Configs configrepo.Store
	Chat    chat.Store
	Events  chatevents.Publisher
	Logger  *zap.Logger
}

type Service struct {
	configs configrepo.Store
	chat    chat.Store
	events  chatevents.Publisher
	logger  *zap.Logger
}

func New(deps Deps) *Service {
	return &Service{configs: deps.Configs, chat: deps.Chat, events: deps.Events, logger: deps.Logger}
}

func (s *Service) GetUserConfig(ctx context.Context, userID string) (common.UserConfig, error) {
	item, err := s.configs.GetUserConfig(ctx, strings.TrimSpace(userID), "settings")
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("config.key", "settings")).Debug("usercontrol config fetched user config")
	}
	return item, err
}

func (s *Service) UpdateUserConfig(ctx context.Context, userID string, value map[string]any) (common.UserConfig, error) {
	item, err := s.configs.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: strings.TrimSpace(userID),
		Key:    "settings",
		Value:  cloneMap(value),
	})
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("config.key", "settings")).Info("usercontrol config updated user config")
	}
	return item, err
}

func (s *Service) ListAutomationRules(ctx context.Context, userID string) ([]common.AutomationRule, error) {
	items, err := s.configs.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.Int("rules.count", len(items))).Debug("usercontrol config listed automation rules")
	}
	return items, err
}

func (s *Service) ReplaceAutomationRules(ctx context.Context, userID string, rules []map[string]any) ([]common.AutomationRule, error) {
	inputs := make([]common.UpsertAutomationRuleInput, 0, len(rules))
	for _, item := range rules {
		payload := cloneMap(item)
		id := stringValue(payload["id"], "")
		enabled, _ := payload["enabled"].(bool)
		delete(payload, "id")
		delete(payload, "enabled")
		if strings.TrimSpace(id) == "" {
			id = "rule_" + uuid.NewString()
		}
		inputs = append(inputs, common.UpsertAutomationRuleInput{
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

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (common.DeleteConversationsResult, error) {
	result, err := s.chat.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
	if err == nil {
		chatevents.PublishDeletedConversations(ctx, s.events, result)
	}
	return result, err
}

func (s *Service) DeleteConversations(ctx context.Context, conversationIDs []string) (common.DeleteConversationsResult, error) {
	result, err := s.chat.DeleteConversations(ctx, conversationIDs)
	if err == nil {
		chatevents.PublishDeletedConversations(ctx, s.events, result)
	}
	return result, err
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
