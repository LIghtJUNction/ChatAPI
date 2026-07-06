package app

import (
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/config"
)

func TestBuildLabURLPrefersTokenAndAvoidsPasswordInURL(t *testing.T) {
	cfg := config.Config{Host: "0.0.0.0", Port: 5000, LabPassword: "dev-password"}
	if got := buildLabURL(cfg); got != "http://127.0.0.1:5000/" {
		t.Fatalf("unexpected password-mode lab url: %q", got)
	}

	cfg.LabPassword = ""
	cfg.LabToken = "lab-token"
	got := buildLabURL(cfg)
	if !strings.Contains(got, "http://127.0.0.1:5000/?token=lab-token") {
		t.Fatalf("unexpected token-mode lab url: %q", got)
	}
}
