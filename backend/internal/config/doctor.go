package config

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	DiagnosticInfo  = "info"
	DiagnosticWarn  = "warn"
	DiagnosticError = "error"
)

var ErrDoctorFailed = errors.New("configuration doctor found errors")

type DiagnosticReport struct {
	OK          bool              `json:"ok"`
	Mode        Mode              `json:"mode"`
	GeneratedAt time.Time         `json:"generated_at"`
	Summary     DiagnosticSummary `json:"summary"`
	Items       []DiagnosticItem  `json:"items"`
}

type DiagnosticSummary struct {
	ListenAddr     string `json:"listen_addr"`
	BaseURL        string `json:"base_url,omitempty"`
	WebDistDir     string `json:"web_dist_dir"`
	DataDir        string `json:"data_dir"`
	DatabaseDriver string `json:"database_driver"`
	DatabaseDSN    string `json:"database_dsn"`
	OIDCEnabled    bool   `json:"oidc_enabled"`
}

type DiagnosticItem struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

func Diagnose(cfg Config, validationErr error) DiagnosticReport {
	report := DiagnosticReport{
		OK:          true,
		Mode:        cfg.Mode,
		GeneratedAt: time.Now().UTC(),
		Summary: DiagnosticSummary{
			ListenAddr:     cfg.ListenAddr(),
			BaseURL:        cfg.BaseURL,
			WebDistDir:     cfg.WebDistDir,
			DataDir:        cfg.DataDir,
			DatabaseDriver: cfg.DatabaseDriver,
			DatabaseDSN:    cfg.DatabaseDSN,
			OIDCEnabled:    cfg.OIDCEnabled,
		},
	}
	if validationErr != nil {
		report.add(DiagnosticError, "config.validation_failed", validationErr.Error())
	}

	report.checkMode(cfg)
	report.checkDatabase(cfg)
	report.checkSecrets(cfg)
	report.checkPaths(cfg)
	report.checkCORS(cfg)
	report.checkOIDC(cfg)
	report.checkLogLevel(cfg)
	report.OK = !report.HasErrors()
	return report
}

func (r *DiagnosticReport) HasErrors() bool {
	for _, item := range r.Items {
		if item.Severity == DiagnosticError {
			return true
		}
	}
	return false
}

func (r *DiagnosticReport) add(severity string, code string, message string) {
	r.Items = append(r.Items, DiagnosticItem{
		Severity: severity,
		Code:     code,
		Message:  message,
	})
}

func (r *DiagnosticReport) checkMode(cfg Config) {
	if cfg.Mode == ModeLab {
		r.add(DiagnosticInfo, "mode.lab", "Lab 模式会跳过生产登录和鉴权，只应用于本地调试。")
		if cfg.Host != "127.0.0.1" && cfg.Host != "localhost" {
			if cfg.LabPassword == "" && cfg.LabToken == "" {
				r.add(DiagnosticError, "lab.remote_without_gate", "远程 Lab 必须配置 CHATAPI_LAB_TOKEN 或 CHATAPI_LAB_PASSWORD。")
			} else {
				r.add(DiagnosticWarn, "lab.remote_enabled", "Lab 正在监听非本地地址，确认只暴露在可信网络。")
			}
		}
		return
	}
	if cfg.Host == "0.0.0.0" {
		r.add(DiagnosticInfo, "serve.listen_all", "serve 模式监听 0.0.0.0，确认反向代理、TLS 和访问控制已配置。")
	}
}

func (r *DiagnosticReport) checkDatabase(cfg Config) {
	switch cfg.DatabaseDriver {
	case "sqlite":
		if cfg.Mode == ModeServe {
			r.add(DiagnosticWarn, "database.sqlite_in_serve", "serve 模式正在使用 SQLite；正式多用户部署建议迁移到 PostgreSQL。")
		}
		if strings.TrimSpace(cfg.DatabaseDSN) == "" {
			r.add(DiagnosticError, "database.sqlite_missing_dsn", "SQLite DSN 不能为空。")
		}
	case "postgres", "postgresql":
		r.add(DiagnosticError, "database.postgresql_not_implemented", "当前 Go 重构分支尚未接入 PostgreSQL repository，不能用 PostgreSQL 启动服务。")
	default:
		r.add(DiagnosticError, "database.unsupported_driver", "CHATAPI_DB_DRIVER 只支持 sqlite；PostgreSQL repository 完成后再开放 postgres。")
	}
}

func (r *DiagnosticReport) checkSecrets(cfg Config) {
	if cfg.Mode == ModeServe {
		if cfg.MasterKey == "" {
			r.add(DiagnosticError, "secret.master_key_missing", "serve 模式必须配置 CHATAPI_MASTER_KEY，用于加密虚拟模型 API Key。")
		} else if len(cfg.MasterKey) < 32 {
			r.add(DiagnosticWarn, "secret.master_key_short", "CHATAPI_MASTER_KEY 长度偏短，建议至少 32 个随机字符并纳入备份。")
		}
		if cfg.AdminPassword == "" {
			r.add(DiagnosticWarn, "secret.admin_password_missing", "CHATAPI_ADMIN_PASSWORD 未配置；正式 session/admin 登录接入前需要补齐恢复入口。")
		} else if cfg.AdminPassword == "change-me" {
			r.add(DiagnosticError, "secret.admin_password_default", "CHATAPI_ADMIN_PASSWORD 仍为 change-me，生产环境必须修改。")
		}
		return
	}
	if cfg.MasterKey == "chatapi-lab-insecure-master-key" {
		r.add(DiagnosticInfo, "secret.lab_master_key", "Lab 模式使用不安全默认 master key；不要复用到生产数据。")
	}
}

func (r *DiagnosticReport) checkPaths(cfg Config) {
	if cfg.DataDir == "" {
		r.add(DiagnosticError, "path.data_dir_missing", "CHATAPI_DATA_DIR 不能为空。")
	} else if stat, err := os.Stat(filepath.Clean(cfg.DataDir)); err != nil {
		if os.IsNotExist(err) {
			r.add(DiagnosticWarn, "path.data_dir_not_found", "data dir 尚不存在，启动服务时会尝试创建。")
		} else {
			r.add(DiagnosticError, "path.data_dir_unreadable", "无法访问 data dir: "+err.Error())
		}
	} else if !stat.IsDir() {
		r.add(DiagnosticError, "path.data_dir_not_directory", "CHATAPI_DATA_DIR 指向的路径不是目录。")
	}
	if cfg.WebDistDir == "" {
		r.add(DiagnosticWarn, "path.web_dist_missing", "CHATAPI_WEB_DIST_DIR 为空，将只提供 API。")
		return
	}
	if stat, err := os.Stat(filepath.Clean(cfg.WebDistDir)); err != nil || !stat.IsDir() {
		r.add(DiagnosticWarn, "path.web_dist_not_found", "未找到前端 dist 目录；当前启动后只提供 API 和兼容接口。")
	}
}

func (r *DiagnosticReport) checkCORS(cfg Config) {
	if cfg.Mode != ModeServe {
		return
	}
	for _, origin := range cfg.CORSOrigins {
		if origin == "*" {
			r.add(DiagnosticWarn, "cors.wildcard", "serve 模式不建议使用通配 CORS origin。")
		}
	}
}

func (r *DiagnosticReport) checkOIDC(cfg Config) {
	if !cfg.OIDCEnabled {
		return
	}
	if cfg.OIDCIssuerURL == "" {
		r.add(DiagnosticError, "oidc.issuer_missing", "OIDC 已启用但 CHATAPI_OIDC_ISSUER_URL 为空。")
	} else if parsed, err := url.Parse(cfg.OIDCIssuerURL); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		r.add(DiagnosticError, "oidc.issuer_invalid", "CHATAPI_OIDC_ISSUER_URL 必须是有效 HTTPS issuer URL。")
	}
	if cfg.OIDCClientID == "" {
		r.add(DiagnosticError, "oidc.client_id_missing", "OIDC 已启用但 CHATAPI_OIDC_CLIENT_ID 为空。")
	}
	if cfg.OIDCClientSecret == "" {
		r.add(DiagnosticError, "oidc.client_secret_missing", "OIDC 私密 RP 必须通过 CHATAPI_OIDC_CLIENT_SECRET 配置 client secret。")
	}
	if cfg.OIDCRedirectURL == "" {
		r.add(DiagnosticError, "oidc.redirect_url_missing", "OIDC 已启用但 CHATAPI_OIDC_REDIRECT_URL 为空。")
	} else if !isSafeRedirectURL(cfg.OIDCRedirectURL) {
		r.add(DiagnosticError, "oidc.redirect_url_invalid", "生产 OIDC redirect URL 必须使用 HTTPS；本地开发仅允许 http://localhost 或 http://127.0.0.1。")
	}
	if !slices.Contains(cfg.OIDCScopes, "openid") {
		r.add(DiagnosticError, "oidc.scope_openid_missing", "CHATAPI_OIDC_SCOPES 必须包含 openid。")
	}
	if len(cfg.OIDCAdminEmails) > 0 && len(cfg.OIDCAllowedDomains) == 0 && len(cfg.OIDCAllowedEmails) == 0 {
		r.add(DiagnosticWarn, "oidc.admin_without_allowlist", "配置了 OIDC 管理员邮箱但没有普通登录 allowlist；确认 IdP client policy 已限制用户范围。")
	}
	if cfg.OIDCAutoCreateUser && len(cfg.OIDCAllowedDomains) == 0 && len(cfg.OIDCAllowedEmails) == 0 {
		r.add(DiagnosticWarn, "oidc.auto_create_without_allowlist", "OIDC 自动创建用户已开启但没有邮箱 allowlist。")
	}
}

func (r *DiagnosticReport) checkLogLevel(cfg Config) {
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		r.add(DiagnosticWarn, "log.level_unknown", "未知 CHATAPI_LOG_LEVEL，将回退到 info 语义。")
	}
}

func isSafeRedirectURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" {
		return false
	}
	host := parsed.Hostname()
	return host == "localhost" || host == "127.0.0.1"
}
