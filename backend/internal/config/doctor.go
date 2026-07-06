package config

import (
	"errors"
	"net/netip"
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
	report.checkTrustedProxies(cfg)
	report.checkStorage(cfg)
	report.checkPending(cfg)
	report.checkRuntime(cfg)
	report.checkRealtime(cfg)
	report.checkSMTP(cfg)
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

func (r *DiagnosticReport) checkTrustedProxies(cfg Config) {
	if len(cfg.TrustedProxies) == 0 {
		return
	}
	for _, rawRule := range cfg.TrustedProxies {
		rule := strings.TrimSpace(rawRule)
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			if _, err := netip.ParsePrefix(rule); err != nil {
				r.add(DiagnosticError, "trusted_proxy.invalid", "CHATAPI_TRUSTED_PROXIES 包含无效 CIDR: "+rule)
			}
			continue
		}
		if _, err := netip.ParseAddr(rule); err != nil {
			r.add(DiagnosticError, "trusted_proxy.invalid", "CHATAPI_TRUSTED_PROXIES 包含无效 IP: "+rule)
		}
	}
	r.add(DiagnosticInfo, "trusted_proxy.enabled", "已启用可信代理；只有来自这些代理的请求才会读取 X-Forwarded-For / X-Real-IP。")
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

func (r *DiagnosticReport) checkSMTP(cfg Config) {
	if !cfg.SMTPEnabled {
		r.add(DiagnosticInfo, "smtp.disabled", "SMTP 未启用；注册验证、密码重置和测试邮件功能不可用。")
		return
	}
	if cfg.SMTPHost == "" {
		r.add(DiagnosticError, "smtp.host_missing", "SMTP 已启用但 CHATAPI_SMTP_HOST 为空。")
	}
	if cfg.SMTPPort <= 0 || cfg.SMTPPort > 65535 {
		r.add(DiagnosticError, "smtp.port_invalid", "CHATAPI_SMTP_PORT 必须在 1-65535 范围内。")
	}
	if cfg.SMTPFrom == "" {
		r.add(DiagnosticError, "smtp.from_missing", "SMTP 已启用但 CHATAPI_SMTP_FROM 为空。")
	}
	switch cfg.SMTPSecurity {
	case "none":
		r.add(DiagnosticWarn, "smtp.security_none", "SMTP 未启用 TLS；仅应在可信内网或本地调试中使用。")
	case "starttls", "tls":
	default:
		r.add(DiagnosticError, "smtp.security_invalid", "CHATAPI_SMTP_SECURITY 只支持 none、starttls 或 tls。")
	}
	if cfg.SMTPPassword != "" && cfg.SMTPUsername == "" {
		r.add(DiagnosticWarn, "smtp.password_without_username", "配置了 SMTP password 但 username 为空，确认服务器是否支持该认证方式。")
	}
}

func (r *DiagnosticReport) checkStorage(cfg Config) {
	if cfg.StorageCleanupKeepRecentConversations < 0 {
		r.add(DiagnosticError, "storage.cleanup_keep_recent_conversations_invalid", "CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_CONVERSATIONS 不能为负数。")
	}
	if cfg.StorageCleanupKeepRecentDays < 0 {
		r.add(DiagnosticError, "storage.cleanup_keep_recent_days_invalid", "CHATAPI_STORAGE_CLEANUP_KEEP_RECENT_DAYS 不能为负数。")
	}
	if cfg.StorageCleanupEnabled {
		if _, _, err := ParseDailyTime(cfg.StorageCleanupTime); err != nil {
			r.add(DiagnosticError, "storage.cleanup_time_invalid", "CHATAPI_STORAGE_CLEANUP_TIME 必须使用 HH:MM。")
		} else {
			r.add(DiagnosticInfo, "storage.cleanup_enabled", "已启用每日存储维护；将按配置清理旧会话和孤儿图片。")
		}
		if cfg.StorageCleanupKeepRecentConversations == 0 && cfg.StorageCleanupKeepRecentDays == 0 {
			r.add(DiagnosticWarn, "storage.cleanup_no_retention", "已启用存储清理但未保留最近会话或最近天数，可能删除所有已关闭会话。")
		}
		if cfg.StorageVacuumEnabled {
			r.add(DiagnosticWarn, "storage.vacuum_enabled", "已启用自动 SQLite VACUUM；该操作可能长时间锁库，建议仅在低峰期使用。")
		}
	}
	if cfg.StorageDefaultQuotaBytes == 0 {
		r.add(DiagnosticInfo, "storage.quota_disabled", "未配置默认用户存储配额；用户图片上传不会按总量阻断。")
		return
	}
	if cfg.StorageDefaultQuotaBytes < 0 {
		r.add(DiagnosticError, "storage.quota_invalid", "CHATAPI_STORAGE_DEFAULT_QUOTA_BYTES 不能为负数。")
		return
	}
	if cfg.StorageDefaultQuotaBytes < 10<<20 {
		r.add(DiagnosticWarn, "storage.quota_low", "默认用户存储配额低于 10MiB，可能影响正常图片上传。")
	}
}

func (r *DiagnosticReport) checkRuntime(cfg Config) {
	if cfg.RuntimeGOGC < 0 {
		r.add(DiagnosticError, "runtime.gogc_invalid", "CHATAPI_RUNTIME_GOGC 不能为负数。")
	} else if cfg.RuntimeGOGC > 0 && cfg.RuntimeGOGC < 20 {
		r.add(DiagnosticWarn, "runtime.gogc_low", "CHATAPI_RUNTIME_GOGC 低于 20，可能导致过于频繁的 GC。")
	}
	if cfg.RuntimeMemoryLimitBytes < 0 {
		r.add(DiagnosticError, "runtime.memory_limit_invalid", "CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES 不能为负数。")
	} else if cfg.RuntimeMemoryLimitBytes > 0 && cfg.RuntimeMemoryLimitBytes < 64<<20 {
		r.add(DiagnosticWarn, "runtime.memory_limit_low", "CHATAPI_RUNTIME_MEMORY_LIMIT_BYTES 低于 64MiB，可能导致频繁 GC 或内存压力。")
	}
}

func (r *DiagnosticReport) checkRealtime(cfg Config) {
	if cfg.RealtimeMaxConnections < 0 {
		r.add(DiagnosticError, "realtime.max_connections_invalid", "CHATAPI_REALTIME_MAX_CONNECTIONS 不能为负数。")
	}
	if cfg.RealtimeMaxConnectionsPerUser < 0 {
		r.add(DiagnosticError, "realtime.max_connections_per_user_invalid", "CHATAPI_REALTIME_MAX_CONNECTIONS_PER_USER 不能为负数。")
	}
	if cfg.RealtimeWebUIReservedPerUser < 0 {
		r.add(DiagnosticError, "realtime.webui_reserved_invalid", "CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER 不能为负数。")
		return
	}
	if cfg.RealtimeMaxConnectionsPerUser == 0 && cfg.RealtimeWebUIReservedPerUser > 0 {
		r.add(DiagnosticInfo, "realtime.webui_reservation_inactive", "未配置单用户 realtime 连接上限；浏览器控制台预留名额暂不会触发。")
		return
	}
	if cfg.RealtimeMaxConnectionsPerUser > 0 && cfg.RealtimeWebUIReservedPerUser >= cfg.RealtimeMaxConnectionsPerUser {
		r.add(DiagnosticWarn, "realtime.webui_reserved_too_high", "CHATAPI_REALTIME_WEBUI_RESERVED_PER_USER 不应大于或等于单用户 realtime 连接上限，否则 API/SSE 连接会被完全挤出。")
	}
}

func (r *DiagnosticReport) checkPending(cfg Config) {
	if cfg.PendingTurnTTL == 0 {
		r.add(DiagnosticInfo, "pending.ttl_disabled", "未配置 pending turn TTL；等待中的模型请求不会由后台自动过期。")
		return
	}
	if cfg.PendingTurnTTL < 0 {
		r.add(DiagnosticError, "pending.ttl_invalid", "CHATAPI_PENDING_TURN_TTL 不能为负数。")
		return
	}
	if cfg.PendingTurnTTL < time.Minute {
		r.add(DiagnosticWarn, "pending.ttl_low", "CHATAPI_PENDING_TURN_TTL 低于 1 分钟，可能导致人工回复来不及完成。")
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
