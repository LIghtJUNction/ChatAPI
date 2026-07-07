package service

import (
	"context"
	"errors"
	"testing"

	"github.com/zyf/chatapi/internal/protocol"
)

func TestToolCallAssistServiceRejectsUnsupportedProviderWhenRegistryEmpty(t *testing.T) {
	svc := NewToolCallAssistService(nil)
	if _, err := svc.Execute(context.Background(), "user", "kirari", "model", "req", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden without workspace, got %v", err)
	}
}

func TestToolCallAssistServiceRegistersProvidersByNormalizedName(t *testing.T) {
	svc := NewToolCallAssistService(&WorkspaceToolCallService{}, stubUpstreamProvider{name: " Kirari "})
	if svc.providers[providerKirari] == nil {
		t.Fatalf("expected normalized kirari provider registration: %#v", svc.providers)
	}
}

type stubUpstreamProvider struct {
	name string
}

func (s stubUpstreamProvider) ProviderName() string {
	return s.name
}

func (s stubUpstreamProvider) ChatCompletions(ctx context.Context, userID string, body any) (map[string]any, error) {
	return nil, nil
}

func TestDecodeAssistOutputSupportsFencedJSON(t *testing.T) {
	parsed, err := decodeAssistOutput("before\n```json\n{\"explanation\":\"ok\",\"tool_call\":{\"name\":\"lookup_weather\",\"arguments\":{}}}\n```\nafter")
	if err != nil {
		t.Fatalf("decode fenced assist output: %v", err)
	}
	if stringValueFromMap(parsed, "explanation") != "ok" {
		t.Fatalf("unexpected parsed fenced assist output: %#v", parsed)
	}
}

func TestFinalizeAssistResultValidatesDeclaredTool(t *testing.T) {
	result := finalizeAssistResult("browser_upstream", "demo-model", map[string]any{"request_id": "req_demo"}, `{"explanation":"ok","tool_call":{"name":"lookup_weather","arguments":{"city":"Beijing"}}}`, []protocol.NormalizedToolSchema{
		{Name: "lookup_weather"},
	})
	if !result.ValidDraft {
		t.Fatalf("expected valid draft: %#v", result)
	}
	if result.ToolCall == nil || stringValueAny(result.ToolCall["name"]) != "lookup_weather" {
		t.Fatalf("unexpected tool call: %#v", result)
	}
}
