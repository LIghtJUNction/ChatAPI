package logging

import (
	"bytes"
	"context"
	"fmt"
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

func TestRequestIDColorIsStableAcrossSummaryAndStructuredLogs(t *testing.T) {
	requestID := "req_color_test"
	color := requestColor(requestID)
	if color == "" {
		t.Fatal("expected request color")
	}

	formatter := HTTPAccessFormatter{useColor: true}
	summary := formatter.FormatSummary(HTTPAccessEntry{RequestID: requestID})
	if !strings.Contains(summary, color+"["+requestID+"]"+ansiReset) {
		t.Fatalf("summary request id does not use stable color: %q", summary)
	}

	var output bytes.Buffer
	core := newRequestColorCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&output),
		zapcore.DebugLevel,
	)
	logger := zap.New(core).With(zap.String("request_id", requestID))
	logger.Debug("first")
	logger.Info("second")

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("unexpected colored log output: %q", output.String())
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, color) || !strings.HasSuffix(line, ansiReset) {
			t.Fatalf("request log does not use request color: %q", line)
		}
	}
}

func TestRequestColorCoreLeavesUnscopedLogsUncolored(t *testing.T) {
	var output bytes.Buffer
	core := newRequestColorCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&output),
		zapcore.DebugLevel,
	)
	zap.New(core).Info("plain")
	if strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("unscoped log should not be colored: %q", output.String())
	}
}

func TestRequestColorUsesModerateTrueColorRange(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM", "xterm-256color")
	color := requestColor("req_truecolor")
	var red, green, blue int
	if _, err := fmt.Sscanf(color, "\x1b[38;2;%d;%d;%dm", &red, &green, &blue); err != nil {
		t.Fatalf("unexpected truecolor sequence %q: %v", color, err)
	}
	for _, channel := range []int{red, green, blue} {
		if channel < 85 || channel > 225 {
			t.Fatalf("truecolor channel outside moderate range: rgb=(%d,%d,%d)", red, green, blue)
		}
	}
}

func TestRequestColorUsesExpanded256ColorRange(t *testing.T) {
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM", "xterm-256color")
	colors := map[string]struct{}{}
	for index := range 128 {
		colors[requestColor(fmt.Sprintf("req_%d", index))] = struct{}{}
	}
	if len(colors) <= len(requestColorPalette) {
		t.Fatalf("256-color mode did not expand the fallback range: got=%d", len(colors))
	}
	for color := range colors {
		if !strings.HasPrefix(color, "\x1b[38;5;") {
			t.Fatalf("unexpected 256-color sequence: %q", color)
		}
	}
}

func TestRequestColorFallsBackToTwelveColorPalette(t *testing.T) {
	t.Setenv("COLORTERM", "")
	t.Setenv("TERM", "xterm")
	color := requestColor("req_fallback")
	for _, candidate := range requestColorPalette {
		if color == candidate {
			return
		}
	}
	t.Fatalf("fallback color is not in the 12-color palette: %q", color)
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

func TestHTTPAccessFormatterStartAndAdaptiveWidth(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	formatter := HTTPAccessFormatter{
		useColor:      true,
		terminalWidth: func() int { return 100 },
	}
	requestID := "b1e0acb5-2f08-462d-a77f-7c30569451d2"
	line := formatter.FormatSummary(HTTPAccessEntry{
		Timestamp: time.Date(2026, 7, 10, 10, 5, 6, int(234*time.Millisecond), time.FixedZone("+0800", 8*3600)),
		Phase:     "start",
		RequestID: requestID,
		Kind:      "SSE",
		Method:    "POST",
		Path:      "/v1/responses?conversation_id=conv_1234567890abcdefghijklmnopqrstuvwxyz",
		Remote:    "127.0.0.1:36922",
	})
	plainFormatter := formatter
	plainFormatter.useColor = false
	plainLine := plainFormatter.FormatSummary(HTTPAccessEntry{
		Timestamp: time.Date(2026, 7, 10, 10, 5, 6, int(234*time.Millisecond), time.FixedZone("+0800", 8*3600)),
		Phase:     "start",
		RequestID: requestID,
		Kind:      "SSE",
		Method:    "POST",
		Path:      "/v1/responses?conversation_id=conv_1234567890abcdefghijklmnopqrstuvwxyz",
		Remote:    "127.0.0.1:36922",
	})
	if len([]rune(plainLine)) > 100 {
		t.Fatalf("summary exceeds terminal width: width=%d line=%q", len([]rune(plainLine)), plainLine)
	}

	for _, want := range []string{"START", "...", "SSE", "POST", "…"} {
		if !strings.Contains(line, want) {
			t.Fatalf("adaptive start summary missing %q: %q", want, line)
		}
	}
	if strings.Contains(line, " 200 ") {
		t.Fatalf("start summary must not claim a response status: %q", line)
	}
	if !strings.Contains(line, requestColor(requestID)) {
		t.Fatalf("shortened request id did not retain the full id color: %q", line)
	}
}

func TestHTTPAccessFormatterRedactsSensitiveQueryValues(t *testing.T) {
	formatter := HTTPAccessFormatter{useColor: false}
	line := formatter.FormatSummary(HTTPAccessEntry{
		RequestID: "req_query",
		Method:    "GET",
		Path:      "/api/callback?code=secret-code&conversation_id=conv_1234567890",
	})
	if strings.Contains(line, "secret-code") {
		t.Fatalf("summary leaked sensitive query value: %q", line)
	}
	if !strings.Contains(line, "redacted") || !strings.Contains(line, "conversation_id") {
		t.Fatalf("summary lost useful sanitized query context: %q", line)
	}
}
