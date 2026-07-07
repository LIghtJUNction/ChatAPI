package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
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
	name    string
	payload map[string]any
	err     error
}

func (s stubUpstreamProvider) ProviderName() string {
	return s.name
}

func (s stubUpstreamProvider) ProviderDescriptor() UpstreamProviderDescriptor {
	return UpstreamProviderDescriptor{
		Name:              normalizeProviderName(s.name),
		DisplayName:       "Stub Provider",
		Protocols:         []string{"chat_completions"},
		SupportsStreaming: false,
		Capabilities: map[string]any{
			"backend_delegated":          true,
			"supports_structured_output": true,
		},
		ErrorCodes: []UpstreamErrorCode{
			{Code: "provider_timeout", Description: "stub timeout"},
			{Code: "provider_request_failed", Description: "stub"},
		},
	}
}

func (s stubUpstreamProvider) ChatCompletions(ctx context.Context, userID string, body any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.payload, nil
}

type stubStreamingUpstreamProvider struct {
	stubUpstreamProvider
	rawBody string
	rawErr  error
}

func (s stubStreamingUpstreamProvider) ChatCompletionsRaw(ctx context.Context, userID string, body any) (*http.Response, error) {
	if s.rawErr != nil {
		return nil, s.rawErr
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(s.rawBody)),
	}, nil
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

func TestToolCallAssistServiceProvidersExposeDescriptors(t *testing.T) {
	svc := NewToolCallAssistService(&WorkspaceToolCallService{}, stubUpstreamProvider{name: " Kirari "})
	providers := svc.Providers()
	if len(providers) != 1 || providers[0].Name != "kirari" || providers[0].DisplayName == "" {
		t.Fatalf("unexpected provider descriptors: %#v", providers)
	}
	if !boolValueAny(providers[0].Capabilities["backend_delegated"]) {
		t.Fatalf("expected provider capabilities in descriptors: %#v", providers)
	}
}

func TestNormalizeToolCallAssistProviderErrorMapsKirariConnection(t *testing.T) {
	err := NormalizeToolCallAssistProviderError("kirari", ErrKirariNotConnected)
	providerErr, ok := err.(*ToolCallAssistProviderError)
	if !ok {
		t.Fatalf("expected provider error type, got %T", err)
	}
	if providerErr.Code != "provider_not_connected" || providerErr.HTTPStatus != http.StatusConflict || providerErr.Provider != "kirari" {
		t.Fatalf("unexpected provider error mapping: %#v", providerErr)
	}
}

func TestNormalizeToolCallAssistProviderErrorMapsTimeout(t *testing.T) {
	err := NormalizeToolCallAssistProviderError("kirari", context.DeadlineExceeded)
	providerErr, ok := err.(*ToolCallAssistProviderError)
	if !ok {
		t.Fatalf("expected provider error type, got %T", err)
	}
	if providerErr.Code != "provider_timeout" || providerErr.HTTPStatus != http.StatusGatewayTimeout || !providerErr.Retryable {
		t.Fatalf("unexpected timeout provider error mapping: %#v", providerErr)
	}
}

func TestNormalizeToolCallAssistProviderErrorMapsCancelled(t *testing.T) {
	err := NormalizeToolCallAssistProviderError("kirari", context.Canceled)
	providerErr, ok := err.(*ToolCallAssistProviderError)
	if !ok {
		t.Fatalf("expected provider error type, got %T", err)
	}
	if providerErr.Code != "provider_cancelled" || providerErr.HTTPStatus != http.StatusRequestTimeout || providerErr.Retryable {
		t.Fatalf("unexpected cancelled provider error mapping: %#v", providerErr)
	}
}

func boolValueAny(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func TestExtractAssistChatCompletionDeltaSupportsOpenAIStyleChunk(t *testing.T) {
	delta := extractAssistChatCompletionDelta(`{"choices":[{"delta":{"content":"hello "}}]}`)
	if delta != "hello " {
		t.Fatalf("unexpected delta: %q", delta)
	}
}

func TestExtractAssistChatCompletionDeltaSupportsContentArrayChunk(t *testing.T) {
	delta := extractAssistChatCompletionDelta(`{"choices":[{"delta":{"content":[{"text":"tool "},{"content":"draft"}]}}]}`)
	if delta != "tool draft" {
		t.Fatalf("unexpected content array delta: %q", delta)
	}
}

func TestStreamAssistChatCompletionResponseCompletesWithParsedDraft(t *testing.T) {
	events := make(chan protocol.StreamEvent, 8)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`data: {"choices":[{"delta":{"content":"{\"explanation\":\"ok\","}}]}`,
			"",
			`data: {"choices":[{"delta":{"content":"\"tool_call\":{\"name\":\"lookup_weather\",\"arguments\":{\"city\":\"Beijing\"}}}"}}]}`,
			"",
			`data: [DONE]`,
			"",
		}, "\n"))),
	}
	go streamAssistChatCompletionResponse(resp, "test_provider", "demo-model", map[string]any{"request_id": "req_demo"}, []protocol.NormalizedToolSchema{{Name: "lookup_weather"}}, events)

	collected := make([]protocol.StreamEvent, 0, 4)
	for event := range events {
		collected = append(collected, event)
	}
	if len(collected) < 3 || collected[0].Event != "assist.started" || collected[1].Event != "assist.delta" || collected[len(collected)-1].Event != "assist.completed" {
		t.Fatalf("unexpected assist stream events: %#v", collected)
	}
	completedData, _ := collected[len(collected)-1].Data.(map[string]any)
	completedAssist, _ := completedData["assist"].(ToolCallAssistResult)
	if !completedAssist.ValidDraft || stringValueAny(completedAssist.ToolCall["name"]) != "lookup_weather" {
		t.Fatalf("unexpected completed assist result: %#v", completedAssist)
	}
}
