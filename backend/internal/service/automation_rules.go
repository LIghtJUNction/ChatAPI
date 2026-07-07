package service

import (
	"context"
	"errors"
	"strings"

	"github.com/zyf/chatapi/internal/store"
)

var ErrInvalidAutomationRule = errors.New("invalid automation rule")

type AutomationRuleService struct {
	store store.Store
}

func NewAutomationRuleService(dataStore store.Store) *AutomationRuleService {
	return &AutomationRuleService{store: dataStore}
}

func (s *AutomationRuleService) Schema() AutomationRuleSchema {
	return BuildAutomationRuleSchema()
}

func (s *AutomationRuleService) AppSchema() AutomationRuleSchema {
	return BuildAppAutomationRuleSchema()
}

func (s *AutomationRuleService) ListRules(ctx context.Context, userID string, allowedIDs map[string]struct{}) ([]map[string]any, error) {
	items, err := s.store.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	rules := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if len(allowedIDs) > 0 {
			if _, ok := allowedIDs[item.ID]; !ok {
				continue
			}
		}
		rules = append(rules, automationRulePayload(item))
	}
	return rules, nil
}

func (s *AutomationRuleService) ReplaceRules(ctx context.Context, userID string, allowedIDs map[string]struct{}, payloads []map[string]any) ([]map[string]any, error) {
	inputs := make([]store.UpsertAutomationRuleInput, 0, len(payloads))
	seen := make(map[string]struct{}, len(payloads))
	for _, payload := range payloads {
		rule, err := ParseAutomationRulePayload(payload)
		if err != nil {
			return nil, err
		}
		id := rule.ID
		if len(allowedIDs) > 0 {
			if _, ok := allowedIDs[id]; !ok {
				return nil, ErrForbidden
			}
		}
		if _, ok := seen[id]; ok {
			return nil, ErrInvalidAutomationRule
		}
		seen[id] = struct{}{}
		inputs = append(inputs, store.UpsertAutomationRuleInput{
			ID:      id,
			UserID:  strings.TrimSpace(userID),
			Enabled: rule.Enabled,
			Payload: rule.ToMap(),
		})
	}
	items, err := s.store.ReplaceAutomationRulesForUser(ctx, strings.TrimSpace(userID), allowedIDs, inputs)
	if err != nil {
		return nil, err
	}
	rules := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if len(allowedIDs) > 0 {
			if _, ok := allowedIDs[item.ID]; !ok {
				continue
			}
		}
		rules = append(rules, automationRulePayload(item))
	}
	return rules, nil
}

func automationRulePayload(item store.AutomationRule) map[string]any {
	rule, err := ParseAutomationRulePayload(item.Payload)
	if err != nil {
		payload := map[string]any{}
		for key, value := range item.Payload {
			payload[key] = value
		}
		payload["id"] = item.ID
		payload["enabled"] = item.Enabled
		return payload
	}
	rule.ID = item.ID
	rule.Enabled = item.Enabled
	return rule.ToMap()
}

func stringFromMap(value map[string]any, key string) string {
	raw, _ := value[key].(string)
	return strings.TrimSpace(raw)
}
