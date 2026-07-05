package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/observability"
	"github.com/zyf/chatapi/internal/platform/browser"
	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service"
)

func Run(ctx context.Context, args []string) error {
	backendRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve backend root: %w", err)
	}
	if err := config.LoadEnv(backendRoot); err != nil {
		return err
	}

	mode, err := parseMode(args)
	if err != nil {
		return err
	}
	cfg, err := config.FromEnv(mode, backendRoot)
	if err != nil {
		return err
	}

	logger := observability.NewLogger(cfg.LogLevel)
	logger.Info("starting chatapi", slog.String("mode", string(cfg.Mode)), slog.String("addr", cfg.ListenAddr()))

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	dataStore, err := sqlitestore.Open(cfg.DatabaseDSN)
	if err != nil {
		return err
	}
	if err := migrations.Bootstrap(ctx, dataStore.DB()); err != nil {
		return err
	}
	pendingRegistry := service.NewPendingRegistry()
	realtimeHub := service.NewRealtimeHub(dataStore)
	chatService := service.NewChatAPIService(dataStore, pendingRegistry, realtimeHub)

	server := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           httpapi.NewRouter(cfg, dataStore, chatService, realtimeHub, pendingRegistry),
		ReadHeaderTimeout: 10 * time.Second,
	}

	if cfg.OpenBrowser && cfg.Mode == config.ModeLab {
		go func() {
			time.Sleep(600 * time.Millisecond)
			url := buildLabURL(cfg)
			if err := browser.Open(url); err != nil {
				logger.Warn("open browser failed", slog.String("error", err.Error()), slog.String("url", url))
			}
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func parseMode(args []string) (config.Mode, error) {
	if len(args) == 0 {
		return config.ModeServe, nil
	}
	switch args[0] {
	case "serve":
		return config.ModeServe, nil
	case "lab":
		return config.ModeLab, nil
	default:
		return "", fmt.Errorf("unknown command %q, supported: serve, lab", args[0])
	}
}

func buildLabURL(cfg config.Config) string {
	host := cfg.Host
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s", host)
	url := fmt.Sprintf("%s:%d", base, cfg.Port)
	if cfg.LabPassword != "" {
		return fmt.Sprintf("%s/?password=%s", url, cfg.LabPassword)
	}
	if cfg.LabToken != "" {
		return fmt.Sprintf("%s/?token=%s", url, cfg.LabToken)
	}
	return url + "/"
}
