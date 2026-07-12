package config

type RedactedConfig struct {
	Mode                                  Mode     `json:"mode"`
	Host                                  string   `json:"host"`
	Port                                  int      `json:"port"`
	ListenAddr                            string   `json:"listen_addr"`
	BaseURL                               string   `json:"base_url,omitempty"`
	EnvFilePath                           string   `json:"env_file_path,omitempty"`
	WebDistDir                            string   `json:"web_dist_dir"`
	DataDir                               string   `json:"data_dir"`
	DatabaseDriver                        string   `json:"database_driver"`
	DatabaseDSN                           string   `json:"database_dsn"`
	MasterKey                             string   `json:"master_key"`
	SessionSecret                         string   `json:"session_secret"`
	AllowRemoteLab                        bool     `json:"allow_remote_lab"`
	OpenBrowser                           bool     `json:"open_browser"`
	LabToken                              string   `json:"lab_token"`
	LabPassword                           string   `json:"lab_password"`
	AdminUsername                         string   `json:"admin_username"`
	AdminPassword                         string   `json:"admin_password"`
	LogLevel                              string   `json:"log_level"`
	LogFormat                             string   `json:"log_format"`
	LogHTTPSummaryEnabled                 bool     `json:"log_http_summary_enabled"`
	CORSOrigins                           []string `json:"cors_origins"`
	TrustedProxies                        []string `json:"trusted_proxies,omitempty"`
	MetricsEnabled                        bool     `json:"metrics_enabled"`
	AccessRateLimitRequests               int      `json:"access_rate_limit_requests"`
	AccessRateLimitWindow                 string   `json:"access_rate_limit_window"`
	UploadMaxBytes                        int64    `json:"upload_max_bytes"`
	StorageDefaultQuotaBytes              int64    `json:"storage_default_quota_bytes"`
	StorageBlockNewConversations          bool     `json:"storage_block_new_conversations"`
	StorageCleanupEnabled                 bool     `json:"storage_cleanup_enabled"`
	StorageCleanupTime                    string   `json:"storage_cleanup_time"`
	StorageCleanupKeepRecentConversations int      `json:"storage_cleanup_keep_recent_conversations"`
	StorageCleanupKeepRecentDays          int      `json:"storage_cleanup_keep_recent_days"`
	StorageVacuumEnabled                  bool     `json:"storage_vacuum_enabled"`
	PendingTurnTTL                        string   `json:"pending_turn_ttl"`
	RuntimeGOGC                           int      `json:"runtime_gogc"`
	RuntimeMemoryLimitBytes               int64    `json:"runtime_memory_limit_bytes"`
	RealtimeMaxConnections                int      `json:"realtime_max_connections"`
	RealtimeMaxConnectionsPerUser         int      `json:"realtime_max_connections_per_user"`
	RealtimeWebUIReservedPerUser          int      `json:"realtime_webui_reserved_per_user"`
	GeetestCaptchaID                      string   `json:"geetest_captcha_id,omitempty"`
	GeetestCaptchaKey                     string   `json:"geetest_captcha_key"`
	GeetestAPIServer                      string   `json:"geetest_api_server,omitempty"`

	SMTPEnabled  bool   `json:"smtp_enabled"`
	SMTPHost     string `json:"smtp_host,omitempty"`
	SMTPPort     int    `json:"smtp_port,omitempty"`
	SMTPUsername string `json:"smtp_username,omitempty"`
	SMTPPassword string `json:"smtp_password"`
	SMTPFrom     string `json:"smtp_from,omitempty"`
	SMTPSecurity string `json:"smtp_security,omitempty"`
	SMTPTimeout  string `json:"smtp_timeout,omitempty"`

	OIDCEnabled        bool     `json:"oidc_enabled"`
	OIDCProviderName   string   `json:"oidc_provider_name,omitempty"`
	OIDCIssuerURL      string   `json:"oidc_issuer_url,omitempty"`
	OIDCClientID       string   `json:"oidc_client_id,omitempty"`
	OIDCClientSecret   string   `json:"oidc_client_secret"`
	OIDCRedirectURL    string   `json:"oidc_redirect_url,omitempty"`
	OIDCScopes         []string `json:"oidc_scopes,omitempty"`
	OIDCAllowedDomains []string `json:"oidc_allowed_domains,omitempty"`
	OIDCAllowedEmails  []string `json:"oidc_allowed_emails,omitempty"`
	OIDCAdminEmails    []string `json:"oidc_admin_emails,omitempty"`
	OIDCAutoCreateUser bool     `json:"oidc_auto_create_user"`
}

func (c Config) Redacted() RedactedConfig {
	return RedactedConfig{
		Mode:                                  c.Mode,
		Host:                                  c.Host,
		Port:                                  c.Port,
		ListenAddr:                            c.ListenAddr(),
		BaseURL:                               c.BaseURL,
		EnvFilePath:                           c.EnvFilePath,
		WebDistDir:                            c.WebDistDir,
		DataDir:                               c.DataDir,
		DatabaseDriver:                        c.DatabaseDriver,
		DatabaseDSN:                           redactDatabaseDSN(c),
		MasterKey:                             redactSecret(c.MasterKey),
		SessionSecret:                         redactSecret(c.SessionSecret),
		AllowRemoteLab:                        c.AllowRemoteLab,
		OpenBrowser:                           c.OpenBrowser,
		LabToken:                              redactSecret(c.LabToken),
		LabPassword:                           redactSecret(c.LabPassword),
		AdminUsername:                         c.AdminUsername,
		AdminPassword:                         redactSecret(c.AdminPassword),
		LogLevel:                              c.LogLevel,
		LogFormat:                             c.LogFormat,
		LogHTTPSummaryEnabled:                 c.LogHTTPSummaryEnabled,
		CORSOrigins:                           append([]string(nil), c.CORSOrigins...),
		TrustedProxies:                        append([]string(nil), c.TrustedProxies...),
		MetricsEnabled:                        c.MetricsEnabled,
		AccessRateLimitRequests:               c.AccessRateLimitRequests,
		AccessRateLimitWindow:                 c.AccessRateLimitWindow.String(),
		UploadMaxBytes:                        c.UploadMaxBytes,
		StorageDefaultQuotaBytes:              c.StorageDefaultQuotaBytes,
		StorageBlockNewConversations:          c.StorageBlockNewConversations,
		StorageCleanupEnabled:                 c.StorageCleanupEnabled,
		StorageCleanupTime:                    c.StorageCleanupTime,
		StorageCleanupKeepRecentConversations: c.StorageCleanupKeepRecentConversations,
		StorageCleanupKeepRecentDays:          c.StorageCleanupKeepRecentDays,
		StorageVacuumEnabled:                  c.StorageVacuumEnabled,
		PendingTurnTTL:                        c.PendingTurnTTL.String(),
		RuntimeGOGC:                           c.RuntimeGOGC,
		RuntimeMemoryLimitBytes:               c.RuntimeMemoryLimitBytes,
		RealtimeMaxConnections:                c.RealtimeMaxConnections,
		RealtimeMaxConnectionsPerUser:         c.RealtimeMaxConnectionsPerUser,
		RealtimeWebUIReservedPerUser:          c.RealtimeWebUIReservedPerUser,
		GeetestCaptchaID:                      c.GeetestCaptchaID,
		GeetestCaptchaKey:                     redactSecret(c.GeetestCaptchaKey),
		GeetestAPIServer:                      c.GeetestAPIServer,
		SMTPEnabled:                           c.SMTPEnabled,
		SMTPHost:                              c.SMTPHost,
		SMTPPort:                              c.SMTPPort,
		SMTPUsername:                          c.SMTPUsername,
		SMTPPassword:                          redactSecret(c.SMTPPassword),
		SMTPFrom:                              c.SMTPFrom,
		SMTPSecurity:                          c.SMTPSecurity,
		SMTPTimeout:                           c.SMTPTimeout.String(),
		OIDCEnabled:                           c.OIDCEnabled,
		OIDCProviderName:                      c.OIDCProviderName,
		OIDCIssuerURL:                         c.OIDCIssuerURL,
		OIDCClientID:                          c.OIDCClientID,
		OIDCClientSecret:                      redactSecret(c.OIDCClientSecret),
		OIDCRedirectURL:                       c.OIDCRedirectURL,
		OIDCScopes:                            append([]string(nil), c.OIDCScopes...),
		OIDCAllowedDomains:                    append([]string(nil), c.OIDCAllowedDomains...),
		OIDCAllowedEmails:                     append([]string(nil), c.OIDCAllowedEmails...),
		OIDCAdminEmails:                       append([]string(nil), c.OIDCAdminEmails...),
		OIDCAutoCreateUser:                    c.OIDCAutoCreateUser,
	}
}

func redactSecret(value string) string {
	if value == "" {
		return ""
	}
	return "<redacted>"
}

func redactDatabaseDSN(cfg Config) string {
	if cfg.DatabaseDriver == "sqlite" {
		return cfg.DatabaseDSN
	}
	return redactSecret(cfg.DatabaseDSN)
}
