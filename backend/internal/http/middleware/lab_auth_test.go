package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/config"
)

func TestRequireLabAccessPasswordFlowSetsCookie(t *testing.T) {
	cfg := config.Config{Mode: config.ModeLab, LabPassword: "dev-password"}
	req := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/health?password=dev-password", nil)
	req.Header.Set("Accept", "application/json")
	rr := httptest.NewRecorder()
	called := false

	handler := RequireLabAccess(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(rr, req)
	if !called || rr.Code != http.StatusOK {
		t.Fatalf("expected password bootstrap request to pass: code=%d called=%v", rr.Code, called)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != labAccessCookieName || cookies[0].Value == "" {
		t.Fatalf("expected lab access cookie, got %#v", cookies)
	}

	req2 := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/health", nil)
	req2.AddCookie(cookies[0])
	rr2 := httptest.NewRecorder()
	called = false
	handler.ServeHTTP(rr2, req2)
	if !called || rr2.Code != http.StatusOK {
		t.Fatalf("expected cookie-authenticated lab request to pass: code=%d called=%v", rr2.Code, called)
	}
}

func TestRequireLabAccessTokenIsSingleUse(t *testing.T) {
	cfg := config.Config{Mode: config.ModeLab, LabToken: "lab-token"}
	handler := RequireLabAccess(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/health?token=lab-token", nil)
	firstReq.Header.Set("Accept", "application/json")
	handler.ServeHTTP(first, firstReq)
	if first.Code != http.StatusOK {
		t.Fatalf("expected first token use to pass, got %d", first.Code)
	}
	cookies := first.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected cookie after first token use")
	}

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/health?token=lab-token", nil)
	secondReq.Header.Set("Accept", "application/json")
	handler.ServeHTTP(second, secondReq)
	if second.Code != http.StatusUnauthorized {
		t.Fatalf("expected second token use to be rejected, got %d", second.Code)
	}

	withCookie := httptest.NewRecorder()
	withCookieReq := httptest.NewRequest(http.MethodGet, "http://chat.example.com/api/health", nil)
	withCookieReq.AddCookie(cookies[0])
	handler.ServeHTTP(withCookie, withCookieReq)
	if withCookie.Code != http.StatusOK {
		t.Fatalf("expected cookie session to remain valid, got %d", withCookie.Code)
	}
}

func TestRequireLabAccessRendersPasswordPageForHTML(t *testing.T) {
	cfg := config.Config{Mode: config.ModeLab, LabPassword: "dev-password"}
	req := httptest.NewRequest(http.MethodGet, "http://chat.example.com/", nil)
	req.Header.Set("Accept", "text/html")
	rr := httptest.NewRecorder()

	handler := RequireLabAccess(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("html request without lab password should not reach protected handler")
	}))
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized password page, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "ChatAPI Lab") || !strings.Contains(body, "name=\"password\"") {
		t.Fatalf("expected password form page, got %q", body)
	}
}
