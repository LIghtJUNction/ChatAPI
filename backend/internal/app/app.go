package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/config"
	httpapi "github.com/zyf/chatapi/internal/http"
	"github.com/zyf/chatapi/internal/migratedb"
	"github.com/zyf/chatapi/internal/observability"
	"github.com/zyf/chatapi/internal/platform/browser"
	"github.com/zyf/chatapi/internal/platform/email"
	"github.com/zyf/chatapi/internal/repository/migrations"
	pgstore "github.com/zyf/chatapi/internal/repository/postgresql"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

const sessionSecretConfigKey = "security.session_secret"

type runtimeStore interface {
	store.Store
}

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
	if len(args) > 0 && args[0] == "migrate-db" {
		return runMigrateDB(ctx, args[1:])
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
	if err := applyRuntimeOptions(&cfg, args[1:]); err != nil {
		return err
	}
	if cfg.Mode == config.ModeLab && strings.TrimSpace(cfg.LabPassword) == "" && strings.TrimSpace(cfg.LabToken) == "" {
		token, err := randomURLToken(24)
		if err != nil {
			return fmt.Errorf("generate lab token: %w", err)
		}
		cfg.LabToken = token
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	logger := observability.NewLogger(cfg.LogLevel)

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	dataStore, closeStore, err := openRuntimeStore(ctx, cfg, true)
	if err != nil {
		return err
	}
	defer closeStore()
	if err := ensureSessionSecret(ctx, &cfg, dataStore, logger); err != nil {
		return err
	}
	pendingRegistry := service.NewPendingRegistry()
	realtimeHub := service.NewRealtimeHub(dataStore, service.NewRealtimeLimits(
		cfg.RealtimeMaxConnections,
		cfg.RealtimeMaxConnectionsPerUser,
		cfg.RealtimeWebUIReservedPerUser,
	))
	chatService := service.NewChatAPIService(cfg, dataStore, pendingRegistry, realtimeHub)
	emailCodeService := service.NewEmailCodeService(dataStore, cfg.MasterKey, email.SMTPConfigFromConfig(cfg), nil)
	startPendingExpirationWorker(ctx, cfg, chatService, logger)
	startEmailCodeCleanupWorker(ctx, emailCodeService, logger)
	startStorageMaintenanceWorker(ctx, cfg, dataStore, logger)

	server := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           httpapi.NewRouter(cfg, dataStore, chatService, realtimeHub, pendingRegistry),
		ReadHeaderTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", cfg.ListenAddr())
	if err != nil {
		return err
	}
	defer listener.Close()
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok && tcpAddr.Port > 0 {
		cfg.Port = tcpAddr.Port
	}

	logger.Info("starting chatapi", slog.String("mode", string(cfg.Mode)), slog.String("addr", cfg.ListenAddr()))
	if cfg.Mode == config.ModeLab {
		logger.Info("lab access ready", slog.String("url", buildLabURL(cfg)), slog.String("models_url", fmt.Sprintf("http://%s/models", cfg.ListenAddr())))
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
		err := server.Serve(listener)
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

func ensureSessionSecret(ctx context.Context, cfg *config.Config, dataStore store.Store, logger *slog.Logger) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	if strings.TrimSpace(cfg.SessionSecret) != "" {
		return nil
	}
	if cfg.Mode == config.ModeLab {
		cfg.SessionSecret = "chatapi-lab-insecure-session-secret"
		return nil
	}
	item, err := dataStore.GetSystemConfig(ctx, sessionSecretConfigKey)
	if err == nil {
		if secret, _ := item.Value["secret"].(string); strings.TrimSpace(secret) != "" {
			cfg.SessionSecret = strings.TrimSpace(secret)
			return nil
		}
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("load session secret: %w", err)
	}
	secret, err := randomURLToken(32)
	if err != nil {
		return fmt.Errorf("generate session secret: %w", err)
	}
	_, err = dataStore.SetSystemConfig(ctx, store.SetSystemConfigInput{
		Key: sessionSecretConfigKey,
		Value: map[string]any{
			"secret":       secret,
			"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		return fmt.Errorf("persist session secret: %w", err)
	}
	cfg.SessionSecret = secret
	if logger != nil {
		logger.Info("generated and persisted session secret", slog.String("config_key", sessionSecretConfigKey))
	}
	return nil
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

func startStorageMaintenanceWorker(ctx context.Context, cfg config.Config, dataStore store.Store, logger *slog.Logger) {
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
			vacuumed := false
			if shouldRunStorageDBMaintenance(cfg) {
				if err := monitor.Checkpoint(runCtx); err != nil {
					logger.Warn("storage scheduled checkpoint failed", slog.String("error", err.Error()))
				} else {
					checkpointed = true
				}
				if cfg.StorageVacuumEnabled {
					if _, err := monitor.Vacuum(runCtx, false); err != nil {
						logger.Warn("storage scheduled vacuum failed", slog.String("error", err.Error()))
					} else {
						vacuumed = true
					}
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

func startEmailCodeCleanupWorker(ctx context.Context, emailCodeService *service.EmailCodeService, logger *slog.Logger) {
	if emailCodeService == nil {
		return
	}
	const interval = 15 * time.Minute
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleted, err := emailCodeService.CleanupExpired(ctx, time.Now().UTC())
				if err != nil {
					logger.Warn("cleanup expired auth verification codes failed", slog.String("error", err.Error()))
					continue
				}
				if deleted > 0 {
					logger.Info("cleaned expired auth verification codes", slog.Int("deleted", deleted))
				}
			}
		}
	}()
}

func shouldRunStorageDBMaintenance(cfg config.Config) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.DatabaseDriver), "sqlite")
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
	report, _ := doctorCommand(backendRoot, mode)
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

type doctorReport struct {
	config.DiagnosticReport
	DBMeta doctorDBMetaReport `json:"db_meta,omitempty"`
	Paths  doctorPathReport   `json:"paths,omitempty"`
}

type doctorDBMetaReport struct {
	Driver         string       `json:"driver"`
	DSN            string       `json:"dsn"`
	Reachable      bool         `json:"reachable"`
	Initialized    bool         `json:"initialized"`
	SchemaVersion  string       `json:"schema_version,omitempty"`
	AppVersion     string       `json:"app_version,omitempty"`
	MigrationDirty bool         `json:"migration_dirty,omitempty"`
	MigrationLock  string       `json:"migration_lock,omitempty"`
	CreatedBy      string       `json:"created_by,omitempty"`
	LastMigratedAt string       `json:"last_migrated_at,omitempty"`
	SQLite         sqliteDBInfo `json:"sqlite,omitempty"`
	Error          string       `json:"error,omitempty"`
}

type doctorPathReport struct {
	DataDir    doctorPathState `json:"data_dir"`
	WebDistDir doctorPathState `json:"web_dist_dir"`
	UploadsDir doctorPathState `json:"uploads_dir"`
}

type doctorPathState struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	IsDir    bool   `json:"is_dir"`
	Writable bool   `json:"writable,omitempty"`
	Error    string `json:"error,omitempty"`
	Note     string `json:"note,omitempty"`
}

func doctorCommand(backendRoot string, mode config.Mode) (doctorReport, error) {
	cfg, loadErr := config.FromEnvUnchecked(mode, backendRoot)
	var validationErr error
	if loadErr == nil {
		validationErr = cfg.Validate()
	}
	report := doctorReport{
		DiagnosticReport: config.Diagnose(cfg, errors.Join(loadErr, validationErr)),
		DBMeta: doctorDBMetaReport{
			Driver: strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver)),
			DSN:    cfg.Redacted().DatabaseDSN,
		},
		Paths: collectDoctorPaths(cfg),
	}
	if loadErr == nil {
		enrichDoctorDatabase(&report, cfg)
		enrichDoctorPaths(&report)
		report.OK = !report.HasErrors()
	}
	return report, nil
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
	SQLite sqliteDBInfo      `json:"sqlite,omitempty"`
	Error  string            `json:"error,omitempty"`
}

type sqliteDBInfo struct {
	Database sqliteFileInfo `json:"database"`
	WAL      sqliteFileInfo `json:"wal"`
	SHM      sqliteFileInfo `json:"shm"`
}

type sqliteFileInfo struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Bytes  int64  `json:"bytes"`
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
			"CHATAPI_SESSION_SECRET",
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
	template, err := service.BuildSetupEnvTemplate("")
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
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
	report, err := dbCheckCommand(backendRoot)
	if writeErr := writeJSONReport(os.Stdout, report); writeErr != nil {
		return writeErr
	}
	return err
}

func dbCheckCommand(backendRoot string) (dbCheckReport, error) {
	cfg, err := config.FromEnvUnchecked(config.ModeServe, backendRoot)
	if err != nil {
		return dbCheckReport{OK: false, Error: err.Error()}, err
	}
	report := dbCheckReport{
		OK:     true,
		Driver: cfg.DatabaseDriver,
		DSN:    cfg.DatabaseDSN,
	}
	if cfg.DatabaseDriver == "sqlite" {
		report.SQLite = sqliteDBFileInfo(cfg.DatabaseDSN)
	}
	status, err := runtimeMigrationStatus(context.Background(), cfg, true)
	if err != nil {
		report.OK = false
		report.Error = err.Error()
		return report, err
	}
	report.Status = status
	if status.MigrationDirty {
		report.OK = false
		report.Error = "migration dirty"
		return report, errors.New(report.Error)
	}
	if cfg.DatabaseDriver == "sqlite" {
		report.SQLite = sqliteDBFileInfo(cfg.DatabaseDSN)
	}
	return report, nil
}

func sqliteDBFileInfo(dsn string) sqliteDBInfo {
	return sqliteDBInfo{
		Database: sqliteFileStat(dsn),
		WAL:      sqliteFileStat(dsn + "-wal"),
		SHM:      sqliteFileStat(dsn + "-shm"),
	}
}

func sqliteFileStat(path string) sqliteFileInfo {
	info := sqliteFileInfo{Path: path}
	stat, err := os.Stat(path)
	if err != nil {
		return info
	}
	info.Exists = true
	info.Bytes = stat.Size()
	return info
}

func collectDoctorPaths(cfg config.Config) doctorPathReport {
	return doctorPathReport{
		DataDir:    inspectPath(cfg.DataDir, true),
		WebDistDir: inspectPath(cfg.WebDistDir, false),
		UploadsDir: inspectPath(filepath.Join(cfg.DataDir, "uploads", "imgs"), true),
	}
}

func enrichDoctorPaths(report *doctorReport) {
	if strings.TrimSpace(report.Paths.DataDir.Error) != "" {
		addDoctorItem(report, config.DiagnosticError, "path.data_dir_unusable", report.Paths.DataDir.Error)
	}
	if strings.TrimSpace(report.Paths.UploadsDir.Error) != "" {
		addDoctorItem(report, config.DiagnosticError, "path.uploads_dir_unusable", report.Paths.UploadsDir.Error)
	}
	if strings.TrimSpace(report.Paths.WebDistDir.Error) != "" {
		addDoctorItem(report, config.DiagnosticWarn, "path.web_dist_unusable", report.Paths.WebDistDir.Error)
	}
}

func enrichDoctorDatabase(report *doctorReport, cfg config.Config) {
	driver := strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver))
	if driver == "" {
		return
	}
	if driver == "sqlite" {
		report.DBMeta.SQLite = sqliteDBFileInfo(cfg.DatabaseDSN)
	}
	switch driver {
	case "sqlite":
		if strings.TrimSpace(cfg.DatabaseDSN) == "" {
			return
		}
	case "postgres", "postgresql":
		if strings.TrimSpace(cfg.DatabaseDSN) == "" {
			return
		}
	default:
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dataStore, closeStore, err := openRuntimeStore(ctx, cfg, false)
	if err != nil {
		report.DBMeta.Error = err.Error()
		addDoctorItem(report, config.DiagnosticError, "database.connection_failed", "数据库连接失败: "+err.Error())
		return
	}
	defer closeStore()
	if err := dataStore.Ping(ctx); err != nil {
		report.DBMeta.Error = err.Error()
		addDoctorItem(report, config.DiagnosticError, "database.ping_failed", "数据库连通性检查失败: "+err.Error())
		return
	}
	report.DBMeta.Reachable = true

	status, err := dataStore.MigrationStatus(ctx)
	if err != nil {
		report.DBMeta.Error = err.Error()
		if isUninitializedDatabaseError(err) {
			addDoctorItem(report, config.DiagnosticWarn, "database.schema_uninitialized", "数据库连接成功，但 schema 尚未初始化；先执行 `chatapi migrate up`。")
			return
		}
		addDoctorItem(report, config.DiagnosticError, "database.status_failed", "无法读取 migration 状态: "+err.Error())
		return
	}

	report.DBMeta.Initialized = true
	report.DBMeta.SchemaVersion = status.SchemaVersion
	report.DBMeta.AppVersion = status.AppVersion
	report.DBMeta.MigrationDirty = status.MigrationDirty
	report.DBMeta.MigrationLock = status.MigrationLock
	report.DBMeta.CreatedBy = status.CreatedBy
	report.DBMeta.LastMigratedAt = status.LastMigratedAt
	if status.MigrationDirty {
		addDoctorItem(report, config.DiagnosticError, "database.migration_dirty", "数据库 migration 处于 dirty 状态；先修复后再启动服务。")
		return
	}
	addDoctorItem(report, config.DiagnosticInfo, "database.connected", "数据库连接正常，migration 元数据可读。")
}

func inspectPath(path string, checkWritable bool) doctorPathState {
	state := doctorPathState{Path: path}
	path = strings.TrimSpace(path)
	if path == "" {
		state.Error = "path is empty"
		return state
	}
	stat, err := os.Stat(path)
	if err == nil {
		state.Exists = true
		state.IsDir = stat.IsDir()
		if !stat.IsDir() {
			state.Error = "path exists but is not a directory"
			return state
		}
		if checkWritable {
			if err := verifyDirWritable(path); err != nil {
				state.Error = "directory is not writable: " + err.Error()
				return state
			}
			state.Writable = true
		}
		return state
	}
	if !os.IsNotExist(err) {
		state.Error = "stat path failed: " + err.Error()
		return state
	}
	parent, parentInfo, parentErr := nearestExistingParent(path)
	if parentErr != nil {
		state.Error = "parent directory is unavailable: " + parentErr.Error()
		return state
	}
	if !parentInfo.IsDir() {
		state.Error = "parent path is not a directory"
		return state
	}
	if checkWritable {
		if err := verifyDirWritable(parent); err != nil {
			state.Error = "parent directory is not writable: " + err.Error()
			return state
		}
		state.Writable = true
	}
	state.Note = "directory does not exist yet"
	if checkWritable {
		state.Note = "directory does not exist yet; service will create it on demand"
	}
	return state
}

func verifyDirWritable(path string) error {
	file, err := os.CreateTemp(path, ".chatapi-doctor-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func nearestExistingParent(path string) (string, os.FileInfo, error) {
	current := filepath.Clean(path)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		stat, err := os.Stat(parent)
		if err == nil {
			return parent, stat, nil
		}
		if !os.IsNotExist(err) {
			return parent, nil, err
		}
		current = parent
	}
	return "", nil, os.ErrNotExist
}

func isUninitializedDatabaseError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "read db_meta") ||
		strings.Contains(message, "no such table: db_meta") ||
		strings.Contains(message, "relation \"db_meta\" does not exist") ||
		strings.Contains(message, "schema_migrations")
}

func addDoctorItem(report *doctorReport, severity string, code string, message string) {
	if report == nil || strings.TrimSpace(message) == "" {
		return
	}
	report.Items = append(report.Items, config.DiagnosticItem{
		Severity: severity,
		Code:     code,
		Message:  strings.TrimSpace(message),
	})
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

type migrateDBOptions struct {
	command  string
	sqlite   string
	postgres string
}

type migrateDBReport struct {
	OK       bool             `json:"ok"`
	Command  string           `json:"command"`
	SQLite   string           `json:"sqlite"`
	Postgres string           `json:"postgres"`
	Result   migratedb.Report `json:"result"`
	Error    string           `json:"error,omitempty"`
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

func runMigrateDB(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("unknown migrate-db command, supported: sqlite-to-postgres --sqlite <path> --postgres <dsn>")
	}
	options, err := parseMigrateDBOptions(args)
	if err != nil {
		return err
	}
	report, err := migrateDBCommand(ctx, options)
	if err != nil {
		_ = writeJSONReport(os.Stdout, report)
		return err
	}
	return writeJSONReport(os.Stdout, report)
}

func parseMigrateDBOptions(args []string) (migrateDBOptions, error) {
	options := migrateDBOptions{command: strings.TrimSpace(args[0])}
	if options.command != "sqlite-to-postgres" {
		return migrateDBOptions{}, fmt.Errorf("unknown migrate-db command %q, supported: sqlite-to-postgres --sqlite <path> --postgres <dsn>", options.command)
	}
	for index := 1; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch arg {
		case "--sqlite":
			index++
			if index >= len(args) {
				return migrateDBOptions{}, errors.New("--sqlite requires a path")
			}
			options.sqlite = strings.TrimSpace(args[index])
		case "--postgres":
			index++
			if index >= len(args) {
				return migrateDBOptions{}, errors.New("--postgres requires a dsn")
			}
			options.postgres = strings.TrimSpace(args[index])
		default:
			return migrateDBOptions{}, fmt.Errorf("unknown migrate-db option %q, supported: --sqlite, --postgres", arg)
		}
	}
	if options.sqlite == "" {
		return migrateDBOptions{}, errors.New("migrate-db sqlite-to-postgres requires --sqlite")
	}
	if options.postgres == "" {
		return migrateDBOptions{}, errors.New("migrate-db sqlite-to-postgres requires --postgres")
	}
	return options, nil
}

func migrateDBCommand(ctx context.Context, options migrateDBOptions) (migrateDBReport, error) {
	report := migrateDBReport{
		OK:       true,
		Command:  options.command,
		SQLite:   options.sqlite,
		Postgres: options.postgres,
	}
	switch options.command {
	case "sqlite-to-postgres":
		result, err := migratedb.SQLiteToPostgres(ctx, options.sqlite, options.postgres)
		report.Result = result
		if err != nil {
			report.OK = false
			report.Error = err.Error()
			return report, err
		}
		return report, nil
	default:
		report.OK = false
		report.Error = fmt.Sprintf("unsupported migrate-db command %q", options.command)
		return report, errors.New(report.Error)
	}
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
		status, err := runtimeMigrateDown(ctx, cfg)
		if err != nil {
			report.OK = false
			report.Error = err.Error()
			return report, err
		}
		report.Status = status
		return report, nil
	}
	status, err := runtimeMigrationStatus(ctx, cfg, options.command == "up")
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

func runtimeMigrateDown(ctx context.Context, cfg config.Config) (migrations.Status, error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver))
	switch driver {
	case "sqlite":
		dataStore, err := sqlitestore.Open(cfg.DatabaseDSN)
		if err != nil {
			return migrations.Status{}, err
		}
		defer dataStore.Close()
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
	case "postgres", "postgresql":
		dataStore, err := pgstore.Open(ctx, cfg.DatabaseDSN)
		if err != nil {
			return migrations.Status{}, err
		}
		defer dataStore.Close()
		before, err := runtimeMigrationStatus(ctx, cfg, false)
		if err != nil {
			return migrations.Status{}, err
		}
		if before.MigrationDirty {
			return migrations.Status{}, errors.New("migration dirty")
		}
		if err := pgstore.Reset(ctx, dataStore.Pool()); err != nil {
			return migrations.Status{}, err
		}
		return before, nil
	default:
		return migrations.Status{}, fmt.Errorf("unsupported database driver %q", cfg.DatabaseDriver)
	}
}

func runtimeMigrationStatus(ctx context.Context, cfg config.Config, bootstrap bool) (migrations.Status, error) {
	dataStore, closeStore, err := openRuntimeStore(ctx, cfg, bootstrap)
	if err != nil {
		return migrations.Status{}, err
	}
	defer closeStore()
	status, err := dataStore.MigrationStatus(ctx)
	if err != nil {
		return migrations.Status{}, err
	}
	return migrations.Status{
		SchemaVersion:  status.SchemaVersion,
		AppVersion:     status.AppVersion,
		MigrationDirty: status.MigrationDirty,
		MigrationLock:  status.MigrationLock,
		CreatedBy:      status.CreatedBy,
		LastMigratedAt: status.LastMigratedAt,
		Applied: func() []migrations.AppliedMigration {
			items := make([]migrations.AppliedMigration, 0, len(status.Applied))
			for _, item := range status.Applied {
				items = append(items, migrations.AppliedMigration{
					Version:   item.Version,
					Name:      item.Name,
					AppliedAt: item.AppliedAt,
					Checksum:  item.Checksum,
					Dirty:     item.Dirty,
				})
			}
			return items
		}(),
	}, nil
}

func openRuntimeStore(ctx context.Context, cfg config.Config, bootstrap bool) (runtimeStore, func(), error) {
	switch strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver)) {
	case "sqlite":
		dataStore, err := sqlitestore.Open(cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, err
		}
		if bootstrap {
			if err := migrations.Bootstrap(ctx, dataStore.DB()); err != nil {
				_ = dataStore.Close()
				return nil, nil, err
			}
		}
		return dataStore, func() { _ = dataStore.Close() }, nil
	case "postgres", "postgresql":
		if strings.TrimSpace(cfg.DatabaseDSN) == "" {
			return nil, nil, errors.New("postgresql database dsn is required")
		}
		dataStore, err := pgstore.Open(ctx, cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, err
		}
		if bootstrap {
			if err := pgstore.Bootstrap(ctx, dataStore.Pool()); err != nil {
				dataStore.Close()
				return nil, nil, err
			}
		}
		return dataStore, dataStore.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported database driver %q", cfg.DatabaseDriver)
	}
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
		return "", fmt.Errorf("unknown command %q, supported: serve, lab, doctor, db check, migrate, migrate-db, config print --redact, oidc test, smtp, setup, version", args[0])
	}
}

func applyRuntimeOptions(cfg *config.Config, args []string) error {
	if cfg == nil {
		return errors.New("config is nil")
	}
	switch cfg.Mode {
	case config.ModeLab:
		return applyLabOptions(cfg, args)
	case config.ModeServe:
		return applyServeOptions(cfg, args)
	default:
		return nil
	}
}

func applyServeOptions(cfg *config.Config, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	host := flags.String("host", cfg.Host, "")
	port := flags.Int("port", cfg.Port, "")
	dataDir := flags.String("data-dir", cfg.DataDir, "")
	openBrowser := flags.Bool("open-browser", cfg.OpenBrowser, "")
	noOpenBrowser := flags.Bool("no-open-browser", false, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg.Host = strings.TrimSpace(*host)
	cfg.Port = *port
	cfg.DataDir = strings.TrimSpace(*dataDir)
	cfg.OpenBrowser = *openBrowser
	if *noOpenBrowser {
		cfg.OpenBrowser = false
	}
	return nil
}

func applyLabOptions(cfg *config.Config, args []string) error {
	flags := flag.NewFlagSet("lab", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	host := flags.String("host", cfg.Host, "")
	port := flags.Int("port", cfg.Port, "")
	dataDir := flags.String("data-dir", cfg.DataDir, "")
	openBrowser := flags.Bool("open-browser", cfg.OpenBrowser, "")
	noOpenBrowser := flags.Bool("no-open-browser", false, "")
	allowRemote := flags.Bool("allow-remote-lab", cfg.AllowRemoteLab, "")
	password := flags.String("password", cfg.LabPassword, "")
	token := flags.String("token", cfg.LabToken, "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg.Host = strings.TrimSpace(*host)
	cfg.Port = *port
	cfg.DataDir = strings.TrimSpace(*dataDir)
	cfg.OpenBrowser = *openBrowser
	if *noOpenBrowser {
		cfg.OpenBrowser = false
	}
	cfg.AllowRemoteLab = *allowRemote
	cfg.LabPassword = strings.TrimSpace(*password)
	cfg.LabToken = strings.TrimSpace(*token)
	if cfg.LabPassword != "" {
		cfg.LabToken = ""
	}
	return nil
}

func buildLabURL(cfg config.Config) string {
	host := cfg.Host
	if host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	base := fmt.Sprintf("http://%s", host)
	url := fmt.Sprintf("%s:%d", base, cfg.Port)
	if cfg.LabToken != "" {
		return fmt.Sprintf("%s/?token=%s", url, cfg.LabToken)
	}
	return url + "/"
}
