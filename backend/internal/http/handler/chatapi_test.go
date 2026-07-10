package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestProtocolRequestDebugLogKeepsOriginalBodyBytes(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	handler := ChatAPIHandler{Logger: zap.New(core)}
	rawBody := "{\n  \"model\": \"gpt-test\",\n  \"tools\": ["
	request := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(rawBody))
	response := httptest.NewRecorder()

	handler.Responses(response, request)

	entries := logs.FilterMessage("protocol request body received").All()
	if len(entries) != 1 {
		t.Fatalf("expected one raw body debug log, got %#v", logs.All())
	}
	context := entries[0].ContextMap()
	if context["protocol"] != "responses" {
		t.Fatalf("unexpected protocol field: %#v", context)
	}
	if context["request.body.raw"] != rawBody {
		t.Fatalf("raw request body changed: got=%#v want=%#v", context["request.body.raw"], rawBody)
	}
	if context["request.body.bytes"] != int64(len(rawBody)) {
		t.Fatalf("unexpected request body length: %#v", context["request.body.bytes"])
	}
}
