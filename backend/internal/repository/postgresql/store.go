package postgresql

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/ops/observability/logging"
	"github.com/zyf/chatapi/internal/repository/migrationplan"
	"github.com/zyf/chatapi/internal/store"
)

type Store struct {
	pool   *pgxpool.Pool
	Logger *zap.Logger
}

var errConflict = store.ErrTurnConflict

const BootstrapVersion = "0001_bootstrap"
const LatestVersion = "0004_postgresql_auth_verification_code_limits"

//go:embed sql/*.up.sql
var migrationFiles embed.FS

var registeredMigrations = mustLoadMigrations()
var bootstrapSchema = mustBootstrapSchema(registeredMigrations)

func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgresql pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgresql: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) logger(ctx context.Context) *zap.Logger {
	return logging.BindContext(s.Logger, ctx)
}

func Bootstrap(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, bootstrapSchema)
	if err != nil {
		return fmt.Errorf("bootstrap postgresql schema: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES
			('schema_version', '0001_bootstrap', NOW()),
			('migration_dirty', '0', NOW()),
			('migration_lock', '', NOW()),
			('created_by', 'go', NOW()),
			('last_migrated_at', TO_CHAR(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), NOW())
		ON CONFLICT(key) DO NOTHING;

		INSERT INTO schema_migrations(version, name, applied_at, checksum, dirty)
		VALUES ('0001_bootstrap', 'bootstrap', NOW(), '', false)
		ON CONFLICT(version) DO NOTHING;
	`); err != nil {
		return fmt.Errorf("seed postgresql migration metadata: %w", err)
	}
	statusStore := &Store{pool: pool}
	status, err := statusStore.MigrationStatus(ctx)
	if err != nil {
		return err
	}
	if status.MigrationDirty {
		return nil
	}
	return applyPendingMigrations(ctx, pool, status)
}

func Reset(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		DROP TABLE IF EXISTS user_configs;
		DROP TABLE IF EXISTS auth_verification_codes;
		DROP TABLE IF EXISTS config;
		DROP TABLE IF EXISTS audit_logs;
		DROP TABLE IF EXISTS automation_rules;
		DROP TABLE IF EXISTS app_api_key_audit_logs;
		DROP TABLE IF EXISTS user_app_api_keys;
		DROP TABLE IF EXISTS user_api_keys;
		DROP TABLE IF EXISTS uploaded_images;
		DROP TABLE IF EXISTS storage_user_quotas;
		DROP TABLE IF EXISTS storage_file_deletion_failures;
		DROP TABLE IF EXISTS messages;
		DROP TABLE IF EXISTS conversations;
		DROP TABLE IF EXISTS schema_migrations;
		DROP TABLE IF EXISTS db_meta;
		DROP TABLE IF EXISTS user_identities;
		DROP TABLE IF EXISTS users;
	`)
	if err != nil {
		return fmt.Errorf("reset postgresql schema: %w", err)
	}
	return nil
}

func applyPendingMigrations(ctx context.Context, pool *pgxpool.Pool, status store.MigrationStatus) error {
	applied := make(map[string]store.AppliedMigration, len(status.Applied))
	for _, item := range status.Applied {
		applied[item.Version] = item
	}
	for _, migration := range registeredMigrations {
		if migration.Version == BootstrapVersion {
			continue
		}
		if _, ok := applied[migration.Version]; ok {
			continue
		}
		if err := applyMigration(ctx, pool, migration); err != nil {
			return err
		}
	}
	return nil
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, migration migrationplan.Step) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, migration.UpSQL); err != nil {
		return fmt.Errorf("apply postgresql migration %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO schema_migrations(version, name, applied_at, checksum, dirty)
		VALUES ($1, $2, NOW(), '', false)
	`, migration.Version, migration.Name); err != nil {
		return fmt.Errorf("record postgresql migration %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES ('schema_version', $1, NOW())
		ON CONFLICT(key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
	`, migration.Version); err != nil {
		return fmt.Errorf("update postgresql schema_version %s: %w", migration.Version, err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO db_meta(key, value, updated_at)
		VALUES ('last_migrated_at', TO_CHAR(NOW() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'), NOW())
		ON CONFLICT(key) DO UPDATE SET
			value = EXCLUDED.value,
			updated_at = EXCLUDED.updated_at
	`); err != nil {
		return fmt.Errorf("update postgresql last_migrated_at %s: %w", migration.Version, err)
	}
	return tx.Commit(ctx)
}

func init() {
	if len(registeredMigrations) == 0 {
		panic("postgresql migrations not loaded")
	}
}

func mustLoadMigrations() []migrationplan.Step {
	steps, err := migrationplan.LoadSteps(migrationFiles, "sql")
	if err != nil {
		panic(err)
	}
	return steps
}

func mustBootstrapSchema(steps []migrationplan.Step) string {
	for _, step := range steps {
		if step.Version == BootstrapVersion {
			return step.UpSQL
		}
	}
	panic("postgresql bootstrap migration not found")
}

func (s *Store) CreateUser(ctx context.Context, input store.CreateUserInput) (store.User, error) {
	now := time.Now().UTC()
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users(id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL)
	`,
		input.ID,
		strings.TrimSpace(input.Username),
		strings.TrimSpace(input.Email),
		input.PasswordHash,
		role,
		input.IsActive,
		input.LocalAdmin,
		now,
		now,
	)
	if err != nil {
		return store.User{}, err
	}
	return store.User{
		ID:           input.ID,
		Username:     strings.TrimSpace(input.Username),
		Email:        strings.TrimSpace(input.Email),
		PasswordHash: input.PasswordHash,
		Role:         role,
		IsActive:     input.IsActive,
		LocalAdmin:   input.LocalAdmin,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (s *Store) UpdateUser(ctx context.Context, input store.UpdateUserInput) (store.User, error) {
	now := time.Now().UTC()
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users
		SET username = $1, email = $2, password_hash = $3, role = $4, is_active = $5, local_admin = $6, updated_at = $7, last_login_at = $8
		WHERE id = $9
	`,
		strings.TrimSpace(input.Username),
		strings.TrimSpace(input.Email),
		input.PasswordHash,
		role,
		input.IsActive,
		input.LocalAdmin,
		now,
		input.LastLoginAt,
		input.ID,
	)
	if err != nil {
		return store.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return store.User{}, store.ErrNotFound
	}
	return s.GetUser(ctx, input.ID)
}

func (s *Store) GetUser(ctx context.Context, id string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE id = $1
	`, id))
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE email = $1
	`, strings.TrimSpace(email)))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (store.User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE username = $1
	`, strings.TrimSpace(username)))
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.User, 0)
	for rows.Next() {
		item, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) PreviewUserDeletion(ctx context.Context, userID string) (store.UserDeletionPreview, error) {
	userID = strings.TrimSpace(userID)
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return store.UserDeletionPreview{}, err
	}

	preview := store.UserDeletionPreview{
		User:      user,
		CanDelete: true,
		PreserveRef: store.UserDeletionPreserveRef{
			AuditLogs:     true,
			Conversations: true,
			Uploads:       true,
		},
	}
	counts := &preview.Counts

	if counts.Identities, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_identities WHERE user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.UserConfigs, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_configs WHERE user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AutomationRules, err = s.countInt(ctx, `SELECT COUNT(*) FROM automation_rules WHERE user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AppAPIKeys, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AppAPIKeyAuditLogs, err = s.countInt(ctx, `SELECT COUNT(*) FROM app_api_key_audit_logs WHERE user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.ModelAPIKeys, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.StorageUserQuotas, err = s.countInt(ctx, `SELECT COUNT(*) FROM storage_user_quotas WHERE owner_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.StorageDeletionFailures, err = s.countInt(ctx, `SELECT COUNT(*) FROM storage_file_deletion_failures WHERE owner_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.OwnedConversations, err = s.countInt(ctx, `SELECT COUNT(*) FROM conversations WHERE COALESCE(metadata_json->>'owner_id', '') = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.OwnedUploadedImages, err = s.countInt(ctx, `SELECT COUNT(*) FROM uploaded_images WHERE owner_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AuditActorLogs, err = s.countInt(ctx, `SELECT COUNT(*) FROM audit_logs WHERE actor_user_id = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AuditMetadataUserReferences, err = s.countInt(ctx, `SELECT COUNT(*) FROM audit_logs WHERE COALESCE(metadata_json->>'user_id', '') = $1`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}

	if counts.OwnedConversations > 0 {
		preview.CanDelete = false
		preview.Blockers = append(preview.Blockers, "owned_conversations")
	}
	if counts.OwnedUploadedImages > 0 {
		preview.CanDelete = false
		preview.Blockers = append(preview.Blockers, "owned_uploaded_images")
	}
	return preview, nil
}

func (s *Store) DeleteUserAccount(ctx context.Context, userID string) error {
	userID = strings.TrimSpace(userID)
	preview, err := s.PreviewUserDeletion(ctx, userID)
	if err != nil {
		return err
	}
	if !preview.CanDelete {
		return errConflict
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	statements := []string{
		`DELETE FROM app_api_key_audit_logs WHERE user_id = $1`,
		`DELETE FROM user_app_api_keys WHERE user_id = $1`,
		`DELETE FROM user_api_keys WHERE user_id = $1`,
		`DELETE FROM user_configs WHERE user_id = $1`,
		`DELETE FROM automation_rules WHERE user_id = $1`,
		`DELETE FROM storage_file_deletion_failures WHERE owner_id = $1`,
		`DELETE FROM storage_user_quotas WHERE owner_id = $1`,
		`DELETE FROM user_identities WHERE user_id = $1`,
		`DELETE FROM users WHERE id = $1`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) UpsertUserIdentity(ctx context.Context, input store.UpsertUserIdentityInput) (store.UserIdentity, error) {
	now := time.Now().UTC()
	profileJSON := mustJSON(ensureMap(input.Profile))
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_identities(id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $10)
		ON CONFLICT(provider, subject) DO UPDATE SET
			user_id = excluded.user_id,
			email = excluded.email,
			email_verified = excluded.email_verified,
			profile_json = excluded.profile_json,
			updated_at = excluded.updated_at,
			last_login_at = excluded.last_login_at
	`,
		input.ID,
		input.UserID,
		strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Subject),
		strings.TrimSpace(input.Email),
		input.EmailVerified,
		profileJSON,
		now,
		now,
		input.LastLoginAt,
	)
	if err != nil {
		return store.UserIdentity{}, err
	}
	return s.GetUserIdentity(ctx, strings.TrimSpace(input.Provider), strings.TrimSpace(input.Subject))
}

func (s *Store) GetUserIdentity(ctx context.Context, provider string, subject string) (store.UserIdentity, error) {
	return scanUserIdentity(s.pool.QueryRow(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE provider = $1 AND subject = $2
	`, strings.TrimSpace(provider), strings.TrimSpace(subject)))
}

func (s *Store) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE user_id = $1
		ORDER BY provider ASC, created_at DESC, id DESC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.UserIdentity, 0)
	for rows.Next() {
		item, err := scanUserIdentity(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteUserIdentity(ctx context.Context, id string, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_identities
		WHERE id = $1 AND user_id = $2
	`, strings.TrimSpace(id), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetSystemConfig(ctx context.Context, key string) (store.SystemConfig, error) {
	return scanSystemConfig(s.pool.QueryRow(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		WHERE key = $1
	`, strings.TrimSpace(key)))
}

func (s *Store) SetSystemConfig(ctx context.Context, input store.SetSystemConfigInput) (store.SystemConfig, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO config(key, value_json, created_at, updated_at)
		VALUES ($1, $2::jsonb, $3, $4)
		ON CONFLICT(key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value)), now, now)
	if err != nil {
		return store.SystemConfig{}, err
	}
	return s.GetSystemConfig(ctx, input.Key)
}

func (s *Store) DeleteSystemConfig(ctx context.Context, key string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM config WHERE key = $1`, strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListSystemConfigs(ctx context.Context) ([]store.SystemConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT key, value_json, created_at, updated_at
		FROM config
		ORDER BY key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.SystemConfig, 0)
	for rows.Next() {
		item, err := scanSystemConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetUserConfig(ctx context.Context, userID string, key string) (store.UserConfig, error) {
	return scanUserConfig(s.pool.QueryRow(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = $1 AND key = $2
	`, strings.TrimSpace(userID), strings.TrimSpace(key)))
}

func (s *Store) SetUserConfig(ctx context.Context, input store.SetUserConfigInput) (store.UserConfig, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_configs(user_id, key, value_json, created_at, updated_at)
		VALUES ($1, $2, $3::jsonb, $4, $5)
		ON CONFLICT(user_id, key) DO UPDATE SET
			value_json = excluded.value_json,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(input.UserID), strings.TrimSpace(input.Key), mustJSON(ensureMap(input.Value)), now, now)
	if err != nil {
		return store.UserConfig{}, err
	}
	return s.GetUserConfig(ctx, input.UserID, input.Key)
}

func (s *Store) DeleteUserConfig(ctx context.Context, userID string, key string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_configs
		WHERE user_id = $1 AND key = $2
	`, strings.TrimSpace(userID), strings.TrimSpace(key))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) ListUserConfigs(ctx context.Context, userID string) ([]store.UserConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT user_id, key, value_json, created_at, updated_at
		FROM user_configs
		WHERE user_id = $1
		ORDER BY key ASC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.UserConfig, 0)
	for rows.Next() {
		item, err := scanUserConfig(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAuthVerificationCode(ctx context.Context, email string, purpose string) (store.AuthVerificationCode, error) {
	return scanAuthVerificationCode(s.pool.QueryRow(ctx, `
		SELECT email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at
		FROM auth_verification_codes
		WHERE email = $1 AND purpose = $2
	`, strings.TrimSpace(strings.ToLower(email)), strings.TrimSpace(purpose)))
}

func (s *Store) UpsertAuthVerificationCode(ctx context.Context, input store.UpsertAuthVerificationCodeInput) (store.AuthVerificationCode, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_verification_codes(email, purpose, code_hash, failed_attempts, expires_at, last_sent_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(email, purpose) DO UPDATE SET
			code_hash = excluded.code_hash,
			failed_attempts = excluded.failed_attempts,
			expires_at = excluded.expires_at,
			last_sent_at = excluded.last_sent_at,
			updated_at = excluded.updated_at
	`, strings.TrimSpace(strings.ToLower(input.Email)), strings.TrimSpace(input.Purpose), strings.TrimSpace(input.CodeHash), input.FailedAttempts, input.ExpiresAt.UTC(), input.LastSentAt.UTC(), now, now)
	if err != nil {
		return store.AuthVerificationCode{}, err
	}
	return s.GetAuthVerificationCode(ctx, input.Email, input.Purpose)
}

func (s *Store) DeleteAuthVerificationCode(ctx context.Context, email string, purpose string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM auth_verification_codes
		WHERE email = $1 AND purpose = $2
	`, strings.TrimSpace(strings.ToLower(email)), strings.TrimSpace(purpose))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteExpiredAuthVerificationCodes(ctx context.Context, before time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM auth_verification_codes
		WHERE expires_at <= $1
	`, before.UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) countInt(ctx context.Context, query string, args ...any) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func scanUser(row rowScanner) (store.User, error) {
	var item store.User
	var lastLoginAt *time.Time
	if err := row.Scan(
		&item.ID,
		&item.Username,
		&item.Email,
		&item.PasswordHash,
		&item.Role,
		&item.IsActive,
		&item.LocalAdmin,
		&item.CreatedAt,
		&item.UpdatedAt,
		&lastLoginAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, store.ErrNotFound
		}
		return store.User{}, err
	}
	item.LastLoginAt = lastLoginAt
	return item, nil
}

func scanUserIdentity(row rowScanner) (store.UserIdentity, error) {
	var item store.UserIdentity
	var profileJSON []byte
	var lastLoginAt *time.Time
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Provider,
		&item.Subject,
		&item.Email,
		&item.EmailVerified,
		&profileJSON,
		&item.CreatedAt,
		&item.UpdatedAt,
		&lastLoginAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.UserIdentity{}, store.ErrNotFound
		}
		return store.UserIdentity{}, err
	}
	item.Profile = parseJSONMap(profileJSON)
	item.LastLoginAt = lastLoginAt
	return item, nil
}

func scanSystemConfig(row rowScanner) (store.SystemConfig, error) {
	var item store.SystemConfig
	var valueJSON []byte
	if err := row.Scan(&item.Key, &valueJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.SystemConfig{}, store.ErrNotFound
		}
		return store.SystemConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	return item, nil
}

func scanAuthVerificationCode(row rowScanner) (store.AuthVerificationCode, error) {
	var item store.AuthVerificationCode
	if err := row.Scan(&item.Email, &item.Purpose, &item.CodeHash, &item.FailedAttempts, &item.ExpiresAt, &item.LastSentAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.AuthVerificationCode{}, store.ErrNotFound
		}
		return store.AuthVerificationCode{}, err
	}
	return item, nil
}

func scanUserConfig(row rowScanner) (store.UserConfig, error) {
	var item store.UserConfig
	var valueJSON []byte
	if err := row.Scan(&item.UserID, &item.Key, &valueJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.UserConfig{}, store.ErrNotFound
		}
		return store.UserConfig{}, err
	}
	item.Value = parseJSONMap(valueJSON)
	return item, nil
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func parseJSONMap(data []byte) map[string]any {
	if len(data) == 0 {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil || result == nil {
		return map[string]any{}
	}
	return result
}
