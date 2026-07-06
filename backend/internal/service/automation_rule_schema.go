package service

import "strings"

type AutomationRuleDocument struct {
	ID         string
	Enabled    bool
	Conditions AutomationConditions
	Action     AutomationAction
}

type AutomationConditions struct {
	Contains []AutomationCondition
	Excludes []AutomationCondition
}

type AutomationCondition struct {
	Type       string
	Field      string
	MatchType  string
	Pattern    string
	Value      string
	Name       string
	ChoiceType string
	FormatType string
	PartType   string
	MediaType  string
	URL        string
}

type AutomationAction struct {
	Type string
	Text string
}

type AutomationRuleSchema struct {
	ActionTypes         []string                        `json:"action_types"`
	LegacyMatchTypes    []string                        `json:"legacy_match_types"`
	LegacyFields        []string                        `json:"legacy_fields"`
	TypedConditionTypes []AutomationConditionTypeSchema `json:"typed_condition_types"`
}

type AutomationConditionTypeSchema struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Fields      []string `json:"fields,omitempty"`
}

func ParseAutomationRulePayload(payload map[string]any) (AutomationRuleDocument, error) {
	if payload == nil {
		return AutomationRuleDocument{}, ErrInvalidAutomationRule
	}
	rule := AutomationRuleDocument{
		ID:      stringFromMap(payload, "id"),
		Enabled: true,
	}
	if rule.ID == "" {
		return AutomationRuleDocument{}, ErrInvalidAutomationRule
	}
	if raw, ok := payload["enabled"].(bool); ok {
		rule.Enabled = raw
	}
	action, ok := payload["action"].(map[string]any)
	if !ok {
		return AutomationRuleDocument{}, ErrInvalidAutomationRule
	}
	rule.Action = AutomationAction{
		Type: stringFromMap(action, "type"),
		Text: stringFromMap(action, "text"),
	}
	if rule.Action.Type != "output_text" || rule.Action.Text == "" {
		return AutomationRuleDocument{}, ErrInvalidAutomationRule
	}
	conditions, _ := payload["conditions"].(map[string]any)
	var err error
	rule.Conditions.Contains, err = parseAutomationConditions(conditions["contains"])
	if err != nil {
		return AutomationRuleDocument{}, err
	}
	rule.Conditions.Excludes, err = parseAutomationConditions(conditions["excludes"])
	if err != nil {
		return AutomationRuleDocument{}, err
	}
	return rule, nil
}

func BuildAutomationRuleSchema() AutomationRuleSchema {
	return AutomationRuleSchema{
		ActionTypes:      []string{"output_text"},
		LegacyMatchTypes: []string{"substring", "exact"},
		LegacyFields: []string{
			"text",
			"user_content",
			"system_content",
			"developer_content",
			"assistant_content",
			"tool_result",
			"input_part.text",
			"input_part.type",
			"input_part.media_type",
			"input_part.url",
			"tool_choice.type",
			"tool_choice.name",
			"response_format.type",
			"response_format.name",
			"model",
			"protocol",
		},
		TypedConditionTypes: []AutomationConditionTypeSchema{
			{Type: "text_contains", Description: "Case-insensitive substring match against combined request text.", Fields: []string{"value"}},
			{Type: "text_is", Description: "Exact match against combined request text.", Fields: []string{"value"}},
			{Type: "user_content_contains", Description: "Case-insensitive substring match against normalized user_content.", Fields: []string{"value"}},
			{Type: "user_content_is", Description: "Exact match against normalized user_content.", Fields: []string{"value"}},
			{Type: "system_content_contains", Description: "Case-insensitive substring match against normalized system content.", Fields: []string{"value"}},
			{Type: "system_content_is", Description: "Exact match against normalized system content.", Fields: []string{"value"}},
			{Type: "developer_content_contains", Description: "Case-insensitive substring match against normalized developer content.", Fields: []string{"value"}},
			{Type: "developer_content_is", Description: "Exact match against normalized developer content.", Fields: []string{"value"}},
			{Type: "assistant_content_contains", Description: "Case-insensitive substring match against normalized assistant content.", Fields: []string{"value"}},
			{Type: "assistant_content_is", Description: "Exact match against normalized assistant content.", Fields: []string{"value"}},
			{Type: "tool_result_contains", Description: "Case-insensitive substring match against normalized tool result content.", Fields: []string{"value"}},
			{Type: "tool_result_is", Description: "Exact match against normalized tool result content.", Fields: []string{"value"}},
			{Type: "model_is", Description: "Exact match against request model.", Fields: []string{"value"}},
			{Type: "protocol_is", Description: "Exact match against normalized protocol name.", Fields: []string{"value"}},
			{Type: "tool_choice_is", Description: "Exact match against tool choice name and/or type.", Fields: []string{"name", "choice_type"}},
			{Type: "response_format_is", Description: "Exact match against response format name and/or type.", Fields: []string{"name", "format_type"}},
			{Type: "input_part_type_is", Description: "Exact match against any input part type.", Fields: []string{"value"}},
			{Type: "input_media_type_contains", Description: "Case-insensitive substring match against any input part media type.", Fields: []string{"value"}},
			{Type: "input_media_type_is", Description: "Exact match against any input part media type.", Fields: []string{"value"}},
			{Type: "input_url_contains", Description: "Case-insensitive substring match against any input part URL.", Fields: []string{"value"}},
		},
	}
}

func parseAutomationConditions(value any) ([]AutomationCondition, error) {
	items := mapSliceFromAny(value)
	if len(items) == 0 {
		return nil, nil
	}
	conditions := make([]AutomationCondition, 0, len(items))
	for _, item := range items {
		condition, err := parseAutomationCondition(item)
		if err != nil {
			return nil, err
		}
		conditions = append(conditions, condition)
	}
	return conditions, nil
}

func parseAutomationCondition(item map[string]any) (AutomationCondition, error) {
	condition := AutomationCondition{
		Type:       normalizeAutomationField(stringFromMap(item, "type")),
		Field:      normalizeAutomationField(stringFromMap(item, "field")),
		MatchType:  normalizeAutomationMatchType(stringFromMap(item, "match_type")),
		Pattern:    firstNonEmptyAutomationMapValue(item, "pattern"),
		Value:      firstNonEmptyAutomationMapValue(item, "value"),
		Name:       firstNonEmptyAutomationMapValue(item, "name"),
		ChoiceType: firstNonEmptyAutomationMapValue(item, "choice_type", "tool_type"),
		FormatType: firstNonEmptyAutomationMapValue(item, "format_type", "response_type"),
		PartType:   firstNonEmptyAutomationMapValue(item, "part_type"),
		MediaType:  firstNonEmptyAutomationMapValue(item, "media_type"),
		URL:        firstNonEmptyAutomationMapValue(item, "url"),
	}
	if condition.Type != "" {
		if !isSupportedAutomationConditionType(condition.Type) {
			return AutomationCondition{}, ErrInvalidAutomationRule
		}
		if !condition.hasTypedValue() {
			return AutomationCondition{}, ErrInvalidAutomationRule
		}
		return condition, nil
	}
	if condition.Pattern == "" {
		return AutomationCondition{}, ErrInvalidAutomationRule
	}
	if condition.MatchType == "" {
		condition.MatchType = "substring"
	}
	return condition, nil
}

func firstNonEmptyAutomationMapValue(matcher map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromMap(matcher, key); value != "" {
			return value
		}
	}
	return ""
}

func (c AutomationCondition) hasTypedValue() bool {
	switch c.Type {
	case "tool_choice_is":
		return c.Name != "" || c.ChoiceType != ""
	case "response_format_is":
		return c.Name != "" || c.FormatType != ""
	case "input_part_type_is":
		return c.Value != "" || c.PartType != ""
	case "input_media_type_contains", "input_media_type_is":
		return c.Value != "" || c.MediaType != ""
	case "input_url_contains":
		return c.Value != "" || c.URL != ""
	default:
		return c.Value != "" || c.Pattern != ""
	}
}

func isSupportedAutomationConditionType(value string) bool {
	switch value {
	case "text_contains", "text_is",
		"user_content_contains", "user_content_is",
		"system_content_contains", "system_content_is",
		"developer_content_contains", "developer_content_is",
		"assistant_content_contains", "assistant_content_is",
		"tool_result_contains", "tool_result_is",
		"model_is", "protocol_is",
		"tool_choice_is", "response_format_is",
		"input_part_type_is",
		"input_media_type_contains", "input_media_type_is",
		"input_url_contains":
		return true
	default:
		return false
	}
}

func normalizeAutomationMatchType(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	switch value {
	case "", "substring", "exact", "equals":
		return value
	default:
		return ""
	}
}

func (r AutomationRuleDocument) ToMap() map[string]any {
	payload := map[string]any{
		"id":      r.ID,
		"enabled": r.Enabled,
		"action": map[string]any{
			"type": r.Action.Type,
			"text": r.Action.Text,
		},
	}
	conditions := map[string]any{}
	if items := automationConditionsToAny(r.Conditions.Contains); len(items) > 0 {
		conditions["contains"] = items
	}
	if items := automationConditionsToAny(r.Conditions.Excludes); len(items) > 0 {
		conditions["excludes"] = items
	}
	if len(conditions) > 0 {
		payload["conditions"] = conditions
	}
	return payload
}

func automationConditionsToAny(items []AutomationCondition) []map[string]any {
	if len(items) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, item.ToMap())
	}
	return out
}

func (c AutomationCondition) ToMap() map[string]any {
	payload := map[string]any{}
	if c.Type != "" {
		payload["type"] = c.Type
		setAutomationField(payload, "value", c.Value)
		setAutomationField(payload, "pattern", c.Pattern)
		setAutomationField(payload, "name", c.Name)
		setAutomationField(payload, "choice_type", c.ChoiceType)
		setAutomationField(payload, "format_type", c.FormatType)
		setAutomationField(payload, "part_type", c.PartType)
		setAutomationField(payload, "media_type", c.MediaType)
		setAutomationField(payload, "url", c.URL)
		return payload
	}
	setAutomationField(payload, "field", c.Field)
	setAutomationField(payload, "match_type", c.MatchType)
	setAutomationField(payload, "pattern", c.Pattern)
	return payload
}

func setAutomationField(payload map[string]any, key string, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		payload[key] = value
	}
}
