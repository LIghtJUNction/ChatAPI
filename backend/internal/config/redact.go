package config

type RedactedConfig struct {
	Mode                     Mode     `json:"mode"`
	Host                     string   `json:"host"`
	Port                     int      `json:"port"`
	ListenAddr               string   `json:"listen_addr"`
	BaseURL                  string   `json:"base_url,omitempty"`
	WebDistDir               string   `json:"web_dist_dir"`
	DataDir                  string   `json:"data_dir"`
	DatabaseDriver           string   `json:"database_driver"`
	DatabaseDSN              string   `json:"database_dsn"`
	MasterKey                string   `json:"master_key"`
	AllowRemoteLab           bool     `json:"allow_remote_lab"`
	OpenBrowser              bool     `json:"open_browser"`
	LabToken                 string   `json:"lab_token"`
	LabPassword              string   `json:"lab_password"`
	AdminPassword            string   `json:"admin_password"`
	LogLevel                 string   `json:"log_level"`
	CORSOrigins              []string `json:"cors_origins"`
	MetricsEnabled           bool     `json:"metrics_enabled"`
	UploadMaxBytes           int64    `json:"upload_max_bytes"`
	StorageDefaultQuotaBytes int64    `json:"storage_default_quota_bytes"`

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
		Mode:                     c.Mode,
		Host:                     c.Host,
		Port:                     c.Port,
		ListenAddr:               c.ListenAddr(),
		BaseURL:                  c.BaseURL,
		WebDistDir:               c.WebDistDir,
		DataDir:                  c.DataDir,
		DatabaseDriver:           c.DatabaseDriver,
		DatabaseDSN:              redactDatabaseDSN(c),
		MasterKey:                redactSecret(c.MasterKey),
		AllowRemoteLab:           c.AllowRemoteLab,
		OpenBrowser:              c.OpenBrowser,
		LabToken:                 redactSecret(c.LabToken),
		LabPassword:              redactSecret(c.LabPassword),
		AdminPassword:            redactSecret(c.AdminPassword),
		LogLevel:                 c.LogLevel,
		CORSOrigins:              append([]string(nil), c.CORSOrigins...),
		MetricsEnabled:           c.MetricsEnabled,
		UploadMaxBytes:           c.UploadMaxBytes,
		StorageDefaultQuotaBytes: c.StorageDefaultQuotaBytes,
		SMTPEnabled:              c.SMTPEnabled,
		SMTPHost:                 c.SMTPHost,
		SMTPPort:                 c.SMTPPort,
		SMTPUsername:             c.SMTPUsername,
		SMTPPassword:             redactSecret(c.SMTPPassword),
		SMTPFrom:                 c.SMTPFrom,
		SMTPSecurity:             c.SMTPSecurity,
		SMTPTimeout:              c.SMTPTimeout.String(),
		OIDCEnabled:              c.OIDCEnabled,
		OIDCProviderName:         c.OIDCProviderName,
		OIDCIssuerURL:            c.OIDCIssuerURL,
		OIDCClientID:             c.OIDCClientID,
		OIDCClientSecret:         redactSecret(c.OIDCClientSecret),
		OIDCRedirectURL:          c.OIDCRedirectURL,
		OIDCScopes:               append([]string(nil), c.OIDCScopes...),
		OIDCAllowedDomains:       append([]string(nil), c.OIDCAllowedDomains...),
		OIDCAllowedEmails:        append([]string(nil), c.OIDCAllowedEmails...),
		OIDCAdminEmails:          append([]string(nil), c.OIDCAdminEmails...),
		OIDCAutoCreateUser:       c.OIDCAutoCreateUser,
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
