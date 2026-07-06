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
	Status      string
	Match       *AutomationMatch
	SkipReasons []string
	SkipDetails []AutomationRuleSkipDetail
}

type AutomationRuleSkipDetail struct {
	RuleID string
	Reason string
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
	skipReasons := make([]string, 0, len(items))
	skipDetails := make([]AutomationRuleSkipDetail, 0, len(items))
	for _, item := range items {
		if item.Enabled {
			enabledRules++
		}
	}
	for _, item := range items {
		match, reason, ok := matchAutomationRule(item, request, conversationID, responseID)
		if ok {
			return AutomationDecision{Status: automationStatusMatched, Match: match}, nil
		}
		if reason != "" {
			skipReasons = append(skipReasons, reason)
			skipDetails = append(skipDetails, AutomationRuleSkipDetail{RuleID: item.ID, Reason: reason})
		}
	}
	if enabledRules == 0 {
		return AutomationDecision{Status: automationStatusNoRules}, nil
	}
	return AutomationDecision{Status: automationStatusNoMatch, SkipReasons: skipReasons, SkipDetails: skipDetails}, nil
}

func matchAutomationRule(item store.AutomationRule, request protocol.TurnRequest, conversationID string, responseID string) (*AutomationMatch, string, bool) {
	if !item.Enabled {
		return nil, "", false
	}
	rule, err := ParseAutomationRulePayload(item.Payload)
	if err != nil {
		return nil, "parse_invalid", false
	}
	if rule.Action.Type != "output_text" {
		return nil, "action_invalid", false
	}
	outputText := strings.TrimSpace(rule.Action.Text)
	if outputText == "" {
		return nil, "empty_output", false
	}
	matched, reason := matchesRuleConditions(rule.Conditions, request)
	if !matched {
		return nil, reason, false
	}
	return &AutomationMatch{
		RuleID: item.ID,
		Input: store.CompletePendingInput{
			ConversationID: conversationID,
			ResponseID:     strings.TrimSpace(responseID),
			OutputText:     truncateRunes(outputText, automationMaxOutputLength),
			Mode:           "assistant_message",
		},
	}, "", true
}

func matchesRuleConditions(conditions AutomationConditions, request protocol.TurnRequest) (bool, string) {
	if len(conditions.Contains) == 0 && len(conditions.Excludes) == 0 {
		return true, ""
	}
	for _, matcher := range conditions.Contains {
		if !requestMatchesCondition(request, matcher) {
			return false, "contains_miss"
		}
	}
	for _, matcher := range conditions.Excludes {
		if requestMatchesCondition(request, matcher) {
			return false, "excluded"
		}
	}
	return true, ""
}

func requestMatchesCondition(request protocol.TurnRequest, matcher AutomationCondition) bool {
	if matcher.Type != "" {
		return requestMatchesTypedCondition(request, matcher)
	}
	pattern := strings.TrimSpace(matcher.Pattern)
	if pattern == "" {
		return false
	}
	matchType := matcher.MatchType
	if matchType == "" {
		matchType = "substring"
	}
	for _, value := range requestMatchCandidates(request, matcher.Field) {
		if matchText(matchType, value, pattern) {
			return true
		}
	}
	return false
}

func requestMatchesTypedCondition(request protocol.TurnRequest, matcher AutomationCondition) bool {
	switch matcher.Type {
	case "text_contains":
		return matchesAutomationField(request, "text", "substring", firstNonEmptyStrings(matcher.Value, matcher.Pattern))
	case "text_is":
		return matchesAutomationField(request, "text", "exact", firstNonEmptyStrings(matcher.Value, matcher.Pattern))
	case "user_content_contains":
		return matchesAutomationField(request, "user_content", "substring", firstNonEmptyStrings(matcher.Value, matcher.Pattern))
	case "user_content_is":
		return matchesAutomationField(request, "user_content", "exact", firstNonEmptyStrings(matcher.Value, matcher.Pattern))
	case "model_is":
		return matchesAutomationField(request, "model", "exact", firstNonEmptyStrings(matcher.Value, matcher.Pattern))
	case "protocol_is":
		return matchesAutomationField(request, "protocol", "exact", firstNonEmptyStrings(matcher.Value, matcher.Pattern))
	case "tool_choice_is":
		name := firstNonEmptyStrings(matcher.Name, matcher.Value, matcher.Pattern)
		choiceType := matcher.ChoiceType
		if name != "" && !matchesAutomationField(request, "tool_choice_name", "exact", name) {
			return false
		}
		if choiceType != "" && !matchesAutomationField(request, "tool_choice_type", "exact", choiceType) {
			return false
		}
		return name != "" || choiceType != ""
	case "response_format_is":
		name := firstNonEmptyStrings(matcher.Name, matcher.Value, matcher.Pattern)
		formatType := matcher.FormatType
		if name != "" && !matchesAutomationField(request, "response_format_name", "exact", name) {
			return false
		}
		if formatType != "" && !matchesAutomationField(request, "response_format_type", "exact", formatType) {
			return false
		}
		return name != "" || formatType != ""
	case "input_part_type_is":
		return matchesAutomationField(request, "input_part_type", "exact", firstNonEmptyStrings(matcher.Value, matcher.Pattern, matcher.PartType))
	case "input_media_type_contains":
		return matchesAutomationField(request, "input_part_media_type", "substring", firstNonEmptyStrings(matcher.Value, matcher.Pattern, matcher.MediaType))
	case "input_media_type_is":
		return matchesAutomationField(request, "input_part_media_type", "exact", firstNonEmptyStrings(matcher.Value, matcher.Pattern, matcher.MediaType))
	case "input_url_contains":
		return matchesAutomationField(request, "input_part_url", "substring", firstNonEmptyStrings(matcher.Value, matcher.Pattern, matcher.URL))
	default:
		return false
	}
}

func firstNonEmptyStrings(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
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
	if typed, ok := value.([]map[string]any); ok {
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	}
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
