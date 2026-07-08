package logging

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zyf2007/ChatAPI/internal/actor"
)

func TestFactoryLayerAddsLayerField(t *testing.T) {
	factory, err := NewFactory(Config{Level: "debug", Format: "console"})
	if err != nil {
		t.Fatalf("new factory: %v", err)
	}
	logger := factory.Layer(LayerTurn)
	if logger.Name() != LayerTurn {
		t.Fatalf("unexpected logger name: %q", logger.Name())
	}
}

func TestActorFields(t *testing.T) {
	fields := ActorFields(actor.Actor{
		UserID:      "user_1",
		Username:    "alice",
		Role:        "model_api",
		Source:      "model_api_key",
		PrincipalID: "key_1",
		EntryPoint:  "virtual_model",
	})
	if len(fields) != 6 {
		t.Fatalf("unexpected actor field count: %d", len(fields))
	}
}

func TestForContextIncludesActorFields(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	factory := &Factory{root: zap.New(core)}
	ctx := actor.WithActor(context.Background(), actor.Actor{
		UserID:     "user_1",
		Source:     "app_api_key",
		EntryPoint: "app_api",
	})
	factory.ForContext(ctx, LayerHTTP).Info("hello")
	entry := logs.All()[0]
	if got := entry.ContextMap()["actor.user_id"]; got != "user_1" {
		t.Fatalf("unexpected actor.user_id: %#v", entry.ContextMap())
	}
	if got := entry.ContextMap()["actor.entry_point"]; got != "app_api" {
		t.Fatalf("unexpected actor.entry_point: %#v", entry.ContextMap())
	}
}

func TestBindContextIncludesRequestAndConnectionIDs(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logger := zap.New(core)
	ctx := WithRequestID(context.Background(), "req_123")
	ctx = WithConnectionID(ctx, "ws_456")

	BindContext(logger, ctx).Info("hello")
	entry := logs.All()[0]

	if got := entry.ContextMap()["request_id"]; got != "req_123" {
		t.Fatalf("unexpected request_id: %#v", entry.ContextMap())
	}
	if got := entry.ContextMap()["connection_id"]; got != "ws_456" {
		t.Fatalf("unexpected connection_id: %#v", entry.ContextMap())
	}
}

func TestHTTPAccessFormatterSummaryWithoutColor(t *testing.T) {
	formatter := HTTPAccessFormatter{useColor: false}
	line := formatter.FormatSummary(HTTPAccessEntry{
		Timestamp: time.Date(2026, 7, 8, 22, 56, 20, 0, time.FixedZone("+0800", 8*3600)),
		RequestID: "6a4e6514",
		Kind:      "HTTP",
		Method:    "POST",
		Path:      "/v1/responses",
		Status:    502,
		Duration:  1500 * time.Millisecond,
		Remote:    "127.0.0.1:12345",
	})

	for _, want := range []string{"2026-07-08T22:56:20.000+08:00", "[6a4e6514]", "HTTP", "502", "POST", "/v1/responses", "1500ms", "127.0.0.1:12345"} {
		if !strings.Contains(line, want) {
			t.Fatalf("summary missing %q: %q", want, line)
		}
	}
}

func TestHTTPAccessFormatterSummaryWithColorHighlightsSpecialValues(t *testing.T) {
	formatter := HTTPAccessFormatter{useColor: true}
	line := formatter.FormatSummary(HTTPAccessEntry{
		Timestamp: time.Date(2026, 7, 8, 22, 56, 20, 0, time.FixedZone("+0800", 8*3600)),
		RequestID: "",
		Kind:      "WS",
		Method:    "DELETE",
		Path:      "/v1/responses",
		Status:    503,
		Duration:  4 * time.Second,
		Remote:    "127.0.0.1:12345",
	})

	for _, want := range []string{
		ansiBgRed,
		ansiFgWhite,
		ansiFgRed,
		"[ - ]",
	} {
		if want == "[ - ]" {
			continue
		}
		if !strings.Contains(line, want) {
			t.Fatalf("summary missing color sequence %q: %q", want, line)
		}
	}
	if !strings.Contains(line, "[-]") {
		t.Fatalf("summary missing request id placeholder: %q", line)
	}
	if !strings.Contains(line, "DELETE") {
		t.Fatalf("summary missing method: %q", line)
	}
	if !strings.Contains(line, "WS") {
		t.Fatalf("summary missing kind: %q", line)
	}
}

func TestHTTPAccessFormatterSummaryEllipsizesLongPath(t *testing.T) {
	formatter := HTTPAccessFormatter{useColor: false}
	line := formatter.FormatSummary(HTTPAccessEntry{
		Timestamp: time.Date(2026, 7, 8, 22, 56, 20, 0, time.FixedZone("+0800", 8*3600)),
		RequestID: "req_long",
		Kind:      "POLL",
		Method:    "GET",
		Path:      "/repo/search/very/long/path/that/should/be/truncated/before/it/pushes/the/rest/of/the/line",
		Status:    200,
		Duration:  25 * time.Millisecond,
		Remote:    "127.0.0.1:12345",
	})

	if !strings.Contains(line, "POLL") {
		t.Fatalf("summary missing poll kind: %q", line)
	}
	if !strings.Contains(line, "…") {
		t.Fatalf("summary missing ellipsis: %q", line)
	}
}
