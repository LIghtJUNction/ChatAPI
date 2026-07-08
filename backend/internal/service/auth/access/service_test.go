package access_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/config"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
)

func TestLabAccessPublicPathsBypassGate(t *testing.T) {
	cfg := config.Default(config.ModeLab, "/tmp/chatapi-test")
	cfg.LabPassword = "secret"
	service := authaccess.NewService(cfg, labauth.NewService(cfg), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	if decision := service.EvaluateLabAccess(req); decision.Kind != authaccess.LabDecisionAllow {
		t.Fatalf("unexpected lab decision: %#v", decision)
	}
}

func TestLabAccessProtectedPathNeedsGate(t *testing.T) {
	cfg := config.Default(config.ModeLab, "/tmp/chatapi-test")
	cfg.LabPassword = "secret"
	service := authaccess.NewService(cfg, labauth.NewService(cfg), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/lab/workspace", nil)
	req.Header.Set("Accept", "text/html")
	if decision := service.EvaluateLabAccess(req); decision.Kind != authaccess.LabDecisionRender {
		t.Fatalf("unexpected lab render decision: %#v", decision)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/lab/workspace?password=secret", nil)
	req.Header.Set("Accept", "text/html")
	decision := service.EvaluateLabAccess(req)
	if decision.Kind != authaccess.LabDecisionGrant || decision.RedirectTo != "/api/lab/workspace" {
		t.Fatalf("unexpected lab bootstrap decision: %#v", decision)
	}
}

func TestSessionCSRFSameOriginPolicy(t *testing.T) {
	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.BaseURL = "https://chatapi.example.com"
	service := authaccess.NewService(cfg, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "https://chatapi.example.com/api/user/config", nil)
	req.Host = "chatapi.example.com"
	req.Header.Set("Origin", "https://chatapi.example.com")
	if !service.ShouldCheckSessionCSRF(req, true) {
		t.Fatal("expected csrf check")
	}
	if !service.ValidSessionCSRFSameOrigin(req) {
		t.Fatal("expected same-origin csrf to pass")
	}

	req = httptest.NewRequest(http.MethodPost, "https://chatapi.example.com/api/user/config", nil)
	req.Host = "chatapi.example.com"
	req.Header.Set("Origin", "https://evil.example.com")
	if !service.ShouldCheckSessionCSRF(req, true) {
		t.Fatal("expected csrf check")
	}
	if service.ValidSessionCSRFSameOrigin(req) {
		t.Fatal("expected cross-origin csrf to fail")
	}
}

func TestSessionCSRFAllowsConfiguredFrontendOrigin(t *testing.T) {
	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.CORSOrigins = []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
	}
	service := authaccess.NewService(cfg, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:5000/api/user/model-keys", nil)
	req.Host = "127.0.0.1:5000"
	req.Header.Set("Origin", "http://localhost:5173")
	if !service.ShouldCheckSessionCSRF(req, true) {
		t.Fatal("expected csrf check")
	}
	if !service.ValidSessionCSRFSameOrigin(req) {
		t.Fatal("expected configured frontend origin to pass csrf")
	}
}

func TestAccessRateLimitDisabledByDefault(t *testing.T) {
	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	service := authaccess.NewService(cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	for range 10 {
		if !service.AllowRequest(req) {
			t.Fatal("expected request to pass when limiter disabled")
		}
	}
}

func TestAccessRateLimitRejectsBurst(t *testing.T) {
	cfg := config.Default(config.ModeServe, "/tmp/chatapi-test")
	cfg.AccessRateLimitRequests = 2
	cfg.AccessRateLimitWindow = time.Minute
	service := authaccess.NewService(cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/conversations", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	if !service.AllowRequest(req) {
		t.Fatal("first request should pass")
	}
	if !service.AllowRequest(req) {
		t.Fatal("second request should pass")
	}
	if service.AllowRequest(req) {
		t.Fatal("third request should be rate limited")
	}
}
