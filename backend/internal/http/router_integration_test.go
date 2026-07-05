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
