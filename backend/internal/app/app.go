package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/observability"
	"github.com/zyf/chatapi/internal/platform/browser"
	"github.com/zyf/chatapi/internal/platform/email"
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
	if len(args) > 0 && args[0] == "migrate" {
		return runMigrate(ctx, args[1:], backendRoot)
	}
	if len(args) > 0 && args[0] == "config" {
		return runConfig(args[1:], backendRoot)
	}
	if len(args) > 0 && args[0] == "smtp" {
		return runSMTP(ctx, args[1:], backendRoot)
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
	realtimeHub := service.NewRealtimeHub(dataStore, service.NewRealtimeLimits(
		cfg.RealtimeMaxConnections,
		cfg.RealtimeMaxConnectionsPerUser,
		cfg.RealtimeWebUIReservedPerUser,
	))
	chatService := service.NewChatAPIService(dataStore, pendingRegistry, realtimeHub)
	startPendingExpirationWorker(ctx, cfg, chatService, logger)
	startStorageMaintenanceWorker(ctx, cfg, dataStore, logger)

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

func startPendingExpirationWorker(ctx context.Context, cfg config.Config, chatService *service.ChatAPIService, logger *slog.Logger) {
	if cfg.PendingTurnTTL <= 0 {
		return
	}
	interval := cfg.PendingTurnTTL / 2
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	if interval > 15*time.Minute {
		interval = 15 * time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := chatService.ExpirePendingTurns(ctx, cfg.PendingTurnTTL, time.Now().UTC())
				if err != nil {
					logger.Warn("expire pending turns failed", slog.String("error", err.Error()))
					continue
				}
				if result.ExpiredConversations > 0 || result.ExpiredActiveTurns > 0 {
					logger.Info(
						"expired pending turns",
						slog.Int("expired_conversations", result.ExpiredConversations),
						slog.Int("expired_active_turns", result.ExpiredActiveTurns),
					)
				}
			}
		}
	}()
}

func startStorageMaintenanceWorker(ctx context.Context, cfg config.Config, dataStore *sqlitestore.Store, logger *slog.Logger) {
	if !cfg.StorageCleanupEnabled {
		return
	}
	monitor := service.NewStorageMonitorService(cfg, dataStore)
	audit := service.NewAuditService(dataStore)
	actor := service.RequestActor{
		UserID:   "system",
		Username: "system",
		Role:     "system",
		Source:   "scheduler",
	}
	go func() {
		for {
			wait, err := durationUntilDailyRun(time.Now(), cfg.StorageCleanupTime)
			if err != nil {
				logger.Warn("storage maintenance disabled due to invalid time", slog.String("error", err.Error()))
				return
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			runCtx := service.WithRequestActor(ctx, actor)
			result, err := monitor.DeleteCleanupCandidates(runCtx, service.StorageCleanupPreviewInput{
				KeepRecentConversations: cfg.StorageCleanupKeepRecentConversations,
				KeepRecentDays:          cfg.StorageCleanupKeepRecentDays,
			})
			if err != nil {
				logger.Warn("storage scheduled cleanup failed", slog.String("error", err.Error()))
				audit.Record(runCtx, service.AuditEventInput{
					EventType:    "admin.storage",
					ResourceType: "storage",
					Action:       "scheduled_cleanup",
					Outcome:      "failure",
					Metadata: map[string]any{
						"error": err.Error(),
					},
				})
				continue
			}
			orphanResult, err := monitor.DeleteOrphanImages(runCtx)
			if err != nil {
				logger.Warn("storage scheduled orphan cleanup failed", slog.String("error", err.Error()))
			}
			checkpointed := false
			if err := monitor.Checkpoint(runCtx); err != nil {
				logger.Warn("storage scheduled checkpoint failed", slog.String("error", err.Error()))
			} else {
				checkpointed = true
			}
			vacuumed := false
			if cfg.StorageVacuumEnabled {
				if _, err := monitor.Vacuum(runCtx, false); err != nil {
					logger.Warn("storage scheduled vacuum failed", slog.String("error", err.Error()))
				} else {
					vacuumed = true
				}
			}
			audit.Record(runCtx, service.AuditEventInput{
				EventType:    "admin.storage",
				ResourceType: "storage",
				Action:       "scheduled_cleanup",
				Outcome:      "success",
				Metadata: map[string]any{
					"keep_recent_conversations": result.KeepRecentConversations,
					"keep_recent_days":          result.KeepRecentDays,
					"deleted_conversations":     result.DeletedConversations,
					"deleted_messages":          result.DeletedMessages,
					"deleted_images":            result.DeletedImages,
					"deleted_image_bytes":       result.DeletedImageBytes,
					"orphan_deleted_count":      orphanResult.DeletedCount,
					"orphan_deleted_bytes":      orphanResult.DeletedBytes,
					"checkpointed":              checkpointed,
					"vacuumed":                  vacuumed,
				},
			})
			logger.Info(
				"storage scheduled cleanup completed",
				slog.Int("deleted_conversations", result.DeletedConversations),
				slog.Int("deleted_messages", result.DeletedMessages),
				slog.Int("deleted_images", result.DeletedImages),
				slog.Int("orphan_deleted_count", orphanResult.DeletedCount),
				slog.Bool("checkpointed", checkpointed),
				slog.Bool("vacuumed", vacuumed),
			)
		}
	}()
}

func durationUntilDailyRun(now time.Time, dailyTime string) (time.Duration, error) {
	hour, minute, err := config.ParseDailyTime(dailyTime)
	if err != nil {
		return 0, err
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now), nil
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

type migrateReport struct {
	OK      bool              `json:"ok"`
	Command string            `json:"command"`
	Driver  string            `json:"driver"`
	DSN     string            `json:"dsn"`
	Status  migrations.Status `json:"status,omitempty"`
	Error   string            `json:"error,omitempty"`
}

type smtpTestReport struct {
	OK          bool                  `json:"ok"`
	DryRun      bool                  `json:"dry_run"`
	ConnectOnly bool                  `json:"connect_only"`
	Sent        bool                  `json:"sent"`
	To          string                `json:"to,omitempty"`
	Check       email.SMTPCheckReport `json:"check"`
	Error       string                `json:"error,omitempty"`
	Warning     string                `json:"warning,omitempty"`
}

func runSMTP(ctx context.Context, args []string, backendRoot string) error {
	if len(args) == 0 || args[0] != "test" {
		return fmt.Errorf("unknown smtp command, supported: test [--dry-run] [--connect-only] [--to email]")
	}
	options, err := parseSMTPTestOptions(args[1:])
	if err != nil {
		return err
	}
	cfg, err := config.FromEnvUnchecked(config.ModeServe, backendRoot)
	if err != nil {
		return err
	}
	smtpConfig := email.SMTPConfigFromConfig(cfg)
	check := email.CheckSMTPConfig(smtpConfig)
	report := smtpTestReport{
		OK:          check.OK,
		DryRun:      options.dryRun || (!options.connectOnly && options.to == ""),
		ConnectOnly: options.connectOnly,
		To:          options.to,
		Check:       check,
	}
	if options.to == "" && !options.dryRun && !options.connectOnly {
		report.Warning = "no recipient provided; ran dry-run only"
	}
	if !check.OK {
		report.Error = strings.Join(check.Errors, "; ")
		_ = writeJSONReport(os.Stdout, report)
		return errors.New(report.Error)
	}
	if report.DryRun {
		return writeJSONReport(os.Stdout, report)
	}
	if report.ConnectOnly {
		if err := email.NewSMTPSender(smtpConfig).CheckConnection(ctx); err != nil {
			report.OK = false
			report.Error = err.Error()
			_ = writeJSONReport(os.Stdout, report)
			return err
		}
		return writeJSONReport(os.Stdout, report)
	}
	message := email.Message{
		To:      []string{options.to},
		Subject: firstNonEmptyString(options.subject, "ChatAPI SMTP test"),
		Text:    "ChatAPI SMTP test email.",
	}
	if err := email.NewSMTPSender(smtpConfig).Send(ctx, message); err != nil {
		report.OK = false
		report.Error = err.Error()
		_ = writeJSONReport(os.Stdout, report)
		return err
	}
	report.Sent = true
	return writeJSONReport(os.Stdout, report)
}

type smtpTestOptions struct {
	dryRun      bool
	connectOnly bool
	to          string
	subject     string
}

func parseSMTPTestOptions(args []string) (smtpTestOptions, error) {
	options := smtpTestOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			options.dryRun = true
		case "--connect-only":
			options.connectOnly = true
		case "--to":
			if i+1 >= len(args) {
				return smtpTestOptions{}, errors.New("--to requires an email address")
			}
			i++
			options.to = strings.TrimSpace(args[i])
		case "--subject":
			if i+1 >= len(args) {
				return smtpTestOptions{}, errors.New("--subject requires a value")
			}
			i++
			options.subject = strings.TrimSpace(args[i])
		default:
			return smtpTestOptions{}, fmt.Errorf("unknown smtp test option %q", args[i])
		}
	}
	if options.dryRun && options.connectOnly {
		return smtpTestOptions{}, errors.New("--dry-run and --connect-only cannot be used together")
	}
	return options, nil
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
	status, err := sqliteMigrationStatus(context.Background(), cfg, true)
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

func runMigrate(ctx context.Context, args []string, backendRoot string) error {
	if len(args) == 0 {
		return fmt.Errorf("unknown migrate command, supported: up, status")
	}
	switch args[0] {
	case "up", "status":
	default:
		return fmt.Errorf("unknown migrate command %q, supported: up, status", args[0])
	}
	report, err := migrateCommand(ctx, args[0], backendRoot)
	if err != nil {
		_ = writeJSONReport(os.Stdout, report)
		return err
	}
	if err := writeJSONReport(os.Stdout, report); err != nil {
		return err
	}
	if report.Status.MigrationDirty {
		return errors.New("migration dirty")
	}
	return nil
}

func migrateCommand(ctx context.Context, command string, backendRoot string) (migrateReport, error) {
	cfg, err := config.FromEnvUnchecked(config.ModeServe, backendRoot)
	if err != nil {
		return migrateReport{OK: false, Command: command, Error: err.Error()}, err
	}
	report := migrateReport{
		OK:      true,
		Command: command,
		Driver:  cfg.DatabaseDriver,
		DSN:     cfg.DatabaseDSN,
	}
	status, err := sqliteMigrationStatus(ctx, cfg, command == "up")
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	report.Status = status
	if status.MigrationDirty {
		report.OK = false
		report.Error = "migration dirty"
	}
	return report, nil
}

func sqliteMigrationStatus(ctx context.Context, cfg config.Config, bootstrap bool) (migrations.Status, error) {
	if cfg.DatabaseDriver != "sqlite" {
		return migrations.Status{}, errors.New("only sqlite migration is implemented in the current Go refactor branch")
	}
	dataStore, err := sqlitestore.Open(cfg.DatabaseDSN)
	if err != nil {
		return migrations.Status{}, err
	}
	defer dataStore.DB().Close()
	if bootstrap {
		if err := migrations.Bootstrap(ctx, dataStore.DB()); err != nil {
			return migrations.Status{}, err
		}
	}
	return migrations.StatusReport(ctx, dataStore.DB())
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
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
		return "", fmt.Errorf("unknown command %q, supported: serve, lab, doctor, db check, migrate, config print --redact, smtp, version", args[0])
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
