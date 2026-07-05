package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service"
)

func TestResponsesDeltaAndComplete(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-responses",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "responses 顺序测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "responses 顺序测试")
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "草稿输出",
	}, http.StatusOK)

	completeResp := env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"mode":            "assistant_message",
	}, http.StatusOK)
	if got := nestedString(completeResp, "output_text"); got != "草稿输出" {
		t.Fatalf("unexpected output_text from complete: %q", got)
	}

	finalResp := <-resultCh
	if got := nestedString(finalResp, "object"); got != "response" {
		t.Fatalf("unexpected responses object: %q", got)
	}
	if got := nestedString(finalResp, "output_text"); got != "草稿输出" {
		t.Fatalf("unexpected responses output_text: %q", got)
	}
}

func TestResponsesAbort(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-abort",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "responses abort 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "responses abort 测试")
	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/abort", map[string]any{
		"error": "人工中止",
	}, http.StatusOK)

	finalResp := <-resultCh
	if nestedPathString(finalResp, "error", "message") != "人工中止" {
		t.Fatalf("unexpected abort message: %#v", finalResp)
	}
}

func TestChatCompletionsProtocolShape(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model": "demo-chat",
		"messages": []map[string]any{
			{"role": "user", "content": "chat completions 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "chat completions 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "chat completions 回复",
		"mode":            "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "object"); got != "chat.completion" {
		t.Fatalf("unexpected chat completion object: %q", got)
	}
	if got := nestedString(finalResp, "model"); got != "demo-chat" {
		t.Fatalf("unexpected chat completion model: %q", got)
	}
	choices, ok := finalResp["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("unexpected chat completion choices: %#v", finalResp["choices"])
	}
	firstChoice := choices[0].(map[string]any)
	message := firstChoice["message"].(map[string]any)
	if got := nestedString(message, "content"); got != "chat completions 回复" {
		t.Fatalf("unexpected chat completion message: %#v", message)
	}
}

func TestAnthropicMessagesProtocolShape(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/messages", map[string]any{
		"model": "claude-demo",
		"messages": []map[string]any{
			{"role": "user", "content": "anthropic messages 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "anthropic messages 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "anthropic 回复",
		"mode":            "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "type"); got != "message" {
		t.Fatalf("unexpected anthropic type: %q", got)
	}
	if got := nestedString(finalResp, "role"); got != "assistant" {
		t.Fatalf("unexpected anthropic role: %q", got)
	}
	if got := nestedString(finalResp, "stop_reason"); got != "end_turn" {
		t.Fatalf("unexpected anthropic stop reason: %q", got)
	}
	content, ok := finalResp["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected anthropic content: %#v", finalResp["content"])
	}
	firstPart := content[0].(map[string]any)
	if got := nestedString(firstPart, "text"); got != "anthropic 回复" {
		t.Fatalf("unexpected anthropic text: %#v", firstPart)
	}
}

type testEnv struct {
	server *httptest.Server
	client *http.Client
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		Mode:           config.ModeLab,
		Host:           "127.0.0.1",
		Port:           0,
		WebDistDir:     tempDir,
		DataDir:        tempDir,
		DatabaseDriver: "sqlite",
		DatabaseDSN:    filepath.Join(tempDir, "chatapi.sqlite3"),
		AllowRemoteLab: false,
		OpenBrowser:    false,
		LogLevel:       "error",
		CORSOrigins:    []string{"http://localhost"},
	}

	store, err := sqlitestore.Open(cfg.DatabaseDSN)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrations.Bootstrap(context.Background(), store.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	pendingRegistry := service.NewPendingRegistry()
	realtimeHub := service.NewRealtimeHub(store)
	chatService := service.NewChatAPIService(store, pendingRegistry, realtimeHub)

	server := httptest.NewServer(httpapi.NewRouter(cfg, store, chatService, realtimeHub, pendingRegistry))
	t.Cleanup(server.Close)

	return &testEnv{
		server: server,
		client: server.Client(),
	}
}

func (e *testEnv) postJSON(t *testing.T, path string, body map[string]any, wantStatus int) map[string]any {
	t.Helper()
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, e.server.URL+path, bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do request %s: %v", path, err)
	}
	defer resp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response %s: %v", path, err)
	}

	if resp.StatusCode != wantStatus {
		t.Fatalf("unexpected status for %s: got %d want %d payload=%#v", path, resp.StatusCode, wantStatus, payload)
	}
	return payload
}

func (e *testEnv) waitForWaitingConversation(t *testing.T, title string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := e.client.Get(e.server.URL + "/api/lab/workspace")
		if err != nil {
			t.Fatalf("get workspace: %v", err)
		}
		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			t.Fatalf("decode workspace: %v", err)
		}
		resp.Body.Close()

		items, ok := payload["conversations"].([]any)
		if ok {
			for _, item := range items {
				record := item.(map[string]any)
				if nestedString(record, "title") == title && nestedString(record["metadata"].(map[string]any), "realtime_status") == "waiting" {
					return record
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("waiting conversation not found: %s", title)
	return nil
}

func startJSONRequest(t *testing.T, url string, body map[string]any) <-chan map[string]any {
	t.Helper()
	resultCh := make(chan map[string]any, 1)

	go func() {
		rawBody, err := json.Marshal(body)
		if err != nil {
			resultCh <- map[string]any{"__error": err.Error()}
			return
		}

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(rawBody))
		if err != nil {
			resultCh <- map[string]any{"__error": err.Error()}
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resultCh <- map[string]any{"__error": err.Error()}
			return
		}
		defer resp.Body.Close()

		var payload map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resultCh <- map[string]any{"__error": err.Error()}
			return
		}
		if resp.StatusCode >= 400 {
			resultCh <- map[string]any{"__status": fmt.Sprintf("%d", resp.StatusCode), "__payload": payload}
			return
		}
		resultCh <- payload
	}()

	return resultCh
}

func nestedString(record map[string]any, key string) string {
	if value, ok := record[key].(string); ok {
		return value
	}
	return ""
}

func nestedPathString(record map[string]any, path ...string) string {
	var current any = record
	for _, key := range path {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = nextMap[key]
	}
	if value, ok := current.(string); ok {
		return value
	}
	return ""
}
