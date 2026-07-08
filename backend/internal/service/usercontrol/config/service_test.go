package config_test

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	userconfig "github.com/zyf/chatapi/internal/service/usercontrol/config"
	"github.com/zyf/chatapi/internal/store"
)

func TestConfigServiceGetUpdateAndReplaceRules(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	svc := userconfig.New(userconfig.Deps{Store: st})
	original := map[string]any{"theme": "dark", "nested": map[string]any{"x": 1}}

	item, err := svc.UpdateUserConfig(ctx, " user_a ", original)
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if item.UserID != "user_a" || item.Key != "settings" {
		t.Fatalf("unexpected config item: %#v", item)
	}
	item.Value["theme"] = "light"
	if original["theme"] != "dark" {
		t.Fatalf("input map should not be mutated: %#v", original)
	}

	got, err := svc.GetUserConfig(ctx, "user_a")
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if got.Value["theme"] != "dark" {
		t.Fatalf("unexpected config value: %#v", got.Value)
	}

	input := []map[string]any{
		{"name": "rule-a", "enabled": true},
		{"id": "rule_fixed", "name": "rule-b", "enabled": false, "kind": "http"},
	}
	rules, err := svc.ReplaceAutomationRules(ctx, "user_a", input)
	if err != nil {
		t.Fatalf("replace rules: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("unexpected rules len: %d", len(rules))
	}
	for _, rule := range rules {
		if rule.ID == "" {
			t.Fatalf("rule id should not be empty: %#v", rule)
		}
		if _, ok := rule.Payload["id"]; ok {
			t.Fatalf("payload should not contain id: %#v", rule.Payload)
		}
		if _, ok := rule.Payload["enabled"]; ok {
			t.Fatalf("payload should not contain enabled: %#v", rule.Payload)
		}
	}
	if _, ok := input[1]["enabled"]; !ok {
		t.Fatalf("input rules should not be mutated: %#v", input[1])
	}
}

func TestConfigServiceReplaceAutomationRulesRandomized(t *testing.T) {
	st := openConfigStore(t)
	ctx := context.Background()
	svc := userconfig.New(userconfig.Deps{Store: st})

	rng := rand.New(rand.NewSource(42))
	inputs := make([]map[string]any, 0, 20)
	for i := 0; i < 20; i++ {
		item := map[string]any{
			"name":    fmt.Sprintf("rule-%02d", i),
			"enabled": rng.Intn(2) == 0,
			"weight":  rng.Intn(1000),
		}
		if i%3 == 0 {
			item["id"] = fmt.Sprintf("rule_fixed_%02d", i)
		}
		inputs = append(inputs, item)
	}

	rules, err := svc.ReplaceAutomationRules(ctx, "user_random", inputs)
	if err != nil {
		t.Fatalf("replace randomized rules: %v", err)
	}
	if len(rules) != len(inputs) {
		t.Fatalf("unexpected rules len: got=%d want=%d", len(rules), len(inputs))
	}
	seen := map[string]struct{}{}
	for _, rule := range rules {
		if _, ok := seen[rule.ID]; ok {
			t.Fatalf("duplicate rule id: %s", rule.ID)
		}
		seen[rule.ID] = struct{}{}
	}
}

func openConfigStore(t *testing.T) store.Store {
	t.Helper()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}
	return st
}
