package session_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/principal"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
)

func TestIssueAndParseRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	svc, err := session.NewService(session.Config{
		Secret: "01234567890123456789012345678901",
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	claims, token, err := svc.IssueToken(principal.Principal{
		Kind:       principal.KindHumanSession,
		SubjectID:  "sess_123",
		UserID:     "user_123",
		Username:   "alice",
		Role:       "admin",
		IsAdmin:    true,
		Source:     "session",
		EntryPoint: "web",
		AuthMethod: "password",
	})
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	parsed, err := svc.ParseToken(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if parsed.SessionID != claims.SessionID || parsed.UserID != claims.UserID || !parsed.IsAdmin {
		t.Fatalf("unexpected parsed claims: %#v", parsed)
	}
	if parsed.Principal().Kind != principal.KindHumanSession || parsed.Principal().Role != "admin" {
		t.Fatalf("unexpected parsed principal: %#v", parsed.Principal())
	}
}

func TestIssueCookieAndReadFromRequest(t *testing.T) {
	svc, err := session.NewService(session.Config{
		Secret: "01234567890123456789012345678901",
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	rec := httptest.NewRecorder()
	if _, err := svc.IssueCookie(rec, principal.Principal{
		Kind:      principal.KindHumanSession,
		SubjectID: "sess_123",
		UserID:    "user_123",
		Username:  "alice",
		Role:      "user",
		Source:    "session",
	}); err != nil {
		t.Fatalf("issue cookie: %v", err)
	}

	resp := rec.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(cookies[0])

	pr, claims, err := svc.PrincipalFromRequest(req)
	if err != nil {
		t.Fatalf("principal from request: %v", err)
	}
	if pr.UserID != "user_123" || claims.SessionID != "sess_123" {
		t.Fatalf("unexpected session principal/claims: %#v %#v", pr, claims)
	}
}
