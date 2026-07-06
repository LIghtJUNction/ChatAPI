package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/platform/apikey"
	passwordhash "github.com/zyf/chatapi/internal/platform/password"
	"github.com/zyf/chatapi/internal/repository/migrations"
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
	"github.com/zyf/chatapi/internal/testutil/pgtest"
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

func TestPendingTurnExpiration(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-expire",
		"input": "pending expiration 测试",
	})
	conversation := env.waitForWaitingConversation(t, "pending expiration 测试")

	result, err := env.chatService.ExpirePendingTurns(context.Background(), time.Nanosecond, time.Now().UTC().Add(time.Second))
	if err != nil {
		t.Fatalf("expire pending turns: %v", err)
	}
	if result.ExpiredConversations != 1 || result.ExpiredActiveTurns != 1 {
		t.Fatalf("unexpected expiration result: %#v", result)
	}

	finalResp := <-resultCh
	if nestedPathString(finalResp, "error", "code") != "request_timeout" {
		t.Fatalf("expected timeout response: %#v", finalResp)
	}
	updated, err := env.store.GetConversation(context.Background(), conversation["id"].(string))
	if err != nil {
		t.Fatalf("get expired conversation: %v", err)
	}
	if nestedString(updated.Metadata, "realtime_status") != "expired" {
		t.Fatalf("expected expired conversation status: %#v", updated.Metadata)
	}

	status, body := env.postText(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "too late",
		"mode": "assistant_message",
	})
	if status != http.StatusConflict || !strings.Contains(body, "pending turn already finalized") {
		t.Fatalf("expected expired turn conflict: status=%d body=%q", status, body)
	}
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
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	createResp := env.postJSON(t, "/api/user/app-api-keys", map[string]any{
		"name":            "managed-key",
		"scopes":          []string{"requests:read"},
		"resource_limits": map[string]any{"allowed_request_actions": []string{"complete"}},
		"expires_at":      expiresAt.Format(time.RFC3339),
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
	if nestedString(item, "expires_at") != expiresAt.Format(time.RFC3339) {
		t.Fatalf("unexpected app api key expires_at: %#v", createResp)
	}

	listResp := env.getJSON(t, "/api/user/app-api-keys", http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) == 0 || nestedString(items[0].(map[string]any), "id") != keyID {
		t.Fatalf("unexpected app api keys list: %#v", listResp)
	}
	if nestedString(items[0].(map[string]any), "expires_at") != expiresAt.Format(time.RFC3339) {
		t.Fatalf("unexpected app api keys list expires_at: %#v", listResp)
	}

	status, body := env.deleteText(t, "/api/user/app-api-keys/"+keyID)
	if status != http.StatusOK || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("unexpected delete response: status=%d body=%q", status, body)
	}
	assertAuditCount(t, env, "user.app_api_key", "app_api_key", keyID, "create", "success", 1)
	assertAuditCount(t, env, "user.app_api_key", "app_api_key", keyID, "delete", "success", 1)

	status, body = env.appGetText(t, "/api/app/me", rawKey)
	if status != http.StatusUnauthorized {
		t.Fatalf("revoked key should be unauthorized: status=%d body=%q", status, body)
	}

	status, body = env.postText(t, "/api/user/app-api-keys", map[string]any{
		"name":       "expired-key",
		"scopes":     []string{"requests:read"},
		"expires_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "expires_at must be in the future") {
		t.Fatalf("expected expired app key creation rejection: status=%d body=%q", status, body)
	}
}

func TestUserConfigManagementInLab(t *testing.T) {
	env := newTestEnv(t)

	initial := env.getJSON(t, "/api/user/config", http.StatusOK)
	if initial["count"] != nil {
		t.Fatalf("unexpected legacy count field: %#v", initial)
	}
	if configMap := initial["config"].(map[string]any); len(configMap) != 0 {
		t.Fatalf("expected empty initial config: %#v", initial)
	}

	updateResp := env.postJSON(t, "/api/user/config", map[string]any{
		"config": map[string]any{
			"workspace": map[string]any{
				"theme":   "dark",
				"compact": true,
			},
			"notifications": map[string]any{
				"email": false,
			},
		},
	}, http.StatusOK)
	if nestedPathString(updateResp, "config", "workspace", "theme") != "dark" {
		t.Fatalf("unexpected user config update response: %#v", updateResp)
	}
	if !nestedPathBool(updateResp, "config", "workspace", "compact") {
		t.Fatalf("missing compact config: %#v", updateResp)
	}

	getResp := env.getJSON(t, "/api/user/config", http.StatusOK)
	if nestedPathString(getResp, "config", "workspace", "theme") != "dark" {
		t.Fatalf("unexpected user config get response: %#v", getResp)
	}
	assertAuditCount(t, env, "user.config", "user_config", "lab-user", "update", "success", 1)
}

func TestUserConfigManagementUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("config-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_config_owner",
		Username:     "config-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed config user: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "config-owner",
		"password": "config-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing user session cookie: %#v", cookies)
	}

	headers := map[string]string{"Origin": env.server.URL}
	updateResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/user/config", map[string]any{
		"workspace": map[string]any{
			"layout": "dense",
		},
	}, sessionCookie, headers, http.StatusOK)
	if nestedPathString(updateResp, "config", "workspace", "layout") != "dense" {
		t.Fatalf("unexpected session user config update: %#v", updateResp)
	}
	getResp := env.getJSONWithCookie(t, "/api/user/config", sessionCookie, http.StatusOK)
	if nestedPathString(getResp, "config", "workspace", "layout") != "dense" {
		t.Fatalf("unexpected session user config get: %#v", getResp)
	}

	items, err := env.store.ListUserConfigs(context.Background(), "user_config_owner")
	if err != nil {
		t.Fatalf("list stored user configs: %v", err)
	}
	if len(items) != 1 || items[0].Key != "workspace" || items[0].Value["layout"] != "dense" {
		t.Fatalf("unexpected stored user configs: %#v", items)
	}
	assertAuditCountForActor(t, env, "user_config_owner", "user.config", "user_config", "user_config_owner", "update", "success", 1)
}

func TestConfigModelsRoutesAndModelsEndpoint(t *testing.T) {
	env := newTestEnv(t)

	initial := env.getJSON(t, "/api/config/models", http.StatusOK)
	initialModels := initial["models"].([]any)
	if len(initialModels) != 1 || initialModels[0].(string) != "chatapi-lab" {
		t.Fatalf("unexpected default config models: %#v", initial)
	}

	updateResp := env.postJSON(t, "/api/config/models", map[string]any{
		"id":       "chatapi-demo",
		"name":     "ChatAPI Demo",
		"owned_by": "chatapi",
		"enabled":  true,
	}, http.StatusOK)
	models := updateResp["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("expected default + custom virtual model: %#v", updateResp)
	}

	modelsResp := env.getJSON(t, "/models", http.StatusOK)
	modelData := modelsResp["data"].([]any)
	foundDefault := false
	foundCustom := false
	for _, rawItem := range modelData {
		item := rawItem.(map[string]any)
		switch nestedString(item, "id") {
		case "chatapi-lab":
			foundDefault = true
		case "chatapi-demo":
			foundCustom = true
		}
	}
	if !foundDefault || !foundCustom {
		t.Fatalf("expected default and custom models in /models response: %#v", modelsResp)
	}

	env.deleteJSON(t, "/api/config/models/chatapi-demo", http.StatusOK)
	afterDelete := env.getJSON(t, "/api/config/models", http.StatusOK)
	afterDeleteModels := afterDelete["models"].([]any)
	if len(afterDeleteModels) != 1 || afterDeleteModels[0].(string) != "chatapi-lab" {
		t.Fatalf("unexpected config models after delete: %#v", afterDelete)
	}

	assertAuditCount(t, env, "user.config", "virtual_model", "chatapi-demo", "upsert", "success", 1)
	assertAuditCount(t, env, "user.config", "virtual_model", "chatapi-demo", "delete", "success", 1)
}

func TestConfigSystemRoutes(t *testing.T) {
	env := newTestEnv(t)

	initial := env.getJSON(t, "/api/config/system", http.StatusOK)
	if nestedPathString(initial, "ntfy_private_url_policy") != "disabled" {
		t.Fatalf("unexpected default system config: %#v", initial)
	}
	if numericValue(initial["realtime_queue_size"]) != 100 {
		t.Fatalf("unexpected default realtime queue size: %#v", initial)
	}

	updateResp := env.postJSON(t, "/api/config/system", map[string]any{
		"title_enabled":           true,
		"title":                   "ChatAPI Test",
		"public_statistics":       true,
		"ntfy_private_url_policy": "admin",
		"registration_email_domain_restriction_enabled": true,
		"registration_email_domains":                    "example.com,example.org",
		"pending_max_output_chars":                      512,
	}, http.StatusOK)
	if !nestedPathBool(updateResp, "title_enabled") || nestedPathString(updateResp, "title") != "ChatAPI Test" {
		t.Fatalf("unexpected updated system config: %#v", updateResp)
	}
	if nestedPathString(updateResp, "ntfy_private_url_policy") != "admin" {
		t.Fatalf("unexpected ntfy policy in updated config: %#v", updateResp)
	}

	getResp := env.getJSON(t, "/api/config/system", http.StatusOK)
	if !nestedPathBool(getResp, "public_statistics") || nestedPathString(getResp, "registration_email_domains") != "example.com,example.org" {
		t.Fatalf("unexpected persisted system config: %#v", getResp)
	}
	assertAuditCount(t, env, "admin.config", "system_settings", "", "update", "success", 1)
}

func TestConfigAutomationRulesRoutes(t *testing.T) {
	env := newTestEnv(t)

	initial := env.getJSON(t, "/api/config/automation-rules", http.StatusOK)
	initialRules := initial["rules"].([]any)
	if len(initialRules) != 0 {
		t.Fatalf("expected empty automation rules by default: %#v", initial)
	}

	updateResp := env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_demo",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "hello"}},
					"excludes": []map[string]any{},
				},
				"timing": map[string]any{
					"delay_seconds":           0,
					"repeat_interval_seconds": 0,
					"max_output_count":        3,
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "world",
				},
			},
		},
	}, http.StatusOK)
	updatedRules := updateResp["rules"].([]any)
	if len(updatedRules) != 1 || nestedString(updatedRules[0].(map[string]any), "id") != "rule_demo" {
		t.Fatalf("unexpected updated automation rules: %#v", updateResp)
	}

	getResp := env.getJSON(t, "/api/config/automation-rules", http.StatusOK)
	persistedRules := getResp["rules"].([]any)
	if len(persistedRules) != 1 || nestedString(persistedRules[0].(map[string]any), "id") != "rule_demo" {
		t.Fatalf("unexpected persisted automation rules: %#v", getResp)
	}
	assertAuditCount(t, env, "user.config", "automation_rule", "", "replace", "success", 1)
}

func TestUserPasswordRoute(t *testing.T) {
	env := newTestEnv(t)

	beforeHash, err := passwordhash.Hash("old-secret-password")
	if err != nil {
		t.Fatalf("hash old password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "lab-user",
		Username:     "lab-user",
		Email:        "lab@example.com",
		PasswordHash: beforeHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed lab user: %v", err)
	}

	before, err := env.store.GetUser(context.Background(), "lab-user")
	if err != nil {
		t.Fatalf("get lab user before password update: %v", err)
	}
	if before.PasswordHash == "" {
		t.Fatal("expected seeded lab user password hash")
	}

	env.postJSON(t, "/api/user/password", map[string]any{
		"password": "new-secret-password",
	}, http.StatusOK)

	after, err := env.store.GetUser(context.Background(), "lab-user")
	if err != nil {
		t.Fatalf("get lab user after password update: %v", err)
	}
	if after.PasswordHash == before.PasswordHash {
		t.Fatal("expected password hash to change")
	}
	result, err := passwordhash.Verify("new-secret-password", after.PasswordHash)
	if err != nil || !result.OK {
		t.Fatalf("verify updated password hash: ok=%v err=%v", result.OK, err)
	}
	assertAuditCount(t, env, "user.password", "user", "lab-user", "update", "success", 1)
}

func TestUserIdentitiesListAndUnlink(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("identity-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_identity_owner",
		Username:     "identity-owner",
		Email:        "identity@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed identity user: %v", err)
	}
	identity, err := env.store.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:            "identity_unlink",
		UserID:        "user_identity_owner",
		Provider:      "oidc",
		Subject:       "sub-unlink",
		Email:         "identity@example.com",
		EmailVerified: true,
		Profile: map[string]any{
			"name": "Identity Owner",
		},
	})
	if err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "identity-owner",
		"password": "identity-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing session cookie: %#v", cookies)
	}

	listResp := env.getJSONWithCookie(t, "/api/user/identities", sessionCookie, http.StatusOK)
	if numericValue(listResp["count"]) != 1 || nestedPathString(listResp["items"].([]any)[0].(map[string]any), "id") != identity.ID {
		t.Fatalf("unexpected identities list: %#v", listResp)
	}

	status, body := env.deleteTextWithCookieAndHeaders(t, "/api/user/identities/"+identity.ID, sessionCookie, nil)
	if status != http.StatusForbidden || !strings.Contains(body, "csrf origin check failed") {
		t.Fatalf("expected csrf rejection, status=%d body=%q", status, body)
	}
	deleteResp := env.deleteJSONWithCookieAndHeaders(t, "/api/user/identities/"+identity.ID, sessionCookie, map[string]string{
		"Origin": env.server.URL,
	}, http.StatusOK)
	if deleteResp["ok"] != true {
		t.Fatalf("unexpected identity delete response: %#v", deleteResp)
	}
	if _, err := env.store.GetUserIdentity(context.Background(), "oidc", "sub-unlink"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected identity to be deleted, got %v", err)
	}
	assertAuditCountForActor(t, env, "user_identity_owner", "user.identity", "user_identity", identity.ID, "unlink", "success", 1)
}

func TestUserIdentityUnlinkRejectsLastLoginMethod(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "oidc_only",
		Email:    "oidc-only@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("seed oidc-only user: %v", err)
	}
	identity, err := env.store.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:       "identity_last",
		UserID:   "oidc_only",
		Provider: "oidc",
		Subject:  "sub-last",
		Email:    "oidc-only@example.com",
	})
	if err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	codec := service.NewSessionCodec("test-session-secret")
	sessionValue, err := codec.Encode(service.RequestActor{
		UserID:   "oidc_only",
		Username: "oidc-only@example.com",
		Role:     "user",
		Source:   "oidc",
	}, service.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("encode session: %v", err)
	}
	sessionCookie := &http.Cookie{Name: service.SessionCookieName, Value: sessionValue}
	status, body := env.deleteTextWithCookieAndHeaders(t, "/api/user/identities/"+identity.ID, sessionCookie, map[string]string{
		"Origin": env.server.URL,
	})
	if status != http.StatusConflict || !strings.Contains(body, "cannot unlink the last login method") {
		t.Fatalf("expected last login method conflict, status=%d body=%q", status, body)
	}
}

func TestExpiredAppAPIKeyRejected(t *testing.T) {
	env := newTestEnv(t)
	expiredAt := time.Now().UTC().Add(-time.Hour)
	_, rawKey, err := env.appKeyService.CreateKey(context.Background(), "lab-user", "already-expired", []string{"requests:read"}, nil, &expiredAt)
	if !errors.Is(err, service.ErrInvalidAppAPIKeyExpiry) {
		t.Fatalf("service should reject expired key creation, got raw=%q err=%v", rawKey, err)
	}

	item, err := env.store.CreateAppAPIKey(context.Background(), store.CreateAppAPIKeyInput{
		ID:        "appkey_expired_test",
		UserID:    "lab-user",
		Name:      "expired fixture",
		KeyHash:   apikey.Hash("ak-expired-fixture"),
		KeyPrefix: apikey.Prefix("ak-expired-fixture"),
		Scopes:    []string{"requests:read"},
		ExpiresAt: &expiredAt,
	})
	if err != nil {
		t.Fatalf("seed expired app api key: %v", err)
	}
	_ = item
	status, body := env.appGetText(t, "/api/app/me", "ak-expired-fixture")
	if status != http.StatusUnauthorized || !strings.Contains(body, "unauthorized") {
		t.Fatalf("expected expired app api key rejection: status=%d body=%q", status, body)
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
	assertAuditCount(t, env, "user.model_api_key", "model_api_key", keyID, "create", "success", 1)
	assertAuditCount(t, env, "user.model_api_key", "model_api_key", keyID, "delete", "success", 1)

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
		"max_model_keys":         1,
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
		"name":  "second-managed-model-key",
		"model": "demo-app-managed-model",
	})
	if status != http.StatusForbidden || !strings.Contains(body, "model key limit exceeded") {
		t.Fatalf("expected model key count limit rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPostText(t, "/api/app/model-keys", appKey, map[string]any{
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
	recreateResp := env.appPostJSON(t, "/api/app/model-keys", appKey, map[string]any{
		"name":  "recreated-managed-model-key",
		"model": "demo-app-managed-model",
	}, http.StatusOK)
	if !strings.HasPrefix(nestedString(recreateResp, "raw_key"), "sk-") {
		t.Fatalf("expected revoked model key not to count against limit: %#v", recreateResp)
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

func TestHealthAndReadyEndpoints(t *testing.T) {
	env := newTestEnv(t)

	healthResp := env.getJSON(t, "/api/health", http.StatusOK)
	if healthResp["ok"] != true || nestedString(healthResp, "driver") != "sqlite" {
		t.Fatalf("unexpected health response: %#v", healthResp)
	}

	readyResp := env.getJSON(t, "/api/ready", http.StatusOK)
	if readyResp["ok"] != true {
		t.Fatalf("unexpected ready response: %#v", readyResp)
	}
	migration := readyResp["migration"].(map[string]any)
	if migration["ok"] != true || nestedString(migration, "schema_version") == "" {
		t.Fatalf("unexpected ready migration response: %#v", readyResp)
	}
}

func TestReadyEndpointRejectsDirtyMigration(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.rawDB.ExecContext(context.Background(), `UPDATE db_meta SET value = '1' WHERE key = 'migration_dirty'`); err != nil {
		t.Fatalf("mark migration dirty: %v", err)
	}

	readyResp := env.getJSON(t, "/api/ready", http.StatusServiceUnavailable)
	if readyResp["ok"] != false {
		t.Fatalf("expected not ready response: %#v", readyResp)
	}
	migration := readyResp["migration"].(map[string]any)
	if migration["ok"] != false || migration["migration_dirty"] != true || nestedString(migration, "error") != "migration dirty" {
		t.Fatalf("unexpected dirty migration readiness response: %#v", readyResp)
	}
}

func TestMetricsDisabledByDefault(t *testing.T) {
	env := newTestEnv(t)

	status, _ := env.getText(t, "/metrics")
	if status != http.StatusNotFound {
		t.Fatalf("expected disabled metrics to be not found, got %d", status)
	}
}

func TestMetricsEndpointWhenEnabled(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.MetricsEnabled = true
	})

	env.getJSON(t, "/api/health", http.StatusOK)
	status, body := env.getText(t, "/metrics")
	if status != http.StatusOK {
		t.Fatalf("expected metrics ok: status=%d body=%q", status, body)
	}
	for _, expected := range []string{
		"# HELP chatapi_go_goroutines",
		"chatapi_system_memory_total_bytes",
		"chatapi_process_open_fds",
		"chatapi_data_dir_disk_total_bytes",
		"chatapi_pending_turns",
		"chatapi_realtime_subscribers",
		"chatapi_sqlite_database_bytes",
		`chatapi_http_requests_total{method="GET",route="/api/health",status="200"} 1`,
		`chatapi_http_request_duration_seconds_count{method="GET",route="/api/health",status="200"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q in body:\n%s", expected, body)
		}
	}
}

func TestMetricsEndpointWhenEnabledWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.MetricsEnabled = true
	})

	env.getJSON(t, "/api/health", http.StatusOK)
	status, body := env.getText(t, "/metrics")
	if status != http.StatusOK {
		t.Fatalf("expected metrics ok: status=%d body=%q", status, body)
	}
	for _, expected := range []string{
		"# HELP chatapi_go_goroutines",
		"chatapi_system_memory_total_bytes",
		"chatapi_process_open_fds",
		"chatapi_data_dir_disk_total_bytes",
		"chatapi_pending_turns",
		"chatapi_realtime_subscribers",
		"chatapi_postgres_pool_max_conns",
		"chatapi_postgres_pool_total_conns",
		`chatapi_http_requests_total{method="GET",route="/api/health",status="200"} 1`,
		`chatapi_http_request_duration_seconds_count{method="GET",route="/api/health",status="200"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q in body:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "chatapi_sqlite_database_bytes") {
		t.Fatalf("postgres metrics should not expose sqlite database bytes:\n%s", body)
	}
}

func TestUploadsImageReadAndUsage(t *testing.T) {
	env := newTestEnv(t)
	uploadDir := filepath.Join(env.dataDir, "uploads", "imgs")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("create upload dir: %v", err)
	}
	content := []byte("fake image bytes")
	if err := os.WriteFile(filepath.Join(uploadDir, "demo.png"), content, 0o644); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	status, body := env.getText(t, "/api/uploads/imgs/demo.png")
	if status != http.StatusOK || body != string(content) {
		t.Fatalf("unexpected upload image response: status=%d body=%q", status, body)
	}

	usageResp := env.getJSON(t, "/api/uploads/imgs/usage", http.StatusOK)
	usage := usageResp["usage"].(map[string]any)
	if numericValue(usage["file_count"]) != 1 || numericValue(usage["bytes"]) != len(content) {
		t.Fatalf("unexpected upload usage response: %#v", usageResp)
	}
}

func TestUploadsImageCreate(t *testing.T) {
	env := newTestEnv(t)

	resp := env.postMultipart(t, "/api/uploads/imgs", "file", "tiny.png", tinyPNG(), http.StatusOK)
	upload := resp["upload"].(map[string]any)
	filename := nestedString(upload, "filename")
	if !strings.HasSuffix(filename, ".png") || nestedString(upload, "content_type") != "image/png" {
		t.Fatalf("unexpected upload response: %#v", resp)
	}
	if nestedString(upload, "id") == "" || nestedString(upload, "owner_id") != "lab-user" || nestedString(upload, "original_filename") != "tiny.png" {
		t.Fatalf("unexpected upload metadata response: %#v", resp)
	}

	status, body := env.getText(t, nestedString(upload, "url"))
	if status != http.StatusOK || len(body) != len(tinyPNG()) {
		t.Fatalf("unexpected uploaded image read: status=%d len=%d", status, len(body))
	}

	var ownerID string
	var originalFilename string
	var contentType string
	var bytes int
	var url string
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT owner_id, original_filename, content_type, bytes, url
		FROM uploaded_images
		WHERE filename = ?
	`, filename).Scan(&ownerID, &originalFilename, &contentType, &bytes, &url); err != nil {
		t.Fatalf("read uploaded image metadata: %v", err)
	}
	if ownerID != "lab-user" || originalFilename != "tiny.png" || contentType != "image/png" || bytes != len(tinyPNG()) || url != nestedString(upload, "url") {
		t.Fatalf("unexpected uploaded image metadata: owner=%q original=%q type=%q bytes=%d url=%q", ownerID, originalFilename, contentType, bytes, url)
	}

	var auditCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'upload'
			AND resource_type = 'image'
			AND action = 'create'
			AND outcome = 'success'
	`).Scan(&auditCount); err != nil {
		t.Fatalf("count upload audit logs: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("expected upload audit log entry, got %d", auditCount)
	}
}

func TestUploadsRejectsUnsupportedType(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.postMultipartText(t, "/api/uploads/imgs", "file", "note.txt", []byte("not an image"))
	if status != http.StatusUnsupportedMediaType || !strings.Contains(body, "unsupported upload type") {
		t.Fatalf("expected unsupported upload rejection: status=%d body=%q", status, body)
	}
}

func TestUploadsRejectsTooLargeFile(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.UploadMaxBytes = 16
	})

	status, body := env.postMultipartText(t, "/api/uploads/imgs", "file", "tiny.png", tinyPNG())
	if status != http.StatusRequestEntityTooLarge || !strings.Contains(body, "upload too large") {
		t.Fatalf("expected too large upload rejection: status=%d body=%q", status, body)
	}
}

func TestUploadsRejectsStorageQuotaExceeded(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.StorageDefaultQuotaBytes = int64(len(tinyPNG()) + 1)
	})

	env.postMultipart(t, "/api/uploads/imgs", "file", "first.png", tinyPNG(), http.StatusOK)
	status, body := env.postMultipartText(t, "/api/uploads/imgs", "file", "second.png", tinyPNG())
	if status != http.StatusInsufficientStorage || !strings.Contains(body, "storage quota exceeded") {
		t.Fatalf("expected storage quota rejection: status=%d body=%q", status, body)
	}

	var uploadCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM uploaded_images
		WHERE owner_id = 'lab-user'
	`).Scan(&uploadCount); err != nil {
		t.Fatalf("count uploaded images: %v", err)
	}
	if uploadCount != 1 {
		t.Fatalf("expected only first upload metadata to be saved, got %d", uploadCount)
	}

	var failureAuditCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'upload'
			AND action = 'create'
			AND outcome = 'failure'
	`).Scan(&failureAuditCount); err != nil {
		t.Fatalf("count failed upload audit logs: %v", err)
	}
	if failureAuditCount != 1 {
		t.Fatalf("expected failed upload audit log, got %d", failureAuditCount)
	}
}

func TestAdminStorageUserQuotaOverrideControlsUploads(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.StorageDefaultQuotaBytes = 1 << 20
	})

	quotaResp := env.putJSON(t, "/api/admin/storage/users/lab-user/quota", map[string]any{
		"quota_bytes": len(tinyPNG()) + 1,
	}, http.StatusOK)
	quota := quotaResp["quota"].(map[string]any)
	if nestedString(quota, "owner_id") != "lab-user" || numericValue(quota["quota_bytes"]) != len(tinyPNG())+1 {
		t.Fatalf("unexpected quota response: %#v", quotaResp)
	}

	env.postMultipart(t, "/api/uploads/imgs", "file", "first.png", tinyPNG(), http.StatusOK)
	status, body := env.postMultipartText(t, "/api/uploads/imgs", "file", "second.png", tinyPNG())
	if status != http.StatusInsufficientStorage || !strings.Contains(body, "storage quota exceeded") {
		t.Fatalf("expected override quota rejection: status=%d body=%q", status, body)
	}

	usersResp := env.getJSON(t, "/api/admin/storage/users", http.StatusOK)
	items := usersResp["items"].([]any)
	foundOverride := false
	for _, item := range items {
		record := item.(map[string]any)
		if nestedString(record, "user_id") == "lab-user" &&
			numericValue(record["storage_quota_default_bytes"]) == 1<<20 &&
			numericValue(record["storage_quota_override_bytes"]) == len(tinyPNG())+1 &&
			numericValue(record["storage_quota_bytes"]) == len(tinyPNG())+1 {
			foundOverride = true
			break
		}
	}
	if !foundOverride {
		t.Fatalf("expected storage quota override in users response: %#v", usersResp)
	}

	env.deleteJSON(t, "/api/admin/storage/users/lab-user/quota", http.StatusOK)
	env.postMultipart(t, "/api/uploads/imgs", "file", "second.png", tinyPNG(), http.StatusOK)
	assertAuditCount(t, env, "admin.storage", "storage_user_quota", "lab-user", "set_quota", "success", 1)
	assertAuditCount(t, env, "admin.storage", "storage_user_quota", "lab-user", "delete_quota", "success", 1)
}

func TestUploadsRejectsUnsafePath(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.getText(t, "/api/uploads/imgs/bad%5Cname.png")
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid upload path") {
		t.Fatalf("expected unsafe upload path rejection: status=%d body=%q", status, body)
	}
}

func TestAdminRuntimeEndpoints(t *testing.T) {
	env := newTestEnv(t)

	summaryResp := env.getJSON(t, "/api/admin/runtime/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	if nestedPathString(summary, "go", "version") == "" || summary["system"] == nil || summary["memory"] == nil || summary["pending"] == nil || summary["realtime"] == nil {
		t.Fatalf("unexpected runtime summary response: %#v", summaryResp)
	}
	system := summary["system"].(map[string]any)
	if nestedString(system, "os") == "" || numericValue(system["num_cpu"]) <= 0 || numericValue(system["process_open_fds"]) <= 0 {
		t.Fatalf("unexpected runtime system summary: %#v", summaryResp)
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

	systemResp := env.getJSON(t, "/api/admin/runtime/system", http.StatusOK)
	system = systemResp["system"].(map[string]any)
	if numericValue(system["system_memory_total_bytes"]) <= 0 || numericValue(system["data_dir_disk_total_bytes"]) <= 0 || numericValue(system["process_rss_bytes"]) <= 0 {
		t.Fatalf("unexpected runtime system response: %#v", systemResp)
	}

	gcResp := env.postJSON(t, "/api/admin/runtime/gc", map[string]any{}, http.StatusOK)
	if gcResp["memory"] == nil {
		t.Fatalf("unexpected runtime gc response: %#v", gcResp)
	}
	var gcAuditCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'admin.runtime'
			AND resource_type = 'runtime'
			AND action = 'gc'
			AND outcome = 'success'
	`).Scan(&gcAuditCount); err != nil {
		t.Fatalf("count runtime gc audit logs: %v", err)
	}
	if gcAuditCount != 1 {
		t.Fatalf("expected runtime gc audit log entry, got %d", gcAuditCount)
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
	if _, ok := queue["slow_disconnects"]; !ok {
		t.Fatalf("unexpected runtime queue response: %#v", queueResp)
	}

	settingsResp := env.getJSON(t, "/api/admin/runtime/settings", http.StatusOK)
	settings := settingsResp["settings"].(map[string]any)
	if numericValue(settings["gogc"]) != 0 || numericValue(settings["memory_limit_bytes"]) != 0 {
		t.Fatalf("unexpected default runtime settings response: %#v", settingsResp)
	}

	updateResp := env.putJSON(t, "/api/admin/runtime/settings", map[string]any{
		"gogc":               100,
		"memory_limit_bytes": 256 << 20,
	}, http.StatusOK)
	updated := updateResp["settings"].(map[string]any)
	if numericValue(updated["gogc"]) != 100 || numericValue(updated["memory_limit_bytes"]) != 256<<20 {
		t.Fatalf("unexpected updated runtime settings response: %#v", updateResp)
	}
	var settingsAuditCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'admin.runtime'
			AND resource_type = 'runtime'
			AND action = 'settings_update'
			AND outcome = 'success'
	`).Scan(&settingsAuditCount); err != nil {
		t.Fatalf("count runtime settings audit logs: %v", err)
	}
	if settingsAuditCount != 1 {
		t.Fatalf("expected runtime settings audit log entry, got %d", settingsAuditCount)
	}

	resetResp := env.putJSON(t, "/api/admin/runtime/settings", map[string]any{
		"gogc":               0,
		"memory_limit_bytes": 0,
	}, http.StatusOK)
	reset := resetResp["settings"].(map[string]any)
	if numericValue(reset["gogc"]) != 0 || numericValue(reset["memory_limit_bytes"]) != 0 {
		t.Fatalf("unexpected reset runtime settings response: %#v", resetResp)
	}

	status, body := env.putText(t, "/api/admin/runtime/settings", map[string]any{
		"gogc": -1,
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "gogc must be non-negative") {
		t.Fatalf("expected runtime settings validation error: status=%d body=%q", status, body)
	}
}

func TestAdminRuntimeEndpointsWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	summaryResp := env.getJSON(t, "/api/admin/runtime/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	database := summary["database"].(map[string]any)
	if nestedString(database, "driver") != "postgresql" {
		t.Fatalf("unexpected postgres runtime database info: %#v", summaryResp)
	}
	if numericValue(database["postgres_max_conns"]) <= 0 {
		t.Fatalf("expected postgres max conns in runtime summary: %#v", summaryResp)
	}
	if _, ok := database["sqlite_path"]; ok {
		t.Fatalf("postgres runtime summary should not expose sqlite path: %#v", summaryResp)
	}

	connectionsResp := env.getJSON(t, "/api/admin/runtime/connections", http.StatusOK)
	if connectionsResp["connections"] == nil {
		t.Fatalf("unexpected runtime connections response: %#v", connectionsResp)
	}
	queueResp := env.getJSON(t, "/api/admin/runtime/queue", http.StatusOK)
	if queueResp["queue"] == nil {
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

func TestServeAdminSessionLoginAndLogout(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.AdminPassword = "admin-secret"
	})

	sessionResp := env.getJSON(t, "/api/auth/session", http.StatusOK)
	if sessionResp["authenticated"] != false {
		t.Fatalf("expected unauthenticated session before login: %#v", sessionResp)
	}

	status, body := env.postText(t, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "wrong",
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "invalid username or password") {
		t.Fatalf("expected invalid login rejection: status=%d body=%q", status, body)
	}

	loginResp, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "admin-secret",
	}, http.StatusOK)
	if loginResp["ok"] != true || nestedPathString(loginResp, "user", "role") != "admin" {
		t.Fatalf("unexpected login response: %#v", loginResp)
	}
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly {
		t.Fatalf("expected signed session cookie, got %#v", cookies)
	}

	adminResp := env.getJSONWithCookie(t, "/api/admin/runtime/summary", sessionCookie, http.StatusOK)
	if adminResp["summary"] == nil {
		t.Fatalf("expected admin runtime summary with session cookie: %#v", adminResp)
	}
	sessionResp = env.getJSONWithCookie(t, "/api/auth/session", sessionCookie, http.StatusOK)
	if sessionResp["authenticated"] != true || nestedPathString(sessionResp, "user", "id") != "admin" {
		t.Fatalf("expected authenticated admin session: %#v", sessionResp)
	}

	status, body, _ = env.postJSONWithCookieText(t, "/api/admin/runtime/gc", map[string]any{}, sessionCookie, nil)
	if status != http.StatusForbidden || !strings.Contains(body, "csrf origin check failed") {
		t.Fatalf("expected csrf rejection for session mutation without origin: status=%d body=%q", status, body)
	}
	gcResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/runtime/gc", map[string]any{}, sessionCookie, map[string]string{
		"Origin": env.server.URL,
	}, http.StatusOK)
	if gcResp["memory"] == nil {
		t.Fatalf("expected csrf-approved gc response: %#v", gcResp)
	}

	_, logoutCookies := env.postJSONWithCookieAndHeaders(t, "/api/auth/logout", map[string]any{}, sessionCookie, map[string]string{
		"Origin": env.server.URL,
	}, http.StatusOK)
	expiredCookie := findCookie(logoutCookies, service.SessionCookieName)
	if expiredCookie == nil || expiredCookie.MaxAge >= 0 {
		t.Fatalf("expected logout to expire session cookie: %#v", logoutCookies)
	}
	assertAuditCountForActor(t, env, "admin", "auth.session", "session", "admin", "login", "success", 1)
	assertAuditCountForActor(t, env, "admin", "auth.session", "session", "admin", "logout", "success", 1)
}

func TestOIDCConfigEndpointReflectsServeSettings(t *testing.T) {
	disabledEnv := newTestEnvWithMode(t, config.ModeServe)
	disabled := disabledEnv.getJSON(t, "/api/auth/oidc/config", http.StatusOK)
	if disabled["enabled"] != false {
		t.Fatalf("expected disabled oidc config: %#v", disabled)
	}

	enabledEnv := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCProviderName = "Kirari"
		cfg.OIDCIssuerURL = "https://issuer.example.com"
		cfg.OIDCClientID = "chatapi"
		cfg.OIDCClientSecret = "secret"
		cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
	})
	enabled := enabledEnv.getJSON(t, "/api/auth/oidc/config", http.StatusOK)
	if enabled["enabled"] != true || enabled["provider_name"] != "Kirari" || enabled["login_url"] != "/api/auth/oidc/login" {
		t.Fatalf("unexpected enabled oidc config: %#v", enabled)
	}
}

func TestOIDCLoginRedirectUsesPKCE(t *testing.T) {
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 issuer,
			"authorization_endpoint": issuer + "/authorize",
			"token_endpoint":         issuer + "/token",
			"jwks_uri":               issuer + "/jwks",
			"userinfo_endpoint":      issuer + "/userinfo",
		})
	}))
	defer provider.Close()
	issuer = provider.URL
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCIssuerURL = issuer
		cfg.OIDCClientID = "chatapi"
		cfg.OIDCClientSecret = "secret"
		cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
	})

	status, location, cookies := env.getRedirect(t, "/api/auth/oidc/login")
	if status != http.StatusFound {
		t.Fatalf("expected oidc redirect, got status=%d location=%q", status, location)
	}
	if !strings.HasPrefix(location, issuer+"/authorize?") ||
		!strings.Contains(location, "code_challenge=") ||
		!strings.Contains(location, "code_challenge_method=S256") ||
		!strings.Contains(location, "nonce=") {
		t.Fatalf("authorization redirect missing oidc/pkce parameters: %s", location)
	}
	if findCookie(cookies, "chatapi_oidc_state") == nil ||
		findCookie(cookies, "chatapi_oidc_nonce") == nil ||
		findCookie(cookies, "chatapi_oidc_pkce") == nil {
		t.Fatalf("expected state, nonce and pkce cookies, got %#v", cookies)
	}
}

func TestServeLocalUserLoginFromUsersTable(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("alice-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_alice",
		Username:     "alice",
		Email:        "alice@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	loginResp, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "alice",
		"password": "alice-secret",
	}, http.StatusOK)
	if loginResp["ok"] != true || nestedPathString(loginResp, "user", "id") != "user_alice" || nestedPathString(loginResp, "user", "role") != "user" {
		t.Fatalf("unexpected local user login response: %#v", loginResp)
	}
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected local user session cookie, got %#v", cookies)
	}
	sessionResp := env.getJSONWithCookie(t, "/api/auth/session", sessionCookie, http.StatusOK)
	if sessionResp["authenticated"] != true || nestedPathString(sessionResp, "user", "id") != "user_alice" {
		t.Fatalf("expected authenticated local user session: %#v", sessionResp)
	}
}

func TestServeLocalUserLoginUpgradesLegacyPasswordHash(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	salt := "legacy-salt"
	sum := sha256.Sum256([]byte(salt + "legacy-secret"))
	legacyHash := fmt.Sprintf("%s$%x", salt, sum[:])
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_legacy",
		Username:     "legacy",
		Email:        "legacy@example.com",
		PasswordHash: legacyHash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed legacy user: %v", err)
	}

	env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "legacy@example.com",
		"password": "legacy-secret",
	}, http.StatusOK)
	updated, err := env.store.GetUser(context.Background(), "user_legacy")
	if err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if !strings.HasPrefix(updated.PasswordHash, "$argon2id$v=19$") {
		t.Fatalf("expected password hash upgrade, got %q", updated.PasswordHash)
	}
	if updated.LastLoginAt == nil || updated.LastLoginAt.IsZero() {
		t.Fatalf("expected last login to be updated: %#v", updated)
	}
}

func TestServeAdminLoginRateLimit(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.AdminPassword = "admin-secret"
	})

	for i := 0; i < 5; i++ {
		status, body := env.postText(t, "/api/auth/login", map[string]any{
			"username": "admin",
			"password": "wrong",
		})
		if status != http.StatusUnauthorized || !strings.Contains(body, "invalid username or password") {
			t.Fatalf("expected invalid login rejection on attempt %d: status=%d body=%q", i+1, status, body)
		}
	}
	status, body := env.postText(t, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "admin-secret",
	})
	if status != http.StatusTooManyRequests || !strings.Contains(body, "too many failed login attempts") {
		t.Fatalf("expected login rate limit: status=%d body=%q", status, body)
	}
}

func TestAdminUsersManageLocalUsers(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.AdminPassword = "admin-secret"
	})
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "admin-secret",
	}, http.StatusOK)
	adminCookie := findCookie(cookies, service.SessionCookieName)
	if adminCookie == nil {
		t.Fatalf("missing admin session cookie: %#v", cookies)
	}
	headers := map[string]string{"Origin": env.server.URL}

	createResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "managed",
		"email":    "managed@example.com",
		"password": "initial-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	userID := nestedPathString(createResp, "user", "id")
	if userID == "" || nestedPathString(createResp, "user", "username") != "managed" || nestedPathString(createResp, "user", "password_hash") != "" {
		t.Fatalf("unexpected user create response: %#v", createResp)
	}

	listResp := env.getJSONWithCookie(t, "/api/admin/users", adminCookie, http.StatusOK)
	if listResp["count"].(float64) < 1 || !responseItemsContainID(listResp, userID) {
		t.Fatalf("expected created user in admin list: %#v", listResp)
	}

	resetResp := env.putJSONWithCookieAndHeaders(t, "/api/admin/users/"+userID+"/password", map[string]any{
		"password": "rotated-secret",
	}, adminCookie, headers, http.StatusOK)
	if nestedPathString(resetResp, "user", "id") != userID {
		t.Fatalf("unexpected password reset response: %#v", resetResp)
	}

	loginResp, userCookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "managed",
		"password": "rotated-secret",
	}, http.StatusOK)
	if nestedPathString(loginResp, "user", "id") != userID {
		t.Fatalf("expected managed user login after reset: %#v", loginResp)
	}
	if findCookie(userCookies, service.SessionCookieName) == nil {
		t.Fatalf("expected managed user session cookie: %#v", userCookies)
	}

	deleteResp := env.deleteJSONWithCookieAndHeaders(t, "/api/admin/users/"+userID, adminCookie, headers, http.StatusOK)
	if nestedPathBool(deleteResp, "user", "is_active") {
		t.Fatalf("expected user to be deactivated: %#v", deleteResp)
	}
	status, body := env.postText(t, "/api/auth/login", map[string]any{
		"username": "managed",
		"password": "rotated-secret",
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "invalid username or password") {
		t.Fatalf("expected deactivated user login rejection: status=%d body=%q", status, body)
	}

	assertAuditCountForActor(t, env, "admin", "admin.user", "user", userID, "create", "success", 1)
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", userID, "reset_password", "success", 1)
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", userID, "deactivate", "success", 1)
}

func TestAdminConfigManagement(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.AdminPassword = "admin-secret"
	})
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "admin",
		"password": "admin-secret",
	}, http.StatusOK)
	adminCookie := findCookie(cookies, service.SessionCookieName)
	if adminCookie == nil {
		t.Fatalf("missing admin session cookie: %#v", cookies)
	}

	initial := env.getJSONWithCookie(t, "/api/admin/config", adminCookie, http.StatusOK)
	if configMap := initial["config"].(map[string]any); len(configMap) != 0 {
		t.Fatalf("expected empty system config: %#v", initial)
	}

	headers := map[string]string{"Origin": env.server.URL}
	updateResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/config", map[string]any{
		"config": map[string]any{
			"runtime": map[string]any{
				"gogc": float64(90),
			},
			"storage": map[string]any{
				"cleanup_enabled": true,
			},
		},
	}, adminCookie, headers, http.StatusOK)
	if numericValue(updateResp["config"].(map[string]any)["runtime"].(map[string]any)["gogc"]) != 90 {
		t.Fatalf("unexpected admin config update: %#v", updateResp)
	}
	if !nestedPathBool(updateResp, "config", "storage", "cleanup_enabled") {
		t.Fatalf("missing storage cleanup config: %#v", updateResp)
	}

	getResp := env.getJSONWithCookie(t, "/api/admin/config", adminCookie, http.StatusOK)
	if numericValue(getResp["config"].(map[string]any)["runtime"].(map[string]any)["gogc"]) != 90 {
		t.Fatalf("unexpected admin config get: %#v", getResp)
	}
	assertAuditCountForActor(t, env, "admin", "admin.config", "system_config", "", "update", "success", 1)

	appKey := env.seedAppAPIKey(t, "admin-config-denied", []string{"statistics:read"}, nil)
	status, body := env.getTextWithHeaders(t, "/api/admin/config", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin config rejection: status=%d body=%q", status, body)
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

	status, body = env.getTextWithHeaders(t, "/api/admin/runtime/settings", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key runtime settings rejection: status=%d body=%q", status, body)
	}
}

func TestAdminStorageEndpoints(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.StorageDefaultQuotaBytes = 1 << 20
	})

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
	env.postMultipart(t, "/api/uploads/imgs", "file", "storage.png", tinyPNG(), http.StatusOK)

	summaryResp := env.getJSON(t, "/api/admin/storage/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	if numericValue(summary["conversation_count"]) < 1 || numericValue(summary["message_count"]) < 2 || numericValue(summary["estimated_bytes"]) < len(tinyPNG()) {
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
		if nestedString(record, "user_id") == "lab-user" &&
			numericValue(record["message_count"]) >= 2 &&
			numericValue(record["image_count"]) == 1 &&
			numericValue(record["image_bytes"]) == len(tinyPNG()) &&
			numericValue(record["storage_quota_bytes"]) == 1<<20 &&
			record["storage_over_quota"] == false {
			foundLabUser = true
			break
		}
	}
	if !foundLabUser {
		t.Fatalf("expected lab-user storage usage: %#v", usersResp)
	}
}

func TestAdminStorageSummaryWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	summaryResp := env.getJSON(t, "/api/admin/storage/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	database := summary["database"].(map[string]any)
	if nestedString(database, "driver") != "postgresql" {
		t.Fatalf("unexpected storage database info: %#v", summaryResp)
	}
	if numericValue(database["postgres_max_conns"]) <= 0 {
		t.Fatalf("expected postgres pool stats in storage summary: %#v", summaryResp)
	}
	if _, ok := database["sqlite_path"]; ok {
		t.Fatalf("postgres storage summary should not expose sqlite path: %#v", summaryResp)
	}
	if nestedString(summary["uploads"].(map[string]any), "path") == "" {
		t.Fatalf("expected uploads summary path: %#v", summaryResp)
	}

	usersResp := env.getJSON(t, "/api/admin/storage/users", http.StatusOK)
	items := usersResp["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("expected empty postgres storage users list in fresh env: %#v", usersResp)
	}
}

func TestAdminStorageVacuumWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	dryRunResp := env.postJSON(t, "/api/admin/storage/vacuum", map[string]any{
		"dry_run": true,
	}, http.StatusOK)
	dryRunResult := dryRunResp["result"].(map[string]any)
	before := dryRunResult["before"].(map[string]any)
	if nestedString(before, "driver") != "postgresql" || dryRunResult["after"] != nil {
		t.Fatalf("unexpected postgres vacuum dry-run response: %#v", dryRunResp)
	}

	status, body := env.postText(t, "/api/admin/storage/vacuum", map[string]any{
		"dry_run": false,
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "supports sqlite only") {
		t.Fatalf("expected postgres vacuum unsupported response: status=%d body=%q", status, body)
	}
}

func TestAdminStorageOrphanImagesPreview(t *testing.T) {
	env := newTestEnv(t)

	knownResp := env.postMultipart(t, "/api/uploads/imgs", "file", "known.png", tinyPNG(), http.StatusOK)
	knownURL := nestedPathString(knownResp, "upload", "url")
	if knownURL == "" {
		t.Fatalf("missing uploaded image url: %#v", knownResp)
	}
	uploadDir := filepath.Join(env.dataDir, "uploads", "imgs")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		t.Fatalf("create upload dir: %v", err)
	}
	orphanPath := filepath.Join(uploadDir, "orphan.png")
	orphanContent := []byte("orphan image bytes")
	if err := os.WriteFile(orphanPath, orphanContent, 0o644); err != nil {
		t.Fatalf("write orphan upload: %v", err)
	}

	resp := env.getJSON(t, "/api/admin/storage/orphans", http.StatusOK)
	if resp["dry_run"] != true {
		t.Fatalf("expected orphan preview dry-run: %#v", resp)
	}
	preview := resp["preview"].(map[string]any)
	if numericValue(preview["file_count"]) != 1 || numericValue(preview["bytes"]) != len(orphanContent) {
		t.Fatalf("unexpected orphan preview totals: %#v", resp)
	}
	items := preview["items"].([]any)
	if len(items) != 1 || nestedString(items[0].(map[string]any), "filename") != "orphan.png" {
		t.Fatalf("unexpected orphan preview items: %#v", resp)
	}

	rejectStatus, rejectBody := env.postText(t, "/api/admin/storage/orphans/cleanup", map[string]any{
		"dry_run": true,
	})
	if rejectStatus != http.StatusBadRequest || !strings.Contains(rejectBody, "dry_run=false") {
		t.Fatalf("expected dry-run cleanup rejection: status=%d body=%q", rejectStatus, rejectBody)
	}
	rejectStatus, rejectBody = env.postText(t, "/api/admin/storage/orphans/cleanup", map[string]any{})
	if rejectStatus != http.StatusBadRequest || !strings.Contains(rejectBody, "dry_run=false") {
		t.Fatalf("expected missing dry_run cleanup rejection: status=%d body=%q", rejectStatus, rejectBody)
	}

	cleanupResp := env.postJSON(t, "/api/admin/storage/orphans/cleanup", map[string]any{
		"dry_run": false,
	}, http.StatusOK)
	result := cleanupResp["result"].(map[string]any)
	if result["dry_run"] != false || numericValue(result["deleted_count"]) != 1 || numericValue(result["deleted_bytes"]) != len(orphanContent) {
		t.Fatalf("unexpected orphan cleanup result: %#v", cleanupResp)
	}
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Fatalf("expected orphan file to be deleted, stat err=%v", err)
	}

	knownStatus, knownBody := env.getText(t, knownURL)
	if knownStatus != http.StatusOK || len(knownBody) == 0 {
		t.Fatalf("known upload should remain readable: status=%d body_len=%d", knownStatus, len(knownBody))
	}

	afterResp := env.getJSON(t, "/api/admin/storage/orphans", http.StatusOK)
	afterPreview := afterResp["preview"].(map[string]any)
	if numericValue(afterPreview["file_count"]) != 0 || numericValue(afterPreview["bytes"]) != 0 {
		t.Fatalf("expected no orphan images after cleanup: %#v", afterResp)
	}
	assertAuditCount(t, env, "admin.storage", "storage_orphans", "", "cleanup", "success", 1)
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
	var cleanupAuditCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'admin.storage'
			AND resource_type = 'storage'
			AND resource_id = 'lab-user'
			AND action = 'cleanup_preview'
			AND outcome = 'success'
	`).Scan(&cleanupAuditCount); err != nil {
		t.Fatalf("count storage cleanup audit logs: %v", err)
	}
	if cleanupAuditCount != 1 {
		t.Fatalf("expected storage cleanup audit log entry, got %d", cleanupAuditCount)
	}
}

func TestAdminStorageCleanupExecuteDeletesClosedConversations(t *testing.T) {
	env := newTestEnv(t)

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-delete-a",
		"input": "storage cleanup delete A",
	})
	firstConversation := env.waitForWaitingConversation(t, "storage cleanup delete A")
	env.postJSON(t, "/api/conversations/"+firstConversation["id"].(string)+"/respond", map[string]any{
		"text": "delete response A",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-firstCh

	secondCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-delete-b",
		"input": "storage cleanup delete B",
	})
	secondConversation := env.waitForWaitingConversation(t, "storage cleanup delete B")
	env.postJSON(t, "/api/conversations/"+secondConversation["id"].(string)+"/respond", map[string]any{
		"text": "delete response B",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-secondCh

	activeCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-active",
		"input": "storage cleanup active",
	})
	activeConversation := env.waitForWaitingConversation(t, "storage cleanup active")

	cleanupResp := env.postJSON(t, "/api/admin/storage/cleanup", map[string]any{
		"dry_run":                   false,
		"owner_id":                  "lab-user",
		"keep_recent_conversations": 0,
		"keep_recent_days":          0,
	}, http.StatusOK)
	result := cleanupResp["result"].(map[string]any)
	if cleanupResp["dry_run"] != false || result["dry_run"] != false {
		t.Fatalf("cleanup execution should not be dry-run: %#v", cleanupResp)
	}
	if numericValue(result["candidate_conversations"]) != 2 || numericValue(result["deleted_conversations"]) != 2 || numericValue(result["deleted_messages"]) < 4 {
		t.Fatalf("unexpected cleanup execution result: %#v", cleanupResp)
	}
	if _, err := env.store.GetConversation(context.Background(), firstConversation["id"].(string)); err == nil {
		t.Fatalf("expected first closed conversation to be deleted")
	}
	if _, err := env.store.GetConversation(context.Background(), secondConversation["id"].(string)); err == nil {
		t.Fatalf("expected second closed conversation to be deleted")
	}
	if _, err := env.store.GetConversation(context.Background(), activeConversation["id"].(string)); err != nil {
		t.Fatalf("active waiting conversation should remain: %v", err)
	}
	env.postJSON(t, "/api/conversations/"+activeConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup test done",
	}, http.StatusOK)
	<-activeCh
	assertAuditCount(t, env, "admin.storage", "storage", "lab-user", "cleanup", "success", 1)
}

func TestAdminStorageCleanupDeletesReferencedUploads(t *testing.T) {
	env := newTestEnv(t)

	uploadResp := env.postMultipart(t, "/api/uploads/imgs", "file", "cleanup.png", tinyPNG(), http.StatusOK)
	upload := uploadResp["upload"].(map[string]any)
	filename := nestedString(upload, "filename")
	uploadURL := nestedString(upload, "url")
	imagePath := filepath.Join(env.dataDir, "uploads", "imgs", filename)
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("expected uploaded image file before cleanup: %v", err)
	}

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-upload",
		"input": "storage cleanup upload",
	})
	conversation := env.waitForWaitingConversation(t, "storage cleanup upload")
	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "image: " + uploadURL,
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh

	cleanupResp := env.postJSON(t, "/api/admin/storage/cleanup", map[string]any{
		"dry_run":                   false,
		"owner_id":                  "lab-user",
		"keep_recent_conversations": 0,
		"keep_recent_days":          0,
	}, http.StatusOK)
	result := cleanupResp["result"].(map[string]any)
	if numericValue(result["candidate_images"]) != 1 || numericValue(result["deleted_images"]) != 1 {
		t.Fatalf("expected cleanup to delete referenced upload: %#v", cleanupResp)
	}
	if numericValue(result["candidate_image_bytes"]) != len(tinyPNG()) || numericValue(result["deleted_image_bytes"]) != len(tinyPNG()) {
		t.Fatalf("unexpected cleanup image bytes: %#v", cleanupResp)
	}
	if _, err := os.Stat(imagePath); !os.IsNotExist(err) {
		t.Fatalf("expected uploaded image file to be removed, err=%v", err)
	}
	var uploadCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM uploaded_images
		WHERE filename = ?
	`, filename).Scan(&uploadCount); err != nil {
		t.Fatalf("count uploaded image metadata: %v", err)
	}
	if uploadCount != 0 {
		t.Fatalf("expected uploaded image metadata to be deleted, got %d", uploadCount)
	}
	assertAuditCount(t, env, "admin.storage", "storage", "lab-user", "cleanup", "success", 1)
}

func TestAdminStorageCleanupKeepsUploadsReferencedByRetainedConversation(t *testing.T) {
	env := newTestEnv(t)

	uploadResp := env.postMultipart(t, "/api/uploads/imgs", "file", "shared.png", tinyPNG(), http.StatusOK)
	upload := uploadResp["upload"].(map[string]any)
	filename := nestedString(upload, "filename")
	uploadURL := nestedString(upload, "url")
	imagePath := filepath.Join(env.dataDir, "uploads", "imgs", filename)

	oldCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-shared-old",
		"input": "shared old",
	})
	oldConversation := env.waitForWaitingConversation(t, "shared old")
	env.postJSON(t, "/api/conversations/"+oldConversation["id"].(string)+"/respond", map[string]any{
		"text": "old image: " + uploadURL,
		"mode": "assistant_message",
	}, http.StatusOK)
	<-oldCh

	newCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-cleanup-shared-new",
		"input": "shared new",
	})
	newConversation := env.waitForWaitingConversation(t, "shared new")
	env.postJSON(t, "/api/conversations/"+newConversation["id"].(string)+"/respond", map[string]any{
		"text": "new image: " + uploadURL,
		"mode": "assistant_message",
	}, http.StatusOK)
	<-newCh

	cleanupResp := env.postJSON(t, "/api/admin/storage/cleanup", map[string]any{
		"dry_run":                   false,
		"owner_id":                  "lab-user",
		"keep_recent_conversations": 1,
		"keep_recent_days":          0,
	}, http.StatusOK)
	result := cleanupResp["result"].(map[string]any)
	if numericValue(result["candidate_conversations"]) != 1 || numericValue(result["candidate_images"]) != 1 || numericValue(result["deleted_images"]) != 0 {
		t.Fatalf("expected shared upload to remain while old conversation is cleaned: %#v", cleanupResp)
	}
	if _, err := env.store.GetConversation(context.Background(), oldConversation["id"].(string)); err == nil {
		t.Fatalf("expected old conversation to be deleted")
	}
	if _, err := env.store.GetConversation(context.Background(), newConversation["id"].(string)); err != nil {
		t.Fatalf("expected retained conversation to remain: %v", err)
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("expected shared upload to remain: %v", err)
	}
	var uploadCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM uploaded_images
		WHERE filename = ?
	`, filename).Scan(&uploadCount); err != nil {
		t.Fatalf("count uploaded image metadata: %v", err)
	}
	if uploadCount != 1 {
		t.Fatalf("expected shared upload metadata to remain, got %d", uploadCount)
	}
}

func TestStorageFileDeletionFailureRetry(t *testing.T) {
	env := newTestEnv(t)

	uploadsDir := filepath.Join(env.dataDir, "uploads", "imgs")
	if err := os.MkdirAll(uploadsDir, 0o755); err != nil {
		t.Fatalf("create uploads dir: %v", err)
	}
	path := filepath.Join(uploadsDir, "retry.png")
	if err := os.WriteFile(path, tinyPNG(), 0o644); err != nil {
		t.Fatalf("write retry image: %v", err)
	}
	if _, err := env.store.CreateUploadedImage(context.Background(), store.CreateUploadedImageInput{
		ID:               "img_retry",
		OwnerID:          "lab-user",
		Filename:         "retry.png",
		OriginalFilename: "retry.png",
		ContentType:      "image/png",
		Bytes:            int64(len(tinyPNG())),
		URL:              "/api/uploads/imgs/retry.png",
	}); err != nil {
		t.Fatalf("seed uploaded image metadata: %v", err)
	}
	if _, err := env.store.UpsertStorageFileDeletionFailure(context.Background(), store.UpsertStorageFileDeletionFailureInput{
		Path:      path,
		Filename:  "retry.png",
		OwnerID:   "lab-user",
		Bytes:     int64(len(tinyPNG())),
		LastError: "initial failure",
	}); err != nil {
		t.Fatalf("seed deletion failure: %v", err)
	}

	monitor := service.NewStorageMonitorService(config.Config{
		DataDir:        env.dataDir,
		DatabaseDriver: "sqlite",
		DatabaseDSN:    filepath.Join(env.dataDir, "chatapi.sqlite3"),
	}, env.store)
	result, err := monitor.RetryFileDeletionFailures(context.Background(), 10)
	if err != nil {
		t.Fatalf("retry deletion failures: %v", err)
	}
	if result.Scanned != 1 || result.Deleted != 1 || result.Failed != 0 {
		t.Fatalf("unexpected retry result: %#v", result)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected retry file to be removed, err=%v", err)
	}
	items, err := env.store.ListStorageFileDeletionFailures(context.Background(), 10)
	if err != nil {
		t.Fatalf("list deletion failures: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected deletion failure queue to be empty: %#v", items)
	}
	var uploadCount int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM uploaded_images
		WHERE filename = 'retry.png'
	`).Scan(&uploadCount); err != nil {
		t.Fatalf("count retried uploaded image metadata: %v", err)
	}
	if uploadCount != 0 {
		t.Fatalf("expected retried upload metadata to be deleted, got %d", uploadCount)
	}
}

func TestStoragePruneOverQuotaUsersKeepsRecentConversation(t *testing.T) {
	env := newTestEnv(t)

	oldCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-quota-prune-old",
		"input": "quota prune old",
	})
	oldConversation := env.waitForWaitingConversation(t, "quota prune old")
	env.postJSON(t, "/api/conversations/"+oldConversation["id"].(string)+"/respond", map[string]any{
		"text": "old response with enough bytes",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-oldCh

	newCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "storage-quota-prune-new",
		"input": "quota prune new",
	})
	newConversation := env.waitForWaitingConversation(t, "quota prune new")
	env.postJSON(t, "/api/conversations/"+newConversation["id"].(string)+"/respond", map[string]any{
		"text": "new response with enough bytes",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-newCh

	monitor := service.NewStorageMonitorService(config.Config{
		DataDir:        env.dataDir,
		DatabaseDriver: "sqlite",
		DatabaseDSN:    filepath.Join(env.dataDir, "chatapi.sqlite3"),
	}, env.store)
	if _, err := monitor.SetUserQuota(context.Background(), "lab-user", 1); err != nil {
		t.Fatalf("set quota: %v", err)
	}

	result, err := monitor.PruneOverQuotaUsers(context.Background(), service.StorageCleanupPreviewInput{
		KeepRecentConversations: 1,
		KeepRecentDays:          0,
	})
	if err != nil {
		t.Fatalf("prune over quota users: %v", err)
	}
	if result.CheckedUsers != 1 || result.OverQuota != 1 || result.PrunedUsers != 1 || len(result.Results) != 1 {
		t.Fatalf("unexpected quota prune result: %#v", result)
	}
	if result.Results[0].OwnerID != "lab-user" || result.Results[0].DeletedConversations != 1 {
		t.Fatalf("unexpected quota cleanup detail: %#v", result.Results[0])
	}
	if _, err := env.store.GetConversation(context.Background(), oldConversation["id"].(string)); err == nil {
		t.Fatalf("expected old conversation to be pruned")
	}
	if _, err := env.store.GetConversation(context.Background(), newConversation["id"].(string)); err != nil {
		t.Fatalf("expected recent conversation to remain: %v", err)
	}
}

func TestAdminStorageCleanupRejectsMissingDryRun(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.postText(t, "/api/admin/storage/cleanup", map[string]any{
		"keep_recent_conversations": 1,
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "requires explicit dry_run") {
		t.Fatalf("expected missing dry_run rejection: status=%d body=%q", status, body)
	}
}

func TestAdminStorageVacuum(t *testing.T) {
	env := newTestEnv(t)

	dryRunResp := env.postJSON(t, "/api/admin/storage/vacuum", map[string]any{
		"dry_run": true,
	}, http.StatusOK)
	dryRunResult := dryRunResp["result"].(map[string]any)
	if dryRunResp["dry_run"] != true || dryRunResult["after"] != nil {
		t.Fatalf("unexpected vacuum dry-run response: %#v", dryRunResp)
	}

	status, body := env.postText(t, "/api/admin/storage/vacuum", map[string]any{})
	if status != http.StatusBadRequest || !strings.Contains(body, "requires explicit dry_run") {
		t.Fatalf("expected missing dry_run vacuum rejection: status=%d body=%q", status, body)
	}

	resultResp := env.postJSON(t, "/api/admin/storage/vacuum", map[string]any{
		"dry_run": false,
	}, http.StatusOK)
	result := resultResp["result"].(map[string]any)
	if resultResp["dry_run"] != false || result["before"] == nil || result["after"] == nil {
		t.Fatalf("unexpected vacuum response: %#v", resultResp)
	}
	assertAuditCount(t, env, "admin.storage", "storage", "", "vacuum_preview", "success", 1)
	assertAuditCount(t, env, "admin.storage", "storage", "", "vacuum", "success", 1)

	var rawMetadata string
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT metadata_json
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'admin.storage'
			AND resource_type = 'storage'
			AND action = 'vacuum'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`).Scan(&rawMetadata); err != nil {
		t.Fatalf("select vacuum audit metadata: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(rawMetadata), &metadata); err != nil {
		t.Fatalf("decode vacuum audit metadata: %v", err)
	}
	if nestedString(metadata, "database_driver") != "sqlite" {
		t.Fatalf("unexpected vacuum audit metadata: %#v", metadata)
	}
	if _, ok := metadata["before"]; !ok {
		t.Fatalf("expected vacuum audit metadata to include before snapshot: %#v", metadata)
	}
	if _, ok := metadata["after"]; !ok {
		t.Fatalf("expected vacuum audit metadata to include after snapshot: %#v", metadata)
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

	status, body = env.getTextWithHeaders(t, "/api/admin/storage/orphans", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage orphan rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPostText(t, "/api/admin/storage/orphans/cleanup", appKey, map[string]any{
		"dry_run": false,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage orphan cleanup rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPostText(t, "/api/admin/storage/cleanup", appKey, map[string]any{
		"dry_run": true,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage cleanup rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPostText(t, "/api/admin/storage/vacuum", appKey, map[string]any{
		"dry_run": true,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage vacuum rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPutText(t, "/api/admin/storage/users/lab-user/quota", appKey, map[string]any{
		"quota_bytes": 1024,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage quota update rejection: status=%d body=%q", status, body)
	}

	status, body = env.appDeleteText(t, "/api/admin/storage/users/lab-user/quota", appKey)
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage quota delete rejection: status=%d body=%q", status, body)
	}
}

func TestAdminAuditLogsEndpoint(t *testing.T) {
	env := newTestEnv(t)

	env.postMultipart(t, "/api/uploads/imgs", "file", "audit.png", tinyPNG(), http.StatusOK)
	env.postJSON(t, "/api/admin/runtime/gc", map[string]any{}, http.StatusOK)

	allResp := env.getJSON(t, "/api/admin/audit/logs?limit=10", http.StatusOK)
	items := allResp["items"].([]any)
	if numericValue(allResp["count"]) != len(items) || len(items) < 2 {
		t.Fatalf("unexpected audit logs response: %#v", allResp)
	}

	uploadResp := env.getJSON(t, "/api/admin/audit/logs?event_type=upload&actor_user_id=lab-user", http.StatusOK)
	uploadItems := uploadResp["items"].([]any)
	if len(uploadItems) != 1 {
		t.Fatalf("expected one upload audit log: %#v", uploadResp)
	}
	upload := uploadItems[0].(map[string]any)
	if nestedString(upload, "event_type") != "upload" || nestedString(upload, "action") != "create" || nestedString(upload, "outcome") != "success" {
		t.Fatalf("unexpected upload audit log: %#v", upload)
	}
	metadata := upload["metadata"].(map[string]any)
	if nestedString(metadata, "content_type") != "image/png" || numericValue(metadata["bytes"]) != len(tinyPNG()) {
		t.Fatalf("unexpected upload audit metadata: %#v", upload)
	}

	runtimeResp := env.getJSON(t, "/api/admin/audit/logs?event_type=admin.runtime&limit=1", http.StatusOK)
	runtimeItems := runtimeResp["items"].([]any)
	if len(runtimeItems) != 1 || nestedString(runtimeItems[0].(map[string]any), "action") != "gc" {
		t.Fatalf("expected runtime gc audit log: %#v", runtimeResp)
	}
}

func TestAdminAuditLogsRejectsAPIKeys(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)

	status, body := env.getTextWithHeaders(t, "/api/admin/audit/logs", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key audit admin rejection: status=%d body=%q", status, body)
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
	if err := env.rawDB.QueryRow(`SELECT COUNT(*) FROM app_api_key_audit_logs`).Scan(&count); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected audit log entry to be written")
	}

	defaultAuditResp := env.getJSON(t, "/api/admin/audit/logs?event_type=app_api.request&limit=10", http.StatusOK)
	if len(defaultAuditResp["items"].([]any)) != 0 {
		t.Fatalf("app api audit should be excluded by default: %#v", defaultAuditResp)
	}

	combinedResp := env.getJSON(t, "/api/admin/audit/logs?include_app_api=1&event_type=app_api.request&limit=10", http.StatusOK)
	items := combinedResp["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected app api audit in combined admin view: %#v", combinedResp)
	}
	item := items[0].(map[string]any)
	if nestedString(item, "event_type") != "app_api.request" ||
		nestedString(item, "actor_user_id") != "lab-user" ||
		nestedString(item, "actor_source") != "app_api_key" ||
		nestedString(item, "resource_type") != "app_api_key" ||
		nestedString(item, "action") != "request" ||
		nestedString(item, "outcome") != "success" {
		t.Fatalf("unexpected combined app api audit item: %#v", item)
	}
	metadata := item["metadata"].(map[string]any)
	if nestedString(metadata, "route") != "/api/app/me" || numericValue(metadata["status_code"]) != http.StatusOK {
		t.Fatalf("unexpected combined app api audit metadata: %#v", item)
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
	if err := env.rawDB.QueryRow(`SELECT COUNT(*) FROM app_api_key_audit_logs WHERE status_code = 403 AND error_code = 'forbidden'`).Scan(&forbiddenCount); err != nil {
		t.Fatalf("count forbidden audit logs: %v", err)
	}
	if forbiddenCount == 0 {
		t.Fatalf("expected forbidden audit log entry")
	}

	combinedResp := env.getJSON(t, "/api/admin/audit/logs?include_app_api=1&event_type=app_api.request&actor_user_id=lab-user&limit=10", http.StatusOK)
	items := combinedResp["items"].([]any)
	foundForbidden := false
	for _, rawItem := range items {
		item := rawItem.(map[string]any)
		if nestedString(item, "outcome") != "failure" {
			continue
		}
		metadata := item["metadata"].(map[string]any)
		if numericValue(metadata["status_code"]) == http.StatusForbidden && nestedString(metadata, "error_code") == "forbidden" {
			foundForbidden = true
			break
		}
	}
	if !foundForbidden {
		t.Fatalf("expected forbidden app api audit in combined admin view: %#v", combinedResp)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestAppAPIRateLimit(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, map[string]any{
		"max_requests_per_minute": 1,
	})

	env.appGetJSON(t, "/api/app/me", appKey, http.StatusOK)
	status, body := env.appGetText(t, "/api/app/me", appKey)
	if status != http.StatusTooManyRequests || !strings.Contains(body, "rate limited") {
		t.Fatalf("expected app api rate limit: status=%d body=%q", status, body)
	}

	var rateLimitedCount int
	if err := env.rawDB.QueryRow(`
		SELECT COUNT(*)
		FROM app_api_key_audit_logs
		WHERE status_code = 429
			AND error_code = 'rate_limited'
	`).Scan(&rateLimitedCount); err != nil {
		t.Fatalf("count rate limited audit logs: %v", err)
	}
	if rateLimitedCount != 1 {
		t.Fatalf("expected one rate limited audit log, got %d", rateLimitedCount)
	}

	combinedResp := env.getJSON(t, "/api/admin/audit/logs?include_app_api=1&event_type=app_api.request&actor_user_id=lab-user&limit=10", http.StatusOK)
	items := combinedResp["items"].([]any)
	foundRateLimited := false
	for _, rawItem := range items {
		item := rawItem.(map[string]any)
		metadata := item["metadata"].(map[string]any)
		if nestedString(item, "outcome") == "failure" &&
			numericValue(metadata["status_code"]) == http.StatusTooManyRequests &&
			nestedString(metadata, "error_code") == "rate_limited" {
			foundRateLimited = true
			break
		}
	}
	if !foundRateLimited {
		t.Fatalf("expected rate limited audit in combined admin view: %#v", combinedResp)
	}
}

func TestAppAPISourceIPLimit(t *testing.T) {
	env := newTestEnv(t)
	allowedKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, map[string]any{
		"allowed_source_ips": []string{"127.0.0.0/8"},
	})
	allowedResp := env.appGetJSON(t, "/api/app/me", allowedKey, http.StatusOK)
	if nestedPathString(allowedResp, "user", "id") != "lab-user" {
		t.Fatalf("unexpected allowed source ip response: %#v", allowedResp)
	}

	blockedKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, map[string]any{
		"allowed_source_ips": []string{"203.0.113.1"},
	})
	status, body := env.appGetText(t, "/api/app/me", blockedKey)
	if status != http.StatusForbidden || !strings.Contains(body, "source ip forbidden") {
		t.Fatalf("expected source ip rejection: status=%d body=%q", status, body)
	}

	combinedResp := env.getJSON(t, "/api/admin/audit/logs?include_app_api=1&event_type=app_api.request&actor_user_id=lab-user&limit=20", http.StatusOK)
	items := combinedResp["items"].([]any)
	foundSourceIPFailure := false
	for _, rawItem := range items {
		item := rawItem.(map[string]any)
		metadata := item["metadata"].(map[string]any)
		if nestedString(item, "outcome") == "failure" &&
			numericValue(metadata["status_code"]) == http.StatusForbidden &&
			nestedString(metadata, "error_code") == "source_ip_forbidden" {
			foundSourceIPFailure = true
			break
		}
	}
	if !foundSourceIPFailure {
		t.Fatalf("expected source ip audit in combined admin view: %#v", combinedResp)
	}
}

func TestAppAPISourceIPLimitUsesTrustedForwardedFor(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.TrustedProxies = []string{"127.0.0.0/8"}
	})
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, map[string]any{
		"allowed_source_ips": []string{"203.0.113.8"},
	})
	status, body := env.appGetTextWithHeaders(t, "/api/app/me", appKey, map[string]string{
		"X-Forwarded-For": "203.0.113.8, 127.0.0.1",
	})
	if status != http.StatusOK {
		t.Fatalf("expected trusted forwarded source ip to pass: status=%d body=%q", status, body)
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

func TestResponsesDeltaAndCompleteWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-pg-responses",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "pg responses 顺序测试"},
				},
			},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg responses 顺序测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversationID,
		"text":            "PG 草稿输出",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"mode":            "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "object"); got != "response" {
		t.Fatalf("unexpected responses object: %q", got)
	}
	if got := nestedString(finalResp, "output_text"); got != "PG 草稿输出" {
		t.Fatalf("unexpected responses output_text: %#v", finalResp)
	}

	conversation, err := env.store.GetConversation(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("get postgres conversation: %v", err)
	}
	if nestedString(conversation.Metadata, "realtime_status") != "closed" {
		t.Fatalf("expected closed postgres conversation: %#v", conversation)
	}
	messages, err := env.store.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("list postgres messages: %v", err)
	}
	if len(messages) < 2 || messages[len(messages)-1].Content != "PG 草稿输出" {
		t.Fatalf("unexpected postgres persisted messages: %#v", messages)
	}
}

func TestChatCompletionsProtocolShapeWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model": "demo-pg-chat",
		"messages": []map[string]any{
			{"role": "user", "content": "pg chat completions 测试"},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg chat completions 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"text":            "pg chat completions 回复",
		"mode":            "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "object"); got != "chat.completion" {
		t.Fatalf("unexpected chat completion object: %q", got)
	}
	if got := nestedString(finalResp, "model"); got != "demo-pg-chat" {
		t.Fatalf("unexpected chat completion model: %q", got)
	}
	choices, ok := finalResp["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("unexpected chat completion choices: %#v", finalResp["choices"])
	}
	message := choices[0].(map[string]any)["message"].(map[string]any)
	if got := nestedString(message, "content"); got != "pg chat completions 回复" {
		t.Fatalf("unexpected chat completion message: %#v", message)
	}

	requestID := env.requestIDForConversation(t, conversationID)
	requestResp := env.getJSON(t, "/lab/requests/"+requestID, http.StatusOK)
	if nestedPathString(requestResp, "request", "request_format") != "chat_completions" {
		t.Fatalf("unexpected postgres request format: %#v", requestResp)
	}
}

func TestAnthropicMessagesProtocolShapeWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/messages", map[string]any{
		"model": "claude-pg-demo",
		"messages": []map[string]any{
			{"role": "user", "content": "pg anthropic messages 测试"},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg anthropic messages 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"text":            "pg anthropic 回复",
		"mode":            "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "type"); got != "message" {
		t.Fatalf("unexpected anthropic type: %q", got)
	}
	if got := nestedString(finalResp, "role"); got != "assistant" {
		t.Fatalf("unexpected anthropic role: %q", got)
	}
	content, ok := finalResp["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("unexpected anthropic content: %#v", finalResp["content"])
	}
	firstPart := content[0].(map[string]any)
	if got := nestedString(firstPart, "text"); got != "pg anthropic 回复" {
		t.Fatalf("unexpected anthropic text: %#v", firstPart)
	}

	messages, err := env.store.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("list postgres anthropic messages: %v", err)
	}
	if len(messages) < 2 {
		t.Fatalf("expected persisted postgres anthropic messages: %#v", messages)
	}
	last := messages[len(messages)-1]
	if last.Content != "pg anthropic 回复" {
		t.Fatalf("unexpected postgres anthropic persisted content: %#v", last)
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

func TestResponsesThinkingModePersistsThinkBlockWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-pg-thinking",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "pg thinking 测试"},
				},
			},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg thinking 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id":       conversationID,
		"text":                  "PG 内部思考内容",
		"mode":                  "thinking",
		"reasoning_stream_mode": "reasoning",
	}, http.StatusOK)

	finalResp := <-resultCh
	if got := nestedString(finalResp, "output_text"); got != "<think>PG 内部思考内容</think>" {
		t.Fatalf("unexpected postgres responses thinking output_text: %#v", finalResp)
	}

	messages, err := env.store.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("list postgres thinking messages: %v", err)
	}
	lastMessage := messages[len(messages)-1]
	if lastMessage.Content != "<think>PG 内部思考内容</think>" {
		t.Fatalf("unexpected postgres thinking message content: %#v", lastMessage)
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

func TestChatCompletionsToolCallShapeWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model": "demo-pg-tool-call",
		"messages": []map[string]any{
			{"role": "user", "content": "pg tool call 测试"},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg tool call 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"text":            "{\"city\":\"Shanghai\"}",
		"mode":            "tool_call",
		"tool_name":       "get_weather",
		"tool_call_id":    "call_pg_test_1",
	}, http.StatusOK)

	finalResp := <-resultCh
	choices := finalResp["choices"].([]any)
	message := choices[0].(map[string]any)["message"].(map[string]any)
	toolCalls := message["tool_calls"].([]any)
	firstToolCall := toolCalls[0].(map[string]any)
	if nestedString(firstToolCall, "id") != "call_pg_test_1" {
		t.Fatalf("unexpected postgres tool_call id: %#v", firstToolCall)
	}
	functionPart := firstToolCall["function"].(map[string]any)
	if nestedString(functionPart, "name") != "get_weather" || nestedString(functionPart, "arguments") != "{\"city\":\"Shanghai\"}" {
		t.Fatalf("unexpected postgres function payload: %#v", functionPart)
	}

	messages, err := env.store.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("list postgres tool_call messages: %v", err)
	}
	lastMessage := messages[len(messages)-1]
	if nestedString(lastMessage.Metadata, "response_mode") != "tool_call" || nestedString(lastMessage.Metadata, "tool_name") != "get_weather" {
		t.Fatalf("unexpected postgres tool call message metadata: %#v", lastMessage.Metadata)
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

func TestResponsesToolResultShapeWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-pg-tool-result",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "pg tool result 测试"},
				},
			},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg tool result 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"text":            "{\"ok\":true}",
		"output":          "{\"ok\":true}",
		"mode":            "tool_result",
		"tool_call_id":    "call_pg_result_1",
	}, http.StatusOK)

	finalResp := <-resultCh
	output := finalResp["output"].([]any)
	firstOutput := output[0].(map[string]any)
	if nestedString(firstOutput, "type") != "function_call_output" || nestedString(firstOutput, "call_id") != "call_pg_result_1" {
		t.Fatalf("unexpected postgres tool result output payload: %#v", firstOutput)
	}
	if nestedString(firstOutput, "output") != "{\"ok\":true}" {
		t.Fatalf("unexpected postgres tool result output body: %#v", firstOutput)
	}

	messages, err := env.store.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("list postgres tool result messages: %v", err)
	}
	lastMessage := messages[len(messages)-1]
	if nestedString(lastMessage.Metadata, "response_mode") != "tool_result" || nestedString(lastMessage.Metadata, "output") != "{\"ok\":true}" {
		t.Fatalf("unexpected postgres tool result metadata: %#v", lastMessage.Metadata)
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

func TestResponsesSSEStreamWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	streamCh := startTextRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model":  "demo-pg-stream",
		"stream": true,
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "pg responses sse 测试"},
				},
			},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg responses sse 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversationID,
		"text":            "PG 第一段",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"mode":            "assistant_message",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "event: response.created") {
		t.Fatalf("missing response.created event: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"delta\":\"PG 第一段\"") {
		t.Fatalf("missing postgres delta payload: %s", streamBody)
	}
	if !strings.Contains(streamBody, "event: response.completed") || !strings.Contains(streamBody, "\"output_text\":\"PG 第一段\"") {
		t.Fatalf("missing postgres completed payload: %s", streamBody)
	}
}

func TestChatCompletionsSSEStreamWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	streamCh := startTextRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model":  "demo-pg-chat-stream",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "pg chat stream 测试"},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg chat stream 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversationID,
		"text":            "PG 流式回复",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"mode":            "assistant_message",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "\"object\":\"chat.completion.chunk\"") {
		t.Fatalf("missing postgres chat completion chunk: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"content\":\"PG 流式回复\"") {
		t.Fatalf("missing postgres chat completion delta content: %s", streamBody)
	}
	if !strings.Contains(streamBody, "data: [DONE]") {
		t.Fatalf("missing postgres done marker: %s", streamBody)
	}
}

func TestAnthropicMessagesSSEStreamWithPostgreSQL(t *testing.T) {
	env := newPostgresTestEnvWithConfig(t, config.ModeLab, nil)

	streamCh := startTextRequest(t, env.server.URL+"/messages", map[string]any{
		"model":  "claude-pg-stream-demo",
		"stream": true,
		"messages": []map[string]any{
			{"role": "user", "content": "pg anthropic stream 测试"},
		},
	})

	conversationView := env.waitForWaitingConversation(t, "pg anthropic stream 测试")
	conversationID := conversationView["id"].(string)
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversationID,
		"text":            "PG Anthropic 流式回复",
	}, http.StatusOK)
	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversationID,
		"mode":            "assistant_message",
	}, http.StatusOK)

	streamBody := <-streamCh
	if !strings.Contains(streamBody, "event: message_start") {
		t.Fatalf("missing postgres anthropic message_start: %s", streamBody)
	}
	if !strings.Contains(streamBody, "event: content_block_start") || !strings.Contains(streamBody, "\"type\":\"text\"") {
		t.Fatalf("missing postgres anthropic text block start: %s", streamBody)
	}
	if !strings.Contains(streamBody, "event: content_block_delta") || !strings.Contains(streamBody, "\"text\":\"PG Anthropic 流式回复\"") {
		t.Fatalf("missing postgres anthropic delta: %s", streamBody)
	}
	if !strings.Contains(streamBody, "\"stop_reason\":\"end_turn\"") || !strings.Contains(streamBody, "event: message_stop") {
		t.Fatalf("missing postgres anthropic completion markers: %s", streamBody)
	}
}

type testEnv struct {
	server          *httptest.Server
	client          *http.Client
	store           store.Store
	sqliteStore     *sqlitestore.Store
	rawDB           *sql.DB
	chatService     *service.ChatAPIService
	pendingRegistry *service.PendingRegistry
	appKeyService   *service.AppAPIKeyService
	modelKeyService *service.ModelAPIKeyService
	dataDir         string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithMode(t, config.ModeLab)
}

func newTestEnvWithMode(t *testing.T, mode config.Mode) *testEnv {
	t.Helper()
	return newTestEnvWithConfig(t, mode, nil)
}

func newTestEnvWithConfig(t *testing.T, mode config.Mode, mutate func(*config.Config)) *testEnv {
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
		SessionSecret:  "test-session-secret",
		LogLevel:       "error",
		CORSOrigins:    []string{"http://localhost"},
	}
	if mutate != nil {
		mutate(&cfg)
	}

	sqliteStore, err := sqlitestore.Open(cfg.DatabaseDSN)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := migrations.Bootstrap(context.Background(), sqliteStore.DB()); err != nil {
		t.Fatalf("bootstrap migrations: %v", err)
	}

	pendingRegistry := service.NewPendingRegistry()
	realtimeHub := service.NewRealtimeHub(sqliteStore)
	chatService := service.NewChatAPIService(sqliteStore, pendingRegistry, realtimeHub)
	appKeyService := service.NewAppAPIKeyService(sqliteStore)
	modelKeyService := service.NewModelAPIKeyService(sqliteStore, cfg.MasterKey)

	server := httptest.NewServer(httpapi.NewRouter(cfg, sqliteStore, chatService, realtimeHub, pendingRegistry))
	t.Cleanup(server.Close)
	t.Cleanup(func() { _ = sqliteStore.Close() })

	return &testEnv{
		server:          server,
		client:          server.Client(),
		store:           sqliteStore,
		sqliteStore:     sqliteStore,
		rawDB:           sqliteStore.DB(),
		chatService:     chatService,
		pendingRegistry: pendingRegistry,
		appKeyService:   appKeyService,
		modelKeyService: modelKeyService,
		dataDir:         cfg.DataDir,
	}
}

func newPostgresTestEnvWithConfig(t *testing.T, mode config.Mode, mutate func(*config.Config)) *testEnv {
	t.Helper()

	dsn := pgtest.IsolatedDSN(t)

	tempDir := t.TempDir()
	cfg := config.Config{
		Mode:           mode,
		Host:           "127.0.0.1",
		Port:           0,
		WebDistDir:     tempDir,
		DataDir:        tempDir,
		DatabaseDriver: "postgresql",
		DatabaseDSN:    dsn,
		AllowRemoteLab: false,
		OpenBrowser:    false,
		MasterKey:      "test-master-key",
		SessionSecret:  "test-session-secret",
		LogLevel:       "error",
		CORSOrigins:    []string{"http://localhost"},
	}
	if mutate != nil {
		mutate(&cfg)
	}

	pgStore, err := pgstore.Open(context.Background(), cfg.DatabaseDSN)
	if err != nil {
		t.Fatalf("open postgresql: %v", err)
	}
	t.Cleanup(pgStore.Close)
	if err := pgstore.Reset(context.Background(), pgStore.Pool()); err != nil {
		t.Fatalf("reset postgresql: %v", err)
	}
	if err := pgstore.Bootstrap(context.Background(), pgStore.Pool()); err != nil {
		t.Fatalf("bootstrap postgresql: %v", err)
	}

	pendingRegistry := service.NewPendingRegistry()
	realtimeHub := service.NewRealtimeHub(pgStore)
	chatService := service.NewChatAPIService(pgStore, pendingRegistry, realtimeHub)
	appKeyService := service.NewAppAPIKeyService(pgStore)
	modelKeyService := service.NewModelAPIKeyService(pgStore, cfg.MasterKey)

	server := httptest.NewServer(httpapi.NewRouter(cfg, pgStore, chatService, realtimeHub, pendingRegistry))
	t.Cleanup(server.Close)

	return &testEnv{
		server:          server,
		client:          server.Client(),
		store:           pgStore,
		sqliteStore:     nil,
		rawDB:           nil,
		chatService:     chatService,
		pendingRegistry: pendingRegistry,
		appKeyService:   appKeyService,
		modelKeyService: modelKeyService,
		dataDir:         cfg.DataDir,
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

func (e *testEnv) postJSONWithCookies(t *testing.T, path string, body map[string]any, wantStatus int) (map[string]any, []*http.Cookie) {
	t.Helper()
	return e.postJSONWithCookie(t, path, body, nil, wantStatus)
}

func (e *testEnv) postJSONWithCookie(t *testing.T, path string, body map[string]any, cookie *http.Cookie, wantStatus int) (map[string]any, []*http.Cookie) {
	t.Helper()
	return e.postJSONWithCookieAndHeaders(t, path, body, cookie, nil, wantStatus)
}

func (e *testEnv) postJSONWithCookieAndHeaders(t *testing.T, path string, body map[string]any, cookie *http.Cookie, headers map[string]string, wantStatus int) (map[string]any, []*http.Cookie) {
	t.Helper()
	status, rawBody, cookies := e.postJSONWithCookieText(t, path, body, cookie, headers)
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawBody), &payload); err != nil {
		t.Fatalf("decode response %s: %v body=%q", path, err, rawBody)
	}
	if status != wantStatus {
		t.Fatalf("unexpected status for %s: got %d want %d payload=%#v", path, status, wantStatus, payload)
	}
	return payload, cookies
}

func (e *testEnv) postJSONWithCookieText(t *testing.T, path string, body map[string]any, cookie *http.Cookie, headers map[string]string) (int, string, []*http.Cookie) {
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
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do request %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data), resp.Cookies()
}

func (e *testEnv) getJSONWithCookie(t *testing.T, path string, cookie *http.Cookie, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do get %s: %v", path, err)
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

func (e *testEnv) postMultipart(t *testing.T, path string, field string, filename string, data []byte, wantStatus int) map[string]any {
	t.Helper()
	status, body := e.postMultipartText(t, path, field, filename, data)
	if status != wantStatus {
		t.Fatalf("unexpected multipart status for %s: got %d want %d body=%q", path, status, wantStatus, body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode multipart response %s: %v body=%q", path, err, body)
	}
	return payload
}

func (e *testEnv) postMultipartText(t *testing.T, path string, field string, filename string, data []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, e.server.URL+path, &body)
	if err != nil {
		t.Fatalf("new multipart request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do multipart request %s: %v", path, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read multipart response %s: %v", path, err)
	}
	return resp.StatusCode, string(responseBody)
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

func (e *testEnv) putJSON(t *testing.T, path string, body map[string]any, wantStatus int) map[string]any {
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

func (e *testEnv) putJSONWithCookieAndHeaders(t *testing.T, path string, body map[string]any, cookie *http.Cookie, headers map[string]string, wantStatus int) map[string]any {
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
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
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

func (e *testEnv) putText(t *testing.T, path string, body map[string]any) (int, string) {
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

func (e *testEnv) deleteTextWithCookieAndHeaders(t *testing.T, path string, cookie *http.Cookie, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
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

func (e *testEnv) deleteJSON(t *testing.T, path string, wantStatus int) map[string]any {
	t.Helper()
	status, body := e.deleteText(t, path)
	if status != wantStatus {
		t.Fatalf("unexpected delete status for %s: got %d want %d body=%q", path, status, wantStatus, body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode delete response %s: %v body=%q", path, err, body)
	}
	return payload
}

func (e *testEnv) deleteJSONWithCookieAndHeaders(t *testing.T, path string, cookie *http.Cookie, headers map[string]string, wantStatus int) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do delete %s: %v", path, err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode delete response %s: %v", path, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("unexpected delete status for %s: got %d want %d payload=%#v", path, resp.StatusCode, wantStatus, payload)
	}
	return payload
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
	return e.appGetTextWithHeaders(t, path, appKey, nil)
}

func (e *testEnv) appGetTextWithHeaders(t *testing.T, path string, appKey string, headers map[string]string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+appKey)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
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
	_, raw, err := e.appKeyService.CreateKey(context.Background(), userID, "test-key", scopes, resourceLimits, nil)
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

func assertAuditCount(t *testing.T, env *testEnv, eventType string, resourceType string, resourceID string, action string, outcome string, want int) {
	t.Helper()
	assertAuditCountForActor(t, env, "lab-user", eventType, resourceType, resourceID, action, outcome, want)
}

func assertAuditCountForActor(t *testing.T, env *testEnv, actorUserID string, eventType string, resourceType string, resourceID string, action string, outcome string, want int) {
	t.Helper()
	var count int
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM audit_logs
		WHERE actor_user_id = ?
			AND event_type = ?
			AND resource_type = ?
			AND resource_id = ?
			AND action = ?
			AND outcome = ?
	`, actorUserID, eventType, resourceType, resourceID, action, outcome).Scan(&count); err != nil {
		t.Fatalf("count audit logs: %v", err)
	}
	if count != want {
		t.Fatalf("expected %s/%s audit count %d, got %d", eventType, action, want, count)
	}
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

func (e *testEnv) getRedirect(t *testing.T, path string) (int, string, []*http.Cookie) {
	t.Helper()
	client := *e.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Get(e.server.URL + path)
	if err != nil {
		t.Fatalf("get redirect %s: %v", path, err)
	}
	defer resp.Body.Close()
	location := ""
	if target, err := resp.Location(); err == nil {
		location = target.String()
	}
	return resp.StatusCode, location, resp.Cookies()
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

func tinyPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
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

func nestedPathBool(record map[string]any, path ...string) bool {
	var current any = record
	for _, key := range path {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return false
		}
		current = nextMap[key]
	}
	value, _ := current.(bool)
	return value
}

func responseItemsContainID(record map[string]any, id string) bool {
	items, _ := record["items"].([]any)
	for _, item := range items {
		asMap, _ := item.(map[string]any)
		if nestedString(asMap, "id") == id {
			return true
		}
	}
	return false
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

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name {
			return cookie
		}
	}
	return nil
}
