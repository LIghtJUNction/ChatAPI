package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

type AdminEmailHandler struct {
	Email *service.AdminEmailService
	Audit *service.AuditService
}

func (h AdminEmailHandler) SendTestEmail(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid test email request", http.StatusBadRequest)
		return
	}
	if err := h.Email.SendTestEmail(r.Context(), body.Email); err != nil {
		h.record(r, body.Email, "failure", err)
		switch {
		case errors.Is(err, service.ErrInvalidAdminEmailInput):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrEmailConfigInvalid):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	h.record(r, body.Email, "success", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "test email sent",
	})
}

func (h AdminEmailHandler) record(r *http.Request, recipient string, outcome string, err error) {
	if h.Audit == nil {
		return
	}
	metadata := map[string]any{
		"recipient_domain": recipientDomain(recipient),
	}
	if err != nil {
		metadata["error"] = err.Error()
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.email",
		ResourceType: "smtp",
		Action:       "send_test_email",
		Outcome:      outcome,
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata:     metadata,
	})
}

func recipientDomain(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
