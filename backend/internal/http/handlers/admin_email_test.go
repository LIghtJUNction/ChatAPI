package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zyf/chatapi/internal/platform/email"
	"github.com/zyf/chatapi/internal/service"
)

type fakeEmailSender struct {
	last email.Message
	err  error
}

func (f *fakeEmailSender) Send(_ context.Context, message email.Message) error {
	f.last = message
	return f.err
}

func TestAdminEmailHandlerSendTestEmailSuccess(t *testing.T) {
	sender := &fakeEmailSender{}
	handler := AdminEmailHandler{
		Email: service.NewAdminEmailService(email.SMTPConfig{
			Enabled:  true,
			Host:     "smtp.example.com",
			Port:     587,
			From:     "noreply@example.com",
			Security: "starttls",
			Timeout:  5 * time.Second,
		}, sender),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/send-test-email", strings.NewReader(`{"email":"user@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.SendTestEmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%q", rec.Code, rec.Body.String())
	}
	if len(sender.last.To) != 1 || sender.last.To[0] != "user@example.com" {
		t.Fatalf("unexpected email recipient: %#v", sender.last)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("unexpected response payload: %#v", payload)
	}
}

func TestAdminEmailHandlerSendTestEmailFailure(t *testing.T) {
	sender := &fakeEmailSender{err: errors.New("smtp send failed")}
	handler := AdminEmailHandler{
		Email: service.NewAdminEmailService(email.SMTPConfig{
			Enabled:  true,
			Host:     "smtp.example.com",
			Port:     587,
			From:     "noreply@example.com",
			Security: "starttls",
			Timeout:  5 * time.Second,
		}, sender),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/send-test-email", strings.NewReader(`{"email":"user@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.SendTestEmail(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d body=%q", rec.Code, rec.Body.String())
	}
}
