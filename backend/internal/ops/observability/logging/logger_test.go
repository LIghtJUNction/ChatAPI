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

func TestHTTPAccessFormatterSummaryWithoutColor(t *testing.T) {
	formatter := HTTPAccessFormatter{useColor: false}
	line := formatter.FormatSummary(HTTPAccessEntry{
		Method:   "POST",
		Path:     "/v1/responses",
		Status:   502,
		Duration: 1500 * time.Millisecond,
		Remote:   "127.0.0.1:12345",
	})

	for _, want := range []string{"◆", "502", "POST", "/v1/responses", "1500ms", "127.0.0.1:12345"} {
		if !strings.Contains(line, want) {
			t.Fatalf("summary missing %q: %q", want, line)
		}
	}
}
