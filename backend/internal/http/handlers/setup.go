package handlers

import (
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

type SetupHandler struct {
	Service *service.SetupService
	Audit   *service.AuditService
}

func (h SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	status, err := h.Service.Status(r.Context())
	if err != nil && !status.Available {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     false,
			"status": status,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"status": status,
	})
}

func (h SetupHandler) HTML(w http.ResponseWriter, r *http.Request) {
	status, _ := h.Service.Status(r.Context())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = setupTemplate.Execute(w, map[string]any{
		"Status": status,
	})
}

func (h SetupHandler) Create(w http.ResponseWriter, r *http.Request) {
	input, err := decodeSetupInput(r)
	if err != nil {
		http.Error(w, "invalid setup request", http.StatusBadRequest)
		return
	}
	report, err := h.Service.Run(r.Context(), input)
	if err != nil {
		h.record(r, report.EnvPath, "apply", "failure", report.Error)
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, service.ErrSetupUnavailable):
			status = http.StatusConflict
		case errors.Is(err, service.ErrSetupEnvExists):
			status = http.StatusConflict
		}
		writeJSON(w, status, report)
		return
	}
	h.record(r, report.EnvPath, "apply", "success", "")
	writeJSON(w, http.StatusOK, report)
}

func decodeSetupInput(r *http.Request) (service.SetupApplyInput, error) {
	var input service.SetupApplyInput
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "application/json") {
		var body struct {
			AdminPassword string `json:"admin_password"`
			WriteEnv      *bool  `json:"write_env"`
			Force         bool   `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return input, err
		}
		input.AdminPassword = body.AdminPassword
		input.Force = body.Force
		input.WriteEnv = body.WriteEnv == nil || *body.WriteEnv
		return input, nil
	}
	if err := r.ParseForm(); err != nil {
		return input, err
	}
	input.AdminPassword = r.FormValue("admin_password")
	input.Force = parseTruthy(r.FormValue("force"))
	input.WriteEnv = !parseFalsy(r.FormValue("write_env"))
	return input, nil
}

func parseTruthy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func parseFalsy(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return true
	default:
		return false
	}
}

var setupTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>ChatAPI Setup</title>
  <style>
    body { font-family: sans-serif; margin: 40px auto; max-width: 720px; padding: 0 16px; }
    form { display: grid; gap: 12px; margin-top: 24px; }
    input, button { font: inherit; padding: 10px 12px; }
    pre { white-space: pre-wrap; background: #f5f5f5; padding: 12px; overflow: auto; }
    .muted { color: #666; }
  </style>
</head>
<body>
  <h1>ChatAPI Setup</h1>
  {{if .Status.Available}}
    <p>Initialization is available. This writes a fresh <code>.env</code> file and enables the recovery admin login.</p>
    <p class="muted">Env path: <code>{{.Status.EnvPath}}</code></p>
    <form method="post" action="/setup">
      <label>
        Admin password
        <input type="password" name="admin_password" placeholder="Leave empty to generate a random password">
      </label>
      <button type="submit">Initialize</button>
    </form>
  {{else}}
    <p>Setup is unavailable.</p>
    <pre>{{.Status.Reason}}</pre>
  {{end}}
</body>
</html>`))

func (h SetupHandler) record(r *http.Request, envPath string, action string, outcome string, errorMessage string) {
	if h.Audit == nil {
		return
	}
	metadata := map[string]any{
		"env_path": envPath,
	}
	if strings.TrimSpace(errorMessage) != "" {
		metadata["error"] = strings.TrimSpace(errorMessage)
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "setup.bootstrap",
		ResourceType: "setup",
		ResourceID:   strings.TrimSpace(envPath),
		Action:       action,
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}
