package migratedb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pgstore "github.com/zyf2007/ChatAPI/internal/repository/postgresql"
	sqlitestore "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
)

type Report struct {
	Source             string    `json:"source"`
	Target             string    `json:"target"`
	StartedAt          time.Time `json:"started_at"`
	CompletedAt        time.Time `json:"completed_at"`
	Users              int       `json:"users"`
	UserIdentities     int       `json:"user_identities"`
	SystemConfigs      int       `json:"system_configs"`
	UserConfigs        int       `json:"user_configs"`
	ModelAPIKeys       int       `json:"model_api_keys"`
	AppAPIKeys         int       `json:"app_api_keys"`
	AppAPIAuditLogs    int       `json:"app_api_audit_logs"`
	AuditLogs          int       `json:"audit_logs"`
	AutomationRules    int       `json:"automation_rules"`
	UploadedImages     int       `json:"uploaded_images"`
	StorageQuotas      int       `json:"storage_quotas"`
	DeletionFailures   int       `json:"deletion_failures"`
	Conversations      int       `json:"conversations"`
	Messages           int       `json:"messages"`
	VerificationCodes  int       `json:"verification_codes"`
	ConversationEvents int       `json:"conversation_events"`
	MediaAssets        int       `json:"media_assets"`
	MediaAssetRefs     int       `json:"media_asset_refs"`
	MediaEventRefs     int       `json:"media_event_refs"`
	MediaStaging       int       `json:"media_staging"`
	VirtualModels      int       `json:"virtual_models"`
}

type snapshot struct {
	Users              []userRow
	UserIdentities     []userIdentityRow
	SystemConfigs      []configRow
	UserConfigs        []userConfigRow
	ModelAPIKeys       []modelAPIKeyRow
	AppAPIKeys         []appAPIKeyRow
	AppAPIKeyAuditLogs []appAPIKeyAuditLogRow
	AuditLogs          []auditLogRow
	AutomationRules    []automationRuleRow
	UploadedImages     []uploadedImageRow
	StorageUserQuotas  []storageUserQuotaRow
	DeletionFailures   []storageFileDeletionFailureRow
	Conversations      []conversationRow
	Messages           []messageRow
	Extended           []tableCopy
}

type tableCopySpec struct {
	Name        string
	Columns     []string
	TimeColumns map[string]bool
	JSONColumns map[string]bool
}

type tableCopy struct {
	Spec tableCopySpec
	Rows [][]any
}

var extendedTableSpecs = []tableCopySpec{
	{Name: "auth_verification_codes", Columns: []string{"email", "purpose", "code_hash", "expires_at", "created_at", "updated_at", "failed_attempts", "last_sent_at"}, TimeColumns: map[string]bool{"expires_at": true, "created_at": true, "updated_at": true, "last_sent_at": true}},
	{Name: "conversation_events", Columns: []string{"id", "conversation_id", "owner_id", "type", "level", "title", "detail", "request_id", "metadata_json", "created_at"}, TimeColumns: map[string]bool{"created_at": true}, JSONColumns: map[string]bool{"metadata_json": true}},
	{Name: "media_assets", Columns: []string{"id", "owner_id", "file_id", "path", "media_type", "bytes", "sha256", "width", "height", "source_kind", "original_name", "original_media_type", "created_at"}, TimeColumns: map[string]bool{"created_at": true}},
	{Name: "media_asset_refs", Columns: []string{"id", "asset_id", "file_id", "owner_id", "request_id", "conversation_id", "message_id", "input_part_index", "created_at"}, TimeColumns: map[string]bool{"created_at": true}},
	{Name: "media_asset_event_refs", Columns: []string{"id", "asset_id", "file_id", "url", "owner_id", "request_id", "conversation_id", "event_id", "purpose", "part_index", "created_at"}, TimeColumns: map[string]bool{"created_at": true}},
	{Name: "media_asset_staging", Columns: []string{"asset_id", "owner_id", "conversation_id", "request_id", "created_at"}, TimeColumns: map[string]bool{"created_at": true}},
	{Name: "user_virtual_models", Columns: []string{"id", "user_id", "name", "created_at"}, TimeColumns: map[string]bool{"created_at": true}},
}

type userRow struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	LocalAdmin   bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
	LastLoginAt  *time.Time
}

type userIdentityRow struct {
	ID            string
	UserID        string
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	ProfileJSON   string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastLoginAt   *time.Time
}

type configRow struct {
	Key       string
	ValueJSON string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type userConfigRow struct {
	UserID    string
	Key       string
	ValueJSON string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type modelAPIKeyRow struct {
	ID            string
	UserID        string
	Name          string
	KeyCiphertext string
	KeyPrefix     string
	Model         string
	LastUsedAt    *time.Time
	CreatedAt     time.Time
	RevokedAt     *time.Time
}

type appAPIKeyRow struct {
	ID                 string
	UserID             string
	Name               string
	KeyHash            string
	KeyPrefix          string
	ScopesJSON         string
	ResourceLimitsJSON string
	ExpiresAt          *time.Time
	LastUsedAt         *time.Time
	CreatedAt          time.Time
	RevokedAt          *time.Time
}

type appAPIKeyAuditLogRow struct {
	ID          string
	AppAPIKeyID string
	UserID      string
	Route       string
	StatusCode  int
	ErrorCode   string
	CreatedAt   time.Time
}

type auditLogRow struct {
	ID           string
	ActorUserID  string
	ActorRole    string
	ActorSource  string
	EventType    string
	ResourceType string
	ResourceID   string
	Action       string
	Outcome      string
	IPAddress    string
	UserAgent    string
	MetadataJSON string
	CreatedAt    time.Time
}

type automationRuleRow struct {
	ID        string
	UserID    string
	Enabled   bool
	RuleJSON  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type uploadedImageRow struct {
	ID               string
	OwnerID          string
	Filename         string
	OriginalFilename string
	ContentType      string
	Bytes            int64
	URL              string
	CreatedAt        time.Time
}

type storageUserQuotaRow struct {
	OwnerID    string
	QuotaBytes int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type storageFileDeletionFailureRow struct {
	Path      string
	Filename  string
	OwnerID   string
	Bytes     int64
	LastError string
	Attempts  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type conversationRow struct {
	ID                 string
	Title              string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	LastMessageAt      time.Time
	MessageCount       int
	LastMessagePreview string
	LastUserText       string
	MetadataJSON       string
}

type messageRow struct {
	ID             string
	ConversationID string
	Role           string
	Content        string
	CreatedAt      time.Time
	Status         *string
	ResponseID     *string
	MetadataJSON   string
}

func SQLiteToPostgres(ctx context.Context, sqlitePath string, postgresDSN string) (Report, error) {
	report := Report{
		Source:    strings.TrimSpace(sqlitePath),
		Target:    safePostgresTarget(postgresDSN),
		StartedAt: time.Now().UTC(),
	}

	src, err := sqlitestore.Open(strings.TrimSpace(sqlitePath))
	if err != nil {
		return report, fmt.Errorf("open sqlite source: %w", err)
	}
	defer src.Close()

	if err := src.Ping(ctx); err != nil {
		return report, fmt.Errorf("ping sqlite source: %w", err)
	}

	dst, err := pgstore.Open(ctx, strings.TrimSpace(postgresDSN))
	if err != nil {
		return report, fmt.Errorf("open postgresql target: %w", err)
	}
	defer dst.Close()

	if err := pgstore.Bootstrap(ctx, dst.Pool()); err != nil {
		return report, fmt.Errorf("bootstrap postgresql target: %w", err)
	}
	if err := ensureEmptyPostgresTarget(ctx, dst.Pool()); err != nil {
		return report, err
	}

	data, err := loadSQLiteSnapshot(ctx, src.DB())
	if err != nil {
		return report, err
	}
	if err := importSnapshot(ctx, dst.Pool(), data); err != nil {
		return report, err
	}

	report.Users = len(data.Users)
	report.UserIdentities = len(data.UserIdentities)
	report.SystemConfigs = len(data.SystemConfigs)
	report.UserConfigs = len(data.UserConfigs)
	report.ModelAPIKeys = len(data.ModelAPIKeys)
	report.AppAPIKeys = len(data.AppAPIKeys)
	report.AppAPIAuditLogs = len(data.AppAPIKeyAuditLogs)
	report.AuditLogs = len(data.AuditLogs)
	report.AutomationRules = len(data.AutomationRules)
	report.UploadedImages = len(data.UploadedImages)
	report.StorageQuotas = len(data.StorageUserQuotas)
	report.DeletionFailures = len(data.DeletionFailures)
	report.Conversations = len(data.Conversations)
	report.Messages = len(data.Messages)
	for _, table := range data.Extended {
		switch table.Spec.Name {
		case "auth_verification_codes":
			report.VerificationCodes = len(table.Rows)
		case "conversation_events":
			report.ConversationEvents = len(table.Rows)
		case "media_assets":
			report.MediaAssets = len(table.Rows)
		case "media_asset_refs":
			report.MediaAssetRefs = len(table.Rows)
		case "media_asset_event_refs":
			report.MediaEventRefs = len(table.Rows)
		case "media_asset_staging":
			report.MediaStaging = len(table.Rows)
		case "user_virtual_models":
			report.VirtualModels = len(table.Rows)
		}
	}
	report.CompletedAt = time.Now().UTC()
	return report, nil
}

func safePostgresTarget(dsn string) string {
	parsed, err := url.Parse(strings.TrimSpace(dsn))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "postgresql"
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}).String()
}

func ensureEmptyPostgresTarget(ctx context.Context, pool *pgxpool.Pool) error {
	for _, table := range []string{
		"users",
		"user_identities",
		"config",
		"user_configs",
		"user_api_keys",
		"user_app_api_keys",
		"app_api_key_audit_logs",
		"audit_logs",
		"automation_rules",
		"uploaded_images",
		"storage_user_quotas",
		"storage_file_deletion_failures",
		"conversations",
		"messages",
		"auth_verification_codes",
		"conversation_events",
		"media_assets",
		"media_asset_refs",
		"media_asset_event_refs",
		"media_asset_staging",
		"user_virtual_models",
	} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
			return fmt.Errorf("check target table %s: %w", table, err)
		}
		if count > 0 {
			return fmt.Errorf("postgresql target is not empty: table %s has %d rows", table, count)
		}
	}
	return nil
}

func loadSQLiteSnapshot(ctx context.Context, db *sql.DB) (snapshot, error) {
	var data snapshot
	var err error
	if data.Users, err = loadUsers(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.UserIdentities, err = loadUserIdentities(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.SystemConfigs, err = loadSystemConfigs(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.UserConfigs, err = loadUserConfigs(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.ModelAPIKeys, err = loadModelAPIKeys(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.AppAPIKeys, err = loadAppAPIKeys(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.AppAPIKeyAuditLogs, err = loadAppAPIKeyAuditLogs(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.AuditLogs, err = loadAuditLogs(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.AutomationRules, err = loadAutomationRules(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.UploadedImages, err = loadUploadedImages(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.StorageUserQuotas, err = loadStorageUserQuotas(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.DeletionFailures, err = loadDeletionFailures(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.Conversations, err = loadConversations(ctx, db); err != nil {
		return snapshot{}, err
	}
	if data.Messages, err = loadMessages(ctx, db); err != nil {
		return snapshot{}, err
	}
	for _, spec := range extendedTableSpecs {
		table, loadErr := loadExtendedTable(ctx, db, spec)
		if loadErr != nil {
			return snapshot{}, loadErr
		}
		data.Extended = append(data.Extended, table)
	}
	return data, nil
}

func importSnapshot(ctx context.Context, pool *pgxpool.Pool, data snapshot) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, item := range data.Users {
		if _, err := tx.Exec(ctx, `
			INSERT INTO users(id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		`, item.ID, item.Username, item.Email, item.PasswordHash, item.Role, item.IsActive, item.LocalAdmin, item.CreatedAt, item.UpdatedAt, item.LastLoginAt); err != nil {
			return fmt.Errorf("import users %s: %w", item.ID, err)
		}
	}
	for _, item := range data.UserIdentities {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_identities(id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
		`, item.ID, item.UserID, item.Provider, item.Subject, item.Email, item.EmailVerified, normalizeJSON(item.ProfileJSON, `{}`), item.CreatedAt, item.UpdatedAt, item.LastLoginAt); err != nil {
			return fmt.Errorf("import user_identities %s: %w", item.ID, err)
		}
	}
	for _, item := range data.SystemConfigs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO config(key, value_json, created_at, updated_at)
			VALUES ($1, $2::jsonb, $3, $4)
		`, item.Key, normalizeJSON(item.ValueJSON, `{}`), item.CreatedAt, item.UpdatedAt); err != nil {
			return fmt.Errorf("import config %s: %w", item.Key, err)
		}
	}
	for _, item := range data.UserConfigs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_configs(user_id, key, value_json, created_at, updated_at)
			VALUES ($1, $2, $3::jsonb, $4, $5)
		`, item.UserID, item.Key, normalizeJSON(item.ValueJSON, `{}`), item.CreatedAt, item.UpdatedAt); err != nil {
			return fmt.Errorf("import user_configs %s/%s: %w", item.UserID, item.Key, err)
		}
	}
	for _, item := range data.ModelAPIKeys {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_api_keys(id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, item.ID, item.UserID, item.Name, item.KeyCiphertext, item.KeyPrefix, item.Model, item.LastUsedAt, item.CreatedAt, item.RevokedAt); err != nil {
			return fmt.Errorf("import user_api_keys %s: %w", item.ID, err)
		}
	}
	for _, item := range data.AppAPIKeys {
		if _, err := tx.Exec(ctx, `
			INSERT INTO user_app_api_keys(id, user_id, name, key_hash, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at)
			VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		`, item.ID, item.UserID, item.Name, item.KeyHash, item.KeyPrefix, normalizeJSON(item.ScopesJSON, `[]`), normalizeJSON(item.ResourceLimitsJSON, `{}`), item.ExpiresAt, item.LastUsedAt, item.CreatedAt, item.RevokedAt); err != nil {
			return fmt.Errorf("import user_app_api_keys %s: %w", item.ID, err)
		}
	}
	for _, item := range data.AppAPIKeyAuditLogs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO app_api_key_audit_logs(id, app_api_key_id, user_id, route, status_code, error_code, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, item.ID, item.AppAPIKeyID, item.UserID, item.Route, item.StatusCode, item.ErrorCode, item.CreatedAt); err != nil {
			return fmt.Errorf("import app_api_key_audit_logs %s: %w", item.ID, err)
		}
	}
	for _, item := range data.AuditLogs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_logs(id, actor_user_id, actor_role, actor_source, event_type, resource_type, resource_id, action, outcome, ip_address, user_agent, metadata_json, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13)
		`, item.ID, item.ActorUserID, item.ActorRole, item.ActorSource, item.EventType, item.ResourceType, item.ResourceID, item.Action, item.Outcome, item.IPAddress, item.UserAgent, normalizeJSON(item.MetadataJSON, `{}`), item.CreatedAt); err != nil {
			return fmt.Errorf("import audit_logs %s: %w", item.ID, err)
		}
	}
	for _, item := range data.AutomationRules {
		if _, err := tx.Exec(ctx, `
			INSERT INTO automation_rules(id, user_id, enabled, rule_json, created_at, updated_at)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)
		`, item.ID, item.UserID, item.Enabled, normalizeJSON(item.RuleJSON, `{}`), item.CreatedAt, item.UpdatedAt); err != nil {
			return fmt.Errorf("import automation_rules %s: %w", item.ID, err)
		}
	}
	for _, item := range data.UploadedImages {
		if _, err := tx.Exec(ctx, `
			INSERT INTO uploaded_images(id, owner_id, filename, original_filename, content_type, bytes, url, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, item.ID, item.OwnerID, item.Filename, item.OriginalFilename, item.ContentType, item.Bytes, item.URL, item.CreatedAt); err != nil {
			return fmt.Errorf("import uploaded_images %s: %w", item.ID, err)
		}
	}
	for _, item := range data.StorageUserQuotas {
		if _, err := tx.Exec(ctx, `
			INSERT INTO storage_user_quotas(owner_id, quota_bytes, created_at, updated_at)
			VALUES ($1, $2, $3, $4)
		`, item.OwnerID, item.QuotaBytes, item.CreatedAt, item.UpdatedAt); err != nil {
			return fmt.Errorf("import storage_user_quotas %s: %w", item.OwnerID, err)
		}
	}
	for _, item := range data.DeletionFailures {
		if _, err := tx.Exec(ctx, `
			INSERT INTO storage_file_deletion_failures(path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, item.Path, item.Filename, item.OwnerID, item.Bytes, item.LastError, item.Attempts, item.CreatedAt, item.UpdatedAt); err != nil {
			return fmt.Errorf("import storage_file_deletion_failures %s: %w", item.Path, err)
		}
	}
	for _, item := range data.Conversations {
		if _, err := tx.Exec(ctx, `
			INSERT INTO conversations(id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		`, item.ID, item.Title, item.CreatedAt, item.UpdatedAt, item.LastMessageAt, item.MessageCount, item.LastMessagePreview, item.LastUserText, normalizeJSON(item.MetadataJSON, `{}`)); err != nil {
			return fmt.Errorf("import conversations %s: %w", item.ID, err)
		}
	}
	for _, item := range data.Messages {
		if _, err := tx.Exec(ctx, `
			INSERT INTO messages(id, conversation_id, role, content, created_at, status, response_id, metadata_json)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
		`, item.ID, item.ConversationID, item.Role, item.Content, item.CreatedAt, item.Status, item.ResponseID, normalizeJSON(item.MetadataJSON, `{}`)); err != nil {
			return fmt.Errorf("import messages %s: %w", item.ID, err)
		}
	}
	for _, table := range data.Extended {
		columns := strings.Join(table.Spec.Columns, ", ")
		values := make([]string, len(table.Spec.Columns))
		for i, column := range table.Spec.Columns {
			values[i] = fmt.Sprintf("$%d", i+1)
			if table.Spec.JSONColumns[column] {
				values[i] += "::jsonb"
			}
		}
		query := "INSERT INTO " + table.Spec.Name + "(" + columns + ") VALUES (" + strings.Join(values, ", ") + ")"
		for i, row := range table.Rows {
			if _, err := tx.Exec(ctx, query, row...); err != nil {
				return fmt.Errorf("import %s row %d: %w", table.Spec.Name, i+1, err)
			}
		}
	}
	return tx.Commit(ctx)
}

func loadExtendedTable(ctx context.Context, db *sql.DB, spec tableCopySpec) (tableCopy, error) {
	table := tableCopy{Spec: spec}
	rows, err := db.QueryContext(ctx, "SELECT "+strings.Join(spec.Columns, ", ")+" FROM "+spec.Name)
	if err != nil {
		return table, fmt.Errorf("read sqlite %s: %w", spec.Name, err)
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]any, len(spec.Columns))
		pointers := make([]any, len(spec.Columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			return table, fmt.Errorf("scan sqlite %s: %w", spec.Name, err)
		}
		for i, column := range spec.Columns {
			if bytes, ok := values[i].([]byte); ok {
				values[i] = string(bytes)
			}
			if spec.TimeColumns[column] {
				raw, _ := values[i].(string)
				values[i] = parseSQLiteTime(raw)
			}
			if spec.JSONColumns[column] {
				raw, _ := values[i].(string)
				values[i] = normalizeJSON(raw, `{}`)
			}
		}
		table.Rows = append(table.Rows, values)
	}
	return table, rows.Err()
}

func normalizeJSON(raw string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return fallback
	}
	return raw
}

func loadUsers(ctx context.Context, db *sql.DB) ([]userRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at FROM users ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite users: %w", err)
	}
	defer rows.Close()
	items := make([]userRow, 0)
	for rows.Next() {
		var item userRow
		var isActive int
		var localAdmin int
		var createdAt, updatedAt string
		var lastLogin sql.NullString
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &item.PasswordHash, &item.Role, &isActive, &localAdmin, &createdAt, &updatedAt, &lastLogin); err != nil {
			return nil, err
		}
		item.IsActive = isActive != 0
		item.LocalAdmin = localAdmin != 0
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		item.LastLoginAt = parseSQLiteNullableTime(lastLogin)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadUserIdentities(ctx context.Context, db *sql.DB) ([]userIdentityRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at FROM user_identities ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite user_identities: %w", err)
	}
	defer rows.Close()
	items := make([]userIdentityRow, 0)
	for rows.Next() {
		var item userIdentityRow
		var emailVerified int
		var createdAt, updatedAt string
		var lastLogin sql.NullString
		if err := rows.Scan(&item.ID, &item.UserID, &item.Provider, &item.Subject, &item.Email, &emailVerified, &item.ProfileJSON, &createdAt, &updatedAt, &lastLogin); err != nil {
			return nil, err
		}
		item.EmailVerified = emailVerified != 0
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		item.LastLoginAt = parseSQLiteNullableTime(lastLogin)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadSystemConfigs(ctx context.Context, db *sql.DB) ([]configRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value_json, created_at, updated_at FROM config ORDER BY key ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite config: %w", err)
	}
	defer rows.Close()
	items := make([]configRow, 0)
	for rows.Next() {
		var item configRow
		var createdAt, updatedAt string
		if err := rows.Scan(&item.Key, &item.ValueJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadUserConfigs(ctx context.Context, db *sql.DB) ([]userConfigRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT user_id, key, value_json, created_at, updated_at FROM user_configs ORDER BY user_id ASC, key ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite user_configs: %w", err)
	}
	defer rows.Close()
	items := make([]userConfigRow, 0)
	for rows.Next() {
		var item userConfigRow
		var createdAt, updatedAt string
		if err := rows.Scan(&item.UserID, &item.Key, &item.ValueJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadModelAPIKeys(ctx context.Context, db *sql.DB) ([]modelAPIKeyRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at FROM user_api_keys ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite user_api_keys: %w", err)
	}
	defer rows.Close()
	items := make([]modelAPIKeyRow, 0)
	for rows.Next() {
		var item modelAPIKeyRow
		var lastUsedAt, revokedAt sql.NullString
		var createdAt string
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.KeyCiphertext, &item.KeyPrefix, &item.Model, &lastUsedAt, &createdAt, &revokedAt); err != nil {
			return nil, err
		}
		item.LastUsedAt = parseSQLiteNullableTime(lastUsedAt)
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.RevokedAt = parseSQLiteNullableTime(revokedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadAppAPIKeys(ctx context.Context, db *sql.DB) ([]appAPIKeyRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, name, key_hash, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at FROM user_app_api_keys ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite user_app_api_keys: %w", err)
	}
	defer rows.Close()
	items := make([]appAPIKeyRow, 0)
	for rows.Next() {
		var item appAPIKeyRow
		var expiresAt, lastUsedAt, revokedAt sql.NullString
		var createdAt string
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &item.KeyHash, &item.KeyPrefix, &item.ScopesJSON, &item.ResourceLimitsJSON, &expiresAt, &lastUsedAt, &createdAt, &revokedAt); err != nil {
			return nil, err
		}
		item.ExpiresAt = parseSQLiteNullableTime(expiresAt)
		item.LastUsedAt = parseSQLiteNullableTime(lastUsedAt)
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.RevokedAt = parseSQLiteNullableTime(revokedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadAppAPIKeyAuditLogs(ctx context.Context, db *sql.DB) ([]appAPIKeyAuditLogRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, app_api_key_id, user_id, route, status_code, error_code, created_at FROM app_api_key_audit_logs ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite app_api_key_audit_logs: %w", err)
	}
	defer rows.Close()
	items := make([]appAPIKeyAuditLogRow, 0)
	for rows.Next() {
		var item appAPIKeyAuditLogRow
		var createdAt string
		if err := rows.Scan(&item.ID, &item.AppAPIKeyID, &item.UserID, &item.Route, &item.StatusCode, &item.ErrorCode, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadAuditLogs(ctx context.Context, db *sql.DB) ([]auditLogRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, actor_user_id, actor_role, actor_source, event_type, resource_type, resource_id, action, outcome, ip_address, user_agent, metadata_json, created_at FROM audit_logs ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite audit_logs: %w", err)
	}
	defer rows.Close()
	items := make([]auditLogRow, 0)
	for rows.Next() {
		var item auditLogRow
		var createdAt string
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.ActorRole, &item.ActorSource, &item.EventType, &item.ResourceType, &item.ResourceID, &item.Action, &item.Outcome, &item.IPAddress, &item.UserAgent, &item.MetadataJSON, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadAutomationRules(ctx context.Context, db *sql.DB) ([]automationRuleRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, user_id, enabled, rule_json, created_at, updated_at FROM automation_rules ORDER BY user_id ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite automation_rules: %w", err)
	}
	defer rows.Close()
	items := make([]automationRuleRow, 0)
	for rows.Next() {
		var item automationRuleRow
		var enabled int
		var createdAt, updatedAt string
		if err := rows.Scan(&item.ID, &item.UserID, &enabled, &item.RuleJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.Enabled = enabled != 0
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadUploadedImages(ctx context.Context, db *sql.DB) ([]uploadedImageRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, owner_id, filename, original_filename, content_type, bytes, url, created_at FROM uploaded_images ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite uploaded_images: %w", err)
	}
	defer rows.Close()
	items := make([]uploadedImageRow, 0)
	for rows.Next() {
		var item uploadedImageRow
		var createdAt string
		if err := rows.Scan(&item.ID, &item.OwnerID, &item.Filename, &item.OriginalFilename, &item.ContentType, &item.Bytes, &item.URL, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadStorageUserQuotas(ctx context.Context, db *sql.DB) ([]storageUserQuotaRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT owner_id, quota_bytes, created_at, updated_at FROM storage_user_quotas ORDER BY owner_id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite storage_user_quotas: %w", err)
	}
	defer rows.Close()
	items := make([]storageUserQuotaRow, 0)
	for rows.Next() {
		var item storageUserQuotaRow
		var createdAt, updatedAt string
		if err := rows.Scan(&item.OwnerID, &item.QuotaBytes, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadDeletionFailures(ctx context.Context, db *sql.DB) ([]storageFileDeletionFailureRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at FROM storage_file_deletion_failures ORDER BY updated_at ASC, path ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite storage_file_deletion_failures: %w", err)
	}
	defer rows.Close()
	items := make([]storageFileDeletionFailureRow, 0)
	for rows.Next() {
		var item storageFileDeletionFailureRow
		var createdAt, updatedAt string
		if err := rows.Scan(&item.Path, &item.Filename, &item.OwnerID, &item.Bytes, &item.LastError, &item.Attempts, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadConversations(ctx context.Context, db *sql.DB) ([]conversationRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json FROM conversations ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite conversations: %w", err)
	}
	defer rows.Close()
	items := make([]conversationRow, 0)
	for rows.Next() {
		var item conversationRow
		var createdAt, updatedAt, lastMessageAt string
		if err := rows.Scan(&item.ID, &item.Title, &createdAt, &updatedAt, &lastMessageAt, &item.MessageCount, &item.LastMessagePreview, &item.LastUserText, &item.MetadataJSON); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		item.UpdatedAt = parseSQLiteTime(updatedAt)
		item.LastMessageAt = parseSQLiteTime(lastMessageAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func loadMessages(ctx context.Context, db *sql.DB) ([]messageRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, conversation_id, role, content, created_at, status, response_id, metadata_json FROM messages ORDER BY created_at ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite messages: %w", err)
	}
	defer rows.Close()
	items := make([]messageRow, 0)
	for rows.Next() {
		var item messageRow
		var createdAt string
		var status, responseID sql.NullString
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.Role, &item.Content, &createdAt, &status, &responseID, &item.MetadataJSON); err != nil {
			return nil, err
		}
		item.CreatedAt = parseSQLiteTime(createdAt)
		if status.Valid {
			value := status.String
			item.Status = &value
		}
		if responseID.Valid {
			value := responseID.String
			item.ResponseID = &value
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ConversationID != items[j].ConversationID {
			return items[i].ConversationID < items[j].ConversationID
		}
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items, rows.Err()
}

func parseSQLiteTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000000000Z07:00",
		"2006-01-02T15:04:05.000000Z07:00",
		"2006-01-02T15:04:05.000Z07:00",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func parseSQLiteNullableTime(raw sql.NullString) *time.Time {
	if !raw.Valid {
		return nil
	}
	parsed := parseSQLiteTime(raw.String)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}
