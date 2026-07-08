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
}
