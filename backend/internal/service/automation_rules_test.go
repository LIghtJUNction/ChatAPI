package service

import (
	"context"
	"testing"
)

func TestAutomationRuleServiceReplaceRulesCanonicalizesTypedConditions(t *testing.T) {
	st := newAutomationTestStore(t)
	svc := NewAutomationRuleService(st)

	rules, err := svc.ReplaceRules(context.Background(), "user_auto", nil, []map[string]any{
		{
			"id":      "rule_canonical",
			"enabled": true,
			"conditions": map[string]any{
				"contains": []map[string]any{
					{"type": "tool_choice_is", "name": "lookup_weather", "choice_type": "function"},
				},
			},
			"action": map[string]any{
				"type": "output_text",
				"text": "ok",
			},
		},
	})
	if err != nil {
		t.Fatalf("replace rules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("unexpected rules response: %#v", rules)
	}
	conditions := nestedMap(t, rules[0], "conditions")
	contains := nestedMapSlice(t, conditions["contains"])
	if len(contains) != 1 || testStringValue(contains[0]["type"], "") != "tool_choice_is" || testStringValue(contains[0]["choice_type"], "") != "function" {
		t.Fatalf("unexpected canonicalized rule payload: %#v", rules[0])
	}
}

func TestAutomationRuleServiceReplaceRulesRejectsInvalidTypedCondition(t *testing.T) {
	st := newAutomationTestStore(t)
	svc := NewAutomationRuleService(st)

	_, err := svc.ReplaceRules(context.Background(), "user_auto", nil, []map[string]any{
		{
			"id":      "rule_invalid",
			"enabled": true,
			"conditions": map[string]any{
				"contains": []map[string]any{
					{"type": "tool_choice_is"},
				},
			},
			"action": map[string]any{
				"type": "output_text",
				"text": "bad",
			},
		},
	})
	if err != ErrInvalidAutomationRule {
		t.Fatalf("expected invalid automation rule, got %v", err)
	}
}

func TestParseAutomationRulePayloadRejectsUnknownConditionType(t *testing.T) {
	_, err := ParseAutomationRulePayload(map[string]any{
		"id":      "rule_unknown_type",
		"enabled": true,
		"conditions": map[string]any{
			"contains": []map[string]any{
				{"type": "unknown_condition", "value": "x"},
			},
		},
		"action": map[string]any{
			"type": "output_text",
			"text": "bad",
		},
	})
	if err != ErrInvalidAutomationRule {
		t.Fatalf("expected invalid automation rule, got %v", err)
	}
}

func TestAutomationRuleServiceSchemaIncludesTypedConditions(t *testing.T) {
	schema := NewAutomationRuleService(nil).Schema()
	if len(schema.ActionTypes) != 1 || schema.ActionTypes[0] != "output_text" {
		t.Fatalf("unexpected action types: %#v", schema)
	}
	if len(schema.LegacyFields) == 0 || len(schema.TypedConditionTypes) == 0 {
		t.Fatalf("unexpected automation schema: %#v", schema)
	}
	foundToolChoice := false
	for _, item := range schema.TypedConditionTypes {
		if item.Type == "tool_choice_is" {
			foundToolChoice = true
			break
		}
	}
	if !foundToolChoice {
		t.Fatalf("expected tool_choice_is typed condition in schema: %#v", schema)
	}
}

func nestedMap(t *testing.T, value map[string]any, key string) map[string]any {
	t.Helper()
	item, ok := value[key].(map[string]any)
	if !ok {
		t.Fatalf("expected map for key %q, got %#v", key, value[key])
	}
	return item
}

func nestedMapSlice(t *testing.T, value any) []map[string]any {
	t.Helper()
	if items, ok := value.([]map[string]any); ok {
		return items
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected slice, got %#v", value)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected map item, got %#v", item)
		}
		out = append(out, record)
	}
	return out
}

func testStringValue(value any, fallback string) string {
	raw, _ := value.(string)
	if raw == "" {
		return fallback
	}
	return raw
}
