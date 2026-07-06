package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	if len(args) > 0 && args[0] == "oidc" {
		return runOIDC(ctx, args[1:], backendRoot)
	}
	if len(args) > 0 && args[0] == "setup" {
		return runSetup(args[1:], backendRoot)
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
			quotaResult, err := monitor.PruneOverQuotaUsers(runCtx, service.StorageCleanupPreviewInput{
				KeepRecentConversations: cfg.StorageCleanupKeepRecentConversations,
				KeepRecentDays:          cfg.StorageCleanupKeepRecentDays,
			})
			if err != nil {
				logger.Warn("storage scheduled quota prune failed", slog.String("error", err.Error()))
			}
			retryResult, err := monitor.RetryFileDeletionFailures(runCtx, 100)
			if err != nil {
				logger.Warn("storage scheduled file deletion retry failed", slog.String("error", err.Error()))
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
					"quota_checked_users":       quotaResult.CheckedUsers,
					"quota_over_quota":          quotaResult.OverQuota,
					"quota_pruned_users":        quotaResult.PrunedUsers,
					"retry_deleted_files":       retryResult.Deleted,
					"retry_failed_files":        retryResult.Failed,
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
				slog.Int("quota_over_quota", quotaResult.OverQuota),
				slog.Int("quota_pruned_users", quotaResult.PrunedUsers),
				slog.Int("retry_deleted_files", retryResult.Deleted),
				slog.Int("retry_failed_files", retryResult.Failed),
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
	Forced  bool              `json:"forced,omitempty"`
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

type oidcTestReport struct {
	OK                     bool     `json:"ok"`
	IssuerURL              string   `json:"issuer_url"`
	DiscoveryURL           string   `json:"discovery_url"`
	ProviderIssuer         string   `json:"provider_issuer,omitempty"`
	ClientIDConfigured     bool     `json:"client_id_configured"`
	ClientSecretConfigured bool     `json:"client_secret_configured"`
	RedirectURLConfigured  bool     `json:"redirect_url_configured"`
	Scopes                 []string `json:"scopes,omitempty"`
	AuthorizationEndpoint  string   `json:"authorization_endpoint,omitempty"`
	TokenEndpoint          string   `json:"token_endpoint,omitempty"`
	JWKSURI                string   `json:"jwks_uri,omitempty"`
	UserInfoEndpoint       string   `json:"userinfo_endpoint,omitempty"`
	Errors                 []string `json:"errors,omitempty"`
}

type setupReport struct {
	OK            bool     `json:"ok"`
	EnvPath       string   `json:"env_path"`
	Written       bool     `json:"written"`
	GeneratedKeys []string `json:"generated_keys"`
	ExistingEnv   bool     `json:"existing_env"`
	NextSteps     []string `json:"next_steps"`
	EnvTemplate   string   `json:"env_template,omitempty"`
	Error         string   `json:"error,omitempty"`
	GeneratedAt   string   `json:"generated_at"`
}

type setupOptions struct {
	writeEnv bool
	force    bool
}

func runOIDC(ctx context.Context, args []string, backendRoot string) error {
	if len(args) == 0 || args[0] != "test" {
		return fmt.Errorf("unknown oidc command, supported: test")
	}
	if len(args) > 1 {
		return fmt.Errorf("unknown oidc test option %q", args[1])
	}
	cfg, err := config.FromEnvUnchecked(config.ModeServe, backendRoot)
	if err != nil {
		return err
	}
	report, err := oidcTestCommand(ctx, cfg, &http.Client{Timeout: 10 * time.Second})
	if writeErr := writeJSONReport(os.Stdout, report); writeErr != nil {
		return writeErr
	}
	return err
}

func oidcTestCommand(ctx context.Context, cfg config.Config, client *http.Client) (oidcTestReport, error) {
	report := oidcTestReport{
		OK:                     true,
		IssuerURL:              strings.TrimRight(strings.TrimSpace(cfg.OIDCIssuerURL), "/"),
		ClientIDConfigured:     strings.TrimSpace(cfg.OIDCClientID) != "",
		ClientSecretConfigured: strings.TrimSpace(cfg.OIDCClientSecret) != "",
		RedirectURLConfigured:  strings.TrimSpace(cfg.OIDCRedirectURL) != "",
		Scopes:                 append([]string(nil), cfg.OIDCScopes...),
	}
	if client == nil {
		client = http.DefaultClient
	}
	if report.IssuerURL == "" {
		report.addError("CHATAPI_OIDC_ISSUER_URL is required")
		return report, errors.New(strings.Join(report.Errors, "; "))
	}
	discoveryURL, err := oidcDiscoveryURL(report.IssuerURL)
	if err != nil {
		report.addError(err.Error())
		return report, err
	}
	report.DiscoveryURL = discoveryURL
	if !report.ClientIDConfigured {
		report.addError("CHATAPI_OIDC_CLIENT_ID is required")
	}
	if !report.ClientSecretConfigured {
		report.addError("CHATAPI_OIDC_CLIENT_SECRET is required for private RP")
	}
	if !report.RedirectURLConfigured {
		report.addError("CHATAPI_OIDC_REDIRECT_URL is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		report.addError(err.Error())
		return report, err
	}
	resp, err := client.Do(req)
	if err != nil {
		report.addError(err.Error())
		return report, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("discovery endpoint returned HTTP %d", resp.StatusCode)
		report.addError(err.Error())
		return report, err
	}
	var discovery struct {
		Issuer                string `json:"issuer"`
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		JWKSURI               string `json:"jwks_uri"`
		UserInfoEndpoint      string `json:"userinfo_endpoint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		report.addError(err.Error())
		return report, err
	}
	report.ProviderIssuer = strings.TrimRight(strings.TrimSpace(discovery.Issuer), "/")
	report.AuthorizationEndpoint = strings.TrimSpace(discovery.AuthorizationEndpoint)
	report.TokenEndpoint = strings.TrimSpace(discovery.TokenEndpoint)
	report.JWKSURI = strings.TrimSpace(discovery.JWKSURI)
	report.UserInfoEndpoint = strings.TrimSpace(discovery.UserInfoEndpoint)
	if report.ProviderIssuer != report.IssuerURL {
		report.addError("discovery issuer does not match CHATAPI_OIDC_ISSUER_URL")
	}
	if report.AuthorizationEndpoint == "" {
		report.addError("discovery authorization_endpoint is missing")
	}
	if report.TokenEndpoint == "" {
		report.addError("discovery token_endpoint is missing")
	}
	if report.JWKSURI == "" {
		report.addError("discovery jwks_uri is missing")
	}
	if len(report.Errors) > 0 {
		return report, errors.New(strings.Join(report.Errors, "; "))
	}
	return report, nil
}

func (r *oidcTestReport) addError(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	r.OK = false
	r.Errors = append(r.Errors, strings.TrimSpace(message))
}

func oidcDiscoveryURL(issuer string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("CHATAPI_OIDC_ISSUER_URL must be an absolute URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/.well-known/openid-configuration"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func runSetup(args []string, backendRoot string) error {
	options, err := parseSetupOptions(args)
	if err != nil {
		return err
	}
	report, err := setupCommand(backendRoot, options)
	if writeErr := writeJSONReport(os.Stdout, report); writeErr != nil {
		return writeErr
	}
	return err
}

func parseSetupOptions(args []string) (setupOptions, error) {
	options := setupOptions{}
	for _, arg := range args {
		switch arg {
		case "--write-env":
			options.writeEnv = true
		case "--force":
			options.force = true
		default:
			return setupOptions{}, fmt.Errorf("unknown setup option %q, supported: --write-env, --force", arg)
		}
	}
	if options.force && !options.writeEnv {
		return setupOptions{}, errors.New("--force requires --write-env")
	}
	return options, nil
}

func setupCommand(backendRoot string, options setupOptions) (setupReport, error) {
	envPath := filepath.Join(backendRoot, ".env")
	existingEnv := fileExists(envPath)
	report := setupReport{
		OK:          true,
		EnvPath:     envPath,
		ExistingEnv: existingEnv,
		GeneratedKeys: []string{
			"CHATAPI_MASTER_KEY",
			"CHATAPI_ADMIN_PASSWORD",
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		NextSteps: []string{
			"review generated .env values",
			"run chatapi doctor serve",
			"run chatapi migrate up",
			"start chatapi serve",
		},
	}
	masterKey, err := randomURLToken(48)
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	adminPassword, err := randomURLToken(24)
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	template := strings.Join([]string{
		"CHATAPI_MASTER_KEY=" + masterKey,
		"CHATAPI_ADMIN_PASSWORD=" + adminPassword,
		"CHATAPI_DB_DRIVER=sqlite",
		"CHATAPI_DB_DSN=./data/chatapi.sqlite3",
		"CHATAPI_DATA_DIR=./data",
		"CHATAPI_LOG_LEVEL=info",
		"CHATAPI_METRICS_ENABLED=0",
		"",
	}, "\n")
	if !options.writeEnv {
		report.EnvTemplate = template
		return report, nil
	}
	if existingEnv && !options.force {
		report.OK = false
		report.Error = ".env already exists; pass --force to overwrite"
		return report, errors.New(report.Error)
	}
	if err := os.WriteFile(envPath, []byte(template), 0o600); err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	report.Written = true
	return report, nil
}

func randomURLToken(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		return "", errors.New("token byte length must be positive")
	}
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
		return fmt.Errorf("unknown migrate command, supported: up, status, down --force")
	}
	options, err := parseMigrateOptions(args)
	if err != nil {
		return err
	}
	report, err := migrateCommand(ctx, options, backendRoot)
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

type migrateOptions struct {
	command string
	force   bool
}

func parseMigrateOptions(args []string) (migrateOptions, error) {
	options := migrateOptions{command: args[0]}
	switch options.command {
	case "up", "status":
		if len(args) > 1 {
			return migrateOptions{}, fmt.Errorf("migrate %s does not accept options", options.command)
		}
	case "down":
		for _, arg := range args[1:] {
			switch arg {
			case "--force":
				options.force = true
			default:
				return migrateOptions{}, fmt.Errorf("unknown migrate down option %q, supported: --force", arg)
			}
		}
		if !options.force {
			return migrateOptions{}, errors.New("migrate down requires --force")
		}
	default:
		return migrateOptions{}, fmt.Errorf("unknown migrate command %q, supported: up, status, down --force", options.command)
	}
	return options, nil
}

func migrateCommand(ctx context.Context, options migrateOptions, backendRoot string) (migrateReport, error) {
	cfg, err := config.FromEnvUnchecked(config.ModeServe, backendRoot)
	if err != nil {
		return migrateReport{OK: false, Command: options.command, Forced: options.force, Error: err.Error()}, err
	}
	report := migrateReport{
		OK:      true,
		Command: options.command,
		Driver:  cfg.DatabaseDriver,
		DSN:     cfg.DatabaseDSN,
		Forced:  options.force,
	}
	if options.command == "down" {
		status, err := sqliteMigrateDown(ctx, cfg)
		if err != nil {
			report.OK = false
			report.Error = err.Error()
			return report, err
		}
		report.Status = status
		return report, nil
	}
	status, err := sqliteMigrationStatus(ctx, cfg, options.command == "up")
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

func sqliteMigrateDown(ctx context.Context, cfg config.Config) (migrations.Status, error) {
	if cfg.DatabaseDriver != "sqlite" {
		return migrations.Status{}, errors.New("only sqlite migration is implemented in the current Go refactor branch")
	}
	dataStore, err := sqlitestore.Open(cfg.DatabaseDSN)
	if err != nil {
		return migrations.Status{}, err
	}
	defer dataStore.DB().Close()
	before, err := migrations.StatusReport(ctx, dataStore.DB())
	if err != nil {
		return migrations.Status{}, err
	}
	if before.MigrationDirty {
		return migrations.Status{}, errors.New("migration dirty")
	}
	if err := migrations.Reset(ctx, dataStore.DB()); err != nil {
		return migrations.Status{}, err
	}
	return before, nil
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
		return "", fmt.Errorf("unknown command %q, supported: serve, lab, doctor, db check, migrate, config print --redact, oidc test, smtp, setup, version", args[0])
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
