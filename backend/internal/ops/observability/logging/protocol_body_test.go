package logging

import (
	"context"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestDebugProtocolRequestBodyPreservesExactRawBody(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	raw := []byte("{\n  \"model\": \"gpt-test\"\n}")
	DebugProtocolRequestBody(zap.New(core), context.Background(), "responses", raw)
	entries := logs.FilterMessage("protocol request body received").All()
	if len(entries) != 1 {
		t.Fatalf("expected raw body log, got %#v", logs.All())
	}
	fields := entries[0].ContextMap()
	if fields["request.body.raw"] != string(raw) || fields["request.body.bytes"] != int64(len(raw)) {
		t.Fatalf("unexpected body fields: %#v", fields)
	}
}

func TestDebugProtocolRequestBodyIsSilentAboveDebugLevel(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	DebugProtocolRequestBody(zap.New(core), context.Background(), "responses", []byte(`{"secret":"kept only in debug"}`))
	if logs.Len() != 0 {
		t.Fatalf("unexpected non-debug body log: %#v", logs.All())
	}
}
