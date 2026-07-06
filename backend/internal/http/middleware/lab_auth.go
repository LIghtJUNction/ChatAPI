package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"

	"github.com/zyf/chatapi/internal/config"
)

const labAccessCookieName = "chatapi_lab_access"

var labPasswordPageTemplate = template.Must(template.New("lab-password-page").Parse(`<!doctype html>
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

func RequireLabAccess(cfg config.Config) func(http.Handler) http.Handler {
	sessionValue := mustRandomLabSessionValue()
	var tokenUsed atomic.Bool

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Mode != config.ModeLab {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.LabPassword == "" && cfg.LabToken == "" {
				next.ServeHTTP(w, r)
				return
			}

			if hasLabAccessCookie(r, sessionValue) {
				next.ServeHTTP(w, r)
				return
			}

			if granted, viaHTMLRedirect := grantLabAccessIfValid(w, r, cfg, sessionValue, &tokenUsed); granted {
				if viaHTMLRedirect {
					http.Redirect(w, r, labRedirectTarget(r), http.StatusSeeOther)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			if shouldRenderLabPasswordPage(r, cfg) {
				renderLabPasswordPage(w, r, invalidLabPasswordMessage(r))
				return
			}
			http.Error(w, "lab access denied", http.StatusUnauthorized)
		})
	}
}

func mustRandomLabSessionValue() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err == nil {
		return base64.RawURLEncoding.EncodeToString(buf)
	}
	return "chatapi-lab-session"
}

func hasLabAccessCookie(r *http.Request, sessionValue string) bool {
	if r == nil {
		return false
	}
	cookie, err := r.Cookie(labAccessCookieName)
	if err != nil {
		return false
	}
	return cookie.Value == sessionValue
}

func grantLabAccessIfValid(w http.ResponseWriter, r *http.Request, cfg config.Config, sessionValue string, tokenUsed *atomic.Bool) (bool, bool) {
	if cfg.LabPassword != "" {
		password := strings.TrimSpace(r.URL.Query().Get("password"))
		if password != "" && password == cfg.LabPassword {
			setLabAccessCookie(w, r, sessionValue)
			return true, shouldRedirectAfterBootstrap(r)
		}
	}

	if cfg.LabToken != "" {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token != "" && token == cfg.LabToken && tokenUsed.CompareAndSwap(false, true) {
			setLabAccessCookie(w, r, sessionValue)
			return true, shouldRedirectAfterBootstrap(r)
		}
	}
	return false, false
}

func setLabAccessCookie(w http.ResponseWriter, r *http.Request, sessionValue string) {
	http.SetCookie(w, &http.Cookie{
		Name:     labAccessCookieName,
		Value:    sessionValue,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r != nil && r.TLS != nil,
	})
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
	if strings.HasPrefix(r.URL.Path, "/api/") ||
		strings.HasPrefix(r.URL.Path, "/v1/") ||
		strings.HasPrefix(r.URL.Path, "/lab/") ||
		r.URL.Path == "/messages" {
		return false
	}
	ext := path.Ext(r.URL.Path)
	return ext == "" || ext == ".html"
}

func labRedirectTarget(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "/"
	}
	if r.URL.Path == "/api/lab/access" {
		return "/"
	}
	query := r.URL.Query()
	query.Del("password")
	query.Del("token")
	target := r.URL.Path
	if target == "" {
		target = "/"
	}
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	return target
}

func shouldRenderLabPasswordPage(r *http.Request, cfg config.Config) bool {
	return cfg.LabPassword != "" && r != nil && r.Method == http.MethodGet && wantsHTML(r)
}

func invalidLabPasswordMessage(r *http.Request) string {
	if r == nil {
		return ""
	}
	if strings.TrimSpace(r.URL.Query().Get("password")) == "" {
		return ""
	}
	return "Lab password is invalid."
}

func renderLabPasswordPage(w http.ResponseWriter, r *http.Request, errText string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	action := "/"
	if r != nil && r.URL != nil {
		action = r.URL.Path
		if action == "" || action == "/api/lab/access" {
			action = "/"
		}
		query := cloneQueryWithoutLabSecrets(r.URL.Query())
		if encoded := query.Encode(); encoded != "" {
			action += "?" + encoded
		}
	}
	_ = labPasswordPageTemplate.Execute(w, map[string]any{
		"Action": action,
		"Error":  errText,
	})
}

func cloneQueryWithoutLabSecrets(values url.Values) url.Values {
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
