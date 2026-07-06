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
