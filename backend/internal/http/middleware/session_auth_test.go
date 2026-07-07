package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zyf/chatapi/internal/actor"
	httpmiddleware "github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service/auth/policy"
	"github.com/zyf/chatapi/internal/service/auth/session"
	"github.com/zyf/chatapi/internal/store"
	"go.uber.org/zap"
)

func TestLoadUserSessionRestoresPrincipalAndActor(t *testing.T) {
	sessionService, err := session.NewService(session.Config{
		Secret: "01234567890123456789012345678901",
	})
	if err != nil {
		t.Fatalf("new session service: %v", err)
	}
	policies := policy.NewService()
	pr := policies.SessionPrincipal(store.User{
		ID:         "user_123",
		Username:   "alice",
		Role:       "admin",
		IsActive:   true,
		LocalAdmin: true,
	}, "sess_123", "password")

	rec := httptest.NewRecorder()
	if _, err := sessionService.IssueCookie(rec, pr); err != nil {
		t.Fatalf("issue cookie: %v", err)
	}
	resp := rec.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}

	var gotActor actor.Actor
	var gotUserID string
	handler := httpmiddleware.LoadUserSession(sessionService, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionPrincipal, ok := httpmiddleware.UserSessionPrincipalFromContext(r.Context())
		if !ok {
			t.Fatal("expected session principal in context")
		}
		gotUserID = sessionPrincipal.UserID
		var okActor bool
		gotActor, okActor = actor.FromContext(r.Context())
		if !okActor {
			t.Fatal("expected actor in context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if gotUserID != "user_123" {
		t.Fatalf("unexpected principal user: %q", gotUserID)
	}
	if gotActor.UserID != "user_123" || gotActor.Role != "admin" || gotActor.Source != "session" {
		t.Fatalf("unexpected actor: %#v", gotActor)
	}
}

func TestRequireUserSessionRejectsMissingSession(t *testing.T) {
	handler := httpmiddleware.RequireUserSession(zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}
