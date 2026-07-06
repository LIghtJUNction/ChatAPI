package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Mode string

const (
	ModeServe Mode = "serve"
	ModeLab   Mode = "lab"
)

type Config struct {
	Mode                                  Mode
	Host                                  string
	Port                                  int
	BaseURL                               string
	WebDistDir                            string
	DataDir                               string
	DatabaseDriver                        string
	DatabaseDSN                           string
	MasterKey                             string
	SessionSecret                         string
	AllowRemoteLab                        bool
	OpenBrowser                           bool
	LabToken                              string
	LabPassword                           string
	AdminPassword                         string
	LogLevel                              string
	CORSOrigins                           []string
	TrustedProxies                        []string
	MetricsEnabled                        bool
	UploadMaxBytes                        int64
	StorageDefaultQuotaBytes              int64
	StorageCleanupEnabled                 bool
	StorageCleanupTime                    string
	StorageCleanupKeepRecentConversations int
	StorageCleanupKeepRecentDays          int
	StorageVacuumEnabled                  bool
	PendingTurnTTL                        time.Duration
	RuntimeGOGC                           int
	RuntimeMemoryLimitBytes               int64
	RealtimeMaxConnections                int
	RealtimeMaxConnectionsPerUser         int
	RealtimeWebUIReservedPerUser          int

	SMTPEnabled  bool
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPSecurity string
	SMTPTimeout  time.Duration

	OIDCEnabled        bool
	OIDCProviderName   string
	OIDCIssuerURL      string
	OIDCClientID       string
	OIDCClientSecret   string
	OIDCRedirectURL    string
	OIDCScopes         []string
	OIDCAllowedDomains []string
	OIDCAllowedEmails  []string
	OIDCAdminEmails    []string
	OIDCAutoCreateUser bool
}

func LoadEnv(backendRoot string) error {
	candidates := []string{
		filepath.Join(backendRoot, ".env"),
		filepath.Join(backendRoot, ".env.local"),
		filepath.Join(filepath.Dir(backendRoot), ".env"),
	}
	if external := strings.TrimSpace(os.Getenv("CHATAPI_ENV_FILE")); external != "" {
		candidates = append(candidates, external)
	}
	for _, path := range candidates {
		_ = godotenv.Overload(path)
	}
	return nil
}

func Default(mode Mode, backendRoot string) Config {
	projectRoot := filepath.Dir(backendRoot)
	dataDir := filepath.Join(backendRoot, "data")
	host := "0.0.0.0"
	openBrowser := false
	masterKey := ""
	if mode == ModeLab {
		host = "127.0.0.1"
		openBrowser = true
		masterKey = "chatapi-lab-insecure-master-key"
	}
	return Config{
		Mode:                                  mode,
		Host:                                  host,
		Port:                                  5000,
		BaseURL:                               "",
		WebDistDir:                            filepath.Join(projectRoot, "frontend", "dist"),
		DataDir:                               dataDir,
		DatabaseDriver:                        "sqlite",
		DatabaseDSN:                           filepath.Join(dataDir, "chatapi.sqlite3"),
		MasterKey:                             masterKey,
		SessionSecret:                         "",
		AllowRemoteLab:                        false,
		OpenBrowser:                           openBrowser,
		LabToken:                              "",
		LabPassword:                           "",
		AdminPassword:                         "",
		LogLevel:                              "info",
		CORSOrigins:                           []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		TrustedProxies:                        nil,
		MetricsEnabled:                        false,
		UploadMaxBytes:                        10 << 20,
		StorageDefaultQuotaBytes:              0,
		StorageCleanupEnabled:                 false,
		StorageCleanupTime:                    "03:00",
		StorageCleanupKeepRecentConversations: 100,
		StorageCleanupKeepRecentDays:          30,
		StorageVacuumEnabled:                  false,
		PendingTurnTTL:                        0,
		RuntimeGOGC:                           0,
		RuntimeMemoryLimitBytes:               0,
		RealtimeMaxConnections:                0,
		RealtimeMaxConnectionsPerUser:         0,
		RealtimeWebUIReservedPerUser:          1,
		SMTPEnabled:                           false,
		SMTPHost:                              "",
		SMTPPort:                              587,
		SMTPUsername:                          "",
		SMTPPassword:                          "",
		SMTPFrom:                              "",
		SMTPSecurity:                          "starttls",
		SMTPTimeout:                           10 * time.Second,

		OIDCEnabled:        false,
		OIDCProviderName:   "",
		OIDCIssuerURL:      "",
		OIDCClientID:       "",
		OIDCClientSecret:   "",
		OIDCRedirectURL:    "",
		OIDCScopes:         []string{"openid", "email", "profile"},
		OIDCAllowedDomains: nil,
		OIDCAllowedEmails:  nil,
		OIDCAdminEmails:    nil,
		OIDCAutoCreateUser: false,
	}
}

func FromEnv(mode Mode, backendRoot string) (Config, error) {
	cfg, err := FromEnvUnchecked(mode, backendRoot)
	if err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func FromEnvUnchecked(mode Mode, backendRoot string) (Config, error) {
	cfg := Default(mode, backendRoot)
	cfg.Host = firstNonEmpty(os.Getenv("CHATAPI_HOST"), cfg.Host)
	cfg.BaseURL = strings.TrimSpace(os.Getenv("CHATAPI_BASE_URL"))
	cfg.WebDistDir = firstNonEmpty(os.Getenv("CHATAPI_WEB_DIST_DIR"), cfg.WebDistDir)
	cfg.DataDir = firstNonEmpty(os.Getenv("CHATAPI_DATA_DIR"), cfg.DataDir)
	cfg.DatabaseDriver = firstNonEmpty(os.Getenv("CHATAPI_DB_DRIVER"), cfg.DatabaseDriver)
	cfg.DatabaseDSN = firstNonEmpty(os.Getenv("CHATAPI_DB_DSN"), cfg.DatabaseDSN)
	if (cfg.DatabaseDriver == "postgres" || cfg.DatabaseDriver == "postgresql") && strings.TrimSpace(os.Getenv("CHATAPI_DB_DSN")) == "" {
		cfg.DatabaseDSN = ""
	}
	cfg.MasterKey = firstNonEmpty(os.Getenv("CHATAPI_MASTER_KEY"), cfg.MasterKey)
	cfg.SessionSecret = firstNonEmpty(os.Getenv("CHATAPI_SESSION_SECRET"), cfg.SessionSecret)
	cfg.LogLevel = strings.ToLower(firstNonEmpty(os.Getenv("CHATAPI_LOG_LEVEL"), cfg.LogLevel))
	cfg.LabToken = strings.TrimSpace(os.Getenv("CHATAPI_LAB_TOKEN"))
	cfg.LabPassword = strings.TrimSpace(os.Getenv("CHATAPI_LAB_PASSWORD"))
	cfg.AdminPassword = strings.TrimSpace(os.Getenv("CHATAPI_ADMIN_PASSWORD"))
	cfg.MetricsEnabled = parseBool(os.Getenv("CHATAPI_METRICS_ENABLED"), cfg.MetricsEnabled)
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_UPLOAD_MAX_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_UPLOAD_MAX_BYTES: %w", err)
		}
		cfg.UploadMaxBytes = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES: %w", err)
		}
		cfg.StorageDefaultQuotaBytes = value
	}
	cfg.StorageCleanupEnabled = parseBool(os.Getenv("CHATAPI_STORAGE_CLEANUP_ENABLED"), cfg.StorageCleanupEnabled)
	cfg.StorageCleanupTime = firstNonEmpty(os.Getenv("CHATAPI_STORAGE_CLEANUP_TIME"), cfg.StorageCleanupTime)
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_CONVERSATIONS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_CONVERSATIONS: %w", err)
		}
		cfg.StorageCleanupKeepRecentConversations = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_DAYS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_DAYS: %w", err)
		}
		cfg.StorageCleanupKeepRecentDays = value
	}
	cfg.StorageVacuumEnabled = parseBool(os.Getenv("CHATAPI_STORAGE_VACUUM_ENABLED"), cfg.StorageVacuumEnabled)
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_PENDING_TURN_TTL")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_PENDING_TURN_TTL: %w", err)
		}
		cfg.PendingTurnTTL = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_RUNTIME_GOGC")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_RUNTIME_GOGC: %w", err)
		}
		cfg.RuntimeGOGC = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES: %w", err)
		}
		cfg.RuntimeMemoryLimitBytes = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_REALTIME_MAX_CONNECTIONS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_REALTIME_MAX_CONNECTIONS: %w", err)
		}
		cfg.RealtimeMaxConnections = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER: %w", err)
		}
		cfg.RealtimeMaxConnectionsPerUser = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER: %w", err)
		}
		cfg.RealtimeWebUIReservedPerUser = value
	}
	cfg.SMTPEnabled = parseBool(os.Getenv("CHATAPI_SMTP_ENABLED"), cfg.SMTPEnabled)
	cfg.SMTPHost = strings.TrimSpace(os.Getenv("CHATAPI_SMTP_HOST"))
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_SMTP_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_SMTP_PORT: %w", err)
		}
		cfg.SMTPPort = port
	}
	cfg.SMTPUsername = strings.TrimSpace(os.Getenv("CHATAPI_SMTP_USERNAME"))
	cfg.SMTPPassword = strings.TrimSpace(os.Getenv("CHATAPI_SMTP_PASSWORD"))
	cfg.SMTPFrom = strings.TrimSpace(os.Getenv("CHATAPI_SMTP_FROM"))
	cfg.SMTPSecurity = strings.ToLower(firstNonEmpty(os.Getenv("CHATAPI_SMTP_SECURITY"), cfg.SMTPSecurity))
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_SMTP_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_SMTP_TIMEOUT: %w", err)
		}
		cfg.SMTPTimeout = timeout
	}

	cfg.OIDCEnabled = parseBool(os.Getenv("CHATAPI_OIDC_ENABLED"), cfg.OIDCEnabled)
	cfg.OIDCProviderName = strings.TrimSpace(os.Getenv("CHATAPI_OIDC_PROVIDER_NAME"))
	cfg.OIDCIssuerURL = strings.TrimSpace(os.Getenv("CHATAPI_OIDC_ISSUER_URL"))
	cfg.OIDCClientID = strings.TrimSpace(os.Getenv("CHATAPI_OIDC_CLIENT_ID"))
	cfg.OIDCClientSecret = strings.TrimSpace(os.Getenv("CHATAPI_OIDC_CLIENT_SECRET"))
	cfg.OIDCRedirectURL = strings.TrimSpace(os.Getenv("CHATAPI_OIDC_REDIRECT_URL"))
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_OIDC_SCOPES")); raw != "" {
		cfg.OIDCScopes = splitCSV(raw)
	}
	cfg.OIDCAllowedDomains = splitCSV(os.Getenv("CHATAPI_OIDC_ALLOWED_DOMAINS"))
	cfg.OIDCAllowedEmails = splitCSV(os.Getenv("CHATAPI_OIDC_ALLOWED_EMAILS"))
	cfg.OIDCAdminEmails = splitCSV(os.Getenv("CHATAPI_OIDC_ADMIN_EMAILS"))
	cfg.OIDCAutoCreateUser = parseBool(os.Getenv("CHATAPI_OIDC_AUTO_CREATE_USER"), cfg.OIDCAutoCreateUser)

	if raw := strings.TrimSpace(os.Getenv("CHATAPI_PORT")); raw != "" {
		port, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_PORT: %w", err)
		}
		cfg.Port = port
	}

	if raw := strings.TrimSpace(os.Getenv("CHATAPI_CORS_ORIGINS")); raw != "" {
		cfg.CORSOrigins = splitCSV(raw)
	}
	cfg.TrustedProxies = splitCSV(os.Getenv("CHATAPI_TRUSTED_PROXIES"))

	cfg.AllowRemoteLab = parseBool(os.Getenv("CHATAPI_ALLOW_REMOTE_LAB"), cfg.AllowRemoteLab)
	cfg.OpenBrowser = parseBool(os.Getenv("CHATAPI_OPEN_BROWSER"), cfg.OpenBrowser)

	if !filepath.IsAbs(cfg.WebDistDir) {
		cfg.WebDistDir = filepath.Join(backendRoot, cfg.WebDistDir)
	}
	if !filepath.IsAbs(cfg.DataDir) {
		cfg.DataDir = filepath.Join(backendRoot, cfg.DataDir)
	}
	if cfg.DatabaseDriver == "sqlite" && !filepath.IsAbs(cfg.DatabaseDSN) {
		cfg.DatabaseDSN = filepath.Join(backendRoot, cfg.DatabaseDSN)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if c.Mode == ModeLab {
		if c.Port < 0 || c.Port > 65535 {
			return errors.New("lab port must be within 0-65535")
		}
	} else if c.Port <= 0 || c.Port > 65535 {
		return errors.New("port must be within 1-65535")
	}
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("host is required")
	}
	if c.Mode == ModeLab && c.AllowRemoteLab && c.Host == "127.0.0.1" {
		return errors.New("remote lab requires non-loopback host")
	}
	if c.Mode == ModeLab && c.Host != "127.0.0.1" && !c.AllowRemoteLab {
		return errors.New("non-local lab host requires CHATAPI_ALLOW_REMOTE_LAB=1")
	}
	if c.Mode == ModeLab && strings.TrimSpace(c.LabToken) == "" && strings.TrimSpace(c.LabPassword) == "" && c.Host != "127.0.0.1" {
		return errors.New("remote lab requires token or password")
	}
	if c.DatabaseDriver == "sqlite" && strings.TrimSpace(c.DatabaseDSN) == "" {
		return errors.New("sqlite database dsn is required")
	}
	if (c.DatabaseDriver == "postgres" || c.DatabaseDriver == "postgresql") && strings.TrimSpace(c.DatabaseDSN) == "" {
		return errors.New("postgresql database dsn is required")
	}
	if c.UploadMaxBytes <= 0 {
		return errors.New("upload max bytes must be positive")
	}
	if c.StorageDefaultQuotaBytes < 0 {
		return errors.New("storage default quota bytes must be non-negative")
	}
	if c.StorageCleanupKeepRecentConversations < 0 {
		return errors.New("storage cleanup keep recent conversations must be non-negative")
	}
	if c.StorageCleanupKeepRecentDays < 0 {
		return errors.New("storage cleanup keep recent days must be non-negative")
	}
	if c.StorageCleanupEnabled {
		if _, _, err := ParseDailyTime(c.StorageCleanupTime); err != nil {
			return err
		}
	}
	if c.PendingTurnTTL < 0 {
		return errors.New("pending turn ttl must be non-negative")
	}
	if c.RuntimeGOGC < 0 {
		return errors.New("runtime gogc must be non-negative")
	}
	if c.RuntimeMemoryLimitBytes < 0 {
		return errors.New("runtime memory limit bytes must be non-negative")
	}
	if c.RealtimeMaxConnections < 0 {
		return errors.New("realtime max connections must be non-negative")
	}
	if c.RealtimeMaxConnectionsPerUser < 0 {
		return errors.New("realtime max connections per user must be non-negative")
	}
	if c.RealtimeWebUIReservedPerUser < 0 {
		return errors.New("realtime webui reserved per user must be non-negative")
	}
	if c.SMTPEnabled {
		if strings.TrimSpace(c.SMTPHost) == "" {
			return errors.New("smtp host is required when smtp is enabled")
		}
		if c.SMTPPort <= 0 || c.SMTPPort > 65535 {
			return errors.New("smtp port must be within 1-65535")
		}
		if strings.TrimSpace(c.SMTPFrom) == "" {
			return errors.New("smtp from is required when smtp is enabled")
		}
		switch c.SMTPSecurity {
		case "none", "starttls", "tls":
		default:
			return errors.New("smtp security must be one of none, starttls, tls")
		}
		if c.SMTPTimeout <= 0 {
			return errors.New("smtp timeout must be positive")
		}
	}
	if c.OIDCEnabled {
		if strings.TrimSpace(c.OIDCIssuerURL) == "" {
			return errors.New("oidc issuer url is required when oidc is enabled")
		}
		if strings.TrimSpace(c.OIDCClientID) == "" {
			return errors.New("oidc client id is required when oidc is enabled")
		}
		if strings.TrimSpace(c.OIDCClientSecret) == "" {
			return errors.New("oidc client secret is required when oidc is enabled")
		}
		if strings.TrimSpace(c.OIDCRedirectURL) == "" {
			return errors.New("oidc redirect url is required when oidc is enabled")
		}
		if !containsString(c.OIDCScopes, "openid") {
			return errors.New("oidc scopes must include openid")
		}
	}
	return nil
}

func (c Config) ListenAddr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func ParseDailyTime(value string) (hour int, minute int, err error) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("storage cleanup time must use HH:MM")
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, errors.New("storage cleanup time must use HH:MM")
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, errors.New("storage cleanup time must use HH:MM")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, errors.New("storage cleanup time must use HH:MM")
	}
	return hour, minute, nil
}

func parseBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}
