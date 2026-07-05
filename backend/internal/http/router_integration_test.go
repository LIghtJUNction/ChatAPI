package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

func TestDraftMarksConversationStreaming(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-streaming-status",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "streaming 状态测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "streaming 状态测试")
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "第一段草稿",
	}, http.StatusOK)

	workspace := env.getJSON(t, "/api/lab/workspace", http.StatusOK)
	items := workspace["conversations"].([]any)
	foundStreaming := false
	for _, item := range items {
		record := item.(map[string]any)
		if nestedString(record, "id") == conversation["id"].(string) && nestedString(record["metadata"].(map[string]any), "realtime_status") == "streaming" {
			foundStreaming = true
			break
		}
	}
	if !foundStreaming {
		t.Fatalf("conversation did not transition to streaming: %#v", workspace)
	}

	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"mode":            "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestRespondConversationPathEndpoint(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-respond-path",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "respond path 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "respond path 测试")
	response := env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "一次性完成回复",
		"mode": "assistant_message",
	}, http.StatusOK)
	if got := nestedString(response, "output_text"); got != "一次性完成回复" {
		t.Fatalf("unexpected respond output_text: %#v", response)
	}

	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "一次性完成回复" {
		t.Fatalf("unexpected final responses output_text: %#v", finalResp)
	}
}

func TestStreamDeltaPathEndpoint(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-stream-path",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "stream path 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "stream path 测试")
	deltaResp := env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/stream/delta", map[string]any{
		"text": "路径流式片段",
	}, http.StatusOK)
	if got := nestedString(deltaResp, "draft_text"); got != "路径流式片段" {
		t.Fatalf("unexpected stream delta response: %#v", deltaResp)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/stream/complete", map[string]any{
		"mode": "assistant_message",
	}, http.StatusOK)
	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "路径流式片段" {
		t.Fatalf("unexpected final stream path output_text: %#v", finalResp)
	}
}

func TestLabRequestEndpointsByRequestID(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-lab-request-id",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "lab request id 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "lab request id 测试")
	messagesResp := env.getJSON(t, "/api/conversations/"+conversation["id"].(string)+"/messages", http.StatusOK)
	items := messagesResp["items"].([]any)
	requestDebug := items[0].(map[string]any)["metadata"].(map[string]any)["request_debug"].(map[string]any)
	requestID := requestDebug["request_id"].(string)

	requestResp := env.getJSON(t, "/lab/requests/"+requestID, http.StatusOK)
	requestRecord := requestResp["request"].(map[string]any)
	if nestedString(requestRecord, "request_id") != requestID || nestedString(requestRecord, "conversation_id") != conversation["id"].(string) {
		t.Fatalf("unexpected lab request payload: %#v", requestResp)
	}

	env.postJSON(t, "/lab/requests/"+requestID+"/delta", map[string]any{
		"text": "通过 request_id 输出",
	}, http.StatusOK)
	env.postJSON(t, "/lab/requests/"+requestID+"/complete", map[string]any{
		"mode": "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "通过 request_id 输出" {
		t.Fatalf("unexpected final request-id output_text: %#v", finalResp)
	}
}

func TestLabRequestsList(t *testing.T) {
	env := newTestEnv(t)

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-list-1",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "list 请求 1"}},
			},
		},
	})
	secondCh := startJSONRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model": "demo-list-2",
		"messages": []map[string]any{
			{"role": "user", "content": "list 请求 2"},
		},
	})

	firstConversation := env.waitForWaitingConversation(t, "list 请求 1")
	secondConversation := env.waitForWaitingConversation(t, "list 请求 2")

	listResp := env.getJSON(t, "/lab/requests", http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) < 2 {
		t.Fatalf("unexpected lab requests list: %#v", listResp)
	}

	foundFirst := false
	foundSecond := false
	for _, item := range items {
		record := item.(map[string]any)
		switch nestedString(record, "conversation_id") {
		case firstConversation["id"].(string):
			foundFirst = nestedString(record, "input_text") == "list 请求 1" && nestedString(record, "status") == "waiting"
		case secondConversation["id"].(string):
			foundSecond = nestedString(record, "input_text") == "list 请求 2" && nestedString(record, "request_format") == "chat_completions"
		}
	}
	if !foundFirst || !foundSecond {
		t.Fatalf("missing expected requests in list: %#v", listResp)
	}

	env.postJSON(t, "/api/conversations/"+firstConversation["id"].(string)+"/respond", map[string]any{
		"text": "done1",
		"mode": "assistant_message",
	}, http.StatusOK)
	env.postJSON(t, "/api/conversations/"+secondConversation["id"].(string)+"/respond", map[string]any{
		"text": "done2",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-firstCh
	<-secondCh
}

func TestAppAPIRequestsReadAndRespond(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read", "requests:respond"}, map[string]any{
		"allowed_request_actions": []string{"complete"},
	})

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-api",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "app api 测试"}},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "app api 测试")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))

	meResp := env.appGetJSON(t, "/api/app/me", appKey, http.StatusOK)
	if nestedPathString(meResp, "user", "id") != "lab-user" {
		t.Fatalf("unexpected app api me response: %#v", meResp)
	}

	listResp := env.appGetJSON(t, "/api/app/requests", appKey, http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("unexpected empty app api requests list: %#v", listResp)
	}

	detailResp := env.appGetJSON(t, "/api/app/requests/"+requestID, appKey, http.StatusOK)
	if nestedPathString(detailResp, "request", "request_id") != requestID {
		t.Fatalf("unexpected app api request detail: %#v", detailResp)
	}

	env.appPostJSON(t, "/api/app/requests/"+requestID+"/complete", appKey, map[string]any{
		"text": "应用 API 完成",
		"mode": "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "应用 API 完成" {
		t.Fatalf("unexpected app api final response: %#v", finalResp)
	}
}

func TestAppAPIRejectsMissingRespondScope(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-api-scope",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "app api scope 测试"}},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "app api scope 测试")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))
	status, body := env.appPostText(t, "/api/app/requests/"+requestID+"/delta", appKey, map[string]any{
		"text": "不应该成功",
	})
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("unexpected app api scope rejection: status=%d body=%q", status, body)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestAppAPIRejectsDisallowedRequestAction(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read", "requests:respond"}, map[string]any{
		"allowed_request_actions": []string{"complete"},
	})

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-api-action",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "app api action 测试"}},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "app api action 测试")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))
	status, body := env.appPostText(t, "/api/app/requests/"+requestID+"/delta", appKey, map[string]any{
		"text": "不允许的 delta",
	})
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("unexpected app api action rejection: status=%d body=%q", status, body)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestAppAPIOwnerIsolation(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "other-user", []string{"requests:read"}, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-api-owner",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "app api owner 测试"}},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "app api owner 测试")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))

	listResp := env.appGetJSON(t, "/api/app/requests", appKey, http.StatusOK)
	if items := listResp["items"].([]any); len(items) != 0 {
		t.Fatalf("unexpected requests visible for foreign owner: %#v", listResp)
	}

	status, body := env.appGetText(t, "/api/app/requests/"+requestID, appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("unexpected owner isolation response: status=%d body=%q", status, body)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestAppAPIConversationsRead(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"conversations:read"}, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-conversations",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "app conversations 测试"}},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "app conversations 测试")

	listResp := env.appGetJSON(t, "/api/app/conversations", appKey, http.StatusOK)
	items := listResp["items"].([]any)
	foundConversation := false
	for _, item := range items {
		record := item.(map[string]any)
		if nestedString(record, "id") == conversation["id"].(string) {
			foundConversation = true
			break
		}
	}
	if !foundConversation {
		t.Fatalf("expected conversation in app api list: %#v", listResp)
	}

	messagesResp := env.appGetJSON(t, "/api/app/conversations/"+conversation["id"].(string)+"/messages", appKey, http.StatusOK)
	messageItems := messagesResp["items"].([]any)
	if len(messageItems) == 0 || nestedString(messageItems[0].(map[string]any), "content") != "app conversations 测试" {
		t.Fatalf("unexpected app api messages response: %#v", messagesResp)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestAppAPIConversationsOwnerIsolation(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "other-user", []string{"conversations:read"}, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-conversations-owner",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "app conv owner"}},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "app conv owner")

	listResp := env.appGetJSON(t, "/api/app/conversations", appKey, http.StatusOK)
	if items := listResp["items"].([]any); len(items) != 0 {
		t.Fatalf("foreign owner should not see conversations: %#v", listResp)
	}

	status, body := env.appGetText(t, "/api/app/conversations/"+conversation["id"].(string)+"/messages", appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("unexpected foreign conversation messages response: status=%d body=%q", status, body)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestUserAppAPIKeysManagement(t *testing.T) {
	env := newTestEnv(t)

	createResp := env.postJSON(t, "/api/user/app-api-keys", map[string]any{
		"name":            "managed-key",
		"scopes":          []string{"requests:read"},
		"resource_limits": map[string]any{"allowed_request_actions": []string{"complete"}},
	}, http.StatusOK)
	rawKey := nestedString(createResp, "raw_key")
	if !strings.HasPrefix(rawKey, "ak-") {
		t.Fatalf("unexpected raw key payload: %#v", createResp)
	}
	item := createResp["item"].(map[string]any)
	keyID := nestedString(item, "id")
	if keyID == "" {
		t.Fatalf("missing key id: %#v", createResp)
	}

	listResp := env.getJSON(t, "/api/user/app-api-keys", http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) == 0 || nestedString(items[0].(map[string]any), "id") != keyID {
		t.Fatalf("unexpected app api keys list: %#v", listResp)
	}

	status, body := env.deleteText(t, "/api/user/app-api-keys/"+keyID)
	if status != http.StatusOK || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("unexpected delete response: status=%d body=%q", status, body)
	}

	status, body = env.appGetText(t, "/api/app/me", rawKey)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked key should be unauthorized: status=%d body=%q", status, body)
	}
}

func TestUserModelAPIKeysManagement(t *testing.T) {
	env := newTestEnv(t)

	createResp := env.postJSON(t, "/api/user/model-api-keys", map[string]any{
		"name":  "managed-model-key",
		"model": "demo-managed-model",
	}, http.StatusOK)
	rawKey := nestedString(createResp, "raw_key")
	if !strings.HasPrefix(rawKey, "sk-") {
		t.Fatalf("unexpected raw model key payload: %#v", createResp)
	}
	item := createResp["item"].(map[string]any)
	keyID := nestedString(item, "id")
	if keyID == "" || nestedString(item, "raw_key") != rawKey {
		t.Fatalf("unexpected created model key item: %#v", createResp)
	}

	listResp := env.getJSON(t, "/api/user/model-api-keys", http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) == 0 || nestedString(items[0].(map[string]any), "raw_key") != rawKey {
		t.Fatalf("expected encrypted model key to be readable by owner: %#v", listResp)
	}

	status, body := env.deleteText(t, "/api/user/model-api-keys/"+keyID)
	if status != http.StatusOK || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("unexpected model key delete response: status=%d body=%q", status, body)
	}

	status, body = postExternalText(t, env.server.URL+"/v1/responses", map[string]string{
		"Authorization": "Bearer " + rawKey,
	}, map[string]any{
		"model": "demo-managed-model",
		"input": "revoked key should fail",
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "model api key unauthorized") {
		t.Fatalf("revoked model key should be unauthorized: status=%d body=%q", status, body)
	}
}

func TestModelAPIKeyOwnsProtocolRequests(t *testing.T) {
	env := newTestEnv(t)
	modelKey := env.seedModelAPIKey(t, "model-user", "owner-key", "demo-owner-model")

	resultCh := startJSONRequestWithHeaders(t, env.server.URL+"/v1/responses", map[string]string{
		"Authorization": "Bearer " + modelKey,
	}, map[string]any{
		"model": "demo-owner-model",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "model key owner 测试"}},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "model key owner 测试")
	metadata := conversation["metadata"].(map[string]any)
	if got := nestedString(metadata, "owner_id"); got != "model-user" {
		t.Fatalf("expected model key owner_id, got %q metadata=%#v", got, metadata)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "model key owner response",
		"mode": "assistant_message",
	}, http.StatusOK)
	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "model key owner response" {
		t.Fatalf("unexpected model key final response: %#v", finalResp)
	}
}

func TestServeModeRejectsMissingModelAPIKey(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)

	status, body := postExternalText(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "demo-serve-auth",
		"input": "serve mode requires model api key",
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "model api key unauthorized") {
		t.Fatalf("expected missing model api key rejection: status=%d body=%q", status, body)
	}
}

func TestAppAPIModelKeysManagement(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"model_keys:read", "model_keys:write", "model_keys:delete"}, map[string]any{
		"allowed_virtual_models": []string{"demo-app-managed-model"},
	})

	createResp := env.appPostJSON(t, "/api/app/model-keys", appKey, map[string]any{
		"name":  "app-managed-model-key",
		"model": "demo-app-managed-model",
	}, http.StatusOK)
	rawKey := nestedString(createResp, "raw_key")
	item := createResp["item"].(map[string]any)
	keyID := nestedString(item, "id")
	if !strings.HasPrefix(rawKey, "sk-") || keyID == "" {
		t.Fatalf("unexpected app model key create response: %#v", createResp)
	}

	status, body := env.appPostText(t, "/api/app/model-keys", appKey, map[string]any{
		"name":  "disallowed-model-key",
		"model": "blocked-model",
	})
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected resource-limited model create rejection: status=%d body=%q", status, body)
	}

	listResp := env.appGetJSON(t, "/api/app/model-keys", appKey, http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) == 0 || nestedString(items[0].(map[string]any), "id") != keyID {
		t.Fatalf("unexpected app model key list: %#v", listResp)
	}

	status, body = env.appDeleteText(t, "/api/app/model-keys/"+keyID, appKey)
	if status != http.StatusOK || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("unexpected app model key delete response: status=%d body=%q", status, body)
	}

	status, body = postExternalText(t, env.server.URL+"/v1/responses", map[string]string{
		"Authorization": "Bearer " + rawKey,
	}, map[string]any{
		"model": "demo-app-managed-model",
		"input": "deleted app managed key should fail",
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("deleted app-managed model key should be unauthorized: status=%d body=%q", status, body)
	}
}

func TestAppAPIAutomationRulesReadWrite(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"automation:read", "automation:write"}, nil)

	rule := map[string]any{
		"id":      "rule_app_api",
		"enabled": true,
		"conditions": map[string]any{
			"contains": []map[string]any{{"match_type": "substring", "pattern": "hello"}},
			"excludes": []map[string]any{},
		},
		"timing": map[string]any{
			"delay_seconds":           0,
			"repeat_interval_seconds": 0,
			"max_output_count":        120,
		},
		"action": map[string]any{
			"type": "output_text",
			"text": "自动化回复",
		},
	}

	putResp := env.appPutJSON(t, "/api/app/automation-rules", appKey, map[string]any{
		"rules": []map[string]any{rule},
	}, http.StatusOK)
	rules := putResp["rules"].([]any)
	if len(rules) != 1 || nestedString(rules[0].(map[string]any), "id") != "rule_app_api" {
		t.Fatalf("unexpected automation rules put response: %#v", putResp)
	}

	listResp := env.appGetJSON(t, "/api/app/automation-rules", appKey, http.StatusOK)
	listRules := listResp["rules"].([]any)
	if len(listRules) != 1 || nestedPathString(listRules[0].(map[string]any), "action", "text") != "自动化回复" {
		t.Fatalf("unexpected automation rules list response: %#v", listResp)
	}
}

func TestAppAPIAutomationRulesRejectsMissingScope(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"automation:read"}, nil)

	status, body := env.appPutText(t, "/api/app/automation-rules", appKey, map[string]any{
		"rules": []map[string]any{{"id": "rule_forbidden", "enabled": true}},
	})
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected automation write scope rejection: status=%d body=%q", status, body)
	}
}

func TestAppAPIAutomationRulesResourceLimit(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"automation:read", "automation:write"}, map[string]any{
		"allowed_automation_rule_ids": []string{"rule_allowed"},
	})

	status, body := env.appPutText(t, "/api/app/automation-rules", appKey, map[string]any{
		"rules": []map[string]any{{"id": "rule_blocked", "enabled": true}},
	})
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected automation resource rejection: status=%d body=%q", status, body)
	}

	putResp := env.appPutJSON(t, "/api/app/automation-rules", appKey, map[string]any{
		"rules": []map[string]any{{"id": "rule_allowed", "enabled": false}},
	}, http.StatusOK)
	rules := putResp["rules"].([]any)
	if len(rules) != 1 || nestedString(rules[0].(map[string]any), "id") != "rule_allowed" {
		t.Fatalf("unexpected resource-limited automation response: %#v", putResp)
	}
}

func TestAppAPIStatisticsSummary(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)
	otherKey := env.seedAppAPIKey(t, "other-user", []string{"statistics:read"}, nil)

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "stats-model-a",
		"input": "统计请求 A",
	})
	secondCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "stats-model-b",
		"input": "统计请求 B",
	})

	firstConversation := env.waitForWaitingConversation(t, "统计请求 A")
	secondConversation := env.waitForWaitingConversation(t, "统计请求 B")
	env.postJSON(t, "/api/conversations/"+firstConversation["id"].(string)+"/respond", map[string]any{
		"text": "stats done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-firstCh

	resp := env.appGetJSON(t, "/api/app/statistics/summary", appKey, http.StatusOK)
	summary := resp["summary"].(map[string]any)
	if numericValue(summary["total_requests"]) != 2 || numericValue(summary["closed_requests"]) != 1 || numericValue(summary["pending_requests"]) != 1 {
		t.Fatalf("unexpected statistics summary: %#v", resp)
	}
	byModel := summary["by_model"].(map[string]any)
	if numericValue(byModel["stats-model-a"]) != 1 || numericValue(byModel["stats-model-b"]) != 1 {
		t.Fatalf("unexpected statistics by_model: %#v", resp)
	}

	otherResp := env.appGetJSON(t, "/api/app/statistics/summary", otherKey, http.StatusOK)
	otherSummary := otherResp["summary"].(map[string]any)
	if numericValue(otherSummary["total_requests"]) != 0 {
		t.Fatalf("foreign owner should not see statistics: %#v", otherResp)
	}

	env.postJSON(t, "/api/conversations/"+secondConversation["id"].(string)+"/respond", map[string]any{
		"text": "stats cleanup",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-secondCh
}

func TestAppAPIStatisticsSummaryRejectsMissingScope(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, nil)

	status, body := env.appGetText(t, "/api/app/statistics/summary", appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected statistics scope rejection: status=%d body=%q", status, body)
	}
}

func TestAdminRuntimeEndpoints(t *testing.T) {
	env := newTestEnv(t)

	summaryResp := env.getJSON(t, "/api/admin/runtime/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	if nestedPathString(summary, "go", "version") == "" || summary["memory"] == nil || summary["pending"] == nil || summary["realtime"] == nil {
		t.Fatalf("unexpected runtime summary response: %#v", summaryResp)
	}
	database := summary["database"].(map[string]any)
	if nestedString(database, "driver") != "sqlite" {
		t.Fatalf("unexpected database runtime info: %#v", summaryResp)
	}

	memoryResp := env.getJSON(t, "/api/admin/runtime/memory", http.StatusOK)
	memory := memoryResp["memory"].(map[string]any)
	if numericValue(memory["sys_bytes"]) <= 0 {
		t.Fatalf("unexpected runtime memory response: %#v", memoryResp)
	}

	gcResp := env.postJSON(t, "/api/admin/runtime/gc", map[string]any{}, http.StatusOK)
	if gcResp["memory"] == nil {
		t.Fatalf("unexpected runtime gc response: %#v", gcResp)
	}

	connectionsResp := env.getJSON(t, "/api/admin/runtime/connections", http.StatusOK)
	connections := connectionsResp["connections"].(map[string]any)
	if _, ok := connections["total_subscribers"]; !ok {
		t.Fatalf("unexpected runtime connections response: %#v", connectionsResp)
	}

	queueResp := env.getJSON(t, "/api/admin/runtime/queue", http.StatusOK)
	queue := queueResp["queue"].(map[string]any)
	if _, ok := queue["queued_events"]; !ok {
		t.Fatalf("unexpected runtime queue response: %#v", queueResp)
	}
}

func TestAdminRuntimeRejectsServeWithoutAdmin(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)

	status, body := env.getText(t, "/api/admin/runtime/summary")
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected admin rejection in serve mode: status=%d body=%q", status, body)
	}
}

func TestAdminRuntimeRejectsAPIKeys(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)
	modelKey := env.seedModelAPIKey(t, "lab-user", "admin-denied-model-key", "admin-denied")

	status, body := env.getTextWithHeaders(t, "/api/admin/runtime/summary", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/admin/runtime/summary", map[string]string{
		"Authorization": "Bearer " + modelKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected model api key admin rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/admin/runtime/queue", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key runtime queue rejection: status=%d body=%q", status, body)
	}
}

func TestAdminStorageEndpoints(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-model",
		"input": "storage usage 测试",
	})
	conversation := env.waitForWaitingConversation(t, "storage usage 测试")
	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "storage response",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh

	summaryResp := env.getJSON(t, "/api/admin/storage/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	if numericValue(summary["conversation_count"]) < 1 || numericValue(summary["message_count"]) < 2 {
		t.Fatalf("unexpected storage summary response: %#v", summaryResp)
	}
	database := summary["database"].(map[string]any)
	if nestedString(database, "driver") != "sqlite" {
		t.Fatalf("unexpected storage database info: %#v", summaryResp)
	}

	usersResp := env.getJSON(t, "/api/admin/storage/users", http.StatusOK)
	items := usersResp["items"].([]any)
	foundLabUser := false
	for _, item := range items {
		record := item.(map[string]any)
		if nestedString(record, "user_id") == "lab-user" && numericValue(record["message_count"]) >= 2 {
			foundLabUser = true
			break
		}
	}
	if !foundLabUser {
		t.Fatalf("expected lab-user storage usage: %#v", usersResp)
	}
}

func TestAdminStorageCleanupPreview(t *testing.T) {
	env := newTestEnv(t)

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-a",
		"input": "storage cleanup A",
	})
	firstConversation := env.waitForWaitingConversation(t, "storage cleanup A")
	env.postJSON(t, "/api/conversations/"+firstConversation["id"].(string)+"/respond", map[string]any{
		"text": "cleanup response A",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-firstCh

	secondCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-b",
		"input": "storage cleanup B",
	})
	secondConversation := env.waitForWaitingConversation(t, "storage cleanup B")
	env.postJSON(t, "/api/conversations/"+secondConversation["id"].(string)+"/respond", map[string]any{
		"text": "cleanup response B",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-secondCh

	previewResp := env.postJSON(t, "/api/admin/storage/cleanup", map[string]any{
		"dry_run":                   true,
		"owner_id":                  "lab-user",
		"keep_recent_conversations": 1,
		"keep_recent_days":          0,
	}, http.StatusOK)
	plan := previewResp["plan"].(map[string]any)
	if previewResp["dry_run"] != true || plan["dry_run"] != true {
		t.Fatalf("cleanup preview should be dry-run only: %#v", previewResp)
	}
	if numericValue(plan["candidate_conversations"]) != 1 || numericValue(plan["candidate_messages"]) < 2 {
		t.Fatalf("unexpected cleanup preview candidates: %#v", previewResp)
	}
	if numericValue(plan["estimated_reclaimable_bytes"]) <= 0 {
		t.Fatalf("expected positive reclaimable estimate: %#v", previewResp)
	}
	byOwner := plan["by_owner"].([]any)
	if len(byOwner) != 1 || nestedString(byOwner[0].(map[string]any), "owner_id") != "lab-user" {
		t.Fatalf("unexpected cleanup owner plan: %#v", previewResp)
	}
}

func TestAdminStorageCleanupRejectsNonDryRun(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.postText(t, "/api/admin/storage/cleanup", map[string]any{
		"dry_run":                   false,
		"keep_recent_conversations": 1,
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "only supports dry_run") {
		t.Fatalf("expected non-dry-run rejection: status=%d body=%q", status, body)
	}
}

func TestAdminStorageRejectsAPIKeys(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)

	status, body := env.getTextWithHeaders(t, "/api/admin/storage/summary", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage admin rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPostText(t, "/api/admin/storage/cleanup", appKey, map[string]any{
		"dry_run": true,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage cleanup rejection: status=%d body=%q", status, body)
	}
}

func TestAdminRequestsOverview(t *testing.T) {
	env := newTestEnv(t)
	modelKey := env.seedModelAPIKey(t, "model-owner", "overview-model-key", "overview-model-b")

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "overview-model-a",
		"input": "overview 请求 A",
	})
	secondCh := startJSONRequestWithHeaders(t, env.server.URL+"/v1/responses", map[string]string{
		"Authorization": "Bearer " + modelKey,
	}, map[string]any{
		"model": "overview-model-b",
		"input": "overview 请求 B",
	})

	firstConversation := env.waitForWaitingConversation(t, "overview 请求 A")
	secondConversation := env.waitForWaitingConversation(t, "overview 请求 B")
	env.postJSON(t, "/api/conversations/"+firstConversation["id"].(string)+"/respond", map[string]any{
		"text": "overview done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-firstCh

	overviewResp := env.getJSON(t, "/api/admin/requests/overview", http.StatusOK)
	overview := overviewResp["overview"].(map[string]any)
	if numericValue(overview["total_requests"]) != 2 || numericValue(overview["closed_requests"]) != 1 || numericValue(overview["pending_requests"]) != 1 {
		t.Fatalf("unexpected admin requests overview: %#v", overviewResp)
	}
	byOwner := overview["by_owner"].(map[string]any)
	if numericValue(byOwner["lab-user"]) != 1 || numericValue(byOwner["model-owner"]) != 1 {
		t.Fatalf("unexpected admin requests owner buckets: %#v", overviewResp)
	}
	byModel := overview["by_model"].(map[string]any)
	if numericValue(byModel["overview-model-a"]) != 1 || numericValue(byModel["overview-model-b"]) != 1 {
		t.Fatalf("unexpected admin requests model buckets: %#v", overviewResp)
	}

	env.postJSON(t, "/api/conversations/"+secondConversation["id"].(string)+"/respond", map[string]any{
		"text": "overview cleanup",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-secondCh
}

func TestAdminRequestsOverviewRejectsAPIKeys(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, nil)

	status, body := env.getTextWithHeaders(t, "/api/admin/requests/overview", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key requests overview admin rejection: status=%d body=%q", status, body)
	}
}

func TestAppAPIAuditLogWritten(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, nil)

	resp := env.appGetJSON(t, "/api/app/me", appKey, http.StatusOK)
	if nestedPathString(resp, "user", "id") != "lab-user" {
		t.Fatalf("unexpected /api/app/me response: %#v", resp)
	}

	var count int
	if err := env.store.DB().QueryRow(`SELECT COUNT(*) FROM app_api_key_audit_logs`).Scan(&count); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected audit log entry to be written")
	}
}

func TestAppAPIAuditLogWrittenForForbidden(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-app-audit-forbidden",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "audit forbidden 测试"}},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "audit forbidden 测试")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))
	status, _ := env.appPostText(t, "/api/app/requests/"+requestID+"/complete", appKey, map[string]any{
		"text": "不允许",
		"mode": "assistant_message",
	})
	if status != http.StatusForbidden {
		t.Fatalf("expected forbidden status, got %d", status)
	}

	var forbiddenCount int
	if err := env.store.DB().QueryRow(`SELECT COUNT(*) FROM app_api_key_audit_logs WHERE status_code = 403 AND error_code = 'forbidden'`).Scan(&forbiddenCount); err != nil {
		t.Fatalf("count forbidden audit logs: %v", err)
	}
	if forbiddenCount == 0 {
		t.Fatalf("expected forbidden audit log entry")
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
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

func TestResponsesThinkingModePersistsThinkBlock(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-thinking",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "thinking 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "thinking 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id":       conversation["id"],
		"text":                  "内部思考内容",
		"mode":                  "thinking",
		"reasoning_stream_mode": "reasoning",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "<think>内部思考内容</think>" {
		t.Fatalf("unexpected responses thinking output_text: %#v", finalResp)
	}

	messagesResp := env.getJSON(t, "/api/conversations/"+conversation["id"].(string)+"/messages", http.StatusOK)
	items := messagesResp["items"].([]any)
	lastMessage := items[len(items)-1].(map[string]any)
	if got := nestedString(lastMessage, "content"); got != "<think>内部思考内容</think>" {
		t.Fatalf("unexpected thinking message content: %#v", lastMessage)
	}
}

func TestChatCompletionsToolCallShape(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model": "demo-tool-call",
		"messages": []map[string]any{
			{"role": "user", "content": "tool call 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "tool call 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "{\"city\":\"Shanghai\"}",
		"mode":            "tool_call",
		"tool_name":       "get_weather",
		"tool_call_id":    "call_test_1",
	}, http.StatusOK)

	finalResp := <-resultCh
	choices := finalResp["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	toolCalls := message["tool_calls"].([]any)
	firstToolCall := toolCalls[0].(map[string]any)
	if nestedString(firstToolCall, "id") != "call_test_1" {
		t.Fatalf("unexpected tool_call id: %#v", firstToolCall)
	}
	functionPart := firstToolCall["function"].(map[string]any)
	if nestedString(functionPart, "name") != "get_weather" || nestedString(functionPart, "arguments") != "{\"city\":\"Shanghai\"}" {
		t.Fatalf("unexpected function payload: %#v", functionPart)
	}

	messagesResp := env.getJSON(t, "/api/conversations/"+conversation["id"].(string)+"/messages", http.StatusOK)
	items := messagesResp["items"].([]any)
	lastMessage := items[len(items)-1].(map[string]any)
	metadata := lastMessage["metadata"].(map[string]any)
	if nestedString(metadata, "response_mode") != "tool_call" || nestedString(metadata, "tool_name") != "get_weather" {
		t.Fatalf("unexpected tool call message metadata: %#v", metadata)
	}
}

func TestResponsesToolResultShape(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-tool-result",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "tool result 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "tool result 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "{\"ok\":true}",
		"output":          "{\"ok\":true}",
		"mode":            "tool_result",
		"tool_call_id":    "call_result_1",
	}, http.StatusOK)

	finalResp := <-resultCh
	output := finalResp["output"].([]any)
	firstOutput := output[0].(map[string]any)
	if nestedString(firstOutput, "type") != "function_call_output" || nestedString(firstOutput, "call_id") != "call_result_1" {
		t.Fatalf("unexpected tool result output payload: %#v", firstOutput)
	}
	if nestedString(firstOutput, "output") != "{\"ok\":true}" {
		t.Fatalf("unexpected tool result output body: %#v", firstOutput)
	}

	messagesResp := env.getJSON(t, "/api/conversations/"+conversation["id"].(string)+"/messages", http.StatusOK)
	items := messagesResp["items"].([]any)
	lastMessage := items[len(items)-1].(map[string]any)
	metadata := lastMessage["metadata"].(map[string]any)
	if nestedString(metadata, "response_mode") != "tool_result" || nestedString(metadata, "output") != "{\"ok\":true}" {
		t.Fatalf("unexpected tool result metadata: %#v", metadata)
	}
}

func TestDeltaAfterCompleteReturnsConflict(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-delta-conflict",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "delta conflict 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "delta conflict 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "已结束",
		"mode":            "assistant_message",
	}, http.StatusOK)
	<-resultCh

	status, body := env.postText(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "不应该再写入",
	})
	if status != http.StatusConflict || !strings.Contains(body, "pending turn already finalized") {
		t.Fatalf("unexpected delta conflict response: status=%d body=%q", status, body)
	}
}

func TestAbortAfterCompleteReturnsConflict(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-abort-conflict",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "abort conflict 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "abort conflict 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "先完成",
		"mode":            "assistant_message",
	}, http.StatusOK)
	<-resultCh

	status, body := env.postText(t, "/api/conversations/"+conversation["id"].(string)+"/abort", map[string]any{
		"error": "后续 abort",
	})
	if status != http.StatusConflict || !strings.Contains(body, "pending turn already finalized") {
		t.Fatalf("unexpected abort conflict response: status=%d body=%q", status, body)
	}
}

func TestCompleteAfterAbortReturnsConflict(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-complete-conflict",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "complete conflict 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "complete conflict 测试")
	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/abort", map[string]any{
		"error": "先 abort",
	}, http.StatusOK)
	<-resultCh

	status, body := env.postText(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "后续完成",
		"mode":            "assistant_message",
	})
	if status != http.StatusConflict || !strings.Contains(body, "pending turn already finalized") {
		t.Fatalf("unexpected complete conflict response: status=%d body=%q", status, body)
	}
}

func TestResponsesSSEStream(t *testing.T) {
	env := newTestEnv(t)

	streamCh := startTextRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model":  "demo-stream",
		"stream": true,
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "responses sse 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "responses sse 测试")
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "第一段",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"mode":            "assistant_message",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "event: response.created") {
		t.Fatalf("missing response.created event: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"delta\":\"第一段\"") {
		t.Fatalf("missing delta payload: %s", streamBody)
	}
	if !strings.Contains(streamBody, "event: response.completed") || !strings.Contains(streamBody, "\"output_text\":\"第一段\"") {
		t.Fatalf("missing completed payload: %s", streamBody)
	}
}

func TestChatCompletionsSSEStream(t *testing.T) {
	env := newTestEnv(t)

	streamCh := startTextRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model":  "demo-chat-stream",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "chat stream 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "chat stream 测试")
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "流式回复",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"mode":            "assistant_message",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "\"object\":\"chat.completion.chunk\"") {
		t.Fatalf("missing chat completion chunk: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"content\":\"流式回复\"") {
		t.Fatalf("missing chat completion delta content: %s", streamBody)
	}
	if !strings.Contains(streamBody, "data: [DONE]") {
		t.Fatalf("missing done marker: %s", streamBody)
	}
}

func TestChatCompletionsToolCallSSEStream(t *testing.T) {
	env := newTestEnv(t)

	streamCh := startTextRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model":  "demo-chat-tool-stream",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "chat tool stream 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "chat tool stream 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "{\"city\":\"Shanghai\"}",
		"mode":            "tool_call",
		"tool_name":       "get_weather",
		"tool_call_id":    "call_stream_1",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "\"tool_calls\"") {
		t.Fatalf("missing tool_calls chunk: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"name\":\"get_weather\"") || !strings.Contains(streamBody, "\"arguments\":\"{\\\"city\\\":\\\"Shanghai\\\"}\"") {
		t.Fatalf("missing tool call function payload: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"finish_reason\":\"tool_calls\"") || !strings.Contains(streamBody, "data: [DONE]") {
		t.Fatalf("missing tool_calls finish marker: %s", streamBody)
	}
}

func TestAnthropicMessagesSSEStream(t *testing.T) {
	env := newTestEnv(t)

	streamCh := startTextRequest(t, env.server.URL+"/messages", map[string]any{
		"model":  "claude-stream-demo",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "anthropic stream 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "anthropic stream 测试")
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "Anthropic 流式回复",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"mode":            "assistant_message",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "event: message_start") {
		t.Fatalf("missing anthropic message_start: %s", streamBody)
	}
	if !strings.Contains(streamBody, "event: content_block_start") || !strings.Contains(streamBody, "\"type\":\"text\"") {
		t.Fatalf("missing anthropic text block start: %s", streamBody)
	}
	if !strings.Contains(streamBody, "event: content_block_delta") || !strings.Contains(streamBody, "\"text\":\"Anthropic 流式回复\"") {
		t.Fatalf("missing anthropic delta: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"stop_reason\":\"end_turn\"") || !strings.Contains(streamBody, "event: message_stop") {
		t.Fatalf("missing anthropic completion markers: %s", streamBody)
	}
}

func TestAnthropicMessagesToolCallSSEStream(t *testing.T) {
	env := newTestEnv(t)

	streamCh := startTextRequest(t, env.server.URL+"/messages", map[string]any{
		"model":  "claude-tool-stream-demo",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "anthropic tool stream 测试"},
		},
	})

	conversation := env.waitForWaitingConversation(t, "anthropic tool stream 测试")
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "{\"city\":\"Shanghai\"}",
		"mode":            "tool_call",
		"tool_name":       "get_weather",
		"tool_call_id":    "toolu_stream_1",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "event: content_block_start") || !strings.Contains(streamBody, "\"type\":\"tool_use\"") {
		t.Fatalf("missing anthropic tool_use block start: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"name\":\"get_weather\"") || !strings.Contains(streamBody, "\"city\":\"Shanghai\"") {
		t.Fatalf("missing anthropic tool_use payload: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"stop_reason\":\"tool_use\"") || !strings.Contains(streamBody, "event: message_stop") {
		t.Fatalf("missing anthropic tool_use completion markers: %s", streamBody)
	}
}

type testEnv struct {
	server          *httptest.Server
	client          *http.Client
	store           *sqlitestore.Store
	appKeyService   *service.AppAPIKeyService
	modelKeyService *service.ModelAPIKeyService
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithMode(t, config.ModeLab)
}

func newTestEnvWithMode(t *testing.T, mode config.Mode) *testEnv {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.Config{
		Mode:           mode,
		Host:           "127.0.0.1",
		Port:           0,
		WebDistDir:     tempDir,
		DataDir:        tempDir,
		DatabaseDriver: "sqlite",
		DatabaseDSN:    filepath.Join(tempDir, "chatapi.sqlite3"),
		AllowRemoteLab: false,
		OpenBrowser:    false,
		MasterKey:      "test-master-key",
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
	appKeyService := service.NewAppAPIKeyService(store)
	modelKeyService := service.NewModelAPIKeyService(store, cfg.MasterKey)

	server := httptest.NewServer(httpapi.NewRouter(cfg, store, chatService, realtimeHub, pendingRegistry))
	t.Cleanup(server.Close)

	return &testEnv{
		server:          server,
		client:          server.Client(),
		store:           store,
		appKeyService:   appKeyService,
		modelKeyService: modelKeyService,
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

func (e *testEnv) postText(t *testing.T, path string, body map[string]any) (int, string) {
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

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
}

func (e *testEnv) deleteText(t *testing.T, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do delete %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
}

func (e *testEnv) appGetJSON(t *testing.T, path string, appKey string, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app get %s: %v", path, err)
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

func (e *testEnv) appGetText(t *testing.T, path string, appKey string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app get %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
}

func (e *testEnv) appPostJSON(t *testing.T, path string, appKey string, body map[string]any, wantStatus int) map[string]any {
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
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app post %s: %v", path, err)
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

func (e *testEnv) appPostText(t *testing.T, path string, appKey string, body map[string]any) (int, string) {
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
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app post %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
}

func (e *testEnv) appPutJSON(t *testing.T, path string, appKey string, body map[string]any, wantStatus int) map[string]any {
	t.Helper()
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, e.server.URL+path, bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app put %s: %v", path, err)
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

func (e *testEnv) appPutText(t *testing.T, path string, appKey string, body map[string]any) (int, string) {
	t.Helper()
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, e.server.URL+path, bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app put %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
}

func (e *testEnv) appDeleteText(t *testing.T, path string, appKey string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+appKey)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do app delete %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
}

func (e *testEnv) seedAppAPIKey(t *testing.T, userID string, scopes []string, resourceLimits map[string]any) string {
	t.Helper()
	_, raw, err := e.appKeyService.CreateKey(context.Background(), userID, "test-key", scopes, resourceLimits)
	if err != nil {
		t.Fatalf("create app api key: %v", err)
	}
	return raw
}

func (e *testEnv) seedModelAPIKey(t *testing.T, userID string, name string, model string) string {
	t.Helper()
	_, raw, err := e.modelKeyService.CreateKey(context.Background(), userID, name, model)
	if err != nil {
		t.Fatalf("create model api key: %v", err)
	}
	return raw
}

func (e *testEnv) requestIDForConversation(t *testing.T, conversationID string) string {
	t.Helper()
	messagesResp := e.getJSON(t, "/api/conversations/"+conversationID+"/messages", http.StatusOK)
	items := messagesResp["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("no messages for conversation %s", conversationID)
	}
	requestDebug := items[0].(map[string]any)["metadata"].(map[string]any)["request_debug"].(map[string]any)
	requestID, _ := requestDebug["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		t.Fatalf("missing request_id for conversation %s", conversationID)
	}
	return requestID
}

func (e *testEnv) getJSON(t *testing.T, path string, wantStatus int) map[string]any {
	t.Helper()
	resp, err := e.client.Get(e.server.URL + path)
	if err != nil {
		t.Fatalf("get request %s: %v", path, err)
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

func (e *testEnv) getText(t *testing.T, path string) (int, string) {
	t.Helper()
	return e.getTextWithHeaders(t, path, nil)
}

func (e *testEnv) getTextWithHeaders(t *testing.T, path string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("get request %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data)
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
	return startJSONRequestWithHeaders(t, url, nil, body)
}

func startJSONRequestWithHeaders(t *testing.T, url string, headers map[string]string, body map[string]any) <-chan map[string]any {
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
		for key, value := range headers {
			req.Header.Set(key, value)
		}

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

func postExternalText(t *testing.T, url string, headers map[string]string, body map[string]any) (int, string) {
	t.Helper()
	rawBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do external post %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read external response %s: %v", url, err)
	}
	return resp.StatusCode, string(data)
}

func startTextRequest(t *testing.T, url string, body map[string]any) <-chan string {
	t.Helper()
	resultCh := make(chan string, 1)

	go func() {
		rawBody, err := json.Marshal(body)
		if err != nil {
			resultCh <- "__error__:" + err.Error()
			return
		}

		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(rawBody))
		if err != nil {
			resultCh <- "__error__:" + err.Error()
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			resultCh <- "__error__:" + err.Error()
			return
		}
		defer resp.Body.Close()

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			resultCh <- "__error__:" + err.Error()
			return
		}
		resultCh <- string(data)
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

func numericValue(value any) int {
	switch raw := value.(type) {
	case float64:
		return int(raw)
	case int:
		return raw
	default:
		return 0
	}
}
