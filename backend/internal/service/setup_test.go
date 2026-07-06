package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
)

func TestBuildSetupEnvTemplate(t *testing.T) {
	template, err := BuildSetupEnvTemplate("admin-secret")
	if err != nil {
		t.Fatalf("build setup env template: %v", err)
	}
	for _, key := range []string{
		"CHATAPI_MASTER_KEY=",
		"CHATAPI_SESSION_SECRET=",
		"CHATAPI_ADMIN_PASSWORD=admin-secret",
		"CHATAPI_DB_DRIVER=sqlite",
	} {
		if !strings.Contains(template, key) {
			t.Fatalf("template missing %q: %q", key, template)
		}
	}
}

func TestSetupServiceRunWritesEnvAndBlocksFurtherSetup(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	st, err := sqlitestore.Open(filepath.Join(tempDir, "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite store: %v", err)
	}

	svc := NewSetupService(st, config.Config{
		Mode:        config.ModeServe,
		EnvFilePath: envPath,
	})
	status, err := svc.Status(context.Background())
	if err != nil || !status.Available {
		t.Fatalf("expected initial setup availability, status=%#v err=%v", status, err)
	}

	report, err := svc.Run(context.Background(), SetupApplyInput{
		AdminPassword: "setup-admin-secret",
		WriteEnv:      true,
	})
	if err != nil {
		t.Fatalf("run setup: %v", err)
	}
	if !report.OK || !report.Written {
		t.Fatalf("unexpected setup report: %#v", report)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read written env: %v", err)
	}
	if !strings.Contains(string(data), "CHATAPI_ADMIN_PASSWORD=setup-admin-secret") {
		t.Fatalf("written env missing admin password: %q", string(data))
	}

	status, err = svc.Status(context.Background())
	if err == nil || status.Available || status.Reason != "admin_already_configured" {
		t.Fatalf("expected setup to become unavailable after writing env: status=%#v err=%v", status, err)
	}
}
