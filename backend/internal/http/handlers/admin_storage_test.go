package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	"github.com/zyf/chatapi/internal/service"
)

func TestAdminStorageVacuumRejectsPostgreSQL(t *testing.T) {
	dsn := os.Getenv("CHATAPI_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("CHATAPI_PG_TEST_DSN is not set")
	}
	ctx := context.Background()
	st, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgresql: %v", err)
	}
	t.Cleanup(st.Close)
	if err := pgstore.Reset(ctx, st.Pool()); err != nil {
		t.Fatalf("reset postgresql: %v", err)
	}
	if err := pgstore.Bootstrap(ctx, st.Pool()); err != nil {
		t.Fatalf("bootstrap postgresql: %v", err)
	}

	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "postgresql"
	cfg.DatabaseDSN = dsn

	handler := AdminStorageHandler{
		Monitor: service.NewStorageMonitorService(cfg, st),
		Audit:   service.NewAuditService(st),
	}
	req := httptest.NewRequest(http.MethodPost, "/api/admin/storage/vacuum", bytes.NewBufferString(`{"dry_run":false}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.Vacuum(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "supports sqlite only") {
		t.Fatalf("unexpected response body: %q", rec.Body.String())
	}
}
