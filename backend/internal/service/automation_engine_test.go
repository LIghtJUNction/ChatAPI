package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/protocol"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
)

func TestAutomationRuleServiceMatchTurnMatchesContainsAndExcludes(t *testing.T) {
	st := newAutomationTestStore(t)
	svc := NewAutomationRuleService(st)
	if _, err := st.ReplaceAutomationRulesForUser(context.Background(), "user_auto", nil, []store.UpsertAutomationRuleInput{
		{
			ID:      "rule_skip",
			UserID:  "user_auto",
			Enabled: true,
			Payload: map[string]any{
				"id":      "rule_skip",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "hello"}},
					"excludes": []map[string]any{{"match_type": "substring", "pattern": "block"}},
				},
				"action": map[string]any{"type": "output_text", "text": "自动回复"},
			},
		},
	}); err != nil {
		t.Fatalf("seed automation rule: %v", err)
	}

	match, err := svc.MatchTurn(context.Background(), "user_auto", protocol.TurnRequest{
		UserContent: "hello automation",
		InputParts: []protocol.InputPart{
			{Type: "text", Text: "hello automation"},
		},
	}, "conv_1", "resp_1")
	if err != nil {
		t.Fatalf("match turn: %v", err)
	}
	if match == nil || match.RuleID != "rule_skip" || match.Input.OutputText != "自动回复" {
		t.Fatalf("unexpected automation match: %#v", match)
	}

	blocked, err := svc.MatchTurn(context.Background(), "user_auto", protocol.TurnRequest{
		UserContent: "hello block",
		InputParts: []protocol.InputPart{
			{Type: "text", Text: "hello block"},
		},
	}, "conv_2", "resp_2")
	if err != nil {
		t.Fatalf("match blocked turn: %v", err)
	}
	if blocked != nil {
		t.Fatalf("expected blocked turn to skip automation match: %#v", blocked)
	}
}

func TestAutomationRuleServiceMatchTurnTruncatesLongOutput(t *testing.T) {
	st := newAutomationTestStore(t)
	svc := NewAutomationRuleService(st)
	longText := make([]rune, automationMaxOutputLength+10)
	for i := range longText {
		longText[i] = 'a'
	}
	if _, err := st.ReplaceAutomationRulesForUser(context.Background(), "user_auto", nil, []store.UpsertAutomationRuleInput{
		{
			ID:      "rule_long",
			UserID:  "user_auto",
			Enabled: true,
			Payload: map[string]any{
				"id":      "rule_long",
				"enabled": true,
				"action":  map[string]any{"type": "output_text", "text": string(longText)},
			},
		},
	}); err != nil {
		t.Fatalf("seed automation rule: %v", err)
	}
	match, err := svc.MatchTurn(context.Background(), "user_auto", protocol.TurnRequest{
		UserContent: "anything",
	}, "conv_1", "resp_1")
	if err != nil {
		t.Fatalf("match long output turn: %v", err)
	}
	if match == nil || len([]rune(match.Input.OutputText)) != automationMaxOutputLength {
		t.Fatalf("unexpected truncated automation output: %#v", match)
	}
}

func TestAutomationRuleServiceMatchTurnMatchesStructuredFields(t *testing.T) {
	st := newAutomationTestStore(t)
	svc := NewAutomationRuleService(st)
	if _, err := st.ReplaceAutomationRulesForUser(context.Background(), "user_auto", nil, []store.UpsertAutomationRuleInput{
		{
			ID:      "rule_structured",
			UserID:  "user_auto",
			Enabled: true,
			Payload: map[string]any{
				"id":      "rule_structured",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{
						{"field": "protocol", "match_type": "exact", "pattern": "responses"},
						{"field": "model", "match_type": "exact", "pattern": "demo-structured"},
						{"field": "tool_choice.name", "match_type": "exact", "pattern": "lookup_weather"},
						{"field": "response_format.name", "match_type": "exact", "pattern": "tool_draft"},
						{"field": "input_part.type", "match_type": "exact", "pattern": "image"},
						{"field": "input_part.media_type", "match_type": "substring", "pattern": "png"},
					},
				},
				"action": map[string]any{"type": "output_text", "text": "结构化命中"},
			},
		},
	}); err != nil {
		t.Fatalf("seed automation rule: %v", err)
	}

	match, err := svc.MatchTurn(context.Background(), "user_auto", protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "demo-structured",
		InputParts: []protocol.InputPart{
			{Type: "text", Text: "show me the chart"},
			{Type: "image", MediaType: "image/png", URL: "https://example.com/demo.png"},
		},
		ToolChoice:     protocol.ToolChoice{Type: "function", Name: "lookup_weather"},
		ResponseFormat: protocol.ResponseFormat{Type: "json_schema", Name: "tool_draft"},
	}, "conv_structured", "resp_structured")
	if err != nil {
		t.Fatalf("match structured turn: %v", err)
	}
	if match == nil || match.RuleID != "rule_structured" || match.Input.OutputText != "结构化命中" {
		t.Fatalf("unexpected structured automation match: %#v", match)
	}

	miss, err := svc.MatchTurn(context.Background(), "user_auto", protocol.TurnRequest{
		Protocol:       protocol.ProtocolResponses,
		Model:          "demo-structured",
		InputParts:     []protocol.InputPart{{Type: "image", MediaType: "image/jpeg", URL: "https://example.com/demo.jpg"}},
		ToolChoice:     protocol.ToolChoice{Type: "function", Name: "lookup_weather"},
		ResponseFormat: protocol.ResponseFormat{Type: "json_schema", Name: "tool_draft"},
	}, "conv_structured_miss", "resp_structured_miss")
	if err != nil {
		t.Fatalf("match structured miss turn: %v", err)
	}
	if miss != nil {
		t.Fatalf("expected structured matcher miss: %#v", miss)
	}
}

func newAutomationTestStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}
	return st
}
