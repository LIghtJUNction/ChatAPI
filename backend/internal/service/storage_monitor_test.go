package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/repository/migrations"
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/store"
	"github.com/zyf/chatapi/internal/testutil/pgtest"
)

func TestStorageMonitorSummaryIncludesSQLiteDatabaseInfo(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	st, err := sqlitestore.Open(dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}

	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "sqlite"
	cfg.DatabaseDSN = dsn

	monitor := NewStorageMonitorService(cfg, st)
	summary, err := monitor.Summary(context.Background())
	if err != nil {
		t.Fatalf("storage summary: %v", err)
	}
	if summary.Database.Driver != "sqlite" {
		t.Fatalf("unexpected database driver: %#v", summary.Database)
	}
	if summary.Database.SQLitePath != dsn {
		t.Fatalf("unexpected sqlite path: %#v", summary.Database)
	}
	if summary.Database.PostgresMaxConns != 0 || summary.Database.PostgresTotalConns != 0 {
		t.Fatalf("sqlite summary should not expose postgres pool stats: %#v", summary.Database)
	}
}

func TestStorageMonitorSummaryIncludesPostgreSQLPoolInfo(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
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

	monitor := NewStorageMonitorService(cfg, st)
	summary, err := monitor.Summary(ctx)
	if err != nil {
		t.Fatalf("storage summary: %v", err)
	}
	if summary.Database.Driver != "postgresql" {
		t.Fatalf("unexpected postgres database driver: %#v", summary.Database)
	}
	if summary.Database.PostgresMaxConns <= 0 {
		t.Fatalf("expected postgres pool max conns: %#v", summary.Database)
	}
	if summary.Database.SQLitePath != "" || summary.Database.SQLiteWAL != "" {
		t.Fatalf("postgres summary should not expose sqlite paths: %#v", summary.Database)
	}
}

func TestStorageMonitorVacuumRejectsPostgreSQL(t *testing.T) {
	dsn := pgtest.IsolatedDSN(t)
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

	monitor := NewStorageMonitorService(cfg, st)
	preview, err := monitor.Vacuum(ctx, true)
	if err != nil {
		t.Fatalf("postgres vacuum dry-run: %v", err)
	}
	if preview.Before.Driver != "postgresql" || preview.After != nil {
		t.Fatalf("unexpected postgres vacuum dry-run response: %#v", preview)
	}

	_, err = monitor.Vacuum(ctx, false)
	if !errors.Is(err, ErrStorageVacuumUnsupported) {
		t.Fatalf("expected postgres vacuum unsupported error, got %v", err)
	}
}

func TestStorageMonitorDeleteOwnershipSelectionSkipsActiveAndReferencedUploads(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "chatapi.sqlite3")
	st, err := sqlitestore.Open(dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := migrations.Bootstrap(context.Background(), st.DB()); err != nil {
		t.Fatalf("bootstrap sqlite: %v", err)
	}

	cfg := config.Default(config.ModeServe, t.TempDir())
	cfg.DatabaseDriver = "sqlite"
	cfg.DatabaseDSN = dsn
	monitor := NewStorageMonitorService(cfg, st)

	if _, _, err := st.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_cleanup_keep",
		RequestID:      "req_cleanup_keep",
		ResponseID:     "resp_cleanup_keep",
		OwnerID:        "owner_cleanup",
		RequestFormat:  "responses",
		Model:          "cleanup-test",
		UserContent:    "keep /api/uploads/imgs/keep.png",
		RequestBody:    map[string]any{"model": "cleanup-test"},
	}); err != nil {
		t.Fatalf("create keep conversation: %v", err)
	}
	closedConversation, _, err := st.CreatePendingTurn(context.Background(), store.CreatePendingInput{
		ConversationID: "conv_cleanup_delete",
		RequestID:      "req_cleanup_delete",
		ResponseID:     "resp_cleanup_delete",
		OwnerID:        "owner_cleanup",
		RequestFormat:  "responses",
		Model:          "cleanup-test",
		UserContent:    "delete /api/uploads/imgs/delete.png",
		RequestBody:    map[string]any{"model": "cleanup-test"},
	})
	if err != nil {
		t.Fatalf("create delete conversation: %v", err)
	}
	if _, _, err := st.CompletePendingTurn(context.Background(), store.CompletePendingInput{
		ConversationID: closedConversation.ID,
		ResponseID:     "resp_cleanup_delete",
		OutputText:     "done",
		Mode:           "assistant_message",
	}); err != nil {
		t.Fatalf("complete delete conversation: %v", err)
	}
	for _, image := range []store.CreateUploadedImageInput{
		{ID: "img_keep", OwnerID: "owner_cleanup", Filename: "keep.png", OriginalFilename: "keep.png", ContentType: "image/png", Bytes: 12, URL: "/api/uploads/imgs/keep.png"},
		{ID: "img_delete", OwnerID: "owner_cleanup", Filename: "delete.png", OriginalFilename: "delete.png", ContentType: "image/png", Bytes: 34, URL: "/api/uploads/imgs/delete.png"},
	} {
		if _, err := st.CreateUploadedImage(context.Background(), image); err != nil {
			t.Fatalf("create image %s: %v", image.Filename, err)
		}
	}

	result, err := monitor.DeleteOwnershipSelection(context.Background(), "owner_cleanup", []string{"conv_cleanup_keep", "conv_cleanup_delete"}, []string{"keep.png"})
	if err != nil {
		t.Fatalf("delete ownership selection: %v", err)
	}
	if result.DeletedConversations != 1 || result.DeletedImages != 1 || result.DeletedImageBytes != 34 {
		t.Fatalf("unexpected cleanup selection delete result: %#v", result)
	}
	if len(result.SkippedActiveConversations) != 1 || result.SkippedActiveConversations[0] != "conv_cleanup_keep" {
		t.Fatalf("expected active conversation skip: %#v", result)
	}
	if len(result.SkippedReferencedUploads) != 1 || result.SkippedReferencedUploads[0] != "keep.png" {
		t.Fatalf("expected referenced upload skip: %#v", result)
	}
	if _, err := st.GetConversation(context.Background(), "conv_cleanup_delete"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected deleted conversation to be gone, got %v", err)
	}
	remaining, err := st.ListUploadedImagesByOwner(context.Background(), "owner_cleanup")
	if err != nil {
		t.Fatalf("list remaining uploads: %v", err)
	}
	if len(remaining) != 1 || remaining[0].Filename != "keep.png" {
		t.Fatalf("unexpected remaining uploads: %#v", remaining)
	}
}
