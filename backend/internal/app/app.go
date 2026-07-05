package app

import (
	"context"
	"encoding/json"
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

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func Run(ctx context.Context, args []string) error {
	backendRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve backend root: %w", err)
	}
	if err := config.LoadEnv(backendRoot); err != nil {
		return err
	}

	if len(args) > 0 && args[0] == "doctor" {
		return runDoctor(args[1:], backendRoot)
	}
	if len(args) > 0 && args[0] == "db" {
		return runDB(args[1:], backendRoot)
	}
	if len(args) > 0 && args[0] == "config" {
		return runConfig(args[1:], backendRoot)
	}
	if len(args) > 0 && args[0] == "version" {
		return runVersion()
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

type versionReport struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

func runVersion() error {
	return writeJSONReport(os.Stdout, versionReport{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
	})
}

func runDoctor(args []string, backendRoot string) error {
	mode := config.ModeServe
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			mode = config.ModeServe
		case "lab":
			mode = config.ModeLab
		default:
			return fmt.Errorf("unknown doctor mode %q, supported: serve, lab", args[0])
		}
	}
	cfg, loadErr := config.FromEnvUnchecked(mode, backendRoot)
	var validationErr error
	if loadErr == nil {
		validationErr = cfg.Validate()
	}
	report := config.Diagnose(cfg, errors.Join(loadErr, validationErr))
	if err := writeJSONReport(os.Stdout, report); err != nil {
		return err
	}
	if report.HasErrors() {
		return config.ErrDoctorFailed
	}
	return nil
}

type configPrintReport struct {
	OK     bool                  `json:"ok"`
	Config config.RedactedConfig `json:"config"`
	Error  string                `json:"error,omitempty"`
}

func runConfig(args []string, backendRoot string) error {
	if len(args) < 2 || args[0] != "print" || args[1] != "--redact" {
		return fmt.Errorf("unknown config command, supported: print --redact [serve|lab]")
	}
	mode := config.ModeServe
	if len(args) > 2 {
		switch args[2] {
		case "serve":
			mode = config.ModeServe
		case "lab":
			mode = config.ModeLab
		default:
			return fmt.Errorf("unknown config print mode %q, supported: serve, lab", args[2])
		}
	}
	cfg, loadErr := config.FromEnvUnchecked(mode, backendRoot)
	report := configPrintReport{
		OK:     loadErr == nil,
		Config: cfg.Redacted(),
	}
	if loadErr != nil {
		report.Error = loadErr.Error()
		_ = writeJSONReport(os.Stdout, report)
		return loadErr
	}
	return writeJSONReport(os.Stdout, report)
}

type dbCheckReport struct {
	OK     bool              `json:"ok"`
	Driver string            `json:"driver"`
	DSN    string            `json:"dsn"`
	Status migrations.Status `json:"status,omitempty"`
	Error  string            `json:"error,omitempty"`
}

func runDB(args []string, backendRoot string) error {
	if len(args) == 0 || args[0] != "check" {
		return fmt.Errorf("unknown db command, supported: check")
	}
	cfg, err := config.FromEnvUnchecked(config.ModeServe, backendRoot)
	if err != nil {
		return err
	}
	report := dbCheckReport{
		OK:     true,
		Driver: cfg.DatabaseDriver,
		DSN:    cfg.DatabaseDSN,
	}
	if cfg.DatabaseDriver != "sqlite" {
		report.OK = false
		report.Error = "only sqlite db check is implemented in the current Go refactor branch"
		_ = writeJSONReport(os.Stdout, report)
		return errors.New(report.Error)
	}
	dataStore, err := sqlitestore.Open(cfg.DatabaseDSN)
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		_ = writeJSONReport(os.Stdout, report)
		return err
	}
	defer dataStore.DB().Close()
	if err := migrations.Bootstrap(context.Background(), dataStore.DB()); err != nil {
		report.OK = false
		report.Error = err.Error()
		_ = writeJSONReport(os.Stdout, report)
		return err
	}
	status, err := migrations.StatusReport(context.Background(), dataStore.DB())
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		_ = writeJSONReport(os.Stdout, report)
		return err
	}
	report.Status = status
	if status.MigrationDirty {
		report.OK = false
		report.Error = "migration dirty"
		_ = writeJSONReport(os.Stdout, report)
		return errors.New(report.Error)
	}
	return writeJSONReport(os.Stdout, report)
}

func writeJSONReport(file *os.File, payload any) error {
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
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
		return "", fmt.Errorf("unknown command %q, supported: serve, lab, doctor, db check, config print --redact, version", args[0])
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
