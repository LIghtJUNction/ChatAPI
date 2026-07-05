package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
)

func TestRequireSessionCSRFAllowsSameOriginReferer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://chat.example.com/api/admin/runtime/gc", nil)
	req.Header.Set("Referer", "http://chat.example.com/admin")
	req = req.WithContext(service.WithRequestActor(req.Context(), service.RequestActor{
		UserID:   "admin",
		Username: "admin",
		Role:     "admin",
		Source:   "session",
	}))
	called := false
	handler := RequireSessionCSRF(config.Config{Mode: config.ModeServe})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatalf("expected same-origin referer to pass")
	}
}

func TestRequireSessionCSRFRejectsCrossOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://chat.example.com/api/admin/runtime/gc", nil)
	req.Header.Set("Origin", "http://evil.example.com")
	req = req.WithContext(service.WithRequestActor(req.Context(), service.RequestActor{
		UserID:   "admin",
		Username: "admin",
		Role:     "admin",
		Source:   "session",
	}))
	handler := RequireSessionCSRF(config.Config{Mode: config.ModeServe})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("cross-origin session mutation should not reach handler")
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d", rr.Code)
	}
}

func TestRequireSessionCSRFSkipsAPIKeyBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "http://chat.example.com/api/app/requests/req/complete", nil)
	req.Header.Set("Authorization", "Bearer ak-test")
	called := false
	handler := RequireSessionCSRF(config.Config{Mode: config.ModeServe})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !called {
		t.Fatalf("expected api key request to skip csrf")
	}
}
