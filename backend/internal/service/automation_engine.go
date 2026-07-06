package service

import (
	"context"
	"strings"

	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/store"
)

const automationMaxOutputLength = 8000

type AutomationMatch struct {
	RuleID string
	Input  store.CompletePendingInput
}

func (s *AutomationRuleService) MatchTurn(ctx context.Context, userID string, request protocol.TurnRequest, conversationID string, responseID string) (*AutomationMatch, error) {
	if s == nil || s.store == nil || strings.TrimSpace(userID) == "" {
		return nil, nil
	}
	items, err := s.store.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		match, ok := matchAutomationRule(item, request, conversationID, responseID)
		if ok {
			return match, nil
		}
	}
	return nil, nil
}

func matchAutomationRule(item store.AutomationRule, request protocol.TurnRequest, conversationID string, responseID string) (*AutomationMatch, bool) {
	if !item.Enabled {
		return nil, false
	}
	action, ok := item.Payload["action"].(map[string]any)
	if !ok || stringFromMap(action, "type") != "output_text" {
		return nil, false
	}
	outputText := strings.TrimSpace(stringFromMap(action, "text"))
	if outputText == "" {
		return nil, false
	}
	if !matchesRuleConditions(item.Payload, request) {
		return nil, false
	}
	return &AutomationMatch{
		RuleID: item.ID,
		Input: store.CompletePendingInput{
			ConversationID: conversationID,
			ResponseID:     strings.TrimSpace(responseID),
			OutputText:     truncateRunes(outputText, automationMaxOutputLength),
			Mode:           "assistant_message",
		},
	}, true
}

func matchesRuleConditions(payload map[string]any, request protocol.TurnRequest) bool {
	conditions, _ := payload["conditions"].(map[string]any)
	if len(conditions) == 0 {
		return true
	}
	for _, matcher := range mapSliceFromAny(conditions["contains"]) {
		if !requestMatchesPattern(request, matcher) {
			return false
		}
	}
	for _, matcher := range mapSliceFromAny(conditions["excludes"]) {
		if requestMatchesPattern(request, matcher) {
			return false
		}
	}
	return true
}

func requestMatchesPattern(request protocol.TurnRequest, matcher map[string]any) bool {
	pattern := strings.TrimSpace(stringFromMap(matcher, "pattern"))
	if pattern == "" {
		return false
	}
	matchType := stringFromMap(matcher, "match_type")
	if matchType == "" {
		matchType = "substring"
	}

	texts := make([]string, 0, 1+len(request.InputParts))
	if strings.TrimSpace(request.UserContent) != "" {
		texts = append(texts, request.UserContent)
	}
	for _, part := range request.InputParts {
		if strings.TrimSpace(part.Text) != "" {
			texts = append(texts, part.Text)
		}
	}
	for _, text := range texts {
		if matchText(matchType, text, pattern) {
			return true
		}
	}
	return false
}

func matchText(matchType string, text string, pattern string) bool {
	text = strings.TrimSpace(text)
	pattern = strings.TrimSpace(pattern)
	switch strings.ToLower(matchType) {
	case "equals", "exact":
		return text == pattern
	default:
		return strings.Contains(strings.ToLower(text), strings.ToLower(pattern))
	}
}

func mapSliceFromAny(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if ok {
			out = append(out, record)
		}
	}
	return out
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
