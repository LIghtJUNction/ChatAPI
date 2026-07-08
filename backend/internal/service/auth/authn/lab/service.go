package lab

import (
	"crypto/rand"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
)

const AccessCookieName = "chatapi_lab_access"
const DefaultOwnerID = "lab_default"

var passwordPageTemplate = template.Must(template.New("lab-password-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>ChatAPI Lab Access</title>
  <style>
    body { font-family: sans-serif; margin: 0; background: #111827; color: #e5e7eb; }
    main { min-height: 100vh; display: grid; place-items: center; padding: 24px; }
    form { width: min(360px, 100%); background: #1f2937; padding: 24px; border-radius: 10px; box-shadow: 0 10px 30px rgba(0,0,0,.35); }
    h1 { margin: 0 0 12px; font-size: 20px; }
    p { margin: 0 0 16px; color: #9ca3af; font-size: 14px; line-height: 1.5; }
    input { width: 100%; box-sizing: border-box; padding: 10px 12px; border-radius: 8px; border: 1px solid #374151; background: #111827; color: #e5e7eb; }
    button { width: 100%; margin-top: 12px; padding: 10px 12px; border: 0; border-radius: 8px; background: #2563eb; color: white; cursor: pointer; }
    .error { margin-top: 12px; color: #fca5a5; font-size: 13px; }
  </style>
</head>
<body>
  <main>
    <form method="get" action="{{.Action}}">
      <h1>ChatAPI Lab</h1>
      <p>Lab 模式已启用访问口令。输入口令后会建立一次本地访问 cookie。</p>
      <input type="password" name="password" placeholder="Lab password" autocomplete="current-password" autofocus>
      <button type="submit">Enter Lab</button>
      {{if .Error}}<div class="error">{{.Error}}</div>{{end}}
    </form>
  </main>
</body>
</html>`))

type Service struct {
	cfg          config.Config
	sessionValue string
	tokenUsed    atomic.Bool
}

func NewService(cfg config.Config) *Service {
	return &Service{
		cfg:          cfg,
		sessionValue: randomSessionValue(),
	}
}

func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Mode == config.ModeLab
}

func (s *Service) RequiresGate() bool {
	if !s.Enabled() {
		return false
	}
	return strings.TrimSpace(s.cfg.LabPassword) != "" || strings.TrimSpace(s.cfg.LabToken) != ""
}

func (s *Service) HasCookieAccess(r *http.Request) bool {
	if s == nil || r == nil {
		return false
	}
	cookie, err := r.Cookie(AccessCookieName)
	if err != nil {
		return false
	}
	return cookie.Value == s.sessionValue
}

func (s *Service) CanGrant(r *http.Request) (granted bool, redirect bool) {
	if s == nil || r == nil || !s.Enabled() {
		return false, false
	}
	if strings.TrimSpace(s.cfg.LabPassword) != "" {
		password := strings.TrimSpace(r.URL.Query().Get("password"))
		if password != "" && password == strings.TrimSpace(s.cfg.LabPassword) {
			return true, shouldRedirectAfterBootstrap(r)
		}
	}
	if strings.TrimSpace(s.cfg.LabToken) != "" {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token != "" && token == strings.TrimSpace(s.cfg.LabToken) && !s.tokenUsed.Load() {
			return true, shouldRedirectAfterBootstrap(r)
		}
	}
	return false, false
}

func (s *Service) GrantIfValid(w http.ResponseWriter, r *http.Request) (granted bool, redirect bool) {
	if s == nil || r == nil || !s.Enabled() {
		return false, false
	}
	if strings.TrimSpace(s.cfg.LabPassword) != "" {
		password := strings.TrimSpace(r.URL.Query().Get("password"))
		if password != "" && password == strings.TrimSpace(s.cfg.LabPassword) {
			s.setAccessCookie(w, r)
			return true, shouldRedirectAfterBootstrap(r)
		}
	}
	if strings.TrimSpace(s.cfg.LabToken) != "" {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token != "" && token == strings.TrimSpace(s.cfg.LabToken) && s.tokenUsed.CompareAndSwap(false, true) {
			s.setAccessCookie(w, r)
			return true, shouldRedirectAfterBootstrap(r)
		}
	}
	return false, false
}

func (s *Service) ShouldRenderPasswordPage(r *http.Request) bool {
	return s != nil && s.Enabled() && strings.TrimSpace(s.cfg.LabPassword) != "" && r != nil && r.Method == http.MethodGet && wantsHTML(r)
}

func (s *Service) RenderPasswordPage(w http.ResponseWriter, r *http.Request) {
	if w == nil {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	action := "/"
	if r != nil && r.URL != nil {
		action = r.URL.Path
		if action == "" || action == "/api/lab/access" {
			action = "/"
		}
		query := cloneQueryWithoutSecrets(r.URL.Query())
		if encoded := query.Encode(); encoded != "" {
			action += "?" + encoded
		}
	}
	_ = passwordPageTemplate.Execute(w, map[string]any{
		"Action": action,
		"Error":  s.InvalidPasswordMessage(r),
	})
}

func (s *Service) InvalidPasswordMessage(r *http.Request) string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.URL.Query().Get("password")) == "" {
		return ""
	}
	return "Lab password is invalid."
}

func (s *Service) RedirectTarget(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	if r.URL.Path == "/api/lab/access" {
		return "/"
	}
	query := cloneQueryWithoutSecrets(r.URL.Query())
	target := r.URL.Path
	if target == "" {
		target = "/"
	}
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target
}

func (s *Service) Actor() actor.Actor {
	return actor.Actor{
		UserID:      DefaultOwnerID,
		Username:    "lab",
		Role:        "admin",
		Source:      "lab",
		PrincipalID: "lab:" + DefaultOwnerID,
		EntryPoint:  "lab",
	}
}

func (s *Service) setAccessCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     AccessCookieName,
		Value:    s.sessionValue,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r != nil && r.TLS != nil,
	})
}

func randomSessionValue() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err == nil {
		return base64.RawURLEncoding.EncodeToString(buf)
	}
	return "chatapi-lab-session"
}

func shouldRedirectAfterBootstrap(r *http.Request) bool {
	return r != nil && r.Method == http.MethodGet && wantsHTML(r)
}

func wantsHTML(r *http.Request) bool {
	if r == nil {
		return false
	}
	accept := strings.ToLower(strings.TrimSpace(r.Header.Get("Accept")))
	if strings.Contains(accept, "text/html") {
		return true
	}
	if accept != "" && !strings.Contains(accept, "*/*") {
		return false
	}
	if r.URL == nil {
		return false
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/lab/") || r.URL.Path == "/messages" {
		return false
	}
	ext := path.Ext(r.URL.Path)
	return ext == "" || ext == ".html"
}

func cloneQueryWithoutSecrets(values url.Values) url.Values {
	out := url.Values{}
	for key, items := range values {
		if key == "password" || key == "token" {
			continue
		}
		for _, item := range items {
			out.Add(key, item)
		}
	}
	return out
}
