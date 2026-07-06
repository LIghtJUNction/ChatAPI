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

type AutomationDecision struct {
	Status string
	Match  *AutomationMatch
}

const (
	automationStatusNoRules = "no_rules"
	automationStatusNoMatch = "no_match"
	automationStatusMatched = "matched"
)

func (s *AutomationRuleService) MatchTurn(ctx context.Context, userID string, request protocol.TurnRequest, conversationID string, responseID string) (AutomationDecision, error) {
	if s == nil || s.store == nil || strings.TrimSpace(userID) == "" {
		return AutomationDecision{Status: automationStatusNoRules}, nil
	}
	items, err := s.store.ListAutomationRulesByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		return AutomationDecision{}, err
	}
	enabledRules := 0
	for _, item := range items {
		if item.Enabled {
			enabledRules++
		}
	}
	for _, item := range items {
		match, ok := matchAutomationRule(item, request, conversationID, responseID)
		if ok {
			return AutomationDecision{Status: automationStatusMatched, Match: match}, nil
		}
	}
	if enabledRules == 0 {
		return AutomationDecision{Status: automationStatusNoRules}, nil
	}
	return AutomationDecision{Status: automationStatusNoMatch}, nil
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
	if conditionType := stringFromMap(matcher, "type"); conditionType != "" {
		return requestMatchesTypedCondition(request, conditionType, matcher)
	}
	pattern := strings.TrimSpace(stringFromMap(matcher, "pattern"))
	if pattern == "" {
		return false
	}
	matchType := stringFromMap(matcher, "match_type")
	if matchType == "" {
		matchType = "substring"
	}
	for _, value := range requestMatchCandidates(request, stringFromMap(matcher, "field")) {
		if matchText(matchType, value, pattern) {
			return true
		}
	}
	return false
}

func requestMatchesTypedCondition(request protocol.TurnRequest, conditionType string, matcher map[string]any) bool {
	switch strings.TrimSpace(strings.ToLower(conditionType)) {
	case "text_contains":
		return matchesAutomationField(request, "text", "substring", firstNonEmptyAutomationValue(matcher, "value", "pattern"))
	case "text_is":
		return matchesAutomationField(request, "text", "exact", firstNonEmptyAutomationValue(matcher, "value", "pattern"))
	case "user_content_contains":
		return matchesAutomationField(request, "user_content", "substring", firstNonEmptyAutomationValue(matcher, "value", "pattern"))
	case "user_content_is":
		return matchesAutomationField(request, "user_content", "exact", firstNonEmptyAutomationValue(matcher, "value", "pattern"))
	case "model_is":
		return matchesAutomationField(request, "model", "exact", firstNonEmptyAutomationValue(matcher, "value", "pattern", "model"))
	case "protocol_is":
		return matchesAutomationField(request, "protocol", "exact", firstNonEmptyAutomationValue(matcher, "value", "pattern", "protocol"))
	case "tool_choice_is":
		name := firstNonEmptyAutomationValue(matcher, "name", "value", "pattern")
		choiceType := firstNonEmptyAutomationValue(matcher, "choice_type", "tool_type")
		if name != "" && !matchesAutomationField(request, "tool_choice_name", "exact", name) {
			return false
		}
		if choiceType != "" && !matchesAutomationField(request, "tool_choice_type", "exact", choiceType) {
			return false
		}
		return name != "" || choiceType != ""
	case "response_format_is":
		name := firstNonEmptyAutomationValue(matcher, "name", "value", "pattern")
		formatType := firstNonEmptyAutomationValue(matcher, "format_type", "response_type")
		if name != "" && !matchesAutomationField(request, "response_format_name", "exact", name) {
			return false
		}
		if formatType != "" && !matchesAutomationField(request, "response_format_type", "exact", formatType) {
			return false
		}
		return name != "" || formatType != ""
	case "input_part_type_is":
		return matchesAutomationField(request, "input_part_type", "exact", firstNonEmptyAutomationValue(matcher, "value", "pattern", "part_type"))
	case "input_media_type_contains":
		return matchesAutomationField(request, "input_part_media_type", "substring", firstNonEmptyAutomationValue(matcher, "value", "pattern", "media_type"))
	case "input_media_type_is":
		return matchesAutomationField(request, "input_part_media_type", "exact", firstNonEmptyAutomationValue(matcher, "value", "pattern", "media_type"))
	case "input_url_contains":
		return matchesAutomationField(request, "input_part_url", "substring", firstNonEmptyAutomationValue(matcher, "value", "pattern", "url"))
	default:
		return false
	}
}

func firstNonEmptyAutomationValue(matcher map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromMap(matcher, key); value != "" {
			return value
		}
	}
	return ""
}

func matchesAutomationField(request protocol.TurnRequest, field string, matchType string, pattern string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	for _, value := range requestMatchCandidates(request, field) {
		if matchText(matchType, value, pattern) {
			return true
		}
	}
	return false
}

func requestMatchCandidates(request protocol.TurnRequest, field string) []string {
	switch normalizeAutomationField(field) {
	case "", "text":
		return appendRequestTextCandidates(request)
	case "user_content", "input_text":
		if strings.TrimSpace(request.UserContent) == "" {
			return nil
		}
		return []string{request.UserContent}
	case "input_part_text":
		return appendInputPartTexts(request.InputParts)
	case "input_part_type":
		return appendInputPartField(request.InputParts, func(part protocol.InputPart) string { return part.Type })
	case "input_part_media_type":
		return appendInputPartField(request.InputParts, func(part protocol.InputPart) string { return part.MediaType })
	case "input_part_url":
		return appendInputPartField(request.InputParts, func(part protocol.InputPart) string { return part.URL })
	case "tool_choice_type":
		return singleCandidate(request.ToolChoice.Type)
	case "tool_choice_name":
		return singleCandidate(request.ToolChoice.Name)
	case "response_format_type":
		return singleCandidate(request.ResponseFormat.Type)
	case "response_format_name":
		return singleCandidate(request.ResponseFormat.Name)
	case "model":
		return singleCandidate(request.Model)
	case "protocol":
		return singleCandidate(request.Protocol.String())
	default:
		return nil
	}
}

func normalizeAutomationField(field string) string {
	field = strings.TrimSpace(strings.ToLower(field))
	field = strings.ReplaceAll(field, ".", "_")
	return field
}

func appendRequestTextCandidates(request protocol.TurnRequest) []string {
	texts := make([]string, 0, 1+len(request.InputParts))
	if strings.TrimSpace(request.UserContent) != "" {
		texts = append(texts, request.UserContent)
	}
	texts = append(texts, appendInputPartTexts(request.InputParts)...)
	return texts
}

func appendInputPartTexts(parts []protocol.InputPart) []string {
	return appendInputPartField(parts, func(part protocol.InputPart) string { return part.Text })
}

func appendInputPartField(parts []protocol.InputPart, getter func(protocol.InputPart) string) []string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(getter(part))
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}

func singleCandidate(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
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
