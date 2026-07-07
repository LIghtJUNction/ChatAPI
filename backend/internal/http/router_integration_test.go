package httpapi_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gorilla/websocket"
	"github.com/pquerna/otp/totp"
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

func TestConversationControlSchemaEndpoints(t *testing.T) {
	env := newTestEnv(t)

	conversationsResp := env.getJSON(t, "/api/conversations/schema", http.StatusOK)
	conversationSchema := conversationsResp["schema"].(map[string]any)
	conversationOperations := conversationSchema["operations"].([]any)
	if len(conversationOperations) != 7 {
		t.Fatalf("unexpected conversation schema response: %#v", conversationsResp)
	}
	if nestedString(conversationOperations[1].(map[string]any), "name") != "respond_conversation" ||
		nestedString(conversationOperations[5].(map[string]any), "name") != "legacy_output_delta" {
		t.Fatalf("unexpected conversation schema operations: %#v", conversationsResp)
	}

	legacyResp := env.getJSON(t, "/api/chat/output/schema", http.StatusOK)
	legacySchema := legacyResp["schema"].(map[string]any)
	legacyOperations := legacySchema["operations"].([]any)
	if len(legacyOperations) != len(conversationOperations) {
		t.Fatalf("unexpected legacy output schema response: %#v", legacyResp)
	}
	if nestedString(legacyOperations[6].(map[string]any), "name") != "legacy_output_complete" {
		t.Fatalf("unexpected legacy output schema operations: %#v", legacyResp)
	}
}

func TestProtocolRequestValidationReturnsProtocolSpecificErrors(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name         string
		path         string
		body         map[string]any
		checkMessage func(t *testing.T, payload map[string]any)
	}{
		{
			name: "responses",
			path: "/v1/responses",
			body: map[string]any{"model": "demo-empty"},
			checkMessage: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if nestedPathString(payload, "error", "param") != "input" {
					t.Fatalf("unexpected responses validation payload: %#v", payload)
				}
			},
		},
		{
			name: "chat_completions",
			path: "/v1/chat/completions",
			body: map[string]any{"model": "demo-empty", "messages": []any{}},
			checkMessage: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if nestedPathString(payload, "error", "param") != "messages" {
					t.Fatalf("unexpected chat validation payload: %#v", payload)
				}
			},
		},
		{
			name: "anthropic_messages",
			path: "/v1/messages",
			body: map[string]any{"model": "demo-empty", "messages": []any{}},
			checkMessage: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if nestedPathString(payload, "type") != "error" || nestedPathString(payload, "error", "type") != "invalid_request_error" {
					t.Fatalf("unexpected anthropic validation payload: %#v", payload)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postExternalText(t, env.server.URL+tc.path, nil, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("unexpected status: %d body=%s", status, body)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("decode payload: %v body=%s", err, body)
			}
			tc.checkMessage(t, payload)
		})
	}
}

func TestAnthropicAbortReturnsAnthropicErrorEnvelope(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/messages", map[string]any{
		"model": "claude-abort",
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "anthropic abort 测试"},
				},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "anthropic abort 测试")
	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/abort", map[string]any{
		"error": "anthropic 人工中止",
	}, http.StatusOK)

	finalResp := <-resultCh
	if nestedPathString(finalResp, "type") != "error" || nestedPathString(finalResp, "error", "message") != "anthropic 人工中止" {
		t.Fatalf("unexpected anthropic abort payload: %#v", finalResp)
	}
}

func TestProtocolRequestValidationRejectsUnknownToolChoice(t *testing.T) {
	env := newTestEnv(t)

	status, body := postExternalText(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"input": "hello",
		"tools": []any{
			map[string]any{
				"type":     "function",
				"function": map[string]any{"name": "weather"},
			},
		},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "lookup"},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", status, body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode payload: %v body=%s", err, body)
	}
	if nestedPathString(payload, "error", "param") != "tool_choice.function.name" {
		t.Fatalf("unexpected tool choice error payload: %#v", payload)
	}
}

func TestProtocolRequestValidationRejectsInvalidJSONSchemaResponseFormat(t *testing.T) {
	env := newTestEnv(t)

	status, body := postExternalText(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"input": "hello",
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "answer"},
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", status, body)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode payload: %v body=%s", err, body)
	}
	if nestedPathString(payload, "error", "param") != "response_format.json_schema.schema" {
		t.Fatalf("unexpected response_format error payload: %#v", payload)
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

func TestProtocolRequestsUseRealtimeConnectionLeases(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.RealtimeMaxConnectionsPerUser = 2
		cfg.RealtimeWebUIReservedPerUser = 1
	})

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-first-lease",
		"input": "first lease request",
	})
	conversation := env.waitForWaitingConversation(t, "first lease request")

	status, rawBody := postExternalText(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "demo-second-lease",
		"input": "second lease request",
	})
	if status != http.StatusTooManyRequests {
		t.Fatalf("expected second long request to be rate limited: status=%d body=%q", status, rawBody)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawBody), &payload); err != nil {
		t.Fatalf("decode rate limit payload: %v body=%q", err, rawBody)
	}
	if nestedPathString(payload, "error", "code") != "connection_limit_exceeded" {
		t.Fatalf("unexpected rate limit payload: %#v", payload)
	}

	wsURL := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/api/ws"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		if resp != nil {
			t.Fatalf("dial websocket: %v status=%d", err, resp.StatusCode)
		}
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	var snapshot map[string]any
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatalf("read websocket snapshot: %v", err)
	}
	if nestedString(snapshot, "type") != "snapshot" {
		t.Fatalf("unexpected websocket snapshot: %#v", snapshot)
	}

	env.postJSON(t, "/api/chat/output/complete", map[string]any{
		"conversation_id": conversation["id"],
		"mode":            "assistant_message",
		"text":            "lease completed",
	}, http.StatusOK)
	finalResp := <-firstCh
	if got := nestedString(finalResp, "output_text"); got != "lease completed" {
		t.Fatalf("unexpected first completion payload: %#v", finalResp)
	}
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

	resultCh := startJSONRequestWithHeaders(t, env.server.URL+"/v1/chat/completions?trace=1&tag=a&tag=b", map[string]string{
		"Cookie":  "session=should-not-store",
		"X-Debug": "lab-request-id",
	}, map[string]any{
		"model": "demo-lab-request-id",
		"messages": []map[string]any{
			{"role": "system", "content": "lab system policy"},
			{"role": "developer", "content": "lab developer hint"},
			{"role": "assistant", "content": "lab previous answer"},
			{"role": "user", "content": "lab request id 测试"},
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
	if nestedString(requestRecord, "system_text") != "lab system policy" ||
		nestedString(requestRecord, "developer_text") != "lab developer hint" ||
		nestedString(requestRecord, "assistant_text") != "lab previous answer" {
		t.Fatalf("unexpected lab request context fields: %#v", requestResp)
	}
	if nestedPathString(requestResp, "parsed", "system_text") != "lab system policy" ||
		nestedPathString(requestResp, "parsed", "developer_text") != "lab developer hint" ||
		nestedPathString(requestResp, "parsed", "assistant_text") != "lab previous answer" ||
		nestedPathString(requestResp, "parsed", "user_text") != "lab request id 测试" {
		t.Fatalf("unexpected lab parsed request view: %#v", requestResp)
	}
	if nestedPathString(requestResp, "parsed", "request_method") != http.MethodPost ||
		nestedPathString(requestResp, "parsed", "request_path") != "/v1/chat/completions" {
		t.Fatalf("unexpected lab parsed replay basics: %#v", requestResp)
	}
	requestQuery := requestResp["parsed"].(map[string]any)["request_query"].(map[string]any)
	if !reflect.DeepEqual(requestQuery["tag"], []any{"a", "b"}) || !reflect.DeepEqual(requestQuery["trace"], []any{"1"}) {
		t.Fatalf("unexpected lab parsed request query: %#v", requestResp)
	}
	requestHeaders := requestResp["parsed"].(map[string]any)["request_headers"].(map[string]any)
	if _, exists := requestHeaders["Cookie"]; exists {
		t.Fatalf("cookie header should not be stored: %#v", requestResp)
	}
	if !reflect.DeepEqual(requestHeaders["X-Debug"], []any{"lab-request-id"}) {
		t.Fatalf("unexpected lab parsed request headers: %#v", requestResp)
	}
	replay := requestResp["parsed"].(map[string]any)["replay"].(map[string]any)
	if !strings.Contains(nestedString(replay, "curl"), env.server.URL+"/v1/chat/completions?tag=a&tag=b&trace=1") ||
		!strings.Contains(nestedString(replay, "curl"), "X-Debug: lab-request-id") {
		t.Fatalf("unexpected lab replay curl: %#v", requestResp)
	}
	copyCurlResp := env.postJSON(t, "/lab/requests/"+requestID+"/copy-curl", map[string]any{}, http.StatusOK)
	if nestedString(copyCurlResp, "request_id") != requestID || nestedString(copyCurlResp, "curl") != nestedString(replay, "curl") {
		t.Fatalf("unexpected lab copy-curl response: %#v", copyCurlResp)
	}

	env.postJSON(t, "/lab/requests/"+requestID+"/delta", map[string]any{
		"text": "通过 request_id 输出",
	}, http.StatusOK)
	env.postJSON(t, "/lab/requests/"+requestID+"/complete", map[string]any{
		"mode": "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	if nestedString(finalResp, "object") != "chat.completion" {
		t.Fatalf("unexpected final request-id object: %#v", finalResp)
	}
	choices, ok := finalResp["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("unexpected final request-id choices: %#v", finalResp)
	}
	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if nestedString(message, "content") != "通过 request_id 输出" {
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
	parsedItems := listResp["parsed_items"].([]any)
	if len(items) < 2 {
		t.Fatalf("unexpected lab requests list: %#v", listResp)
	}
	if len(parsedItems) != len(items) {
		t.Fatalf("unexpected lab parsed_items size: %#v", listResp)
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

	foundFirstParsed := false
	foundSecondParsed := false
	for _, item := range parsedItems {
		record := item.(map[string]any)
		switch nestedString(record, "model") {
		case "demo-list-1":
			foundFirstParsed = nestedString(record, "request_format") == "responses" &&
				nestedString(record, "user_text") == "list 请求 1"
		case "demo-list-2":
			partTypes, ok := record["input_part_types"].([]any)
			foundSecondParsed = ok &&
				len(partTypes) == 1 &&
				partTypes[0] == "text" &&
				nestedString(record, "request_format") == "chat_completions"
		}
	}
	if !foundFirstParsed || !foundSecondParsed {
		t.Fatalf("missing expected parsed requests in list: %#v", listResp)
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

	meSchemaResp := env.appGetJSON(t, "/api/app/me/schema", appKey, http.StatusOK)
	meSchema := meSchemaResp["schema"].(map[string]any)
	meOperations := meSchema["operations"].([]any)
	if len(meOperations) != 2 || nestedString(meOperations[0].(map[string]any), "name") != "me" {
		t.Fatalf("unexpected app overview schema response: %#v", meSchemaResp)
	}

	schemaResp := env.appGetJSON(t, "/api/app/requests/schema", appKey, http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 6 ||
		nestedString(operations[0].(map[string]any), "name") != "list_requests" ||
		nestedString(operations[2].(map[string]any), "name") != "copy_request_curl" ||
		nestedString(operations[5].(map[string]any), "name") != "request_abort" {
		t.Fatalf("unexpected app requests schema response: %#v", schemaResp)
	}
	if !containsMapItemWithStringField(schema["parsed_item_fields"], "key", "normalized_tool_schemas") ||
		!containsMapItemWithStringField(schema["parsed_detail_fields"], "key", "replay") ||
		!containsMapItemWithStringField(schema["replay_fields"], "key", "headers") {
		t.Fatalf("unexpected app requests parsed/replay schema metadata: %#v", schemaResp)
	}

	resultCh := startJSONRequest(t, env.server.URL+"/v1/chat/completions", map[string]any{
		"model": "demo-app-api",
		"messages": []map[string]any{
			{"role": "system", "content": "app system policy"},
			{"role": "developer", "content": "app developer hint"},
			{"role": "assistant", "content": "previous app answer"},
			{"role": "user", "content": "app api 测试"},
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
	parsedItems := listResp["parsed_items"].([]any)
	if len(parsedItems) != len(items) {
		t.Fatalf("unexpected app api parsed_items size: %#v", listResp)
	}
	foundParsed := false
	for _, item := range parsedItems {
		record := item.(map[string]any)
		if nestedString(record, "request_id") != requestID {
			continue
		}
		partTypes, ok := record["input_part_types"].([]any)
		foundParsed = nestedString(record, "system_text") == "app system policy" &&
			nestedString(record, "developer_text") == "app developer hint" &&
			nestedString(record, "assistant_text") == "previous app answer" &&
			nestedString(record, "user_text") == "app api 测试" &&
			ok &&
			len(partTypes) == 1 &&
			partTypes[0] == "text"
	}
	if !foundParsed {
		t.Fatalf("missing expected parsed app api request in list: %#v", listResp)
	}

	detailResp := env.appGetJSON(t, "/api/app/requests/"+requestID, appKey, http.StatusOK)
	if nestedPathString(detailResp, "request", "request_id") != requestID {
		t.Fatalf("unexpected app api request detail: %#v", detailResp)
	}
	if nestedPathString(detailResp, "request", "system_text") != "app system policy" ||
		nestedPathString(detailResp, "request", "developer_text") != "app developer hint" ||
		nestedPathString(detailResp, "request", "assistant_text") != "previous app answer" {
		t.Fatalf("unexpected app api request context fields: %#v", detailResp)
	}
	if nestedPathString(detailResp, "parsed", "system_text") != "app system policy" ||
		nestedPathString(detailResp, "parsed", "developer_text") != "app developer hint" ||
		nestedPathString(detailResp, "parsed", "assistant_text") != "previous app answer" ||
		nestedPathString(detailResp, "parsed", "user_text") != "app api 测试" {
		t.Fatalf("unexpected app api parsed request view: %#v", detailResp)
	}
	copyCurlResp := env.appPostJSON(t, "/api/app/requests/"+requestID+"/copy-curl", appKey, map[string]any{}, http.StatusOK)
	if nestedString(copyCurlResp, "request_id") != requestID || !strings.Contains(nestedString(copyCurlResp, "curl"), "/v1/chat/completions") {
		t.Fatalf("unexpected app request copy-curl response: %#v", copyCurlResp)
	}

	env.appPostJSON(t, "/api/app/requests/"+requestID+"/complete", appKey, map[string]any{
		"text": "应用 API 完成",
		"mode": "assistant_message",
	}, http.StatusOK)

	finalResp := <-resultCh
	choices, ok := finalResp["choices"].([]any)
	if !ok || len(choices) != 1 {
		t.Fatalf("unexpected app api final choices: %#v", finalResp)
	}
	choice := choices[0].(map[string]any)
	message := choice["message"].(map[string]any)
	if nestedString(message, "content") != "应用 API 完成" {
		t.Fatalf("unexpected app api final response: %#v", finalResp)
	}
}

func TestAppAPIRequestsResourceLimits(t *testing.T) {
	env := newTestEnv(t)

	allowedCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-requests-allowed",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "allowed request"}},
			},
		},
	})
	blockedCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-requests-blocked",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "blocked request"}},
			},
		},
	})

	allowedConversation := env.waitForWaitingConversation(t, "allowed request")
	blockedConversation := env.waitForWaitingConversation(t, "blocked request")
	allowedRequestID := env.requestIDForConversation(t, allowedConversation["id"].(string))
	blockedRequestID := env.requestIDForConversation(t, blockedConversation["id"].(string))

	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read", "requests:respond"}, map[string]any{
		"allowed_request_ids":     []string{allowedRequestID},
		"allowed_virtual_models":  []string{"demo-requests-allowed"},
		"allowed_request_actions": []string{"complete"},
	})

	listResp := env.appGetJSON(t, "/api/app/requests", appKey, http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) != 1 || nestedString(items[0].(map[string]any), "request_id") != allowedRequestID {
		t.Fatalf("unexpected resource-limited request list: %#v", listResp)
	}
	parsedItems := listResp["parsed_items"].([]any)
	if len(parsedItems) != 1 || nestedString(parsedItems[0].(map[string]any), "request_id") != allowedRequestID {
		t.Fatalf("unexpected resource-limited parsed request list: %#v", listResp)
	}
	copyCurlResp := env.appPostJSON(t, "/api/app/requests/"+allowedRequestID+"/copy-curl", appKey, map[string]any{}, http.StatusOK)
	if nestedString(copyCurlResp, "request_id") != allowedRequestID {
		t.Fatalf("unexpected allowed request copy-curl payload: %#v", copyCurlResp)
	}

	allowedResp := env.appGetJSON(t, "/api/app/requests/"+allowedRequestID, appKey, http.StatusOK)
	if nestedPathString(allowedResp, "request", "request_id") != allowedRequestID {
		t.Fatalf("unexpected allowed request detail: %#v", allowedResp)
	}

	status, body := env.appGetText(t, "/api/app/requests/"+blockedRequestID, appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("expected blocked request detail rejection: status=%d body=%q", status, body)
	}
	status, body = env.appPostText(t, "/api/app/requests/"+blockedRequestID+"/copy-curl", appKey, map[string]any{})
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("expected blocked request copy-curl rejection: status=%d body=%q", status, body)
	}

	status, body = env.appPostText(t, "/api/app/requests/"+blockedRequestID+"/complete", appKey, map[string]any{
		"text": "should not pass",
		"mode": "assistant_message",
	})
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("expected blocked request complete rejection: status=%d body=%q", status, body)
	}

	env.appPostJSON(t, "/api/app/requests/"+allowedRequestID+"/complete", appKey, map[string]any{
		"text": "allowed app api complete",
		"mode": "assistant_message",
	}, http.StatusOK)
	env.postJSON(t, "/api/conversations/"+blockedConversation["id"].(string)+"/respond", map[string]any{
		"text": "blocked fallback",
		"mode": "assistant_message",
	}, http.StatusOK)

	allowedFinal := <-allowedCh
	if got := nestedString(allowedFinal, "output_text"); got != "allowed app api complete" {
		t.Fatalf("unexpected allowed request final response: %#v", allowedFinal)
	}
	blockedFinal := <-blockedCh
	if got := nestedString(blockedFinal, "output_text"); got != "blocked fallback" {
		t.Fatalf("unexpected blocked request final response: %#v", blockedFinal)
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

	status, body = env.appGetText(t, "/api/app/requests/schema", env.seedAppAPIKey(t, "lab-user", []string{"conversations:read"}, nil))
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("unexpected app requests schema scope rejection: status=%d body=%q", status, body)
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

	schemaResp := env.appGetJSON(t, "/api/app/conversations/schema", appKey, http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 2 ||
		nestedString(operations[0].(map[string]any), "name") != "list_conversations" ||
		nestedString(operations[1].(map[string]any), "name") != "list_conversation_messages" {
		t.Fatalf("unexpected app conversations schema response: %#v", schemaResp)
	}

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

func TestAppAPIConversationResourceLimits(t *testing.T) {
	env := newTestEnv(t)

	allowedCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-conv-allowed",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "allowed conversation"}},
			},
		},
	})
	blockedCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-conv-blocked",
		"input": []map[string]any{
			{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "blocked conversation"}},
			},
		},
	})

	allowedConversation := env.waitForWaitingConversation(t, "allowed conversation")
	blockedConversation := env.waitForWaitingConversation(t, "blocked conversation")

	appKey := env.seedAppAPIKey(t, "lab-user", []string{"conversations:read"}, map[string]any{
		"allowed_conversation_ids": []string{allowedConversation["id"].(string)},
		"allowed_virtual_models":   []string{"demo-conv-allowed"},
	})

	listResp := env.appGetJSON(t, "/api/app/conversations", appKey, http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) != 1 || nestedString(items[0].(map[string]any), "id") != allowedConversation["id"].(string) {
		t.Fatalf("unexpected resource-limited conversation list: %#v", listResp)
	}

	messagesResp := env.appGetJSON(t, "/api/app/conversations/"+allowedConversation["id"].(string)+"/messages", appKey, http.StatusOK)
	messageItems := messagesResp["items"].([]any)
	if len(messageItems) == 0 || nestedString(messageItems[0].(map[string]any), "content") != "allowed conversation" {
		t.Fatalf("unexpected allowed conversation messages: %#v", messagesResp)
	}

	status, body := env.appGetText(t, "/api/app/conversations/"+blockedConversation["id"].(string)+"/messages", appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "forbidden") {
		t.Fatalf("expected blocked conversation messages rejection: status=%d body=%q", status, body)
	}

	env.postJSON(t, "/api/conversations/"+allowedConversation["id"].(string)+"/respond", map[string]any{
		"text": "allowed conversation fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	env.postJSON(t, "/api/conversations/"+blockedConversation["id"].(string)+"/respond", map[string]any{
		"text": "blocked conversation fallback",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-allowedCh
	<-blockedCh
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

func TestAppAPIConversationsSchemaRejectsWrongScope(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"requests:read"}, nil)

	status, body := env.appGetText(t, "/api/app/conversations/schema", appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("unexpected app conversations schema scope rejection: status=%d body=%q", status, body)
	}
}

func TestUserAppAPIKeysManagement(t *testing.T) {
	env := newTestEnv(t)
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)

	createResp := env.postJSON(t, "/api/user/app-api-keys", map[string]any{
		"name":            "managed-key",
		"scopes":          []string{"requests:read", "requests:respond"},
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

func TestUserAppAPIKeysRejectInvalidConfig(t *testing.T) {
	env := newTestEnv(t)

	cases := []struct {
		name    string
		body    map[string]any
		wantErr string
	}{
		{
			name: "missing_scopes",
			body: map[string]any{
				"name": "invalid-key",
			},
			wantErr: "scopes are required",
		},
		{
			name: "unknown_scope",
			body: map[string]any{
				"name":   "invalid-key",
				"scopes": []string{"unknown:scope"},
			},
			wantErr: "unsupported app api key scope",
		},
		{
			name: "unknown_resource_limit",
			body: map[string]any{
				"name":            "invalid-key",
				"scopes":          []string{"requests:read"},
				"resource_limits": map[string]any{"unexpected_limit": true},
			},
			wantErr: "unsupported app api key resource limit",
		},
		{
			name: "request_actions_without_scope",
			body: map[string]any{
				"name":            "invalid-key",
				"scopes":          []string{"requests:read"},
				"resource_limits": map[string]any{"allowed_request_actions": []string{"complete"}},
			},
			wantErr: "requires requests:respond scope",
		},
		{
			name: "request_ids_without_request_scope",
			body: map[string]any{
				"name":            "invalid-key",
				"scopes":          []string{"statistics:read"},
				"resource_limits": map[string]any{"allowed_request_ids": []string{"req_1"}},
			},
			wantErr: "allowed_request_ids requires one of scopes",
		},
		{
			name: "virtual_models_without_matching_scope",
			body: map[string]any{
				"name":            "invalid-key",
				"scopes":          []string{"statistics:read"},
				"resource_limits": map[string]any{"allowed_virtual_models": []string{"demo-a"}},
			},
			wantErr: "allowed_virtual_models requires one of scopes",
		},
		{
			name: "max_model_keys_without_write_scope",
			body: map[string]any{
				"name":            "invalid-key",
				"scopes":          []string{"model_keys:read"},
				"resource_limits": map[string]any{"max_model_keys": 1},
			},
			wantErr: "max_model_keys requires one of scopes",
		},
		{
			name: "invalid_source_ip",
			body: map[string]any{
				"name":            "invalid-key",
				"scopes":          []string{"requests:read"},
				"resource_limits": map[string]any{"allowed_source_ips": []string{"not-an-ip"}},
			},
			wantErr: "invalid allowed_source_ips entry",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := env.postText(t, "/api/user/app-api-keys", tc.body)
			if status != http.StatusBadRequest || !strings.Contains(body, tc.wantErr) {
				t.Fatalf("expected invalid config rejection: status=%d body=%q", status, body)
			}
		})
	}
}

func TestUserAppAPIKeysSchema(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/api/user/app-api-keys/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	scopes := schema["scopes"].([]any)
	resourceLimits := schema["resource_limits"].([]any)
	if len(scopes) == 0 || len(resourceLimits) == 0 {
		t.Fatalf("unexpected app api key schema response: %#v", resp)
	}
	foundRespondScope := false
	foundRequestActions := false
	for _, raw := range scopes {
		item := raw.(map[string]any)
		if nestedString(item, "name") == "requests:respond" {
			foundRespondScope = true
			break
		}
	}
	for _, raw := range resourceLimits {
		item := raw.(map[string]any)
		if nestedString(item, "name") != "allowed_request_actions" {
			continue
		}
		foundRequestActions = true
		if !containsStringValue(item["requires_any_scopes"], "requests:respond") || !containsStringValue(item["allowed_values"], "complete") {
			t.Fatalf("unexpected request action schema item: %#v", item)
		}
	}
	if !foundRespondScope || !foundRequestActions {
		t.Fatalf("unexpected app api key schema response: %#v", resp)
	}
}

func TestUserAppAPIKeysManagementUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("app-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_app_owner",
		Username:     "app-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed app owner: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "app-owner",
		"password": "app-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing user session cookie: %#v", cookies)
	}

	headers := map[string]string{"Origin": env.server.URL}
	createResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/user/app-api-keys", map[string]any{
		"name":   "session-managed-app-key",
		"scopes": []string{"requests:read"},
	}, sessionCookie, headers, http.StatusOK)
	keyID := nestedPathString(createResp, "item", "id")
	if keyID == "" || !strings.HasPrefix(nestedString(createResp, "raw_key"), "ak-") {
		t.Fatalf("unexpected session app key create response: %#v", createResp)
	}

	listResp := env.getJSONWithCookie(t, "/api/user/app-api-keys", sessionCookie, http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) != 1 || nestedString(items[0].(map[string]any), "id") != keyID {
		t.Fatalf("unexpected session app api key list: %#v", listResp)
	}

	status, body := env.deleteTextWithCookieAndHeaders(t, "/api/user/app-api-keys/"+keyID, sessionCookie, headers)
	if status != http.StatusOK || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("unexpected session app api key delete response: status=%d body=%q", status, body)
	}
	assertAuditCountForActor(t, env, "user_app_owner", "user.app_api_key", "app_api_key", keyID, "create", "success", 1)
	assertAuditCountForActor(t, env, "user_app_owner", "user.app_api_key", "app_api_key", keyID, "delete", "success", 1)
}

func TestUserAppAPIKeysSchemaUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("schema-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_app_schema_owner",
		Username:     "app-schema-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed app schema owner: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "app-schema-owner",
		"password": "schema-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing user session cookie: %#v", cookies)
	}

	resp := env.getJSONWithCookie(t, "/api/user/app-api-keys/schema", sessionCookie, http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if !containsMapItemWithStringField(schema["scopes"], "name", "requests:read") {
		t.Fatalf("unexpected session app api key schema response: %#v", resp)
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

func TestUserConfigSchemaRoute(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/api/user/config/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if allowUnknown, ok := schema["allow_unknown_keys"].(bool); !ok || !allowUnknown {
		t.Fatalf("expected allow_unknown_keys=true: %#v", resp)
	}
	if !containsMapItemWithStringField(schema["fields"], "key", "ntfy_url_enabled") {
		t.Fatalf("unexpected user config schema fields: %#v", resp)
	}
}

func TestUserConfigSchemaUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("schema-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_config_schema_owner",
		Username:     "config-schema-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed user config schema user: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "config-schema-owner",
		"password": "schema-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing user session cookie: %#v", cookies)
	}

	resp := env.getJSONWithCookie(t, "/api/user/config/schema", sessionCookie, http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if !containsMapItemWithStringField(schema["fields"], "key", "messages_per_minute_limit") {
		t.Fatalf("unexpected session user config schema response: %#v", resp)
	}
}

func TestWorkspaceToolCallAssistContextInLab(t *testing.T) {
	env := newTestEnv(t)

	schemaResp := env.getJSON(t, "/api/workspace/tool-call/schema", http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 4 {
		t.Fatalf("unexpected tool-call schema response: %#v", schemaResp)
	}
	operation := operations[0].(map[string]any)
	if nestedString(operation, "name") != "assist_context" || nestedString(operation, "path") != "/api/workspace/tool-call/assist-context" {
		t.Fatalf("unexpected tool-call schema operation: %#v", schemaResp)
	}
	if !containsMapItemWithStringField(operation["fields"], "key", "candidate_base_url") {
		t.Fatalf("expected candidate_base_url field in tool-call schema: %#v", schemaResp)
	}
	assistOperation := operations[1].(map[string]any)
	if nestedString(assistOperation, "name") != "assist_execute" || nestedString(assistOperation, "path") != "/api/workspace/tool-call/assist" {
		t.Fatalf("unexpected tool-call assist operation: %#v", schemaResp)
	}
	if !containsMapItemWithStringField(assistOperation["fields"], "key", "provider") {
		t.Fatalf("expected provider field in tool-call assist schema: %#v", schemaResp)
	}
	streamOperation := operations[2].(map[string]any)
	if nestedString(streamOperation, "name") != "assist_stream" || nestedString(streamOperation, "path") != "/api/workspace/tool-call/assist/stream" {
		t.Fatalf("unexpected tool-call assist stream operation: %#v", schemaResp)
	}
	if !containsStringValue(streamOperation["response_sections"], "event: assist.completed") {
		t.Fatalf("expected assist.completed event in tool-call stream schema: %#v", schemaResp)
	}
	parseOperation := operations[3].(map[string]any)
	if nestedString(parseOperation, "name") != "assist_parse" || nestedString(parseOperation, "path") != "/api/workspace/tool-call/assist/parse" {
		t.Fatalf("unexpected tool-call assist parse operation: %#v", schemaResp)
	}
	if !containsMapItemWithStringField(parseOperation["fields"], "key", "raw_output") {
		t.Fatalf("expected raw_output field in tool-call assist parse schema: %#v", schemaResp)
	}

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-tool-assist-lab",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "请帮我准备 tool call 草稿"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_weather",
					"description": "Lookup the weather.",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"city": map[string]any{"type": "string"}},
					},
				},
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
			"name": "lookup_weather",
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "tool_draft",
				"schema": map[string]any{"type": "object"},
			},
		},
	})

	conversation := env.waitForWaitingConversation(t, "请帮我准备 tool call 草稿")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))
	env.postJSON(t, "/api/chat/output/delta", map[string]any{
		"conversation_id": conversation["id"],
		"text":            "draft chunk",
	}, http.StatusOK)

	resp := env.getJSON(t, "/api/workspace/tool-call/assist-context?request_id="+requestID+"&candidate_base_url="+neturl.QueryEscape(env.server.URL+"/v1"), http.StatusOK)
	if nestedPathString(resp, "request", "request_id") != requestID {
		t.Fatalf("unexpected assist request: %#v", resp)
	}
	if nestedPathString(resp, "parsed", "tool_choice", "name") != "lookup_weather" {
		t.Fatalf("unexpected assist parsed tool choice: %#v", resp)
	}
	parsed := resp["parsed"].(map[string]any)
	toolSchemas, ok := parsed["tool_schemas"].([]any)
	if !ok || len(toolSchemas) != 1 {
		t.Fatalf("unexpected assist tool schemas: %#v", resp)
	}
	if !containsMapItemWithStringField(parsed["normalized_tool_schemas"], "name", "lookup_weather") {
		t.Fatalf("unexpected assist normalized tool schemas: %#v", resp)
	}
	if nestedPathString(resp, "draft", "text") != "draft chunk" {
		t.Fatalf("unexpected assist draft: %#v", resp)
	}
	if !containsStringValue(resp["assist_schema"].(map[string]any)["confidence_levels"], "medium") {
		t.Fatalf("unexpected assist schema confidence levels: %#v", resp)
	}
	if !containsMapItemWithStringField(resp["upstream_assistant_schema"].(map[string]any)["fields"], "key", "base_url") {
		t.Fatalf("unexpected upstream assistant schema: %#v", resp)
	}
	if nestedPathString(resp, "upstream_assistant_schema", "default_config", "protocol") != "responses" {
		t.Fatalf("unexpected upstream assistant default config: %#v", resp)
	}
	if !containsMapItemWithStringField(resp["upstream_assistant_schema"].(map[string]any)["error_codes"], "code", "upstream_assistant.recursive_base_url") {
		t.Fatalf("unexpected upstream assistant error codes: %#v", resp)
	}
	if !containsMapItemWithStringField(resp["upstream_protocol_templates"], "protocol", "responses") ||
		!containsMapItemWithStringField(resp["upstream_protocol_templates"], "protocol", "chat_completions") ||
		!containsMapItemWithStringField(resp["upstream_protocol_templates"], "protocol", "anthropic_messages") {
		t.Fatalf("unexpected upstream protocol templates: %#v", resp)
	}
	if !nestedPathBool(resp, "upstream_hints", "candidate_recursive") {
		t.Fatalf("expected recursive upstream hint: %#v", resp)
	}
	upstreamInputHints := resp["upstream_input_hints"].(map[string]any)
	if numericValue(upstreamInputHints["available_messages"]) < 1 {
		t.Fatalf("unexpected upstream input hints message count: %#v", resp)
	}
	recommendedMessages, ok := upstreamInputHints["recommended_messages"].([]any)
	if !ok || len(recommendedMessages) < 1 {
		t.Fatalf("unexpected upstream recommended messages: %#v", resp)
	}
	if !containsStringValue(upstreamInputHints["construction_rules"], "Do not convert the current draft into a committed assistant message automatically.") {
		t.Fatalf("unexpected upstream input construction rules: %#v", resp)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestWorkspaceToolCallAssistContextUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("assist-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "assist_owner",
		Username:     "assist-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed assist owner: %v", err)
	}
	modelKey := env.seedModelAPIKey(t, "assist_owner", "assist-owner-key", "assist-owner-model")
	resultCh := startJSONRequestWithHeaders(t, env.server.URL+"/v1/responses", map[string]string{
		"Authorization": "Bearer " + modelKey,
	}, map[string]any{
		"model": "assist-owner-model",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "session assist 测试"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "assist_lookup",
					"description": "Lookup from session assist test.",
					"parameters": map[string]any{
						"type": "object",
					},
				},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "session assist 测试")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))

	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "assist-owner",
		"password": "assist-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing assist session cookie: %#v", cookies)
	}

	headers := map[string]string{"Origin": env.server.URL}
	_, _ = env.postJSONWithCookieAndHeaders(t, "/api/conversations/"+conversation["id"].(string)+"/stream/delta", map[string]any{
		"text": "session draft",
	}, sessionCookie, headers, http.StatusOK)

	resp := env.getJSONWithCookie(t, "/api/workspace/tool-call/assist-context?conversation_id="+conversation["id"].(string), sessionCookie, http.StatusOK)
	if nestedPathString(resp, "request", "request_id") != requestID {
		t.Fatalf("unexpected session assist request: %#v", resp)
	}
	if nestedPathString(resp, "conversation", "id") != conversation["id"].(string) {
		t.Fatalf("unexpected session assist conversation: %#v", resp)
	}
	if !containsMapItemWithStringField(resp["parsed"].(map[string]any)["normalized_tool_schemas"], "name", "assist_lookup") {
		t.Fatalf("unexpected session normalized tool schemas: %#v", resp)
	}
	if nestedPathString(resp, "draft", "text") != "session draft" {
		t.Fatalf("unexpected session assist draft: %#v", resp)
	}
	if !containsStringValue(resp["assist_schema"].(map[string]any)["notes"], "Do not auto-submit the draft tool call.") {
		t.Fatalf("unexpected session assist schema notes: %#v", resp)
	}
	if !containsStringValue(resp["upstream_assistant_schema"].(map[string]any)["sensitive_fields"], "api_key") {
		t.Fatalf("unexpected upstream assistant sensitive fields: %#v", resp)
	}
	if !containsStringValue(resp["upstream_assistant_schema"].(map[string]any)["validation_rules"], "base_url must be an absolute http or https URL.") {
		t.Fatalf("unexpected upstream assistant validation rules: %#v", resp)
	}
	if !containsMapItemWithStringField(resp["upstream_protocol_templates"], "protocol", "responses") {
		t.Fatalf("unexpected session upstream protocol templates: %#v", resp)
	}
	if nestedPathBool(resp, "upstream_hints", "candidate_recursive") {
		t.Fatalf("did not expect recursive upstream hint: %#v", resp)
	}
	upstreamInputHints := resp["upstream_input_hints"].(map[string]any)
	if !nestedPathBool(resp, "upstream_input_hints", "truncated") && numericValue(upstreamInputHints["default_max_input_messages"]) != 20 {
		t.Fatalf("unexpected upstream input default window: %#v", resp)
	}

	_, _ = env.postJSONWithCookieAndHeaders(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "session done",
		"mode": "assistant_message",
	}, sessionCookie, headers, http.StatusOK)
	<-resultCh
}

func TestWorkspaceToolCallAssistContextRejectsProgrammaticActors(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	appKey := env.seedAppAPIKey(t, "assist-denied-user", []string{"requests:read"}, nil)
	modelKey := env.seedModelAPIKey(t, "assist-denied-user", "assist-denied-model", "assist-denied")

	status, body := env.getTextWithHeaders(t, "/api/workspace/tool-call/assist-context?request_id=req_demo", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected app key assist-context rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/workspace/tool-call/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected app key tool-call schema rejection: status=%d body=%q", status, body)
	}
	status, body, _ = env.postJSONWithCookieText(t, "/api/workspace/tool-call/assist", map[string]any{
		"provider":   "kirari",
		"model":      "demo",
		"request_id": "req_demo",
	}, nil, map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected app key tool-call assist rejection: status=%d body=%q", status, body)
	}
	status, body, _ = env.postJSONWithCookieText(t, "/api/workspace/tool-call/assist/parse", map[string]any{
		"provider":   "browser_upstream",
		"model":      "demo",
		"request_id": "req_demo",
		"raw_output": "{}",
	}, nil, map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected app key tool-call assist parse rejection: status=%d body=%q", status, body)
	}
	status, body, _ = env.postJSONWithCookieText(t, "/api/workspace/tool-call/assist/stream", map[string]any{
		"provider":   "kirari",
		"model":      "demo",
		"request_id": "req_demo",
	}, nil, map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected app key tool-call assist stream rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/workspace/tool-call/assist-context?request_id=req_demo", map[string]string{
		"Authorization": "Bearer " + modelKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected model key assist-context rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/workspace/tool-call/schema", map[string]string{
		"Authorization": "Bearer " + modelKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected model key tool-call schema rejection: status=%d body=%q", status, body)
	}
	status, body, _ = env.postJSONWithCookieText(t, "/api/workspace/tool-call/assist", map[string]any{
		"provider":   "kirari",
		"model":      "demo",
		"request_id": "req_demo",
	}, nil, map[string]string{
		"Authorization": "Bearer " + modelKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected model key tool-call assist rejection: status=%d body=%q", status, body)
	}
	status, body, _ = env.postJSONWithCookieText(t, "/api/workspace/tool-call/assist/parse", map[string]any{
		"provider":   "browser_upstream",
		"model":      "demo",
		"request_id": "req_demo",
		"raw_output": "{}",
	}, nil, map[string]string{
		"Authorization": "Bearer " + modelKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected model key tool-call assist parse rejection: status=%d body=%q", status, body)
	}
	status, body, _ = env.postJSONWithCookieText(t, "/api/workspace/tool-call/assist/stream", map[string]any{
		"provider":   "kirari",
		"model":      "demo",
		"request_id": "req_demo",
	}, nil, map[string]string{
		"Authorization": "Bearer " + modelKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected model key tool-call assist stream rejection: status=%d body=%q", status, body)
	}
}

func TestWorkspaceToolCallAssistKirariLifecycle(t *testing.T) {
	provider := newTestKirariProvider(t)
	provider.chatCompletionsResponse = map[string]any{
		"choices": []map[string]any{
			{
				"message": map[string]any{
					"content": `{"explanation":"使用天气查询工具读取北京天气。","tool_call":{"name":"lookup_weather","arguments":{"city":"Beijing","unit":"c"}},"confidence":"high","warnings":["city inferred from recent user message"]}`,
				},
			},
		},
	}
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.KirariEnabled = true
		cfg.KirariIssuerURL = provider.Issuer()
		cfg.KirariClientID = "chatapi"
		cfg.KirariClientSecret = "secret"
		cfg.KirariRedirectURL = "http://chat.example.com/api/integrations/kirari/callback"
		cfg.KirariAllowedIssuers = []string{provider.Issuer()}
		cfg.KirariScopes = []string{"openid", "profile", "email", "offline_access", "llm:read", "llm:stream"}
	})

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-kirari-assist",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "帮我查一下北京天气，先准备 tool call"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_weather",
					"description": "Lookup the weather.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
							"unit": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "帮我查一下北京天气，先准备 tool call")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))

	_, location, cookies := env.getRedirect(t, "/api/user/integrations/kirari/connect")
	locationURL, err := neturl.Parse(location)
	if err != nil {
		t.Fatalf("parse kirari redirect: %v", err)
	}
	provider.idTokenClaims["nonce"] = locationURL.Query().Get("nonce")
	env.getJSONAndCookiesWithCookies(t, "/api/integrations/kirari/callback?code=kirari-code&state="+neturl.QueryEscape(locationURL.Query().Get("state")), cookies, http.StatusOK)

	assistResp := env.postJSON(t, "/api/workspace/tool-call/assist", map[string]any{
		"provider":   "kirari",
		"model":      "kirari-model",
		"request_id": requestID,
	}, http.StatusOK)
	if nestedPathString(assistResp, "assist", "provider") != "kirari" || nestedPathString(assistResp, "assist", "model") != "kirari-model" {
		t.Fatalf("unexpected assist provider/model: %#v", assistResp)
	}
	if !nestedPathBool(assistResp, "assist", "valid_draft") {
		t.Fatalf("expected valid kirari assist draft: %#v", assistResp)
	}
	if nestedPathString(assistResp, "assist", "tool_call", "name") != "lookup_weather" {
		t.Fatalf("unexpected assist tool call name: %#v", assistResp)
	}
	arguments, _ := nestedPath(assistResp, "assist", "tool_call", "arguments").(map[string]any)
	if nestedString(arguments, "city") != "Beijing" || nestedString(arguments, "unit") != "c" {
		t.Fatalf("unexpected assist tool call arguments: %#v", assistResp)
	}
	if !containsStringValue(nestedPath(assistResp, "assist", "warnings"), "city inferred from recent user message") {
		t.Fatalf("unexpected assist warnings: %#v", assistResp)
	}
	if provider.chatCompletionsCalls != 1 {
		t.Fatalf("expected one kirari chat completions call, got %d", provider.chatCompletionsCalls)
	}
	assertAuditCountForActor(t, env, "lab-user", "user.tool_call", "workspace_tool_call", requestID, "assist", "success", 1)
	assistMetadata := latestAuditMetadataForActorAction(t, env, "lab-user", "user.tool_call", "assist", "success")
	if nestedString(assistMetadata, "provider") != "kirari" || nestedString(assistMetadata, "model") != "kirari-model" {
		t.Fatalf("unexpected tool call assist audit metadata: %#v", assistMetadata)
	}
	if nestedString(assistMetadata, "tool_name") != "lookup_weather" || !nestedPathBool(map[string]any{"metadata": assistMetadata}, "metadata", "valid_draft") {
		t.Fatalf("unexpected tool call assist draft audit metadata: %#v", assistMetadata)
	}
	if nestedString(assistMetadata, "issuer_url") != provider.Issuer() || nestedString(assistMetadata, "subject") != "kirari-test-sub" {
		t.Fatalf("unexpected kirari audit identity metadata: %#v", assistMetadata)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestWorkspaceToolCallAssistParseLifecycle(t *testing.T) {
	env := newTestEnv(t)

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-browser-assist-parse",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "请准备查询北京天气的 tool call"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_weather",
					"description": "Lookup the weather.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
							"unit": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "请准备查询北京天气的 tool call")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))

	parseResp := env.postJSON(t, "/api/workspace/tool-call/assist/parse", map[string]any{
		"provider":   "browser_upstream",
		"model":      "demo-local-model",
		"request_id": requestID,
		"raw_output": "```json\n{\"explanation\":\"建议调用天气工具查询北京天气。\",\"tool_call\":{\"name\":\"lookup_weather\",\"arguments\":{\"city\":\"Beijing\",\"unit\":\"c\"}},\"confidence\":\"high\",\"warnings\":[\"city inferred from recent user message\"]}\n```",
	}, http.StatusOK)
	if nestedPathString(parseResp, "assist", "provider") != "browser_upstream" || nestedPathString(parseResp, "assist", "model") != "demo-local-model" {
		t.Fatalf("unexpected assist parse provider/model: %#v", parseResp)
	}
	if !nestedPathBool(parseResp, "assist", "valid_draft") {
		t.Fatalf("expected valid parsed draft: %#v", parseResp)
	}
	if nestedPathString(parseResp, "assist", "tool_call", "name") != "lookup_weather" {
		t.Fatalf("unexpected parsed tool call name: %#v", parseResp)
	}
	if !containsStringValue(nestedPath(parseResp, "assist", "warnings"), "city inferred from recent user message") {
		t.Fatalf("unexpected parsed warnings: %#v", parseResp)
	}
	assertAuditCountForActor(t, env, "lab-user", "user.tool_call", "workspace_tool_call", requestID, "assist_parse", "success", 1)

	invalidResp := env.postJSON(t, "/api/workspace/tool-call/assist/parse", map[string]any{
		"provider":   "browser_upstream",
		"model":      "demo-local-model",
		"request_id": requestID,
		"raw_output": "{\"explanation\":\"wrong tool\",\"tool_call\":{\"name\":\"not_declared\",\"arguments\":{}}}",
	}, http.StatusOK)
	if nestedPathBool(invalidResp, "assist", "valid_draft") {
		t.Fatalf("expected invalid parsed draft for undeclared tool: %#v", invalidResp)
	}
	if !strings.Contains(fmt.Sprintf("%v", nestedPath(invalidResp, "assist", "validation_errors")), "not_declared") {
		t.Fatalf("expected undeclared tool validation error: %#v", invalidResp)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestWorkspaceToolCallAssistKirariStreamLifecycle(t *testing.T) {
	provider := newTestKirariProvider(t)
	provider.chatCompletionsStreamBody = strings.Join([]string{
		`data: {"choices":[{"delta":{"content":"{\"explanation\":\"使用天气查询工具读取北京天气。\""}}]}`,
		"",
		`data: {"choices":[{"delta":{"content":",\"tool_call\":{\"name\":\"lookup_weather\",\"arguments\":{\"city\":\"Beijing\",\"unit\":\"c\"}},\"confidence\":\"high\",\"warnings\":[\"city inferred from recent user message\"]}"}}]}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.KirariEnabled = true
		cfg.KirariIssuerURL = provider.Issuer()
		cfg.KirariClientID = "chatapi"
		cfg.KirariClientSecret = "secret"
		cfg.KirariRedirectURL = "http://chat.example.com/api/integrations/kirari/callback"
		cfg.KirariAllowedIssuers = []string{provider.Issuer()}
		cfg.KirariScopes = []string{"openid", "profile", "email", "offline_access", "llm:read", "llm:stream"}
	})

	resultCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "demo-kirari-assist-stream",
		"input": []map[string]any{
			{
				"type": "message",
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "帮我查一下北京天气，先准备 tool call"},
				},
			},
		},
		"tools": []map[string]any{
			{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup_weather",
					"description": "Lookup the weather.",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
							"unit": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	})
	conversation := env.waitForWaitingConversation(t, "帮我查一下北京天气，先准备 tool call")
	requestID := env.requestIDForConversation(t, conversation["id"].(string))

	_, location, cookies := env.getRedirect(t, "/api/user/integrations/kirari/connect")
	locationURL, err := neturl.Parse(location)
	if err != nil {
		t.Fatalf("parse kirari redirect: %v", err)
	}
	provider.idTokenClaims["nonce"] = locationURL.Query().Get("nonce")
	env.getJSONAndCookiesWithCookies(t, "/api/integrations/kirari/callback?code=kirari-code&state="+neturl.QueryEscape(locationURL.Query().Get("state")), cookies, http.StatusOK)

	status, streamBody := env.postStreamText(t, "/api/workspace/tool-call/assist/stream", map[string]any{
		"provider":   "kirari",
		"model":      "kirari-model",
		"request_id": requestID,
	})
	if status != http.StatusOK {
		t.Fatalf("unexpected assist stream status: %d body=%q", status, streamBody)
	}
	if !strings.Contains(streamBody, "event: assist.started") ||
		!strings.Contains(streamBody, "event: assist.delta") ||
		!strings.Contains(streamBody, "event: assist.completed") {
		t.Fatalf("unexpected assist stream lifecycle: %q", streamBody)
	}
	if !strings.Contains(streamBody, `"valid_draft":true`) ||
		!strings.Contains(streamBody, `"lookup_weather"`) ||
		!strings.Contains(streamBody, `"Beijing"`) {
		t.Fatalf("unexpected assist stream payload: %q", streamBody)
	}
	assertAuditCountForActor(t, env, "lab-user", "user.tool_call", "workspace_tool_call", requestID, "assist_stream", "success", 1)
	assistMetadata := latestAuditMetadataForActorAction(t, env, "lab-user", "user.tool_call", "assist_stream", "success")
	if nestedString(assistMetadata, "provider") != "kirari" || nestedString(assistMetadata, "model") != "kirari-model" {
		t.Fatalf("unexpected stream assist audit metadata: %#v", assistMetadata)
	}
	if nestedString(assistMetadata, "tool_name") != "lookup_weather" || !nestedPathBool(map[string]any{"metadata": assistMetadata}, "metadata", "valid_draft") {
		t.Fatalf("unexpected stream assist draft audit metadata: %#v", assistMetadata)
	}

	env.postJSON(t, "/api/conversations/"+conversation["id"].(string)+"/respond", map[string]any{
		"text": "done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-resultCh
}

func TestWorkspaceToolCallAssistKirariRequiresConnection(t *testing.T) {
	provider := newTestKirariProvider(t)
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.KirariEnabled = true
		cfg.KirariIssuerURL = provider.Issuer()
		cfg.KirariClientID = "chatapi"
		cfg.KirariClientSecret = "secret"
		cfg.KirariRedirectURL = "http://chat.example.com/api/integrations/kirari/callback"
		cfg.KirariAllowedIssuers = []string{provider.Issuer()}
	})

	conversation, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_kirari_unconnected",
		RequestID:      "req_kirari_unconnected",
		ResponseID:     "resp_kirari_unconnected",
		OwnerID:        "lab-user",
		RequestFormat:  "responses",
		Model:          "kirari-model",
		UserContent:    "kirari assist without connection",
		RequestBody:    map[string]any{"model": "kirari-model"},
		ToolSchemas: []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":       "lookup_weather",
					"parameters": map[string]any{"type": "object"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("create pending turn: %v", err)
	}
	if conversation.ID == "" {
		t.Fatalf("expected conversation id for kirari unconnected test")
	}

	status, body := env.postText(t, "/api/workspace/tool-call/assist", map[string]any{
		"provider":   "kirari",
		"model":      "kirari-model",
		"request_id": "req_kirari_unconnected",
	})
	if status != http.StatusConflict || !strings.Contains(body, service.ErrKirariNotConnected.Error()) {
		t.Fatalf("expected kirari not connected rejection: status=%d body=%q", status, body)
	}
	assertAuditCountForActor(t, env, "lab-user", "user.tool_call", "workspace_tool_call", "req_kirari_unconnected", "assist", "failure", 1)
	assistMetadata := latestAuditMetadataForActorAction(t, env, "lab-user", "user.tool_call", "assist", "failure")
	if nestedString(assistMetadata, "provider") != "kirari" || nestedString(assistMetadata, "model") != "kirari-model" {
		t.Fatalf("unexpected failed tool call assist audit metadata: %#v", assistMetadata)
	}
	if !strings.Contains(nestedString(assistMetadata, "error"), service.ErrKirariNotConnected.Error()) {
		t.Fatalf("unexpected failed tool call assist audit error metadata: %#v", assistMetadata)
	}
}

func TestConfigModelsRoutesAndModelsEndpoint(t *testing.T) {
	env := newTestEnv(t)

	schemaResp := env.getJSON(t, "/api/config/models/schema", http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	if !containsMapItemWithStringField(schema["create_fields"], "name", "id") || !containsMapItemWithStringField(schema["create_fields"], "name", "name") {
		t.Fatalf("unexpected config models schema response: %#v", schemaResp)
	}

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

func TestConfigModelsRejectMissingRequiredFields(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.postText(t, "/api/config/models", map[string]any{
		"name": "Missing ID",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "virtual model id is required") {
		t.Fatalf("expected missing id rejection: status=%d body=%q", status, body)
	}

	status, body = env.postText(t, "/api/config/models", map[string]any{
		"id": "demo-missing-name",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "virtual model name is required") {
		t.Fatalf("expected missing name rejection: status=%d body=%q", status, body)
	}
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
		"title_enabled":                   true,
		"title":                           "ChatAPI Test",
		"public_statistics":               true,
		"ntfy_private_url_policy":         "admin",
		"storage_block_new_conversations": true,
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
	if !nestedPathBool(updateResp, "storage_block_new_conversations") {
		t.Fatalf("unexpected storage_block_new_conversations in updated config: %#v", updateResp)
	}

	getResp := env.getJSON(t, "/api/config/system", http.StatusOK)
	if !nestedPathBool(getResp, "public_statistics") || nestedPathString(getResp, "registration_email_domains") != "example.com,example.org" {
		t.Fatalf("unexpected persisted system config: %#v", getResp)
	}
	assertAuditCount(t, env, "admin.config", "system_settings", "", "update", "success", 1)
}

func TestConfigSystemSchemaRoute(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/api/config/system/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if !containsMapItemWithStringField(schema["fields"], "key", "public_statistics") {
		t.Fatalf("unexpected system settings schema fields: %#v", resp)
	}
	if !containsMapItemWithStringField(schema["fields"], "key", "image_usage") {
		t.Fatalf("expected image_usage in system settings schema: %#v", resp)
	}
	if !containsMapItemWithStringField(schema["fields"], "key", "storage_block_new_conversations") {
		t.Fatalf("expected storage_block_new_conversations in system settings schema: %#v", resp)
	}
}

func TestProtocolRequestsRejectNewConversationsWhenStorageOverQuota(t *testing.T) {
	env := newTestEnv(t)

	uploadResp := env.postMultipart(t, "/api/uploads/imgs", "file", "quota.png", tinyPNG(), http.StatusOK)
	if nestedPathString(uploadResp, "upload", "filename") == "" {
		t.Fatalf("expected uploaded file before quota block test: %#v", uploadResp)
	}
	env.putJSON(t, "/api/admin/storage/users/lab-user/quota", map[string]any{
		"quota_bytes": len(tinyPNG()) - 1,
	}, http.StatusOK)
	env.postJSON(t, "/api/config/system", map[string]any{
		"storage_block_new_conversations": true,
	}, http.StatusOK)

	cases := []struct {
		name         string
		path         string
		body         map[string]any
		validateBody func(t *testing.T, payload map[string]any)
	}{
		{
			name: "responses",
			path: "/v1/responses",
			body: map[string]any{
				"model": "quota-block-model",
				"input": "new conversation should be blocked",
			},
			validateBody: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if nestedPathString(payload, "error", "code") != "storage_quota_exceeded" {
					t.Fatalf("unexpected responses quota error payload: %#v", payload)
				}
			},
		},
		{
			name: "chat_completions_stream",
			path: "/v1/chat/completions",
			body: map[string]any{
				"model":  "quota-block-model",
				"stream": true,
				"messages": []map[string]any{
					{"role": "user", "content": "streamed conversation should be blocked"},
				},
			},
			validateBody: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if nestedPathString(payload, "error", "type") != "insufficient_storage" {
					t.Fatalf("unexpected chat completions quota error payload: %#v", payload)
				}
			},
		},
		{
			name: "anthropic_messages",
			path: "/v1/messages",
			body: map[string]any{
				"model": "quota-block-model",
				"messages": []map[string]any{
					{
						"role": "user",
						"content": []map[string]any{
							{"type": "text", "text": "anthropic conversation should be blocked"},
						},
					},
				},
			},
			validateBody: func(t *testing.T, payload map[string]any) {
				t.Helper()
				if nestedPathString(payload, "type") != "error" || nestedPathString(payload, "error", "type") != "insufficient_storage" {
					t.Fatalf("unexpected anthropic quota error payload: %#v", payload)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := postExternalText(t, env.server.URL+tc.path, nil, tc.body)
			if status != http.StatusInsufficientStorage {
				t.Fatalf("expected insufficient storage rejection: status=%d body=%q", status, body)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(body), &payload); err != nil {
				t.Fatalf("decode quota rejection payload: %v body=%q", err, body)
			}
			tc.validateBody(t, payload)
		})
	}

	conversations, err := env.store.ListConversations(context.Background())
	if err != nil {
		t.Fatalf("list conversations after quota rejection: %v", err)
	}
	if len(conversations) != 0 {
		t.Fatalf("expected no conversations to be created when quota blocks new conversations: %#v", conversations)
	}
}

func TestAdminSendTestEmailRouteRejectsInvalidSMTPConfig(t *testing.T) {
	env := newTestEnv(t)

	schemaResp := env.getJSON(t, "/api/admin/send-test-email/schema", http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 1 || nestedString(operations[0].(map[string]any), "name") != "send_test_email" {
		t.Fatalf("unexpected admin email schema response: %#v", schemaResp)
	}

	status, body := env.postText(t, "/api/admin/send-test-email", map[string]any{
		"email": "admin@example.com",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "email config invalid") {
		t.Fatalf("expected invalid smtp config error: status=%d body=%q", status, body)
	}
	assertAuditCount(t, env, "admin.email", "smtp", "", "send_test_email", "failure", 1)

	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)
	status, body = env.getTextWithHeaders(t, "/api/admin/send-test-email/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin email schema rejection: status=%d body=%q", status, body)
	}
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

func TestConfigAutomationRulesRejectInvalidTypedCondition(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.postText(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_invalid_typed",
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
		},
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid automation rule") {
		t.Fatalf("expected invalid automation rule rejection: status=%d body=%q", status, body)
	}
}

func TestConfigAutomationRulesSchemaRoute(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/api/config/automation-rules/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	typed := schema["typed_condition_types"].([]any)
	if len(typed) == 0 || nestedString(typed[0].(map[string]any), "type") == "" {
		t.Fatalf("unexpected automation schema response: %#v", resp)
	}
	if !containsStringValue(schema["action_types"], "output_text") || !containsStringValue(schema["legacy_fields"], "tool_choice.name") {
		t.Fatalf("unexpected automation schema response: %#v", resp)
	}
}

func TestAutomationRuleAutoCompletesResponsesRequest(t *testing.T) {
	env := newTestEnv(t)

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_auto_complete",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "auto hello"}},
					"excludes": []map[string]any{},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "自动化命中回复",
				},
			},
		},
	}, http.StatusOK)

	resp := postExternalJSON(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "demo-auto-rule",
		"input": "auto hello from rule",
	})
	if nestedString(resp, "output_text") != "自动化命中回复" {
		t.Fatalf("unexpected automation response payload: %#v", resp)
	}

	requests := env.getJSON(t, "/lab/requests", http.StatusOK)
	items := requests["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected request item after automation completion: %#v", requests)
	}
	foundClosed := false
	for _, item := range items {
		record := item.(map[string]any)
		if nestedString(record, "input_text") == "auto hello from rule" {
			foundClosed = nestedString(record, "status") == "completed" || nestedString(record, "status") == "closed"
			break
		}
	}
	if !foundClosed {
		t.Fatalf("expected automation-completed request in list: %#v", requests)
	}
	assertAuditCount(t, env, "automation.rule", "automation_rule", "rule_auto_complete", "auto_complete", "success", 1)
}

func TestLabRequestsSchema(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/lab/requests/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 6 {
		t.Fatalf("unexpected lab requests schema response: %#v", resp)
	}
	if !containsMapItemWithStringField(schema["parsed_detail_fields"], "key", "request_method") ||
		!containsMapItemWithStringField(schema["replay_fields"], "key", "curl") {
		t.Fatalf("unexpected lab parsed/replay schema metadata: %#v", resp)
	}
	if nestedString(operations[0].(map[string]any), "name") != "list_requests" ||
		nestedString(operations[2].(map[string]any), "name") != "copy_request_curl" ||
		nestedString(operations[5].(map[string]any), "name") != "request_abort" {
		t.Fatalf("unexpected lab requests schema operations: %#v", resp)
	}
}

func TestLabAccessPasswordBootstrapAndHTMLPage(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.LabPassword = "dev-password"
	})

	status, body := env.getText(t, "/api/health")
	if status != http.StatusUnauthorized || !strings.Contains(body, "lab access denied") {
		t.Fatalf("expected gated lab health rejection: status=%d body=%q", status, body)
	}

	htmlStatus, htmlBody, _ := env.getTextAndCookiesWithCookies(t, "/", nil)
	if htmlStatus != http.StatusUnauthorized || !strings.Contains(htmlBody, "ChatAPI Lab") || !strings.Contains(htmlBody, "name=\"password\"") {
		t.Fatalf("expected lab password page: status=%d body=%q", htmlStatus, htmlBody)
	}

	bootstrapStatus, bootstrapBody, cookies := env.getTextAndCookiesWithCookies(t, "/api/health?password=dev-password", nil)
	if bootstrapStatus != http.StatusOK || !strings.Contains(bootstrapBody, "\"ok\":true") {
		t.Fatalf("expected lab password bootstrap success: status=%d body=%q", bootstrapStatus, bootstrapBody)
	}
	labCookie := findCookie(cookies, "chatapi_lab_access")
	if labCookie == nil || labCookie.Value == "" {
		t.Fatalf("expected lab access cookie after bootstrap: %#v", cookies)
	}

	againStatus, againBody, _ := env.getTextAndCookiesWithCookies(t, "/api/health", []*http.Cookie{labCookie})
	if againStatus != http.StatusOK || !strings.Contains(againBody, "\"ok\":true") {
		t.Fatalf("expected lab cookie access success: status=%d body=%q", againStatus, againBody)
	}
}

func TestLabAccessTokenIsSingleUse(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.LabToken = "lab-token"
	})

	firstStatus, firstBody, cookies := env.getTextAndCookiesWithCookies(t, "/api/health?token=lab-token", nil)
	if firstStatus != http.StatusOK || !strings.Contains(firstBody, "\"ok\":true") {
		t.Fatalf("expected first lab token use to pass: status=%d body=%q", firstStatus, firstBody)
	}
	labCookie := findCookie(cookies, "chatapi_lab_access")
	if labCookie == nil || labCookie.Value == "" {
		t.Fatalf("expected lab cookie after token bootstrap: %#v", cookies)
	}

	secondStatus, secondBody := env.getText(t, "/api/health?token=lab-token")
	if secondStatus != http.StatusUnauthorized || !strings.Contains(secondBody, "lab access denied") {
		t.Fatalf("expected single-use token rejection: status=%d body=%q", secondStatus, secondBody)
	}

	cookieStatus, cookieBody, _ := env.getTextAndCookiesWithCookies(t, "/api/health", []*http.Cookie{labCookie})
	if cookieStatus != http.StatusOK || !strings.Contains(cookieBody, "\"ok\":true") {
		t.Fatalf("expected lab cookie to keep working after token use: status=%d body=%q", cookieStatus, cookieBody)
	}
}

func TestAutomationRuleAutoCompletesStructuredResponsesRequest(t *testing.T) {
	env := newTestEnv(t)

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_auto_structured",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{
						{"field": "protocol", "match_type": "exact", "pattern": "responses"},
						{"field": "model", "match_type": "exact", "pattern": "demo-auto-structured"},
						{"field": "tool_choice.name", "match_type": "exact", "pattern": "lookup_weather"},
						{"field": "response_format.name", "match_type": "exact", "pattern": "tool_draft"},
						{"field": "input_part.type", "match_type": "exact", "pattern": "image"},
					},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "结构化自动命中回复",
				},
			},
		},
	}, http.StatusOK)

	resp := postExternalJSON(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "demo-auto-structured",
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "请帮我准备 tool call"},
					{"type": "input_image", "image_url": "https://example.com/demo.png", "media_type": "image/png"},
				},
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
			"name": "lookup_weather",
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "tool_draft",
				"schema": map[string]any{"type": "object"},
			},
		},
	})
	if nestedString(resp, "output_text") != "结构化自动命中回复" {
		t.Fatalf("unexpected structured automation response payload: %#v", resp)
	}
	assertAuditCount(t, env, "automation.rule", "automation_rule", "rule_auto_structured", "auto_complete", "success", 1)
}

func TestAutomationRuleAutoCompletesTypedStructuredResponsesRequest(t *testing.T) {
	env := newTestEnv(t)

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_auto_typed_structured",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{
						{"type": "text_contains", "value": "tool draft"},
						{"type": "model_is", "value": "demo-auto-typed"},
						{"type": "protocol_is", "value": "responses"},
						{"type": "tool_choice_is", "name": "lookup_weather", "choice_type": "function"},
						{"type": "response_format_is", "name": "tool_draft", "format_type": "json_schema"},
						{"type": "input_part_type_is", "value": "image"},
						{"type": "input_media_type_contains", "value": "png"},
					},
					"excludes": []map[string]any{
						{"type": "input_url_contains", "value": "blocked.example"},
					},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "类型化结构命中回复",
				},
			},
		},
	}, http.StatusOK)

	resp := postExternalJSON(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "demo-auto-typed",
		"input": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "input_text", "text": "please prepare a tool draft"},
					{"type": "input_image", "image_url": "https://example.com/demo.png", "media_type": "image/png"},
				},
			},
		},
		"tool_choice": map[string]any{
			"type": "function",
			"name": "lookup_weather",
		},
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "tool_draft",
				"schema": map[string]any{"type": "object"},
			},
		},
	})
	if nestedString(resp, "output_text") != "类型化结构命中回复" {
		t.Fatalf("unexpected typed structured automation response payload: %#v", resp)
	}
	assertAuditCount(t, env, "automation.rule", "automation_rule", "rule_auto_typed_structured", "auto_complete", "success", 1)
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

func TestTOTPSetupConfirmLoginAndReset(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)

	hash, err := passwordhash.Hash("totp-secret")
	if err != nil {
		t.Fatalf("hash totp password: %v", err)
	}
	user, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "totp-user",
		Username:     "totp-user",
		Email:        "totp@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	})
	if err != nil {
		t.Fatalf("seed totp user: %v", err)
	}

	loginResp, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": user.Username,
		"password": "totp-secret",
	}, http.StatusOK)
	if nestedPathString(loginResp, "user", "id") != user.ID {
		t.Fatalf("unexpected initial login response: %#v", loginResp)
	}
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing session cookie after initial login: %#v", cookies)
	}

	setupResp := env.getJSONWithCookie(t, "/api/auth/totp/setup", sessionCookie, http.StatusOK)
	secret := nestedPathString(setupResp, "secret")
	if secret == "" || nestedPathString(setupResp, "uri") == "" || nestedPathString(setupResp, "qr_base64") == "" {
		t.Fatalf("unexpected totp setup response: %#v", setupResp)
	}

	configResp := env.getJSONWithCookie(t, "/api/user/config", sessionCookie, http.StatusOK)
	if _, exists := configResp["security.totp"]; exists {
		t.Fatalf("sensitive totp config leaked to user config response: %#v", configResp)
	}

	code, err := totp.GenerateCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("generate totp code: %v", err)
	}
	_, _ = env.postJSONWithCookieAndHeaders(t, "/api/auth/totp/confirm", map[string]any{
		"secret": secret,
		"code":   code,
	}, sessionCookie, map[string]string{"Origin": env.server.URL}, http.StatusOK)

	sessionResp := env.getJSONWithCookie(t, "/api/auth/session", sessionCookie, http.StatusOK)
	if !nestedPathBool(sessionResp, "totp_enabled") {
		t.Fatalf("expected session totp_enabled after confirm: %#v", sessionResp)
	}

	status, body := env.postText(t, "/api/auth/login", map[string]any{
		"username": user.Username,
		"password": "totp-secret",
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "totp code is required") || !strings.Contains(body, "totp_required") {
		t.Fatalf("expected totp login challenge: status=%d body=%q", status, body)
	}

	loginCode, err := totp.GenerateCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("generate second totp code: %v", err)
	}
	challengedLoginResp, challengedCookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": user.Username,
		"password": "totp-secret",
		"totp":     loginCode,
	}, http.StatusOK)
	if nestedPathString(challengedLoginResp, "user", "id") != user.ID {
		t.Fatalf("unexpected totp login response: %#v", challengedLoginResp)
	}
	totpSessionCookie := findCookie(challengedCookies, service.SessionCookieName)
	if totpSessionCookie == nil {
		t.Fatalf("missing session cookie after totp login: %#v", challengedCookies)
	}

	_, _ = env.postJSONWithCookieAndHeaders(t, "/api/auth/totp/reset", map[string]any{}, totpSessionCookie, map[string]string{"Origin": env.server.URL}, http.StatusOK)
	sessionAfterReset := env.getJSONWithCookie(t, "/api/auth/session", totpSessionCookie, http.StatusOK)
	if nestedPathBool(sessionAfterReset, "totp_enabled") {
		t.Fatalf("expected totp to be disabled after reset: %#v", sessionAfterReset)
	}

	assertAuditCountForActor(t, env, user.ID, "auth.session", "session", user.ID, "totp_setup", "success", 1)
	assertAuditCountForActor(t, env, user.ID, "auth.session", "session", user.ID, "totp_confirm", "success", 1)
	assertAuditCountForActor(t, env, user.ID, "auth.session", "session", user.ID, "totp_reset", "success", 1)
}

func TestRegistrationWithEmailVerification(t *testing.T) {
	smtpServer := newFakeSMTPServer(t)
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.SMTPEnabled = true
		cfg.SMTPHost = smtpServer.host
		cfg.SMTPPort = smtpServer.port
		cfg.SMTPSecurity = "none"
		cfg.SMTPFrom = "noreply@example.com"
		cfg.SMTPTimeout = 10 * time.Second
	})
	if _, err := env.store.SetSystemConfig(context.Background(), store.SetSystemConfigInput{
		Key: "system_settings",
		Value: map[string]any{
			"external_registration_enabled":                 true,
			"email_verification_enabled":                    true,
			"registration_email_domain_restriction_enabled": true,
			"registration_email_domains":                    "example.com",
		},
	}); err != nil {
		t.Fatalf("seed system settings: %v", err)
	}

	configResp := env.getJSON(t, "/api/auth/register/config", http.StatusOK)
	if !nestedPathBool(configResp, "registration_enabled") || !nestedPathBool(configResp, "email_verification_enabled") {
		t.Fatalf("unexpected register config: %#v", configResp)
	}

	status, body := env.postText(t, "/api/auth/register/send-code", map[string]any{
		"email": "newuser@example.com",
	})
	if status != http.StatusOK {
		t.Fatalf("unexpected register send-code response: status=%d body=%q", status, body)
	}
	status, body = env.postText(t, "/api/auth/register/send-code", map[string]any{
		"email": "newuser@example.com",
	})
	if status != http.StatusTooManyRequests || !strings.Contains(body, "rate limited") {
		t.Fatalf("expected register send-code rate limit: status=%d body=%q", status, body)
	}
	code := smtpServer.waitForLatestCode(t)

	registerResp := env.postJSON(t, "/api/auth/register", map[string]any{
		"email":    "newuser@example.com",
		"password": "register-secret",
		"code":     code,
	}, http.StatusOK)
	userID := nestedPathString(registerResp, "user", "id")
	if userID == "" {
		t.Fatalf("unexpected register response: %#v", registerResp)
	}

	loginResp, _ := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "newuser@example.com",
		"password": "register-secret",
	}, http.StatusOK)
	if nestedPathString(loginResp, "user", "id") != userID {
		t.Fatalf("unexpected login after register: %#v", loginResp)
	}
}

func TestPasswordResetWithEmailCode(t *testing.T) {
	smtpServer := newFakeSMTPServer(t)
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.SMTPEnabled = true
		cfg.SMTPHost = smtpServer.host
		cfg.SMTPPort = smtpServer.port
		cfg.SMTPSecurity = "none"
		cfg.SMTPFrom = "noreply@example.com"
		cfg.SMTPTimeout = 10 * time.Second
	})
	hash, err := passwordhash.Hash("old-reset-secret")
	if err != nil {
		t.Fatalf("hash reset password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "reset-user",
		Username:     "reset-user@example.com",
		Email:        "reset-user@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed reset user: %v", err)
	}

	configResp := env.getJSON(t, "/api/auth/password/config", http.StatusOK)
	if !nestedPathBool(configResp, "password_reset_enabled") {
		t.Fatalf("unexpected password config: %#v", configResp)
	}

	status, body := env.postText(t, "/api/auth/password/send-code", map[string]any{
		"email": "reset-user@example.com",
	})
	if status != http.StatusOK {
		t.Fatalf("unexpected password send-code response: status=%d body=%q", status, body)
	}
	code := smtpServer.waitForLatestCode(t)

	env.postJSON(t, "/api/auth/password/reset", map[string]any{
		"email":    "reset-user@example.com",
		"code":     code,
		"password": "new-reset-secret",
	}, http.StatusOK)

	loginResp, _ := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "reset-user@example.com",
		"password": "new-reset-secret",
	}, http.StatusOK)
	if nestedPathString(loginResp, "user", "id") != "reset-user" {
		t.Fatalf("unexpected login after password reset: %#v", loginResp)
	}
}

func TestPasswordResetCodeTooManyAttempts(t *testing.T) {
	smtpServer := newFakeSMTPServer(t)
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.SMTPEnabled = true
		cfg.SMTPHost = smtpServer.host
		cfg.SMTPPort = smtpServer.port
		cfg.SMTPSecurity = "none"
		cfg.SMTPFrom = "noreply@example.com"
		cfg.SMTPTimeout = 10 * time.Second
	})
	hash, err := passwordhash.Hash("attempt-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "attempt-user",
		Username:     "attempt-user@example.com",
		Email:        "attempt-user@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed attempt user: %v", err)
	}

	status, body := env.postText(t, "/api/auth/password/send-code", map[string]any{
		"email": "attempt-user@example.com",
	})
	if status != http.StatusOK {
		t.Fatalf("unexpected send-code response: status=%d body=%q", status, body)
	}
	code := smtpServer.waitForLatestCode(t)

	for i := 0; i < service.EmailCodeMaxFailedAttempts(); i++ {
		status, body = env.postText(t, "/api/auth/password/reset", map[string]any{
			"email":    "attempt-user@example.com",
			"code":     "000000",
			"password": "new-attempt-secret",
		})
		if status != http.StatusBadRequest || !strings.Contains(body, "invalid") {
			t.Fatalf("expected invalid code on attempt %d: status=%d body=%q", i+1, status, body)
		}
	}

	status, body = env.postText(t, "/api/auth/password/reset", map[string]any{
		"email":    "attempt-user@example.com",
		"code":     code,
		"password": "new-attempt-secret",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "too many failed attempts") {
		t.Fatalf("expected too many attempts rejection: status=%d body=%q", status, body)
	}
}

func TestLoginRequiresGeeTestWhenEnabled(t *testing.T) {
	geetestServer := newFakeGeeTestServer(t)
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.GeetestCaptchaID = "captcha-id"
		cfg.GeetestCaptchaKey = "captcha-key"
		cfg.GeetestAPIServer = geetestServer.server.URL
	})
	hash, err := passwordhash.Hash("login-secret")
	if err != nil {
		t.Fatalf("hash login password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "geetest-login-user",
		Username:     "geetest-login@example.com",
		Email:        "geetest-login@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed login user: %v", err)
	}

	sessionResp := env.getJSON(t, "/api/auth/session", http.StatusOK)
	if !nestedPathBool(sessionResp, "geetest_enabled") || nestedPathString(sessionResp, "geetest_captcha_id") != "captcha-id" {
		t.Fatalf("unexpected auth session geetest flags: %#v", sessionResp)
	}

	status, body := env.postText(t, "/api/auth/login", map[string]any{
		"username": "geetest-login@example.com",
		"password": "login-secret",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "geetest verification is required") {
		t.Fatalf("expected geetest challenge on login: status=%d body=%q", status, body)
	}

	loginResp, _ := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username":       "geetest-login@example.com",
		"password":       "login-secret",
		"geetest_params": geetestServer.params(),
	}, http.StatusOK)
	if nestedPathString(loginResp, "user", "id") != "geetest-login-user" {
		t.Fatalf("unexpected login response: %#v", loginResp)
	}
}

func TestRegistrationSendCodeAndDirectRegisterRequireGeeTestWhenEnabled(t *testing.T) {
	geetestServer := newFakeGeeTestServer(t)
	smtpServer := newFakeSMTPServer(t)
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.SMTPEnabled = true
		cfg.SMTPHost = smtpServer.host
		cfg.SMTPPort = smtpServer.port
		cfg.SMTPSecurity = "none"
		cfg.SMTPFrom = "noreply@example.com"
		cfg.SMTPTimeout = 10 * time.Second
		cfg.GeetestCaptchaID = "captcha-id"
		cfg.GeetestCaptchaKey = "captcha-key"
		cfg.GeetestAPIServer = geetestServer.server.URL
	})
	if _, err := env.store.SetSystemConfig(context.Background(), store.SetSystemConfigInput{
		Key: "system_settings",
		Value: map[string]any{
			"external_registration_enabled": true,
			"email_verification_enabled":    true,
		},
	}); err != nil {
		t.Fatalf("seed register settings: %v", err)
	}

	configResp := env.getJSON(t, "/api/auth/register/config", http.StatusOK)
	if !nestedPathBool(configResp, "geetest_enabled") || nestedPathString(configResp, "geetest_captcha_id") != "captcha-id" {
		t.Fatalf("unexpected register config geetest flags: %#v", configResp)
	}

	status, body := env.postText(t, "/api/auth/register/send-code", map[string]any{
		"email": "geetest-register@example.com",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "geetest verification is required") {
		t.Fatalf("expected geetest challenge on register send-code: status=%d body=%q", status, body)
	}

	status, body = env.postText(t, "/api/auth/register/send-code", map[string]any{
		"email":          "geetest-register@example.com",
		"geetest_params": geetestServer.params(),
	})
	if status != http.StatusOK {
		t.Fatalf("unexpected register send-code response: status=%d body=%q", status, body)
	}

	envNoCode := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.GeetestCaptchaID = "captcha-id"
		cfg.GeetestCaptchaKey = "captcha-key"
		cfg.GeetestAPIServer = geetestServer.server.URL
	})
	if _, err := envNoCode.store.SetSystemConfig(context.Background(), store.SetSystemConfigInput{
		Key: "system_settings",
		Value: map[string]any{
			"external_registration_enabled": true,
			"email_verification_enabled":    false,
		},
	}); err != nil {
		t.Fatalf("seed direct register settings: %v", err)
	}

	status, body = envNoCode.postText(t, "/api/auth/register", map[string]any{
		"email":    "direct-register@example.com",
		"password": "direct-secret",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "geetest verification is required") {
		t.Fatalf("expected geetest challenge on direct register: status=%d body=%q", status, body)
	}
	registerResp := envNoCode.postJSON(t, "/api/auth/register", map[string]any{
		"email":          "direct-register@example.com",
		"password":       "direct-secret",
		"geetest_params": geetestServer.params(),
	}, http.StatusOK)
	if nestedPathString(registerResp, "user", "id") == "" {
		t.Fatalf("unexpected direct register response: %#v", registerResp)
	}
}

func TestPasswordSendCodeRequiresGeeTestWhenEnabled(t *testing.T) {
	geetestServer := newFakeGeeTestServer(t)
	smtpServer := newFakeSMTPServer(t)
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.SMTPEnabled = true
		cfg.SMTPHost = smtpServer.host
		cfg.SMTPPort = smtpServer.port
		cfg.SMTPSecurity = "none"
		cfg.SMTPFrom = "noreply@example.com"
		cfg.SMTPTimeout = 10 * time.Second
		cfg.GeetestCaptchaID = "captcha-id"
		cfg.GeetestCaptchaKey = "captcha-key"
		cfg.GeetestAPIServer = geetestServer.server.URL
	})
	hash, err := passwordhash.Hash("reset-secret")
	if err != nil {
		t.Fatalf("hash reset password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "geetest-reset-user",
		Username:     "geetest-reset@example.com",
		Email:        "geetest-reset@example.com",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed reset user: %v", err)
	}

	configResp := env.getJSON(t, "/api/auth/password/config", http.StatusOK)
	if !nestedPathBool(configResp, "geetest_enabled") || nestedPathString(configResp, "geetest_captcha_id") != "captcha-id" {
		t.Fatalf("unexpected password config geetest flags: %#v", configResp)
	}

	status, body := env.postText(t, "/api/auth/password/send-code", map[string]any{
		"email": "geetest-reset@example.com",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "geetest verification is required") {
		t.Fatalf("expected geetest challenge on password send-code: status=%d body=%q", status, body)
	}
	status, body = env.postText(t, "/api/auth/password/send-code", map[string]any{
		"email":          "geetest-reset@example.com",
		"geetest_params": geetestServer.params(),
	})
	if status != http.StatusOK {
		t.Fatalf("unexpected password send-code response: status=%d body=%q", status, body)
	}
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

	schemaResp := env.getJSONWithCookie(t, "/api/user/identities/schema", sessionCookie, http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 3 || nestedString(operations[0].(map[string]any), "name") != "list_identities" || nestedString(operations[1].(map[string]any), "name") != "link_identity" || nestedString(operations[2].(map[string]any), "name") != "unlink_identity" {
		t.Fatalf("unexpected user identities schema response: %#v", schemaResp)
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

	appKey := env.seedAppAPIKey(t, "user_identity_owner", []string{"requests:read"}, nil)
	status, body = env.getTextWithHeaders(t, "/api/user/identities/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
		t.Fatalf("expected app api key user identities schema rejection: status=%d body=%q", status, body)
	}
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

func TestUserModelAPIKeysSchema(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/api/user/model-api-keys/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if nestedString(schema, "key_prefix") != "sk-" {
		t.Fatalf("unexpected user model api key schema response: %#v", resp)
	}
	if !containsMapItemWithStringField(schema["create_fields"], "name", "model") {
		t.Fatalf("unexpected user model api key schema fields: %#v", resp)
	}
}

func TestUserModelAPIKeysRejectMissingModel(t *testing.T) {
	env := newTestEnv(t)

	status, body := env.postText(t, "/api/user/model-api-keys", map[string]any{
		"name": "missing-model",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "model is required") {
		t.Fatalf("expected missing model rejection: status=%d body=%q", status, body)
	}
}

func TestUserModelAPIKeysManagementUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("model-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_model_owner",
		Username:     "model-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed model owner: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "model-owner",
		"password": "model-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing user session cookie: %#v", cookies)
	}

	headers := map[string]string{"Origin": env.server.URL}
	createResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/user/model-api-keys", map[string]any{
		"name":  "session-managed-model-key",
		"model": "demo-session-model",
	}, sessionCookie, headers, http.StatusOK)
	keyID := nestedPathString(createResp, "item", "id")
	rawKey := nestedString(createResp, "raw_key")
	if keyID == "" || !strings.HasPrefix(rawKey, "sk-") {
		t.Fatalf("unexpected session model key create response: %#v", createResp)
	}

	listResp := env.getJSONWithCookie(t, "/api/user/model-api-keys", sessionCookie, http.StatusOK)
	items := listResp["items"].([]any)
	if len(items) != 1 || nestedString(items[0].(map[string]any), "id") != keyID || nestedString(items[0].(map[string]any), "raw_key") != rawKey {
		t.Fatalf("unexpected session model api key list: %#v", listResp)
	}

	status, body := env.deleteTextWithCookieAndHeaders(t, "/api/user/model-api-keys/"+keyID, sessionCookie, headers)
	if status != http.StatusOK || !strings.Contains(body, "\"ok\":true") {
		t.Fatalf("unexpected session model api key delete response: status=%d body=%q", status, body)
	}
	assertAuditCountForActor(t, env, "user_model_owner", "user.model_api_key", "model_api_key", keyID, "create", "success", 1)
	assertAuditCountForActor(t, env, "user_model_owner", "user.model_api_key", "model_api_key", keyID, "delete", "success", 1)
}

func TestUserModelAPIKeysSchemaUsesSessionActor(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("model-schema-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_model_schema_owner",
		Username:     "model-schema-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed model schema owner: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "model-schema-owner",
		"password": "model-schema-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("missing model schema session cookie: %#v", cookies)
	}

	resp := env.getJSONWithCookie(t, "/api/user/model-api-keys/schema", sessionCookie, http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if nestedString(schema, "key_prefix") != "sk-" {
		t.Fatalf("unexpected session model api key schema response: %#v", resp)
	}
}

func TestUserRoutesRejectAPIKeyActors(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeServe)
	hash, err := passwordhash.Hash("user-routes-secret")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_route_owner",
		Username:     "user-route-owner",
		PasswordHash: hash,
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed user route owner: %v", err)
	}
	appKey := env.seedAppAPIKey(t, "user_route_owner", []string{"requests:read"}, nil)
	modelKey := env.seedModelAPIKey(t, "user_route_owner", "user-routes-model", "demo-user-route-model")

	for _, testCase := range []struct {
		name   string
		path   string
		apiKey string
	}{
		{name: "app_api_key", path: "/api/user/config", apiKey: appKey},
		{name: "model_api_key", path: "/api/user/config", apiKey: modelKey},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			status, body := env.getTextWithHeaders(t, testCase.path, map[string]string{
				"Authorization": "Bearer " + testCase.apiKey,
			})
			if status != http.StatusUnauthorized || !strings.Contains(body, "session required") {
				t.Fatalf("expected user route rejection for %s: status=%d body=%q", testCase.name, status, body)
			}
		})
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

func TestAppAPIModelKeySchema(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"model_keys:read"}, nil)

	resp := env.appGetJSON(t, "/api/app/model-keys/schema", appKey, http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if nestedString(schema, "key_prefix") != "sk-" {
		t.Fatalf("unexpected app model key schema response: %#v", resp)
	}
	if !containsMapItemWithStringField(schema["create_fields"], "name", "model") {
		t.Fatalf("unexpected app model key schema fields: %#v", resp)
	}
}

func TestAppAPIModelKeysRejectMissingModel(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"model_keys:write"}, nil)

	status, body := env.appPostText(t, "/api/app/model-keys", appKey, map[string]any{
		"name": "missing-model",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "model is required") {
		t.Fatalf("expected missing model app api rejection: status=%d body=%q", status, body)
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
		"rules": []map[string]any{{
			"id":      "rule_blocked",
			"enabled": true,
			"action":  map[string]any{"type": "output_text", "text": "blocked"},
		}},
	})
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected automation resource rejection: status=%d body=%q", status, body)
	}

	putResp := env.appPutJSON(t, "/api/app/automation-rules", appKey, map[string]any{
		"rules": []map[string]any{{
			"id":      "rule_allowed",
			"enabled": false,
			"action":  map[string]any{"type": "output_text", "text": "allowed"},
		}},
	}, http.StatusOK)
	rules := putResp["rules"].([]any)
	if len(rules) != 1 || nestedString(rules[0].(map[string]any), "id") != "rule_allowed" {
		t.Fatalf("unexpected resource-limited automation response: %#v", putResp)
	}
}

func TestAppAPIAutomationRulesRejectInvalidTypedCondition(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"automation:write"}, nil)

	status, body := env.appPutText(t, "/api/app/automation-rules", appKey, map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_invalid_typed_app",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{
						{"type": "response_format_is"},
					},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "bad",
				},
			},
		},
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "invalid automation rule") {
		t.Fatalf("expected invalid typed automation rule rejection: status=%d body=%q", status, body)
	}
}

func TestAppAPIAutomationRulesSchema(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"automation:read"}, nil)

	resp := env.appGetJSON(t, "/api/app/automation-rules/schema", appKey, http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if !containsStringValue(schema["action_types"], "output_text") || !containsStringValue(schema["legacy_match_types"], "substring") {
		t.Fatalf("unexpected automation schema response: %#v", resp)
	}
}

func TestAppAPIAutomationRulesSchemaRejectsMissingScope(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)

	status, body := env.appGetText(t, "/api/app/automation-rules/schema", appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected automation schema scope rejection: status=%d body=%q", status, body)
	}
}

func TestAppAPIStatisticsSummary(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)
	otherKey := env.seedAppAPIKey(t, "other-user", []string{"statistics:read"}, nil)

	schemaResp := env.appGetJSON(t, "/api/app/statistics/schema", appKey, http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 2 || nestedString(operations[1].(map[string]any), "name") != "statistics_summary" {
		t.Fatalf("unexpected app statistics schema response: %#v", schemaResp)
	}

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_stats_auto",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "统计自动"}},
					"excludes": []map[string]any{},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "统计自动回复",
				},
			},
		},
	}, http.StatusOK)

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "stats-model-a",
		"input": "统计请求 A",
	})
	secondCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "stats-model-b",
		"input": "统计请求 B",
	})
	autoResp := postExternalJSON(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "stats-model-auto",
		"input": "统计自动 请求",
	})
	if nestedString(autoResp, "output_text") != "统计自动回复" {
		t.Fatalf("unexpected automation statistics response: %#v", autoResp)
	}

	firstConversation := env.waitForWaitingConversation(t, "统计请求 A")
	secondConversation := env.waitForWaitingConversation(t, "统计请求 B")
	env.postJSON(t, "/api/conversations/"+firstConversation["id"].(string)+"/respond", map[string]any{
		"text": "stats done",
		"mode": "assistant_message",
	}, http.StatusOK)
	<-firstCh

	resp := env.appGetJSON(t, "/api/app/statistics/summary", appKey, http.StatusOK)
	summary := resp["summary"].(map[string]any)
	if numericValue(summary["total_requests"]) != 3 || numericValue(summary["closed_requests"]) != 2 || numericValue(summary["pending_requests"]) != 1 || numericValue(summary["automation_hits"]) != 1 {
		t.Fatalf("unexpected statistics summary: %#v", resp)
	}
	byModel := summary["by_model"].(map[string]any)
	if numericValue(byModel["stats-model-a"]) != 1 || numericValue(byModel["stats-model-b"]) != 1 || numericValue(byModel["stats-model-auto"]) != 1 {
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

	status, body = env.appGetText(t, "/api/app/statistics/schema", appKey)
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected statistics schema scope rejection: status=%d body=%q", status, body)
	}

	statisticsOnly := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)
	status, body = env.appGetText(t, "/api/app/me/schema", statisticsOnly)
	if status != http.StatusForbidden || !strings.Contains(body, "app api key forbidden") {
		t.Fatalf("expected me schema scope rejection: status=%d body=%q", status, body)
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

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "metrics-no-rules",
		"input": "metrics no rules",
	})
	firstConversation := env.waitForWaitingConversation(t, "metrics no rules")
	status, body := env.postText(t, "/api/conversations/"+firstConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup",
	})
	if status != http.StatusOK {
		t.Fatalf("expected abort ok for metrics no rules request: status=%d body=%q", status, body)
	}
	<-firstCh

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "metrics_rule_never_match",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "never-hit"}},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "never",
				},
			},
		},
	}, http.StatusOK)
	secondCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "metrics-no-match",
		"input": "metrics no match",
	})
	secondConversation := env.waitForWaitingConversation(t, "metrics no match")
	status, body = env.postText(t, "/api/conversations/"+secondConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup",
	})
	if status != http.StatusOK {
		t.Fatalf("expected abort ok for metrics no match request: status=%d body=%q", status, body)
	}
	<-secondCh

	env.getJSON(t, "/api/health", http.StatusOK)
	status, body = env.getText(t, "/metrics")
	if status != http.StatusOK {
		t.Fatalf("expected metrics ok: status=%d body=%q", status, body)
	}
	for _, expected := range []string{
		"# HELP chatapi_go_goroutines",
		"chatapi_system_memory_total_bytes",
		"chatapi_process_open_fds",
		"chatapi_data_dir_disk_total_bytes",
		"chatapi_automation_failures_total",
		"chatapi_automation_no_rules_total",
		"chatapi_automation_no_match_total",
		"chatapi_automation_no_rules_total 1",
		"chatapi_automation_no_match_total 1",
		`chatapi_automation_rule_skips_total{reason="contains_miss"} 1`,
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

	firstCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "runtime-no-rules",
		"input": "runtime no rules",
	})
	firstConversation := env.waitForWaitingConversation(t, "runtime no rules")
	status, body := env.postText(t, "/api/conversations/"+firstConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup",
	})
	if status != http.StatusOK {
		t.Fatalf("expected abort ok for runtime no rules request: status=%d body=%q", status, body)
	}
	<-firstCh

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "runtime_rule_never_match",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "will-not-match"}},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "never",
				},
			},
		},
	}, http.StatusOK)
	secondCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "runtime-no-match",
		"input": "runtime no match",
	})
	secondConversation := env.waitForWaitingConversation(t, "runtime no match")
	status, body = env.postText(t, "/api/conversations/"+secondConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup",
	})
	if status != http.StatusOK {
		t.Fatalf("expected abort ok for runtime no match request: status=%d body=%q", status, body)
	}
	<-secondCh

	summaryResp := env.getJSON(t, "/api/admin/runtime/summary", http.StatusOK)
	summary := summaryResp["summary"].(map[string]any)
	if nestedPathString(summary, "go", "version") == "" || summary["system"] == nil || summary["memory"] == nil || summary["pending"] == nil || summary["realtime"] == nil {
		t.Fatalf("unexpected runtime summary response: %#v", summaryResp)
	}
	automation := summary["automation"].(map[string]any)
	if numericValue(automation["no_rules"]) != 1 || numericValue(automation["no_match"]) != 1 {
		t.Fatalf("unexpected runtime automation summary: %#v", summaryResp)
	}
	skipByReason := automation["skip_by_reason"].(map[string]any)
	if numericValue(skipByReason["contains_miss"]) != 1 {
		t.Fatalf("unexpected runtime automation skip summary: %#v", summaryResp)
	}
	skipByRule := automation["skip_by_rule"].(map[string]any)
	ruleSummary := skipByRule["runtime_rule_never_match"].(map[string]any)
	if numericValue(ruleSummary["total"]) != 1 {
		t.Fatalf("unexpected runtime automation rule summary: %#v", summaryResp)
	}
	ruleReasons := ruleSummary["by_reason"].(map[string]any)
	if numericValue(ruleReasons["contains_miss"]) != 1 {
		t.Fatalf("unexpected runtime automation rule reasons: %#v", summaryResp)
	}
	recentSkips := automation["recent_skips"].([]any)
	if len(recentSkips) == 0 {
		t.Fatalf("expected recent automation skip samples: %#v", summaryResp)
	}
	firstSkip := recentSkips[0].(map[string]any)
	if nestedString(firstSkip, "rule_id") != "runtime_rule_never_match" || nestedString(firstSkip, "reason") != "contains_miss" {
		t.Fatalf("unexpected recent automation skip sample: %#v", summaryResp)
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

	status, body = env.putText(t, "/api/admin/runtime/settings", map[string]any{
		"gogc": -1,
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "gogc must be non-negative") {
		t.Fatalf("expected runtime settings validation error: status=%d body=%q", status, body)
	}

	automationResp := env.getJSON(t, "/api/admin/runtime/automation", http.StatusOK)
	automationPayload := automationResp["automation"].(map[string]any)
	if numericValue(automationPayload["no_match"]) != 1 {
		t.Fatalf("unexpected runtime automation endpoint response: %#v", automationResp)
	}
	if _, ok := automationPayload["recent_skips"]; !ok {
		t.Fatalf("expected runtime automation endpoint recent skips: %#v", automationResp)
	}

	filteredResp := env.getJSON(t, "/api/admin/runtime/automation?rule_id=runtime_rule_never_match&reason=contains_miss&limit=1", http.StatusOK)
	filteredAutomation := filteredResp["automation"].(map[string]any)
	filteredRecent := filteredAutomation["recent_skips"].([]any)
	if len(filteredRecent) != 1 {
		t.Fatalf("expected filtered runtime automation samples: %#v", filteredResp)
	}
	filteredFirst := filteredRecent[0].(map[string]any)
	if nestedString(filteredFirst, "rule_id") != "runtime_rule_never_match" || nestedString(filteredFirst, "reason") != "contains_miss" {
		t.Fatalf("unexpected filtered runtime automation sample: %#v", filteredResp)
	}

	schemaResp := env.getJSON(t, "/api/admin/runtime/schema", http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 3 {
		t.Fatalf("unexpected runtime schema operations: %#v", schemaResp)
	}
	if nestedString(operations[0].(map[string]any), "name") != "automation_diagnostics" {
		t.Fatalf("unexpected first runtime schema operation: %#v", schemaResp)
	}
	if nestedString(operations[1].(map[string]any), "name") != "update_runtime_settings" {
		t.Fatalf("unexpected second runtime schema operation: %#v", schemaResp)
	}
	if nestedString(operations[2].(map[string]any), "name") != "force_gc" {
		t.Fatalf("unexpected third runtime schema operation: %#v", schemaResp)
	}
}

func TestAutomationSkipAudits(t *testing.T) {
	env := newTestEnv(t)

	noRulesCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "audit-no-rules",
		"input": "audit no rules",
	})
	noRulesConversation := env.waitForWaitingConversation(t, "audit no rules")
	status, body := env.postText(t, "/api/conversations/"+noRulesConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup",
	})
	if status != http.StatusOK {
		t.Fatalf("expected abort ok for no-rules request: status=%d body=%q", status, body)
	}
	<-noRulesCh

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "audit_rule_never_match",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "will-not-match"}},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "never",
				},
			},
		},
	}, http.StatusOK)
	noMatchCh := startJSONRequest(t, env.server.URL+"/v1/responses", map[string]any{
		"model": "audit-no-match",
		"input": "audit no match",
	})
	noMatchConversation := env.waitForWaitingConversation(t, "audit no match")
	status, body = env.postText(t, "/api/conversations/"+noMatchConversation["id"].(string)+"/abort", map[string]any{
		"error": "cleanup",
	})
	if status != http.StatusOK {
		t.Fatalf("expected abort ok for no-match request: status=%d body=%q", status, body)
	}
	<-noMatchCh

	var noRulesMetadataRaw string
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT metadata_json
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'automation.rule'
			AND action = 'auto_complete'
			AND outcome = 'skipped'
			AND json_extract(metadata_json, '$.reason') = 'no_rules'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`).Scan(&noRulesMetadataRaw); err != nil {
		t.Fatalf("select no-rules automation audit: %v", err)
	}
	var noRulesMetadata map[string]any
	if err := json.Unmarshal([]byte(noRulesMetadataRaw), &noRulesMetadata); err != nil {
		t.Fatalf("decode no-rules automation audit metadata: %v", err)
	}
	if nestedString(noRulesMetadata, "conversation_id") == "" || nestedString(noRulesMetadata, "model") != "audit-no-rules" {
		t.Fatalf("unexpected no-rules audit metadata: %#v", noRulesMetadata)
	}

	var noMatchMetadataRaw string
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT metadata_json
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'automation.rule'
			AND action = 'auto_complete'
			AND outcome = 'skipped'
			AND json_extract(metadata_json, '$.reason') = 'no_match'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`).Scan(&noMatchMetadataRaw); err != nil {
		t.Fatalf("select no-match automation audit: %v", err)
	}
	var noMatchMetadata map[string]any
	if err := json.Unmarshal([]byte(noMatchMetadataRaw), &noMatchMetadata); err != nil {
		t.Fatalf("decode no-match automation audit metadata: %v", err)
	}
	if numericValue(noMatchMetadata["skip_count"]) != 1 || nestedString(noMatchMetadata, "model") != "audit-no-match" {
		t.Fatalf("unexpected no-match audit metadata: %#v", noMatchMetadata)
	}
	if !containsStringValue(noMatchMetadata["skip_reasons"], "contains_miss") {
		t.Fatalf("unexpected no-match audit skip reasons: %#v", noMatchMetadata)
	}

	var ruleSkipMetadataRaw string
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT metadata_json
		FROM audit_logs
		WHERE actor_user_id = 'lab-user'
			AND event_type = 'automation.rule'
			AND resource_type = 'automation_rule'
			AND resource_id = 'audit_rule_never_match'
			AND action = 'rule_skip'
			AND outcome = 'skipped'
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`).Scan(&ruleSkipMetadataRaw); err != nil {
		t.Fatalf("select rule-skip automation audit: %v", err)
	}
	var ruleSkipMetadata map[string]any
	if err := json.Unmarshal([]byte(ruleSkipMetadataRaw), &ruleSkipMetadata); err != nil {
		t.Fatalf("decode rule-skip automation audit metadata: %v", err)
	}
	if nestedString(ruleSkipMetadata, "reason") != "contains_miss" || nestedString(ruleSkipMetadata, "model") != "audit-no-match" {
		t.Fatalf("unexpected rule-skip audit metadata: %#v", ruleSkipMetadata)
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
	loginMetadata := latestAuditMetadataForActorAction(t, env, "admin", "auth.session", "login", "success")
	if nestedString(loginMetadata, "auth_method") != "admin_recovery" || nestedString(loginMetadata, "username") != "admin" {
		t.Fatalf("unexpected admin login audit metadata: %#v", loginMetadata)
	}
	logoutMetadata := latestAuditMetadataForActorAction(t, env, "admin", "auth.session", "logout", "success")
	if nestedString(logoutMetadata, "auth_source") != "session" || nestedString(logoutMetadata, "role") != "admin" {
		t.Fatalf("unexpected logout audit metadata: %#v", logoutMetadata)
	}
}

func TestAuthSchemaReflectsServeSettings(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCProviderName = "Kirari"
		cfg.SMTPEnabled = true
	})
	if _, err := env.store.SetSystemConfig(context.Background(), store.SetSystemConfigInput{
		Key: "system_settings",
		Value: map[string]any{
			"external_registration_enabled": true,
			"email_verification_enabled":    true,
		},
	}); err != nil {
		t.Fatalf("seed auth schema system settings: %v", err)
	}

	resp := env.getJSON(t, "/api/auth/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	capabilities := schema["capabilities"].(map[string]any)
	if capabilities["lab_mode"] != false ||
		capabilities["oidc_enabled"] != true ||
		capabilities["registration_enabled"] != true ||
		capabilities["password_reset_enabled"] != true ||
		nestedString(capabilities, "oidc_provider_name") != "Kirari" {
		t.Fatalf("unexpected auth schema capabilities: %#v", resp)
	}
	operations := schema["operations"].([]any)
	if len(operations) != 16 {
		t.Fatalf("unexpected auth schema operations: %#v", resp)
	}
	if nestedString(operations[0].(map[string]any), "name") != "session" ||
		nestedString(operations[1].(map[string]any), "name") != "login" ||
		nestedString(operations[12].(map[string]any), "name") != "oidc_config" ||
		nestedString(operations[14].(map[string]any), "name") != "oidc_link" {
		t.Fatalf("unexpected auth schema operation ordering: %#v", resp)
	}
}

func TestAuthSchemaReflectsLabMode(t *testing.T) {
	env := newTestEnvWithMode(t, config.ModeLab)

	resp := env.getJSON(t, "/api/auth/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	capabilities := schema["capabilities"].(map[string]any)
	if capabilities["lab_mode"] != true || capabilities["oidc_enabled"] != false {
		t.Fatalf("unexpected lab auth schema capabilities: %#v", resp)
	}
}

func TestWebSetupStatusAndCreate(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.AdminPassword = ""
	})

	statusResp := env.getJSON(t, "/api/setup/status", http.StatusOK)
	statusPayload := statusResp["status"].(map[string]any)
	if statusResp["ok"] != true || !nestedPathBool(map[string]any{"status": statusPayload}, "status", "available") {
		t.Fatalf("unexpected setup status response: %#v", statusResp)
	}
	if nestedString(statusPayload, "env_path") == "" {
		t.Fatalf("expected setup env path: %#v", statusResp)
	}

	htmlStatus, htmlBody := env.getText(t, "/setup")
	if htmlStatus != http.StatusOK || !strings.Contains(htmlBody, "ChatAPI Setup") {
		t.Fatalf("unexpected setup html response: status=%d body=%q", htmlStatus, htmlBody)
	}

	createResp := env.postJSON(t, "/setup", map[string]any{
		"admin_password": "web-setup-secret",
	}, http.StatusOK)
	if createResp["ok"] != true || createResp["written"] != true {
		t.Fatalf("unexpected setup create response: %#v", createResp)
	}
	envPath := nestedString(createResp, "env_path")
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read setup env file: %v", err)
	}
	if !strings.Contains(string(data), "CHATAPI_ADMIN_PASSWORD=web-setup-secret") {
		t.Fatalf("unexpected setup env content: %q", string(data))
	}
	assertAuditCountForActor(t, env, "", "setup.bootstrap", "setup", envPath, "apply", "success", 1)

	statusResp = env.getJSON(t, "/api/setup/status", http.StatusOK)
	statusPayload = statusResp["status"].(map[string]any)
	if statusResp["ok"] != false || nestedPathBool(map[string]any{"status": statusPayload}, "status", "available") || nestedString(statusPayload, "reason") != "admin_already_configured" {
		t.Fatalf("expected setup to become unavailable: %#v", statusResp)
	}
}

func TestWebSetupUnavailableWhenAdminAlreadyConfigured(t *testing.T) {
	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.AdminPassword = "admin-secret"
	})

	statusResp := env.getJSON(t, "/api/setup/status", http.StatusOK)
	statusPayload := statusResp["status"].(map[string]any)
	if statusResp["ok"] != false || nestedString(statusPayload, "reason") != "admin_already_configured" {
		t.Fatalf("unexpected unavailable setup status: %#v", statusResp)
	}

	status, body := env.postText(t, "/setup", map[string]any{
		"admin_password": "other-secret",
	})
	if status != http.StatusConflict || !strings.Contains(body, "admin_already_configured") {
		t.Fatalf("expected setup conflict when admin exists: status=%d body=%q", status, body)
	}
	assertAuditCountForActor(t, env, "", "setup.bootstrap", "setup", nestedString(statusPayload, "env_path"), "apply", "failure", 1)
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
	if enabled["enabled"] != true || enabled["provider_name"] != "Kirari" || enabled["login_url"] != "/api/auth/oidc/login" || enabled["link_url"] != "/api/auth/oidc/link" {
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
	assertAuditCountForActor(t, env, "", "auth.session", "session", "admin", "oidc_login_start", "success", 1)
}

func TestOIDCCallbackCreatesSessionFromVerifiedAdminEmail(t *testing.T) {
	const state = "state-success"
	const nonce = "nonce-success"
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		IDTokenClaims: map[string]any{
			"sub":   "oidc-admin-sub",
			"nonce": nonce,
		},
		UserInfoClaims: map[string]any{
			"sub":            "oidc-admin-sub",
			"email":          "admin@example.com",
			"email_verified": true,
			"name":           "OIDC Admin",
		},
	})
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCIssuerURL = provider.Issuer()
		cfg.OIDCClientID = "chatapi"
		cfg.OIDCClientSecret = "secret"
		cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
		cfg.OIDCAutoCreateUser = true
		cfg.OIDCAdminEmails = []string{"admin@example.com"}
	})

	callbackResp, callbackCookies := env.getJSONAndCookiesWithCookies(t, "/api/auth/oidc/callback?code=success-code&state="+neturl.QueryEscape(state), []*http.Cookie{
		{Name: "chatapi_oidc_state", Value: neturl.QueryEscape(state), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_nonce", Value: neturl.QueryEscape(nonce), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_pkce", Value: neturl.QueryEscape("pkce-success"), Path: "/api/auth/oidc"},
	}, http.StatusOK)
	if callbackResp["ok"] != true || nestedPathString(callbackResp, "user", "role") != "admin" {
		t.Fatalf("unexpected oidc callback response: %#v", callbackResp)
	}
	sessionCookie := findCookie(callbackCookies, service.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatalf("expected session cookie after oidc callback, got %#v", callbackCookies)
	}
	sessionResp := env.getJSONWithCookie(t, "/api/auth/session", sessionCookie, http.StatusOK)
	if sessionResp["authenticated"] != true || nestedPathString(sessionResp, "user", "role") != "admin" {
		t.Fatalf("expected authenticated oidc admin session: %#v", sessionResp)
	}

	user, err := env.store.GetUserByEmail(context.Background(), "admin@example.com")
	if err != nil {
		t.Fatalf("load oidc user by email: %v", err)
	}
	if user.Role != "admin" || user.LastLoginAt == nil {
		t.Fatalf("unexpected stored oidc user: %#v", user)
	}
	identity, err := env.store.GetUserIdentity(context.Background(), "oidc", "oidc-admin-sub")
	if err != nil {
		t.Fatalf("load oidc identity: %v", err)
	}
	if identity.UserID != user.ID || !identity.EmailVerified {
		t.Fatalf("unexpected oidc identity after callback: %#v", identity)
	}
	assertAuditCountForActor(t, env, user.ID, "auth.session", "session", user.ID, "login", "success", 1)
	assertAuditCountForActor(t, env, user.ID, "auth.session", "session", user.ID, "oidc_login", "success", 1)
	oidcLoginMetadata := latestAuditMetadataForActorAction(t, env, user.ID, "auth.session", "oidc_login", "success")
	if nestedString(oidcLoginMetadata, "provider") != "OIDC" || nestedString(oidcLoginMetadata, "identity_sub") != "oidc-admin-sub" {
		t.Fatalf("unexpected oidc login audit metadata: %#v", oidcLoginMetadata)
	}
	loginMetadata := latestAuditMetadataForActorAction(t, env, user.ID, "auth.session", "login", "success")
	if nestedString(loginMetadata, "auth_method") != "oidc" || nestedString(loginMetadata, "identity_sub") != "oidc-admin-sub" {
		t.Fatalf("unexpected oidc session login audit metadata: %#v", loginMetadata)
	}
}

func TestOIDCLinkBindsIdentityToCurrentSessionUser(t *testing.T) {
	const state = "state-link"
	const nonce = "nonce-link"
	const pkce = "pkce-link"
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		IDTokenClaims: map[string]any{
			"sub":   "oidc-link-sub",
			"nonce": nonce,
		},
		UserInfoClaims: map[string]any{
			"sub":            "oidc-link-sub",
			"email":          "linked@example.com",
			"email_verified": true,
			"name":           "Linked User",
		},
	})
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCIssuerURL = provider.Issuer()
		cfg.OIDCClientID = "chatapi"
		cfg.OIDCClientSecret = "secret"
		cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
	})
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_link_owner",
		Username:     "link-owner",
		Email:        "",
		PasswordHash: mustPasswordHash(t, "link-secret"),
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed link owner: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "link-owner",
		"password": "link-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("expected session cookie after local login")
	}

	status, location, linkCookies := env.getRedirectWithCookies(t, "/api/auth/oidc/link", []*http.Cookie{sessionCookie})
	if status != http.StatusFound || !strings.Contains(location, "code_challenge=") {
		t.Fatalf("expected oidc link redirect, got status=%d location=%q", status, location)
	}
	intentCookie := findCookie(linkCookies, "chatapi_oidc_intent")
	if findCookie(linkCookies, "chatapi_oidc_state") == nil ||
		findCookie(linkCookies, "chatapi_oidc_nonce") == nil ||
		findCookie(linkCookies, "chatapi_oidc_pkce") == nil ||
		intentCookie == nil || neturl.QueryEscape("link") != intentCookie.Value {
		t.Fatalf("expected oidc link cookies, got %#v", linkCookies)
	}

	callbackResp, callbackCookies := env.getJSONAndCookiesWithCookies(t, "/api/auth/oidc/callback?code=success-code&state="+neturl.QueryEscape(state), []*http.Cookie{
		sessionCookie,
		{Name: "chatapi_oidc_state", Value: neturl.QueryEscape(state), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_nonce", Value: neturl.QueryEscape(nonce), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_pkce", Value: neturl.QueryEscape(pkce), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_intent", Value: neturl.QueryEscape("link"), Path: "/api/auth/oidc"},
	}, http.StatusOK)
	if callbackResp["ok"] != true || callbackResp["linked"] != true || nestedPathString(callbackResp, "identity", "subject") != "oidc-link-sub" {
		t.Fatalf("unexpected oidc link callback response: %#v", callbackResp)
	}
	refreshedSession := findCookie(callbackCookies, service.SessionCookieName)
	if refreshedSession == nil || refreshedSession.Value == "" {
		t.Fatalf("expected refreshed session cookie after oidc link, got %#v", callbackCookies)
	}
	listResp := env.getJSONWithCookie(t, "/api/user/identities", refreshedSession, http.StatusOK)
	if numericValue(listResp["count"]) != 1 {
		t.Fatalf("expected linked identity to appear in list: %#v", listResp)
	}
	identity, err := env.store.GetUserIdentity(context.Background(), "oidc", "oidc-link-sub")
	if err != nil {
		t.Fatalf("load linked oidc identity: %v", err)
	}
	if identity.UserID != "user_link_owner" {
		t.Fatalf("unexpected linked identity owner: %#v", identity)
	}
	assertAuditCountForActor(t, env, "user_link_owner", "auth.session", "session", "user_link_owner", "oidc_link", "success", 1)
	assertAuditCountForActor(t, env, "user_link_owner", "auth.session", "session", "user_link_owner", "oidc_link_start", "success", 1)
}

func TestOIDCLinkPromotesSessionUserAndRecordsRoleSync(t *testing.T) {
	const state = "state-link-admin"
	const nonce = "nonce-link-admin"
	const pkce = "pkce-link-admin"
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		IDTokenClaims: map[string]any{
			"sub":   "oidc-link-admin-sub",
			"nonce": nonce,
		},
		UserInfoClaims: map[string]any{
			"sub":            "oidc-link-admin-sub",
			"email":          "admin@example.com",
			"email_verified": true,
			"name":           "OIDC Admin Link",
		},
	})
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCIssuerURL = provider.Issuer()
		cfg.OIDCClientID = "chatapi"
		cfg.OIDCClientSecret = "secret"
		cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
		cfg.OIDCAdminEmails = []string{"admin@example.com"}
	})
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:           "user_link_promote",
		Username:     "link-promote",
		Email:        "member@example.com",
		PasswordHash: mustPasswordHash(t, "promote-secret"),
		Role:         "user",
		IsActive:     true,
	}); err != nil {
		t.Fatalf("seed promote user: %v", err)
	}
	_, cookies := env.postJSONWithCookies(t, "/api/auth/login", map[string]any{
		"username": "link-promote",
		"password": "promote-secret",
	}, http.StatusOK)
	sessionCookie := findCookie(cookies, service.SessionCookieName)
	if sessionCookie == nil {
		t.Fatalf("expected session cookie after local login")
	}

	callbackResp, callbackCookies := env.getJSONAndCookiesWithCookies(t, "/api/auth/oidc/callback?code=success-code&state="+neturl.QueryEscape(state), []*http.Cookie{
		sessionCookie,
		{Name: "chatapi_oidc_state", Value: neturl.QueryEscape(state), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_nonce", Value: neturl.QueryEscape(nonce), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_pkce", Value: neturl.QueryEscape(pkce), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_intent", Value: neturl.QueryEscape("link"), Path: "/api/auth/oidc"},
	}, http.StatusOK)
	if nestedPathString(callbackResp, "user", "role") != "admin" {
		t.Fatalf("expected promoted admin role after oidc link: %#v", callbackResp)
	}
	refreshedSession := findCookie(callbackCookies, service.SessionCookieName)
	sessionResp := env.getJSONWithCookie(t, "/api/auth/session", refreshedSession, http.StatusOK)
	if nestedPathString(sessionResp, "user", "role") != "admin" {
		t.Fatalf("expected session role refresh after oidc link: %#v", sessionResp)
	}
	assertAuditCountForActor(t, env, "user_link_promote", "auth.session", "session", "user_link_promote", "oidc_role_sync", "success", 1)
}

func TestOIDCCallbackRejectsUserInfoSubjectMismatch(t *testing.T) {
	const state = "state-mismatch"
	const nonce = "nonce-mismatch"
	provider := newTestOIDCProvider(t, testOIDCProviderConfig{
		IDTokenClaims: map[string]any{
			"sub":            "oidc-subject-id-token",
			"nonce":          nonce,
			"email":          "user@example.com",
			"email_verified": true,
		},
		UserInfoClaims: map[string]any{
			"sub":            "oidc-subject-userinfo-other",
			"email":          "user@example.com",
			"email_verified": true,
		},
	})
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeServe, func(cfg *config.Config) {
		cfg.OIDCEnabled = true
		cfg.OIDCIssuerURL = provider.Issuer()
		cfg.OIDCClientID = "chatapi"
		cfg.OIDCClientSecret = "secret"
		cfg.OIDCRedirectURL = "http://chat.example.com/api/auth/oidc/callback"
		cfg.OIDCAutoCreateUser = true
	})

	status, body, callbackCookies := env.getTextAndCookiesWithCookies(t, "/api/auth/oidc/callback?code=success-code&state="+neturl.QueryEscape(state), []*http.Cookie{
		{Name: "chatapi_oidc_state", Value: neturl.QueryEscape(state), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_nonce", Value: neturl.QueryEscape(nonce), Path: "/api/auth/oidc"},
		{Name: "chatapi_oidc_pkce", Value: neturl.QueryEscape("pkce-mismatch"), Path: "/api/auth/oidc"},
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "oidc userinfo claims are invalid") {
		t.Fatalf("expected oidc userinfo mismatch rejection: status=%d body=%q", status, body)
	}
	if sessionCookie := findCookie(callbackCookies, service.SessionCookieName); sessionCookie != nil && sessionCookie.Value != "" {
		t.Fatalf("unexpected session cookie on oidc userinfo mismatch: %#v", callbackCookies)
	}
	assertAuditCountForActor(t, env, "", "auth.session", "session", "admin", "oidc_userinfo", "failure", 1)
}

func TestKirariIntegrationLifecycle(t *testing.T) {
	provider := newTestKirariProvider(t)
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.KirariEnabled = true
		cfg.KirariIssuerURL = provider.Issuer()
		cfg.KirariClientID = "chatapi"
		cfg.KirariClientSecret = "secret"
		cfg.KirariRedirectURL = "http://chat.example.com/api/integrations/kirari/callback"
		cfg.KirariAllowedIssuers = []string{provider.Issuer()}
		cfg.KirariScopes = []string{"openid", "profile", "email", "offline_access", "llm:read", "llm:stream"}
	})

	schemaResp := env.getJSON(t, "/api/user/integrations/kirari/schema", http.StatusOK)
	operations := schemaResp["schema"].(map[string]any)["operations"].([]any)
	if len(operations) != 5 || nestedString(operations[1].(map[string]any), "path") != "/api/user/integrations/kirari/connect" {
		t.Fatalf("unexpected kirari schema response: %#v", schemaResp)
	}

	initialStatus := env.getJSON(t, "/api/user/integrations/kirari", http.StatusOK)
	if !nestedPathBool(initialStatus, "status", "enabled") || nestedPathBool(initialStatus, "status", "connected") {
		t.Fatalf("unexpected initial kirari status: %#v", initialStatus)
	}

	redirectStatus, location, cookies := env.getRedirect(t, "/api/user/integrations/kirari/connect")
	if redirectStatus != http.StatusFound {
		t.Fatalf("expected kirari connect redirect, got status=%d location=%q", redirectStatus, location)
	}
	locationURL, err := neturl.Parse(location)
	if err != nil {
		t.Fatalf("parse kirari redirect: %v", err)
	}
	query := locationURL.Query()
	provider.idTokenClaims["nonce"] = query.Get("nonce")

	callbackResp, callbackCookies := env.getJSONAndCookiesWithCookies(t, "/api/integrations/kirari/callback?code=kirari-code&state="+neturl.QueryEscape(query.Get("state")), cookies, http.StatusOK)
	if !nestedPathBool(callbackResp, "status", "connected") || nestedPathString(callbackResp, "status", "subject") != "kirari-test-sub" {
		t.Fatalf("unexpected kirari callback response: %#v", callbackResp)
	}
	if findCookie(callbackCookies, "chatapi_kirari_state") == nil {
		t.Fatalf("expected kirari callback cleanup cookies: %#v", callbackCookies)
	}

	storedConfig, err := env.store.GetUserConfig(context.Background(), "lab-user", "security.kirari")
	if err != nil {
		t.Fatalf("load stored kirari config: %v", err)
	}
	if nestedString(storedConfig.Value, "access_token_ciphertext") == "" || nestedString(storedConfig.Value, "refresh_token_ciphertext") == "" {
		t.Fatalf("expected encrypted kirari tokens in stored config: %#v", storedConfig)
	}

	connectedStatus := env.getJSON(t, "/api/user/integrations/kirari", http.StatusOK)
	if !nestedPathBool(connectedStatus, "status", "connected") || !nestedPathBool(connectedStatus, "status", "has_refresh_token") {
		t.Fatalf("unexpected connected kirari status: %#v", connectedStatus)
	}

	metaResp := env.getJSON(t, "/api/user/integrations/kirari/meta", http.StatusOK)
	metaModels, _ := nestedPath(metaResp, "meta", "models").([]any)
	if metaResp["cached"] != false || len(metaModels) != 1 || nestedString(metaModels[0].(map[string]any), "id") != "kirari-model" {
		t.Fatalf("unexpected kirari meta response: %#v", metaResp)
	}
	if provider.metaCalls != 1 {
		t.Fatalf("expected one kirari meta call, got %d", provider.metaCalls)
	}

	cachedMetaResp := env.getJSON(t, "/api/user/integrations/kirari/meta", http.StatusOK)
	if cachedMetaResp["cached"] != true || provider.metaCalls != 1 {
		t.Fatalf("expected cached kirari meta response: resp=%#v calls=%d", cachedMetaResp, provider.metaCalls)
	}

	forcedMetaResp := env.getJSON(t, "/api/user/integrations/kirari/meta?force_refresh=1", http.StatusOK)
	if forcedMetaResp["cached"] != false || provider.metaCalls != 2 {
		t.Fatalf("expected forced kirari meta refresh: resp=%#v calls=%d", forcedMetaResp, provider.metaCalls)
	}

	deleteResp := env.deleteJSON(t, "/api/user/integrations/kirari", http.StatusOK)
	if deleteResp["disconnected"] != true {
		t.Fatalf("unexpected kirari disconnect response: %#v", deleteResp)
	}
	if _, err := env.store.GetUserConfig(context.Background(), "lab-user", "security.kirari"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected kirari config deletion, got %v", err)
	}
	assertAuditCountForActor(t, env, "lab-user", "user.kirari", "kirari_connection", "lab-user", "connect_start", "success", 1)
	assertAuditCountForActor(t, env, "lab-user", "user.kirari", "kirari_connection", "lab-user", "connect_complete", "success", 1)
	assertAuditCountForActor(t, env, "lab-user", "user.kirari", "kirari_connection", "lab-user", "disconnect", "success", 1)
}

func TestKirariIntegrationRejectsInvalidState(t *testing.T) {
	provider := newTestKirariProvider(t)
	defer provider.Close()

	env := newTestEnvWithConfig(t, config.ModeLab, func(cfg *config.Config) {
		cfg.KirariEnabled = true
		cfg.KirariIssuerURL = provider.Issuer()
		cfg.KirariClientID = "chatapi"
		cfg.KirariClientSecret = "secret"
		cfg.KirariRedirectURL = "http://chat.example.com/api/integrations/kirari/callback"
		cfg.KirariAllowedIssuers = []string{provider.Issuer()}
	})

	_, location, cookies := env.getRedirect(t, "/api/user/integrations/kirari/connect")
	locationURL, err := neturl.Parse(location)
	if err != nil {
		t.Fatalf("parse kirari redirect: %v", err)
	}
	provider.idTokenClaims["nonce"] = locationURL.Query().Get("nonce")

	status, body, _ := env.getTextAndCookiesWithCookies(t, "/api/integrations/kirari/callback?code=kirari-code&state=wrong-state", cookies)
	if status != http.StatusBadRequest || !strings.Contains(body, service.ErrKirariInvalidState.Error()) {
		t.Fatalf("expected invalid kirari state rejection: status=%d body=%q", status, body)
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
	loginMetadata := latestAuditMetadataForActorAction(t, env, "user_alice", "auth.session", "login", "success")
	if nestedString(loginMetadata, "auth_method") != "local_password" || nestedString(loginMetadata, "username") != "alice" {
		t.Fatalf("unexpected local login audit metadata: %#v", loginMetadata)
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

	schemaResp := env.getJSONWithCookie(t, "/api/admin/users/schema", adminCookie, http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 14 {
		t.Fatalf("unexpected admin users schema response: %#v", schemaResp)
	}
	if nestedString(operations[0].(map[string]any), "name") != "list_users" || nestedString(operations[1].(map[string]any), "name") != "create_user" {
		t.Fatalf("unexpected admin users schema operations: %#v", schemaResp)
	}

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
	if !responseUsersContainID(listResp, userID) {
		t.Fatalf("expected created user in admin users alias: %#v", listResp)
	}

	identity, err := env.store.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:            "identity_admin_managed",
		UserID:        userID,
		Provider:      "oidc",
		Subject:       "admin-managed-sub",
		Email:         "managed@example.com",
		EmailVerified: true,
		Profile: map[string]any{
			"name": "Managed User",
		},
	})
	if err != nil {
		t.Fatalf("seed managed identity: %v", err)
	}

	identityListResp := env.getJSONWithCookie(t, "/api/admin/users/"+userID+"/identities", adminCookie, http.StatusOK)
	if nestedPathString(identityListResp, "user", "id") != userID || numericValue(identityListResp["count"]) != 1 {
		t.Fatalf("unexpected admin identity list response: %#v", identityListResp)
	}
	if nestedPathString(identityListResp["items"].([]any)[0].(map[string]any), "id") != identity.ID {
		t.Fatalf("unexpected admin identity item: %#v", identityListResp)
	}

	previewBeforeHistoryResp := env.getJSONWithCookie(t, "/api/admin/users/"+userID+"/delete-preview", adminCookie, http.StatusOK)
	if nestedPathBool(previewBeforeHistoryResp, "preview", "can_delete") != true {
		t.Fatalf("expected initial delete preview to allow purge: %#v", previewBeforeHistoryResp)
	}

	firstConversation, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_history_one",
		RequestID:      "req_history_one",
		ResponseID:     "resp_history_one",
		OwnerID:        userID,
		RequestFormat:  "responses",
		Model:          "history-model",
		UserContent:    "first question",
		RequestBody:    map[string]any{"model": "history-model"},
	})
	if err != nil {
		t.Fatalf("seed first history conversation: %v", err)
	}
	if _, _, err := env.store.CompletePendingTurn(context.Background(), store.CompletePendingInput{
		ConversationID: firstConversation.ID,
		ResponseID:     "resp_history_one",
		OutputText:     "first answer",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete first history conversation: %v", err)
	}

	secondConversation, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_history_two",
		RequestID:      "req_history_two",
		ResponseID:     "resp_history_two",
		OwnerID:        userID,
		RequestFormat:  "responses",
		Model:          "history-model",
		UserContent:    "second question",
		RequestBody:    map[string]any{"model": "history-model"},
	})
	if err != nil {
		t.Fatalf("seed second history conversation: %v", err)
	}
	if _, _, err := env.store.CompletePendingTurn(context.Background(), store.CompletePendingInput{
		ConversationID: secondConversation.ID,
		ResponseID:     "resp_history_two",
		OutputText:     "second answer",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete second history conversation: %v", err)
	}

	historyResp := env.getJSONWithCookie(t, "/api/admin/users/"+userID+"/history?limit=3", adminCookie, http.StatusOK)
	if nestedPathString(historyResp, "user", "id") != userID {
		t.Fatalf("unexpected history user response: %#v", historyResp)
	}
	recentMessages, _ := historyResp["recent_messages"].([]any)
	if len(recentMessages) != 3 {
		t.Fatalf("expected 3 recent messages after limit applied: %#v", historyResp)
	}
	if nestedString(recentMessages[0].(map[string]any), "conversation_id") != "conv_history_two" {
		t.Fatalf("expected latest conversation first in user history: %#v", historyResp)
	}
	if nestedString(recentMessages[0].(map[string]any), "conversation_title") != "second question" {
		t.Fatalf("expected conversation title in user history: %#v", historyResp)
	}

	previewResp := env.getJSONWithCookie(t, "/api/admin/users/"+userID+"/delete-preview", adminCookie, http.StatusOK)
	if nestedPathString(previewResp, "user", "id") != userID {
		t.Fatalf("unexpected delete preview user response: %#v", previewResp)
	}
	if nestedPathBool(previewResp, "preview", "can_delete") {
		t.Fatalf("expected delete preview to be blocked by history: %#v", previewResp)
	}
	if numericNestedPathValue(previewResp, "preview", "counts", "owned_conversations") != 2 {
		t.Fatalf("expected conversation count in delete preview: %#v", previewResp)
	}
	blockers, _ := nestedPath(previewResp, "preview", "blockers").([]any)
	if len(blockers) != 1 || blockers[0] != "owned_conversations" {
		t.Fatalf("unexpected delete preview blockers: %#v", previewResp)
	}
	overviewResp := env.getJSONWithCookie(t, "/api/admin/users/"+userID+"/delete-overview", adminCookie, http.StatusOK)
	if nestedPathString(overviewResp, "user", "id") != userID ||
		nestedPathString(overviewResp, "overview", "user", "id") != userID {
		t.Fatalf("unexpected delete overview user response: %#v", overviewResp)
	}
	if !reflect.DeepEqual(
		nestedPath(overviewResp, "overview", "preview"),
		nestedPath(previewResp, "preview"),
	) {
		t.Fatalf("expected delete overview preview to match preview endpoint: overview=%#v preview=%#v", overviewResp, previewResp)
	}
	if numericNestedPathValue(overviewResp, "overview", "ownership_conversation_count") != 2 ||
		numericNestedPathValue(overviewResp, "overview", "ownership_upload_count") != 0 {
		t.Fatalf("unexpected delete overview ownership counts: %#v", overviewResp)
	}
	overviewItems, _ := nestedPath(overviewResp, "overview", "ownership_items", "conversations").([]any)
	if len(overviewItems) != 2 {
		t.Fatalf("expected delete overview conversation items: %#v", overviewResp)
	}
	recommendedActions, _ := nestedPath(overviewResp, "overview", "recommended_next_actions").([]any)
	if len(recommendedActions) < 2 || recommendedActions[0] != "review_ownership_items" || recommendedActions[1] != "transfer_or_cleanup_conversations" {
		t.Fatalf("unexpected delete overview recommended actions: %#v", overviewResp)
	}

	purgeBlockedResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+userID+"/purge", map[string]any{}, adminCookie, headers, http.StatusConflict)
	if purgeBlockedResp["ok"] != false || nestedPathBool(purgeBlockedResp, "preview", "can_delete") {
		t.Fatalf("expected blocked purge response: %#v", purgeBlockedResp)
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

	deleteIdentityResp := env.deleteJSONWithCookieAndHeaders(t, "/api/admin/users/"+userID+"/identities/"+identity.ID, adminCookie, headers, http.StatusOK)
	if deleteIdentityResp["ok"] != true {
		t.Fatalf("unexpected admin identity delete response: %#v", deleteIdentityResp)
	}
	if _, err := env.store.GetUserIdentity(context.Background(), "oidc", "admin-managed-sub"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected admin-managed identity to be deleted, got %v", err)
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
	assertAuditCountForActor(t, env, "admin", "admin.user_identity", "user_identity", identity.ID, "unlink", "success", 1)

	transferSourceResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "transfer-source",
		"email":    "transfer-source@example.com",
		"password": "transfer-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	transferSourceUserID := nestedPathString(transferSourceResp, "user", "id")
	transferTargetResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "transfer-target",
		"email":    "transfer-target@example.com",
		"password": "transfer-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	transferTargetUserID := nestedPathString(transferTargetResp, "user", "id")
	if transferSourceUserID == "" || transferTargetUserID == "" {
		t.Fatalf("missing transfer users: source=%#v target=%#v", transferSourceResp, transferTargetResp)
	}
	if _, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_transfer_http",
		RequestID:      "req_transfer_http",
		ResponseID:     "resp_transfer_http",
		OwnerID:        transferSourceUserID,
		RequestFormat:  "responses",
		Model:          "transfer-model",
		UserContent:    "transfer request",
		RequestBody:    map[string]any{"model": "transfer-model"},
	}); err != nil {
		t.Fatalf("seed transfer source conversation: %v", err)
	}
	if _, err := env.store.CreateUploadedImage(context.Background(), store.CreateUploadedImageInput{
		ID:               "img_transfer_http",
		OwnerID:          transferSourceUserID,
		Filename:         "transfer-http.png",
		OriginalFilename: "transfer-http.png",
		ContentType:      "image/png",
		Bytes:            42,
		URL:              "/api/uploads/imgs/transfer-http.png",
	}); err != nil {
		t.Fatalf("seed transfer source image: %v", err)
	}
	if _, err := env.store.UpsertStorageFileDeletionFailure(context.Background(), store.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/transfer-http.png",
		Filename:  "transfer-http.png",
		OwnerID:   transferSourceUserID,
		Bytes:     42,
		LastError: "busy",
	}); err != nil {
		t.Fatalf("seed transfer source deletion failure: %v", err)
	}
	if _, err := env.store.SetStorageUserQuota(context.Background(), transferSourceUserID, 1234); err != nil {
		t.Fatalf("seed transfer source quota: %v", err)
	}

	transferPreviewResp := env.getJSONWithCookie(t, "/api/admin/users/"+transferSourceUserID+"/delete-preview", adminCookie, http.StatusOK)
	if nestedPathBool(transferPreviewResp, "preview", "can_delete") {
		t.Fatalf("expected transfer source to be blocked before ownership transfer: %#v", transferPreviewResp)
	}

	transferResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+transferSourceUserID+"/transfer-ownership", map[string]any{
		"target_user_id": transferTargetUserID,
	}, adminCookie, headers, http.StatusOK)
	if nestedPathString(transferResp, "result", "target_user_id") != transferTargetUserID {
		t.Fatalf("unexpected ownership transfer response: %#v", transferResp)
	}
	if numericNestedPathValue(transferResp, "result", "transferred_conversations") != 1 ||
		numericNestedPathValue(transferResp, "result", "transferred_uploaded_images") != 1 ||
		numericNestedPathValue(transferResp, "result", "transferred_deletion_failures") != 1 ||
		!nestedPathBool(transferResp, "preview", "can_delete") {
		t.Fatalf("unexpected ownership transfer result: %#v", transferResp)
	}
	transferConversation, err := env.store.GetConversation(context.Background(), "conv_transfer_http")
	if err != nil {
		t.Fatalf("load transferred conversation: %v", err)
	}
	if nestedString(transferConversation.Metadata, "owner_id") != transferTargetUserID {
		t.Fatalf("expected transferred conversation owner: %#v", transferConversation)
	}
	targetTransferImages, err := env.store.ListUploadedImagesByOwner(context.Background(), transferTargetUserID)
	if err != nil {
		t.Fatalf("load target transferred images: %v", err)
	}
	if len(targetTransferImages) != 1 || targetTransferImages[0].ID != "img_transfer_http" {
		t.Fatalf("expected transferred image on target: %#v", targetTransferImages)
	}

	transferPurgeResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+transferSourceUserID+"/purge", map[string]any{}, adminCookie, headers, http.StatusOK)
	if transferPurgeResp["ok"] != true || transferPurgeResp["deleted"] != true {
		t.Fatalf("unexpected transferred source purge response: %#v", transferPurgeResp)
	}
	if _, err := env.store.GetUser(context.Background(), transferSourceUserID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected transferred source user to be deleted, got %v", err)
	}
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", transferSourceUserID, "transfer_ownership", "success", 1)
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", transferSourceUserID, "purge", "success", 1)

	selectSourceResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "select-source",
		"email":    "select-source@example.com",
		"password": "select-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	selectSourceUserID := nestedPathString(selectSourceResp, "user", "id")
	selectTargetResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "select-target",
		"email":    "select-target@example.com",
		"password": "select-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	selectTargetUserID := nestedPathString(selectTargetResp, "user", "id")
	if _, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_select_http_one",
		RequestID:      "req_select_http_one",
		ResponseID:     "resp_select_http_one",
		OwnerID:        selectSourceUserID,
		RequestFormat:  "responses",
		Model:          "select-model",
		UserContent:    "select one",
		RequestBody:    map[string]any{"model": "select-model"},
	}); err != nil {
		t.Fatalf("seed selective transfer conversation one: %v", err)
	}
	if _, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_select_http_two",
		RequestID:      "req_select_http_two",
		ResponseID:     "resp_select_http_two",
		OwnerID:        selectSourceUserID,
		RequestFormat:  "responses",
		Model:          "select-model",
		UserContent:    "select two",
		RequestBody:    map[string]any{"model": "select-model"},
	}); err != nil {
		t.Fatalf("seed selective transfer conversation two: %v", err)
	}
	for _, filename := range []string{"select-http-one.png", "select-http-two.png"} {
		if _, err := env.store.CreateUploadedImage(context.Background(), store.CreateUploadedImageInput{
			ID:               "img_" + filename,
			OwnerID:          selectSourceUserID,
			Filename:         filename,
			OriginalFilename: filename,
			ContentType:      "image/png",
			Bytes:            21,
			URL:              "/api/uploads/imgs/" + filename,
		}); err != nil {
			t.Fatalf("seed selective transfer upload %s: %v", filename, err)
		}
		if _, err := env.store.UpsertStorageFileDeletionFailure(context.Background(), store.UpsertStorageFileDeletionFailureInput{
			Path:      "/tmp/" + filename,
			Filename:  filename,
			OwnerID:   selectSourceUserID,
			Bytes:     21,
			LastError: "busy",
		}); err != nil {
			t.Fatalf("seed selective transfer failure %s: %v", filename, err)
		}
	}

	itemsResp := env.getJSONWithCookie(t, "/api/admin/users/"+selectSourceUserID+"/ownership-items", adminCookie, http.StatusOK)
	ownershipItems := itemsResp["items"].(map[string]any)
	if len(ownershipItems["conversations"].([]any)) != 2 || len(ownershipItems["uploads"].([]any)) != 2 {
		t.Fatalf("unexpected ownership items response: %#v", itemsResp)
	}

	selectTransferResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+selectSourceUserID+"/transfer-ownership-selection", map[string]any{
		"target_user_id":   selectTargetUserID,
		"conversation_ids": []string{"conv_select_http_one"},
		"filenames":        []string{"select-http-one.png"},
	}, adminCookie, headers, http.StatusOK)
	if numericNestedPathValue(selectTransferResp, "result", "transferred_conversations") != 1 ||
		numericNestedPathValue(selectTransferResp, "result", "transferred_uploaded_images") != 1 ||
		nestedPathBool(selectTransferResp, "preview", "can_delete") {
		t.Fatalf("unexpected selective transfer response: %#v", selectTransferResp)
	}
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", selectSourceUserID, "transfer_ownership_selection", "success", 1)

	selectPreviewResp := env.getJSONWithCookie(t, "/api/admin/users/"+selectSourceUserID+"/delete-preview", adminCookie, http.StatusOK)
	if numericNestedPathValue(selectPreviewResp, "preview", "counts", "owned_conversations") != 1 ||
		numericNestedPathValue(selectPreviewResp, "preview", "counts", "owned_uploaded_images") != 1 {
		t.Fatalf("expected selective transfer to leave one blocker each: %#v", selectPreviewResp)
	}

	selectTransferResp, _ = env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+selectSourceUserID+"/transfer-ownership-selection", map[string]any{
		"target_user_id":   selectTargetUserID,
		"conversation_ids": []string{"conv_select_http_two"},
		"filenames":        []string{"select-http-two.png"},
	}, adminCookie, headers, http.StatusOK)
	if !nestedPathBool(selectTransferResp, "preview", "can_delete") {
		t.Fatalf("expected selective transfer to clear blockers after second pass: %#v", selectTransferResp)
	}

	cleanupSourceResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "cleanup-source",
		"email":    "cleanup-source@example.com",
		"password": "cleanup-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	cleanupSourceUserID := nestedPathString(cleanupSourceResp, "user", "id")
	if _, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_cleanup_http_keep",
		RequestID:      "req_cleanup_http_keep",
		ResponseID:     "resp_cleanup_http_keep",
		OwnerID:        cleanupSourceUserID,
		RequestFormat:  "responses",
		Model:          "cleanup-model",
		UserContent:    "keep /api/uploads/imgs/keep-http.png",
		RequestBody:    map[string]any{"model": "cleanup-model"},
	}); err != nil {
		t.Fatalf("seed cleanup keep conversation: %v", err)
	}
	cleanupDeleteConversation, _, err := env.store.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_cleanup_http_delete",
		RequestID:      "req_cleanup_http_delete",
		ResponseID:     "resp_cleanup_http_delete",
		OwnerID:        cleanupSourceUserID,
		RequestFormat:  "responses",
		Model:          "cleanup-model",
		UserContent:    "delete /api/uploads/imgs/delete-http.png",
		RequestBody:    map[string]any{"model": "cleanup-model"},
	})
	if err != nil {
		t.Fatalf("seed cleanup delete conversation: %v", err)
	}
	if _, _, err := env.store.CompletePendingTurn(context.Background(), store.CompletePendingInput{
		ConversationID: cleanupDeleteConversation.ID,
		ResponseID:     "resp_cleanup_http_delete",
		OutputText:     "done",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete cleanup delete conversation: %v", err)
	}
	for _, filename := range []string{"keep-http.png", "delete-http.png"} {
		if _, err := env.store.CreateUploadedImage(context.Background(), store.CreateUploadedImageInput{
			ID:               "img_" + filename,
			OwnerID:          cleanupSourceUserID,
			Filename:         filename,
			OriginalFilename: filename,
			ContentType:      "image/png",
			Bytes:            18,
			URL:              "/api/uploads/imgs/" + filename,
		}); err != nil {
			t.Fatalf("seed cleanup upload %s: %v", filename, err)
		}
	}

	cleanupResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+cleanupSourceUserID+"/cleanup-selection", map[string]any{
		"conversation_ids": []string{"conv_cleanup_http_keep", "conv_cleanup_http_delete"},
		"filenames":        []string{"keep-http.png"},
	}, adminCookie, headers, http.StatusOK)
	if numericNestedPathValue(cleanupResp, "result", "deleted_conversations") != 1 ||
		numericNestedPathValue(cleanupResp, "result", "deleted_images") != 1 ||
		numericNestedPathValue(cleanupResp, "result", "deleted_image_bytes") != 18 {
		t.Fatalf("unexpected cleanup selection response: %#v", cleanupResp)
	}
	skippedActive, _ := nestedPath(cleanupResp, "result", "skipped_active_conversations").([]any)
	if len(skippedActive) != 1 || skippedActive[0] != "conv_cleanup_http_keep" {
		t.Fatalf("expected active conversation skip in cleanup selection: %#v", cleanupResp)
	}
	skippedUploads, _ := nestedPath(cleanupResp, "result", "skipped_referenced_uploads").([]any)
	if len(skippedUploads) != 1 || skippedUploads[0] != "keep-http.png" {
		t.Fatalf("expected referenced upload skip in cleanup selection: %#v", cleanupResp)
	}
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", cleanupSourceUserID, "cleanup_selection", "success", 1)
	if _, err := env.store.GetConversation(context.Background(), "conv_cleanup_http_delete"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected cleanup-selected conversation to be deleted, got %v", err)
	}
	remainingCleanupUploads, err := env.store.ListUploadedImagesByOwner(context.Background(), cleanupSourceUserID)
	if err != nil {
		t.Fatalf("list remaining cleanup uploads: %v", err)
	}
	if len(remainingCleanupUploads) != 1 || remainingCleanupUploads[0].Filename != "keep-http.png" {
		t.Fatalf("unexpected remaining cleanup uploads: %#v", remainingCleanupUploads)
	}

	purgeableResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users", map[string]any{
		"username": "purgeable",
		"email":    "purgeable@example.com",
		"password": "purge-secret",
		"role":     "user",
	}, adminCookie, headers, http.StatusCreated)
	purgeableUserID := nestedPathString(purgeableResp, "user", "id")
	if purgeableUserID == "" {
		t.Fatalf("missing purgeable user id: %#v", purgeableResp)
	}
	if _, err := env.store.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:       "identity_admin_purgeable",
		UserID:   purgeableUserID,
		Provider: "oidc",
		Subject:  "admin-purgeable-sub",
		Email:    "purgeable@example.com",
	}); err != nil {
		t.Fatalf("seed purgeable identity: %v", err)
	}
	if _, err := env.store.SetUserConfig(context.Background(), store.SetUserConfigInput{
		UserID: purgeableUserID,
		Key:    "workspace",
		Value:  map[string]any{"compact": true},
	}); err != nil {
		t.Fatalf("seed purgeable config: %v", err)
	}
	if _, err := env.store.ReplaceAutomationRulesForUser(context.Background(), purgeableUserID, nil, []store.UpsertAutomationRuleInput{{
		ID:      "rule_admin_purgeable",
		Enabled: true,
		Payload: map[string]any{"kind": "demo"},
	}}); err != nil {
		t.Fatalf("seed purgeable automation rules: %v", err)
	}
	if _, err := env.store.CreateAppAPIKey(context.Background(), store.CreateAppAPIKeyInput{
		ID:        "app_key_admin_purgeable",
		UserID:    purgeableUserID,
		Name:      "purgeable-app",
		KeyHash:   "hash",
		KeyPrefix: "capi_purgeable",
		Scopes:    []string{"requests:read"},
	}); err != nil {
		t.Fatalf("seed purgeable app key: %v", err)
	}
	if err := env.store.CreateAppAPIKeyAuditLog(context.Background(), store.AppAPIKeyAuditLog{
		ID:          "app_audit_admin_purgeable",
		AppAPIKeyID: "app_key_admin_purgeable",
		UserID:      purgeableUserID,
		Route:       "/api/app/requests",
		StatusCode:  200,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed purgeable app key audit log: %v", err)
	}
	if _, err := env.store.CreateModelAPIKey(context.Background(), store.CreateModelAPIKeyInput{
		ID:            "model_key_admin_purgeable",
		UserID:        purgeableUserID,
		Name:          "purgeable-model",
		KeyCiphertext: "ciphertext",
		KeyPrefix:     "vk_purgeable",
		Model:         "gpt-test",
	}); err != nil {
		t.Fatalf("seed purgeable model key: %v", err)
	}
	if _, err := env.store.SetStorageUserQuota(context.Background(), purgeableUserID, 4096); err != nil {
		t.Fatalf("seed purgeable storage quota: %v", err)
	}
	if _, err := env.store.UpsertStorageFileDeletionFailure(context.Background(), store.UpsertStorageFileDeletionFailureInput{
		Path:      "/tmp/purgeable.png",
		Filename:  "purgeable.png",
		OwnerID:   purgeableUserID,
		Bytes:     64,
		LastError: "busy",
	}); err != nil {
		t.Fatalf("seed purgeable storage failure: %v", err)
	}

	purgePreviewResp := env.getJSONWithCookie(t, "/api/admin/users/"+purgeableUserID+"/delete-preview", adminCookie, http.StatusOK)
	if !nestedPathBool(purgePreviewResp, "preview", "can_delete") {
		t.Fatalf("expected purgeable delete preview to allow purge: %#v", purgePreviewResp)
	}
	if numericNestedPathValue(purgePreviewResp, "preview", "counts", "identities") != 1 ||
		numericNestedPathValue(purgePreviewResp, "preview", "counts", "user_configs") != 1 ||
		numericNestedPathValue(purgePreviewResp, "preview", "counts", "automation_rules") != 1 ||
		numericNestedPathValue(purgePreviewResp, "preview", "counts", "app_api_keys") != 1 ||
		numericNestedPathValue(purgePreviewResp, "preview", "counts", "model_api_keys") != 1 {
		t.Fatalf("unexpected purgeable delete preview counts: %#v", purgePreviewResp)
	}
	purgeOverviewResp := env.getJSONWithCookie(t, "/api/admin/users/"+purgeableUserID+"/delete-overview", adminCookie, http.StatusOK)
	if !nestedPathBool(purgeOverviewResp, "overview", "preview", "can_delete") {
		t.Fatalf("expected purgeable delete overview to allow purge: %#v", purgeOverviewResp)
	}
	if numericNestedPathValue(purgeOverviewResp, "overview", "ownership_conversation_count") != 0 ||
		numericNestedPathValue(purgeOverviewResp, "overview", "ownership_upload_count") != 0 {
		t.Fatalf("expected purgeable delete overview to have no ownership blockers: %#v", purgeOverviewResp)
	}
	purgeRecommendedActions, _ := nestedPath(purgeOverviewResp, "overview", "recommended_next_actions").([]any)
	if len(purgeRecommendedActions) != 1 || purgeRecommendedActions[0] != "purge_user" {
		t.Fatalf("unexpected purgeable delete overview actions: %#v", purgeOverviewResp)
	}

	purgeResp, _ := env.postJSONWithCookieAndHeaders(t, "/api/admin/users/"+purgeableUserID+"/purge", map[string]any{}, adminCookie, headers, http.StatusOK)
	if purgeResp["ok"] != true || purgeResp["deleted"] != true {
		t.Fatalf("unexpected purge response: %#v", purgeResp)
	}
	if _, err := env.store.GetUser(context.Background(), purgeableUserID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected purgeable user to be deleted, got %v", err)
	}
	if keys, err := env.store.ListAppAPIKeysByUser(context.Background(), purgeableUserID); err != nil || len(keys) != 0 {
		t.Fatalf("expected purgeable app keys removed, keys=%#v err=%v", keys, err)
	}
	if keys, err := env.store.ListModelAPIKeysByUser(context.Background(), purgeableUserID); err != nil || len(keys) != 0 {
		t.Fatalf("expected purgeable model keys removed, keys=%#v err=%v", keys, err)
	}
	assertAuditCountForActor(t, env, "admin", "admin.user", "user", purgeableUserID, "purge", "success", 1)
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

	schemaResp := env.getJSONWithCookie(t, "/api/admin/config/schema", adminCookie, http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 2 {
		t.Fatalf("unexpected admin config schema response: %#v", schemaResp)
	}
	if nestedString(operations[0].(map[string]any), "name") != "get_config" || nestedString(operations[1].(map[string]any), "name") != "set_config" {
		t.Fatalf("unexpected admin config schema operations: %#v", schemaResp)
	}
	assertAuditCountForActor(t, env, "admin", "admin.config", "system_config", "", "update", "success", 1)

	appKey := env.seedAppAPIKey(t, "admin-config-denied", []string{"statistics:read"}, nil)
	status, body := env.getTextWithHeaders(t, "/api/admin/config", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin config rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/admin/config/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin config schema rejection: status=%d body=%q", status, body)
	}
}

func TestAdminUserIdentityUnlinkRejectsLastLoginMethod(t *testing.T) {
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
	if _, err := env.store.CreateUser(context.Background(), store.CreateUserInput{
		ID:       "admin_oidc_only",
		Email:    "admin-oidc-only@example.com",
		Role:     "user",
		IsActive: true,
	}); err != nil {
		t.Fatalf("seed admin oidc-only user: %v", err)
	}
	identity, err := env.store.UpsertUserIdentity(context.Background(), store.UpsertUserIdentityInput{
		ID:            "identity_admin_last",
		UserID:        "admin_oidc_only",
		Provider:      "oidc",
		Subject:       "admin-last-sub",
		Email:         "admin-oidc-only@example.com",
		EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("seed admin oidc-only identity: %v", err)
	}

	status, body := env.deleteTextWithCookieAndHeaders(t, "/api/admin/users/admin_oidc_only/identities/"+identity.ID, adminCookie, map[string]string{
		"Origin": env.server.URL,
	})
	if status != http.StatusConflict || !strings.Contains(body, "cannot unlink the last login method") {
		t.Fatalf("expected last-login-method conflict: status=%d body=%q", status, body)
	}
}

func TestAdminUsersRejectsAPIKeys(t *testing.T) {
	env := newTestEnv(t)
	appKey := env.seedAppAPIKey(t, "lab-user", []string{"statistics:read"}, nil)

	status, body := env.getTextWithHeaders(t, "/api/admin/users", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin users rejection: status=%d body=%q", status, body)
	}

	status, body = env.getTextWithHeaders(t, "/api/admin/users/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key admin users schema rejection: status=%d body=%q", status, body)
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

	status, body = env.getTextWithHeaders(t, "/api/admin/runtime/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key runtime schema rejection: status=%d body=%q", status, body)
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

func TestAdminStorageSchema(t *testing.T) {
	env := newTestEnv(t)

	resp := env.getJSON(t, "/api/admin/storage/schema", http.StatusOK)
	schema := resp["schema"].(map[string]any)
	if !containsMapItemWithStringField(schema["operations"], "name", "set_user_quota") ||
		!containsMapItemWithStringField(schema["operations"], "name", "cleanup_preview_or_execute") ||
		!containsMapItemWithStringField(schema["operations"], "name", "vacuum") {
		t.Fatalf("unexpected admin storage schema operations: %#v", resp)
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

	status, body = env.getTextWithHeaders(t, "/api/admin/storage/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key storage schema rejection: status=%d body=%q", status, body)
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

	schemaResp := env.getJSON(t, "/api/admin/audit/schema", http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 1 {
		t.Fatalf("unexpected admin audit schema response: %#v", schemaResp)
	}
	operation := operations[0].(map[string]any)
	if nestedString(operation, "name") != "list_audit_logs" || nestedString(operation, "path") != "/api/admin/audit/logs" {
		t.Fatalf("unexpected admin audit schema operation: %#v", schemaResp)
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

	status, body = env.getTextWithHeaders(t, "/api/admin/audit/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key audit schema rejection: status=%d body=%q", status, body)
	}
}

func TestAdminRequestsOverview(t *testing.T) {
	env := newTestEnv(t)
	modelKey := env.seedModelAPIKey(t, "model-owner", "overview-model-key", "overview-model-b")

	env.postJSON(t, "/api/config/automation-rules", map[string]any{
		"rules": []map[string]any{
			{
				"id":      "rule_overview_auto",
				"enabled": true,
				"conditions": map[string]any{
					"contains": []map[string]any{{"match_type": "substring", "pattern": "overview auto"}},
					"excludes": []map[string]any{},
				},
				"action": map[string]any{
					"type": "output_text",
					"text": "overview auto done",
				},
			},
		},
	}, http.StatusOK)

	firstResp := postExternalJSON(t, env.server.URL+"/v1/responses", nil, map[string]any{
		"model": "overview-model-a",
		"input": "overview auto 请求 A",
	})
	if nestedString(firstResp, "output_text") != "overview auto done" {
		t.Fatalf("unexpected overview automation response: %#v", firstResp)
	}
	secondCh := startJSONRequestWithHeaders(t, env.server.URL+"/v1/responses", map[string]string{
		"Authorization": "Bearer " + modelKey,
	}, map[string]any{
		"model": "overview-model-b",
		"input": "overview 请求 B",
	})

	secondConversation := env.waitForWaitingConversation(t, "overview 请求 B")

	overviewResp := env.getJSON(t, "/api/admin/requests/overview", http.StatusOK)
	overview := overviewResp["overview"].(map[string]any)
	if numericValue(overview["total_requests"]) != 2 || numericValue(overview["closed_requests"]) != 1 || numericValue(overview["pending_requests"]) != 1 || numericValue(overview["automation_hits"]) != 1 {
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

	schemaResp := env.getJSON(t, "/api/admin/requests/schema", http.StatusOK)
	schema := schemaResp["schema"].(map[string]any)
	operations := schema["operations"].([]any)
	if len(operations) != 1 {
		t.Fatalf("unexpected admin requests schema response: %#v", schemaResp)
	}
	operation := operations[0].(map[string]any)
	if nestedString(operation, "name") != "requests_overview" || nestedString(operation, "path") != "/api/admin/requests/overview" {
		t.Fatalf("unexpected admin requests schema operation: %#v", schemaResp)
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

	status, body = env.getTextWithHeaders(t, "/api/admin/requests/schema", map[string]string{
		"Authorization": "Bearer " + appKey,
	})
	if status != http.StatusUnauthorized || !strings.Contains(body, "admin session required") {
		t.Fatalf("expected app api key requests schema rejection: status=%d body=%q", status, body)
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
		EnvFilePath:    filepath.Join(tempDir, ".env"),
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
	realtimeHub := service.NewRealtimeHub(sqliteStore, service.NewRealtimeLimits(
		cfg.RealtimeMaxConnections,
		cfg.RealtimeMaxConnectionsPerUser,
		cfg.RealtimeWebUIReservedPerUser,
	))
	chatService := service.NewChatAPIService(cfg, sqliteStore, pendingRegistry, realtimeHub)
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
	realtimeHub := service.NewRealtimeHub(pgStore, service.NewRealtimeLimits(
		cfg.RealtimeMaxConnections,
		cfg.RealtimeMaxConnectionsPerUser,
		cfg.RealtimeWebUIReservedPerUser,
	))
	chatService := service.NewChatAPIService(cfg, pgStore, pendingRegistry, realtimeHub)
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

type testOIDCProviderConfig struct {
	IDTokenClaims  map[string]any
	UserInfoClaims map[string]any
}

type testOIDCProvider struct {
	server         *httptest.Server
	privateKey     *rsa.PrivateKey
	keyID          string
	idTokenClaims  map[string]any
	userInfoClaims map[string]any
}

func newTestOIDCProvider(t *testing.T, cfg testOIDCProviderConfig) *testOIDCProvider {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate oidc test rsa key: %v", err)
	}
	provider := &testOIDCProvider{
		privateKey:     privateKey,
		keyID:          "test-key-1",
		idTokenClaims:  cloneMap(cfg.IDTokenClaims),
		userInfoClaims: cloneMap(cfg.UserInfoClaims),
	}
	if provider.idTokenClaims == nil {
		provider.idTokenClaims = map[string]any{
			"sub": "oidc-test-sub",
		}
	}
	if provider.userInfoClaims == nil {
		provider.userInfoClaims = cloneMap(provider.idTokenClaims)
	}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 provider.server.URL,
				"authorization_endpoint": provider.server.URL + "/authorize",
				"token_endpoint":         provider.server.URL + "/token",
				"jwks_uri":               provider.server.URL + "/jwks",
				"userinfo_endpoint":      provider.server.URL + "/userinfo",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{provider.publicJWK()},
			})
		case "/token":
			idToken, err := provider.signedIDToken()
			if err != nil {
				http.Error(w, "failed to sign id token", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "oidc-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     idToken,
			})
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(provider.userInfoClaims)
		default:
			http.NotFound(w, r)
		}
	}))
	return provider
}

type testKirariProvider struct {
	server                    *httptest.Server
	privateKey                *rsa.PrivateKey
	keyID                     string
	idTokenClaims             map[string]any
	userInfoClaims            map[string]any
	metaCalls                 int
	chatCompletionsCalls      int
	chatCompletionsStreamBody string
	chatCompletionsResponse   map[string]any
}

func newTestKirariProvider(t *testing.T) *testKirariProvider {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate kirari test rsa key: %v", err)
	}
	provider := &testKirariProvider{
		privateKey: privateKey,
		keyID:      "kirari-key-1",
		idTokenClaims: map[string]any{
			"sub":            "kirari-test-sub",
			"email":          "kirari@example.com",
			"email_verified": true,
		},
		userInfoClaims: map[string]any{
			"sub":                "kirari-test-sub",
			"email":              "kirari@example.com",
			"email_verified":     true,
			"name":               "Kirari Test User",
			"preferred_username": "kirari-user",
		},
	}
	provider.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                        provider.server.URL,
				"authorization_endpoint":        provider.server.URL + "/authorize",
				"token_endpoint":                provider.server.URL + "/token",
				"jwks_uri":                      provider.server.URL + "/jwks",
				"userinfo_endpoint":             provider.server.URL + "/userinfo",
				"llm_meta_endpoint":             provider.server.URL + "/api/llm/meta",
				"llm_chat_completions_endpoint": provider.server.URL + "/api/llm/chat/completions",
			})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"keys": []map[string]any{provider.publicJWK()},
			})
		case "/token":
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			grantType := r.Form.Get("grant_type")
			idToken, err := provider.signedIDToken()
			if err != nil {
				http.Error(w, "failed to sign kirari id token", http.StatusInternalServerError)
				return
			}
			response := map[string]any{
				"token_type": "Bearer",
				"expires_in": 3600,
				"id_token":   idToken,
			}
			switch grantType {
			case "authorization_code":
				response["access_token"] = "kirari-access-token"
				response["refresh_token"] = "kirari-refresh-token"
				response["scope"] = "openid profile email offline_access llm:read llm:stream"
			case "refresh_token":
				response["access_token"] = "kirari-access-token-refreshed"
				response["refresh_token"] = "kirari-refresh-token-refreshed"
				response["scope"] = "openid profile email offline_access llm:read llm:stream"
			default:
				http.Error(w, "unsupported grant type", http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(response)
		case "/userinfo":
			_ = json.NewEncoder(w).Encode(provider.userInfoClaims)
		case "/api/llm/meta":
			provider.metaCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"id": "kirari-model", "available": true, "price": map[string]any{"input": 1.2}},
				},
			})
		case "/api/llm/chat/completions":
			provider.chatCompletionsCalls++
			var requestBody map[string]any
			_ = json.NewDecoder(r.Body).Decode(&requestBody)
			if stream, _ := requestBody["stream"].(bool); stream {
				w.Header().Set("Content-Type", "text/event-stream")
				streamBody := provider.chatCompletionsStreamBody
				if strings.TrimSpace(streamBody) == "" {
					streamBody = strings.Join([]string{
						`data: {"choices":[{"delta":{"content":"{\"explanation\":\"default kirari assist response\"}"}}]}`,
						"",
						`data: {"choices":[{"delta":{"content":",\"tool_call\":{\"name\":\"lookup_weather\",\"arguments\":{}}}"}}]}`,
						"",
						`data: [DONE]`,
						"",
					}, "\n")
				}
				_, _ = io.WriteString(w, streamBody)
				return
			}
			response := provider.chatCompletionsResponse
			if response == nil {
				response = map[string]any{
					"choices": []map[string]any{
						{
							"message": map[string]any{
								"content": `{"explanation":"default kirari assist response","tool_call":{"name":"lookup_weather","arguments":{}}}`,
							},
						},
					},
				}
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	return provider
}

func (p *testKirariProvider) Close() {
	if p != nil && p.server != nil {
		p.server.Close()
	}
}

func (p *testKirariProvider) Issuer() string {
	if p == nil || p.server == nil {
		return ""
	}
	return p.server.URL
}

func (p *testKirariProvider) publicJWK() map[string]any {
	return map[string]any{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": p.keyID,
		"n":   base64.RawURLEncoding.EncodeToString(p.privateKey.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.privateKey.PublicKey.E)).Bytes()),
	}
}

func (p *testKirariProvider) signedIDToken() (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key: jose.JSONWebKey{
			Key:       p.privateKey,
			KeyID:     p.keyID,
			Use:       "sig",
			Algorithm: string(jose.RS256),
		},
	}, nil)
	if err != nil {
		return "", err
	}
	claims := cloneMap(p.idTokenClaims)
	now := time.Now().UTC()
	claims["iss"] = p.server.URL
	claims["aud"] = "chatapi"
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(time.Hour).Unix()
	return jwt.Signed(signer).Claims(claims).Serialize()
}

func (p *testOIDCProvider) Close() {
	if p != nil && p.server != nil {
		p.server.Close()
	}
}

func (p *testOIDCProvider) Issuer() string {
	if p == nil || p.server == nil {
		return ""
	}
	return p.server.URL
}

func (p *testOIDCProvider) publicJWK() map[string]any {
	return map[string]any{
		"kty": "RSA",
		"alg": "RS256",
		"use": "sig",
		"kid": p.keyID,
		"n":   base64.RawURLEncoding.EncodeToString(p.privateKey.PublicKey.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.privateKey.PublicKey.E)).Bytes()),
	}
}

func (p *testOIDCProvider) signedIDToken() (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{
		Algorithm: jose.RS256,
		Key: jose.JSONWebKey{
			Key:       p.privateKey,
			KeyID:     p.keyID,
			Use:       "sig",
			Algorithm: string(jose.RS256),
		},
	}, nil)
	if err != nil {
		return "", err
	}
	claims := cloneMap(p.idTokenClaims)
	now := time.Now().UTC()
	claims["iss"] = p.server.URL
	claims["aud"] = "chatapi"
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(time.Hour).Unix()
	return jwt.Signed(signer).Claims(claims).Serialize()
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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

func (e *testEnv) getJSONAndCookiesWithCookies(t *testing.T, path string, cookies []*http.Cookie, wantStatus int) (map[string]any, []*http.Cookie) {
	t.Helper()
	status, body, responseCookies := e.getTextAndCookiesWithCookies(t, path, cookies)
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode response %s: %v body=%q", path, err, body)
	}
	if status != wantStatus {
		t.Fatalf("unexpected status for %s: got %d want %d payload=%#v", path, status, wantStatus, payload)
	}
	return payload, responseCookies
}

func (e *testEnv) getTextAndCookiesWithCookies(t *testing.T, path string, cookies []*http.Cookie) (int, string, []*http.Cookie) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	for _, cookie := range cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("do get %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response %s: %v", path, err)
	}
	return resp.StatusCode, string(data), resp.Cookies()
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

func (e *testEnv) postStreamText(t *testing.T, path string, body map[string]any) (int, string) {
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
		t.Fatalf("do stream post %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read stream response %s: %v", path, err)
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

func latestAuditMetadataForActorAction(t *testing.T, env *testEnv, actorUserID string, eventType string, action string, outcome string) map[string]any {
	t.Helper()
	var metadataJSON string
	if err := env.rawDB.QueryRowContext(context.Background(), `
		SELECT metadata_json
		FROM audit_logs
		WHERE actor_user_id = ?
			AND event_type = ?
			AND action = ?
			AND outcome = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1
	`, actorUserID, eventType, action, outcome).Scan(&metadataJSON); err != nil {
		t.Fatalf("load latest audit metadata: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		t.Fatalf("decode audit metadata: %v json=%q", err, metadataJSON)
	}
	return metadata
}

func containsStringValue(value any, want string) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok && text == want {
				return true
			}
		}
	case []string:
		for _, item := range typed {
			if item == want {
				return true
			}
		}
	}
	return false
}

func containsMapItemWithStringField(value any, field string, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range items {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if nestedString(record, field) == want {
			return true
		}
	}
	return false
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

func (e *testEnv) getRedirectWithCookies(t *testing.T, path string, cookies []*http.Cookie) (int, string, []*http.Cookie) {
	t.Helper()
	client := *e.client
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	req, err := http.NewRequest(http.MethodGet, e.server.URL+path, nil)
	if err != nil {
		t.Fatalf("new redirect request %s: %v", path, err)
	}
	for _, cookie := range cookies {
		if cookie != nil {
			req.AddCookie(cookie)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get redirect with cookies %s: %v", path, err)
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

func postExternalJSON(t *testing.T, url string, headers map[string]string, body map[string]any) map[string]any {
	t.Helper()
	status, rawBody := postExternalText(t, url, headers, body)
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawBody), &payload); err != nil {
		t.Fatalf("decode external json response %s: %v body=%q", url, err, rawBody)
	}
	if status >= 400 {
		t.Fatalf("unexpected external status for %s: %d payload=%#v", url, status, payload)
	}
	return payload
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
	current := nestedPath(record, path...)
	value, _ := current.(string)
	return value
}

func nestedPath(record map[string]any, path ...string) any {
	var current any = record
	for _, key := range path {
		nextMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = nextMap[key]
	}
	return current
}

func nestedPathBool(record map[string]any, path ...string) bool {
	current := nestedPath(record, path...)
	value, _ := current.(bool)
	return value
}

func numericNestedPathValue(record map[string]any, path ...string) int {
	return numericValue(nestedPath(record, path...))
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

func responseUsersContainID(record map[string]any, id string) bool {
	items, _ := record["users"].([]any)
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

func mustPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := passwordhash.Hash(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	return hash
}

type fakeSMTPServer struct {
	listener net.Listener
	host     string
	port     int

	mu       sync.Mutex
	messages []string
}

type fakeGeeTestServer struct {
	server *httptest.Server
}

func newFakeSMTPServer(t *testing.T) *fakeSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake smtp: %v", err)
	}
	host, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split fake smtp addr: %v", err)
	}
	server := &fakeSMTPServer{
		listener: ln,
		host:     host,
		port:     atoi(portText),
	}
	go server.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return server
}

func newFakeGeeTestServer(t *testing.T) *fakeGeeTestServer {
	t.Helper()
	server := &fakeGeeTestServer{}
	server.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected geetest method: %s", r.Method)
		}
		if got := r.URL.Query().Get("captcha_id"); got != "captcha-id" {
			t.Fatalf("unexpected geetest captcha_id: %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse geetest form: %v", err)
		}
		for _, key := range []string{"lot_number", "captcha_output", "pass_token", "gen_time", "sign_token"} {
			if strings.TrimSpace(r.PostForm.Get(key)) == "" {
				t.Fatalf("missing geetest form key %q: %#v", key, r.PostForm)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	t.Cleanup(server.server.Close)
	return server
}

func (s *fakeGeeTestServer) params() map[string]any {
	return map[string]any{
		"lot_number":     "lot-123",
		"captcha_output": "captcha-output",
		"pass_token":     "pass-token",
		"gen_time":       "1720000000",
	}
}

func (s *fakeSMTPServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeSMTPServer) handleConn(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	writeLine := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return false
		}
		return writer.Flush() == nil
	}
	if !writeLine("220 fake-smtp") {
		return
	}

	var dataLines []string
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				s.mu.Lock()
				s.messages = append(s.messages, strings.Join(dataLines, "\n"))
				s.mu.Unlock()
				dataLines = nil
				inData = false
				if !writeLine("250 queued") {
					return
				}
				continue
			}
			dataLines = append(dataLines, line)
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			if _, err := writer.WriteString("250-fake-smtp\r\n250 OK\r\n"); err != nil {
				return
			}
			if err := writer.Flush(); err != nil {
				return
			}
		case strings.HasPrefix(upper, "MAIL FROM:"),
			strings.HasPrefix(upper, "RCPT TO:"):
			if !writeLine("250 OK") {
				return
			}
		case strings.HasPrefix(upper, "DATA"):
			inData = true
			if !writeLine("354 End data with <CR><LF>.<CR><LF>") {
				return
			}
		case strings.HasPrefix(upper, "QUIT"):
			_ = writeLine("221 Bye")
			return
		default:
			if !writeLine("250 OK") {
				return
			}
		}
	}
}

func (s *fakeSMTPServer) waitForLatestCode(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		count := len(s.messages)
		var latest string
		if count > 0 {
			latest = s.messages[count-1]
		}
		s.mu.Unlock()
		if latest != "" {
			re := regexp.MustCompile(`验证码：([0-9]{6})`)
			matches := re.FindStringSubmatch(latest)
			if len(matches) == 2 {
				return matches[1]
			}
			t.Fatalf("email sent but verification code not found: %q", latest)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("timed out waiting for fake smtp message")
	return ""
}

func atoi(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}
