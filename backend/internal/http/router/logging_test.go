package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
)

func TestRequestLoggingMiddlewareFallsBackToJSONLogger(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	base := zap.New(core)

	handler := requestLoggingMiddleware(nil, base)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("unexpected status code: %d", rec.Code)
	}
	if got := rec.Header().Get(logging.RequestIDHeader); got == "" {
		t.Fatal("expected response request id header")
	}
	if logs.Len() != 1 {
		t.Fatalf("unexpected log count: %d", logs.Len())
	}

	entry := logs.All()[0]
	if entry.Message != logging.HTTPAccessMessage() {
		t.Fatalf("unexpected message: %q", entry.Message)
	}
	if got := entry.ContextMap()["http.status_code"]; got != int64(http.StatusCreated) {
		t.Fatalf("unexpected status field: %#v", entry.ContextMap())
	}
	if got := entry.ContextMap()["http.path"]; got != "/v1/responses" {
		t.Fatalf("unexpected path field: %#v", entry.ContextMap())
	}
	if got := entry.ContextMap()["request_id"]; got == "" {
		t.Fatalf("expected request_id field: %#v", entry.ContextMap())
	}
}

func TestRequestLoggingMiddlewarePreservesIncomingRequestID(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	base := zap.New(core)

	handler := requestLoggingMiddleware(nil, base)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	req.Header.Set(logging.RequestIDHeader, "req_from_client")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(logging.RequestIDHeader); got != "req_from_client" {
		t.Fatalf("unexpected response request id: %q", got)
	}
	entry := logs.All()[0]
	if got := entry.ContextMap()["request_id"]; got != "req_from_client" {
		t.Fatalf("unexpected request_id field: %#v", entry.ContextMap())
	}
}

func TestHTTPAccessKindDetectsWebsocketSSEAndPolling(t *testing.T) {
	wsReq := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	wsReq.Header.Set("Upgrade", "websocket")
	if got := httpAccessKind(wsReq); got != "WS" {
		t.Fatalf("unexpected ws kind: %q", got)
	}

	sseReq := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	sseReq.Header.Set("Accept", "text/event-stream")
	if got := httpAccessKind(sseReq); got != "SSE" {
		t.Fatalf("unexpected sse kind: %q", got)
	}

	pollReq := httptest.NewRequest(http.MethodGet, "/user/events", nil)
	if got := httpAccessKind(pollReq); got != "POLL" {
		t.Fatalf("unexpected poll kind: %q", got)
	}
}
