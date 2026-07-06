package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zyf/chatapi/internal/service"
)

func TestRequireUserActorAllowsInteractiveUser(t *testing.T) {
	handler := RequireUserActor()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/user/config", nil)
	req = req.WithContext(service.WithRequestActor(req.Context(), service.RequestActor{
		UserID:   "user_1",
		Username: "alice",
		Role:     "user",
		Source:   "session",
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected interactive actor to pass, got status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRequireUserActorRejectsAppAPIActor(t *testing.T) {
	handler := RequireUserActor()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/user/config", nil)
	req = req.WithContext(service.WithRequestActor(req.Context(), service.RequestActor{
		UserID:   "user_1",
		Username: "automation",
		Role:     "app_api",
		Source:   "app_api_key",
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "session required\n" {
		t.Fatalf("expected app api actor rejection, got status=%d body=%q", rec.Code, rec.Body.String())
	}
}
