package debugview

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

func TestProjectRequestEmitsProviderSpecificChips(t *testing.T) {
	request := protocol.ParseRequest("anthropic_messages", map[string]any{
		"model":          "claude-test",
		"messages":       []any{map[string]any{"role": "user", "content": "hello"}},
		"temperature":    0.4,
		"top_k":          float64(40),
		"thinking":       map[string]any{"type": "enabled", "budget_tokens": 1024},
		"mcp_servers":    []any{map[string]any{"type": "url", "url": "https://mcp.example"}},
		"unknown_vendor": "kept",
	})

	projection := ProjectRequest(request)
	if len(projection.OptionChips) == 0 {
		t.Fatal("expected option chips")
	}
	assertChip(t, projection, "temperature", "temp", SupportNormalized)
	assertChip(t, projection, "top_k", "top_k", SupportNormalized)
	assertChip(t, projection, "thinking", "thinking", SupportProviderSpecific)
	assertChip(t, projection, "mcp_servers", "mcp", SupportProviderSpecific)
	assertChip(t, projection, "provider_extras", "extras", SupportStoredOnly)
}

func TestProjectRequestMarksUnsupportedChatN(t *testing.T) {
	request := protocol.ParseRequest("chat_completions", map[string]any{
		"model":    "gpt-test",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
		"n":        float64(3),
	})

	projection := ProjectRequest(request)
	assertChip(t, projection, "n", "n", SupportUnsupported)
}

func assertChip(t *testing.T, projection Projection, key string, label string, support SupportLevel) {
	t.Helper()
	for _, chip := range projection.OptionChips {
		if chip.Key == key {
			if chip.Label != label || chip.SupportLevel != support {
				t.Fatalf("unexpected chip for %s: %#v", key, chip)
			}
			return
		}
	}
	t.Fatalf("missing chip %q in %#v", key, projection.OptionChips)
}
