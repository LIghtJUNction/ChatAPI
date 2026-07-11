package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
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
	settingsEnvironment                   map[string]bool
	Mode                                  Mode
	Host                                  string
	Port                                  int
	BaseURL                               string
	EnvFilePath                           string
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
	AdminUsername                         string
	AdminPassword                         string
	LogLevel                              string
	LogFormat                             string
	LogHTTPSummaryEnabled                 bool
	CORSOrigins                           []string
	TrustedProxies                        []string
	MetricsEnabled                        bool
	AccessRateLimitRequests               int
	AccessRateLimitWindow                 time.Duration
	UploadMaxBytes                        int64
	MediaProcessEnabled                   bool
	MediaAllowRemoteURL                   bool
	MediaAllowDataURL                     bool
	MediaAllowBase64                      bool
	MediaAllowSVG                         bool
	MediaMaxBytes                         int64
	MediaMaxImagesPerRequest              int
	MediaDerivedDir                       string
	MediaAVIFQuality                      int
	StorageDefaultQuotaBytes              int64
	StorageBlockNewConversations          bool
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

	GeetestCaptchaID  string
	GeetestCaptchaKey string
	GeetestAPIServer  string

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

	KirariEnabled                    bool
	KirariIssuerURL                  string
	KirariClientID                   string
	KirariClientSecret               string
	KirariRedirectURL                string
	KirariScopes                     []string
	KirariAllowedIssuers             []string
	KirariMetaEndpointURL            string
	KirariChatCompletionsEndpointURL string
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
		EnvFilePath:                           filepath.Join(backendRoot, ".env"),
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
		AdminUsername:                         "",
		AdminPassword:                         "",
		LogLevel:                              "info",
		LogFormat:                             defaultLogFormat(mode),
		LogHTTPSummaryEnabled:                 defaultLogHTTPSummaryEnabled(mode),
		CORSOrigins:                           []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		TrustedProxies:                        nil,
		MetricsEnabled:                        false,
		AccessRateLimitRequests:               0,
		AccessRateLimitWindow:                 time.Minute,
		UploadMaxBytes:                        10 << 20,
		MediaProcessEnabled:                   true,
		MediaAllowRemoteURL:                   true,
		MediaAllowDataURL:                     true,
		MediaAllowBase64:                      true,
		MediaAllowSVG:                         false,
		MediaMaxBytes:                         10 << 20,
		MediaMaxImagesPerRequest:              8,
		MediaDerivedDir:                       filepath.Join(dataDir, "derived", "request_media"),
		MediaAVIFQuality:                      60,
		StorageDefaultQuotaBytes:              0,
		StorageBlockNewConversations:          false,
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
		GeetestCaptchaID:                      "",
		GeetestCaptchaKey:                     "",
		GeetestAPIServer:                      "https://gcaptcha4.geetest.com",
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

		KirariEnabled:                    false,
		KirariIssuerURL:                  "",
		KirariClientID:                   "",
		KirariClientSecret:               "",
		KirariRedirectURL:                "",
		KirariScopes:                     []string{"openid", "profile", "email", "offline_access", "llm:read", "llm:stream"},
		KirariAllowedIssuers:             nil,
		KirariMetaEndpointURL:            "",
		KirariChatCompletionsEndpointURL: "",
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
	cfg.settingsEnvironment = detectSettingsEnvironment()
	return cfg, nil
}

func FromEnvUnchecked(mode Mode, backendRoot string) (Config, error) {
	cfg := Default(mode, backendRoot)
	cfg.Host = firstNonEmpty(os.Getenv("CHATAPI_HOST"), cfg.Host)
	cfg.BaseURL = strings.TrimSpace(os.Getenv("CHATAPI_BASE_URL"))
	cfg.EnvFilePath = firstNonEmpty(os.Getenv("CHATAPI_ENV_FILE"), cfg.EnvFilePath)
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
	cfg.LogFormat = strings.ToLower(firstNonEmpty(os.Getenv("CHATAPI_LOG_FORMAT"), cfg.LogFormat))
	cfg.LogHTTPSummaryEnabled = parseBool(os.Getenv("CHATAPI_LOG_HTTP_SUMMARY_ENABLED"), cfg.LogHTTPSummaryEnabled)
	cfg.LabToken = strings.TrimSpace(os.Getenv("CHATAPI_LAB_TOKEN"))
	cfg.LabPassword = strings.TrimSpace(os.Getenv("CHATAPI_LAB_PASSWORD"))
	cfg.AdminUsername = strings.TrimSpace(os.Getenv("CHATAPI_ADMIN_USERNAME"))
	cfg.AdminPassword = strings.TrimSpace(os.Getenv("CHATAPI_ADMIN_PASSWORD"))
	cfg.MetricsEnabled = parseBool(os.Getenv("CHATAPI_METRICS_ENABLED"), cfg.MetricsEnabled)
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_ACCESS_RATE_LIMIT_REQUESTS")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_ACCESS_RATE_LIMIT_REQUESTS: %w", err)
		}
		cfg.AccessRateLimitRequests = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_ACCESS_RATE_LIMIT_WINDOW")); raw != "" {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_ACCESS_RATE_LIMIT_WINDOW: %w", err)
		}
		cfg.AccessRateLimitWindow = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_UPLOAD_MAX_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_UPLOAD_MAX_BYTES: %w", err)
		}
		cfg.UploadMaxBytes = value
	}
	cfg.MediaProcessEnabled = parseBool(os.Getenv("CHATAPI_MEDIA_PROCESS_ENABLED"), cfg.MediaProcessEnabled)
	cfg.MediaAllowRemoteURL = parseBool(os.Getenv("CHATAPI_MEDIA_ALLOW_REMOTE_URL"), cfg.MediaAllowRemoteURL)
	cfg.MediaAllowDataURL = parseBool(os.Getenv("CHATAPI_MEDIA_ALLOW_DATA_URL"), cfg.MediaAllowDataURL)
	cfg.MediaAllowBase64 = parseBool(os.Getenv("CHATAPI_MEDIA_ALLOW_BASE64"), cfg.MediaAllowBase64)
	cfg.MediaAllowSVG = parseBool(os.Getenv("CHATAPI_MEDIA_ALLOW_SVG"), cfg.MediaAllowSVG)
	cfg.MediaDerivedDir = firstNonEmpty(os.Getenv("CHATAPI_MEDIA_DERIVED_DIR"), cfg.MediaDerivedDir)
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_MEDIA_MAX_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_MEDIA_MAX_BYTES: %w", err)
		}
		cfg.MediaMaxBytes = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_MEDIA_MAX_IMAGES_PER_REQUEST")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_MEDIA_MAX_IMAGES_PER_REQUEST: %w", err)
		}
		cfg.MediaMaxImagesPerRequest = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_MEDIA_AVIF_QUALITY")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_MEDIA_AVIF_QUALITY: %w", err)
		}
		cfg.MediaAVIFQuality = value
	}
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES: %w", err)
		}
		cfg.StorageDefaultQuotaBytes = value
	}
	cfg.StorageBlockNewConversations = parseBool(os.Getenv("CHATAPI_STORAGE_BLOCK_NEW_CONVERSATIONS"), cfg.StorageBlockNewConversations)
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
	cfg.GeetestCaptchaID = firstNonEmpty(os.Getenv("CHATAPI_GEETEST_CAPTCHA_ID"), os.Getenv("GEETEST_CAPTCHA_ID"))
	cfg.GeetestCaptchaKey = firstNonEmpty(os.Getenv("CHATAPI_GEETEST_CAPTCHA_KEY"), os.Getenv("GEETEST_CAPTCHA_KEY"))
	cfg.GeetestAPIServer = firstNonEmpty(os.Getenv("CHATAPI_GEETEST_API_SERVER"), os.Getenv("GEETEST_API_SERVER"), cfg.GeetestAPIServer)
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

	cfg.KirariEnabled = parseBool(os.Getenv("CHATAPI_KIRARI_ENABLED"), cfg.KirariEnabled)
	cfg.KirariIssuerURL = strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_ISSUER_URL"))
	cfg.KirariClientID = strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_CLIENT_ID"))
	cfg.KirariClientSecret = strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_CLIENT_SECRET"))
	cfg.KirariRedirectURL = strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_REDIRECT_URL"))
	if raw := strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_SCOPES")); raw != "" {
		cfg.KirariScopes = splitCSV(raw)
	}
	cfg.KirariAllowedIssuers = splitCSV(os.Getenv("CHATAPI_KIRARI_ALLOWED_ISSUERS"))
	cfg.KirariMetaEndpointURL = strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_META_ENDPOINT_URL"))
	cfg.KirariChatCompletionsEndpointURL = strings.TrimSpace(os.Getenv("CHATAPI_KIRARI_CHAT_COMPLETIONS_ENDPOINT_URL"))

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
	if !filepath.IsAbs(cfg.EnvFilePath) {
		cfg.EnvFilePath = filepath.Join(backendRoot, cfg.EnvFilePath)
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
	switch c.LogFormat {
	case "console", "json":
	default:
		return errors.New("log format must be console or json")
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
	if c.AccessRateLimitRequests < 0 {
		return errors.New("access rate limit requests must be non-negative")
	}
	if c.AccessRateLimitRequests > 0 && c.AccessRateLimitWindow <= 0 {
		return errors.New("access rate limit window must be positive when access rate limiting is enabled")
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
	if (strings.TrimSpace(c.GeetestCaptchaID) == "") != (strings.TrimSpace(c.GeetestCaptchaKey) == "") {
		return errors.New("geetest captcha id and key must be configured together")
	}
	if strings.TrimSpace(c.GeetestCaptchaID) != "" {
		parsed, err := url.Parse(strings.TrimSpace(c.GeetestAPIServer))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return errors.New("geetest api server must be a valid http/https url")
		}
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
	if c.KirariEnabled {
		if strings.TrimSpace(c.KirariIssuerURL) == "" {
			return errors.New("kirari issuer url is required when kirari is enabled")
		}
		if strings.TrimSpace(c.KirariClientID) == "" {
			return errors.New("kirari client id is required when kirari is enabled")
		}
		if strings.TrimSpace(c.KirariClientSecret) == "" {
			return errors.New("kirari client secret is required when kirari is enabled")
		}
		if strings.TrimSpace(c.KirariRedirectURL) == "" {
			return errors.New("kirari redirect url is required when kirari is enabled")
		}
		if !containsString(c.KirariScopes, "openid") {
			return errors.New("kirari scopes must include openid")
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

func defaultLogFormat(mode Mode) string {
	if mode == ModeLab {
		return "console"
	}
	return "json"
}

func defaultLogHTTPSummaryEnabled(mode Mode) bool {
	return defaultLogFormat(mode) == "console"
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
