package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	_ "modernc.org/sqlite"

	"github.com/zyf/chatapi/internal/repository/migrations"
	"github.com/zyf/chatapi/internal/store"
)

type Store struct {
	db *sql.DB
}

var errNotFound = store.ErrNotFound
var errConflict = store.ErrTurnConflict

func Open(dsn string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite dir: %w", err)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) MigrationStatus(ctx context.Context) (store.MigrationStatus, error) {
	status, err := migrations.StatusReport(ctx, s.db)
	if err != nil {
		return store.MigrationStatus{}, err
	}
	applied := make([]store.AppliedMigration, 0, len(status.Applied))
	for _, item := range status.Applied {
		applied = append(applied, store.AppliedMigration{
			Version:   item.Version,
			Name:      item.Name,
			AppliedAt: item.AppliedAt,
			Checksum:  item.Checksum,
			Dirty:     item.Dirty,
		})
	}
	return store.MigrationStatus{
		SchemaVersion:  status.SchemaVersion,
		AppVersion:     status.AppVersion,
		MigrationDirty: status.MigrationDirty,
		MigrationLock:  status.MigrationLock,
		CreatedBy:      status.CreatedBy,
		LastMigratedAt: status.LastMigratedAt,
		Applied:        applied,
	}, nil
}

func (s *Store) Checkpoint(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
		return fmt.Errorf("sqlite wal checkpoint: %w", err)
	}
	return nil
}

func (s *Store) Vacuum(ctx context.Context) error {
	if err := s.Checkpoint(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `VACUUM;`); err != nil {
		return fmt.Errorf("sqlite vacuum: %w", err)
	}
	return nil
}

func (s *Store) ListConversations(ctx context.Context) ([]store.Conversation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Conversation, 0)
	for rows.Next() {
		var item store.Conversation
		var createdAt, updatedAt, lastMessageAt string
		var metadataJSON string
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&createdAt,
			&updatedAt,
			&lastMessageAt,
			&item.MessageCount,
			&item.LastMessagePreview,
			&item.LastUserText,
			&metadataJSON,
			&item.ResponseID,
		); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.UpdatedAt = parseTime(updatedAt)
		item.LastMessageAt = parseTime(lastMessageAt)
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetConversation(ctx context.Context, conversationID string) (store.Conversation, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, title, created_at, updated_at, last_message_at, message_count, last_message_preview, last_user_text, metadata_json, COALESCE(json_extract(metadata_json, '$.response_id'), '')
		FROM conversations
		WHERE id = ?
	`, conversationID)

	var item store.Conversation
	var createdAt, updatedAt, lastMessageAt string
	var metadataJSON string
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&createdAt,
		&updatedAt,
		&lastMessageAt,
		&item.MessageCount,
		&item.LastMessagePreview,
		&item.LastUserText,
		&metadataJSON,
		&item.ResponseID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Conversation{}, errNotFound
		}
		return store.Conversation{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.LastMessageAt = parseTime(lastMessageAt)
	item.Metadata = parseJSONMap(metadataJSON)
	return item, nil
}

func (s *Store) ListRequests(ctx context.Context) ([]store.Request, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND json_extract(m.metadata_json, '$.request_debug.request_id') IS NOT NULL
		ORDER BY c.updated_at DESC, m.created_at DESC, m.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Request, 0)
	for rows.Next() {
		item, err := scanRequestRow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetRequest(ctx context.Context, requestID string) (store.Request, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			m.conversation_id,
			m.created_at,
			m.metadata_json,
			c.updated_at,
			c.metadata_json
		FROM messages m
		JOIN conversations c ON c.id = m.conversation_id
		WHERE m.role = 'user'
			AND json_extract(m.metadata_json, '$.request_debug.request_id') = ?
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, requestID)

	item, err := scanRequestRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.Request{}, errNotFound
		}
		return store.Request{}, err
	}
	if item.RequestID == "" {
		item.RequestID = requestID
	}
	return item, nil
}

func (s *Store) CreateAppAPIKey(ctx context.Context, input store.CreateAppAPIKeyInput) (store.AppAPIKey, error) {
	createdAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_app_api_keys(
			id, user_id, name, key_hash, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		input.UserID,
		input.Name,
		input.KeyHash,
		input.KeyPrefix,
		mustJSON(input.Scopes),
		mustJSON(input.ResourceLimits),
		formatNullableTime(input.ExpiresAt),
		nil,
		formatTime(createdAt),
		nil,
	); err != nil {
		return store.AppAPIKey{}, err
	}
	return store.AppAPIKey{
		ID:             input.ID,
		UserID:         input.UserID,
		Name:           input.Name,
		KeyHash:        input.KeyHash,
		KeyPrefix:      input.KeyPrefix,
		Scopes:         input.Scopes,
		ResourceLimits: input.ResourceLimits,
		ExpiresAt:      input.ExpiresAt,
		CreatedAt:      createdAt,
	}, nil
}

func (s *Store) ListAppAPIKeysByUser(ctx context.Context, userID string) ([]store.AppAPIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		FROM user_app_api_keys
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.AppAPIKey, 0)
	for rows.Next() {
		item, err := scanAppAPIKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAppAPIKeyByPrefix(ctx context.Context, prefix string) (store.AppAPIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_hash, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		FROM user_app_api_keys
		WHERE key_prefix = ?
		LIMIT 1
	`, prefix)

	item, err := scanAppAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.AppAPIKey{}, errNotFound
		}
		return store.AppAPIKey{}, err
	}
	return item, nil
}

func (s *Store) UpdateAppAPIKeyLastUsedAt(ctx context.Context, id string, usedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_app_api_keys
		SET last_used_at = ?
		WHERE id = ?
	`, formatTime(usedAt), id)
	return err
}

func (s *Store) RevokeAppAPIKey(ctx context.Context, id string, userID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE user_app_api_keys
		SET revoked_at = ?
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL
	`, formatTime(time.Now().UTC()), id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) CreateAppAPIKeyAuditLog(ctx context.Context, item store.AppAPIKeyAuditLog) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_api_key_audit_logs(
			id, app_api_key_id, user_id, route, status_code, error_code, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.AppAPIKeyID, item.UserID, item.Route, item.StatusCode, item.ErrorCode, formatTime(item.CreatedAt))
	return err
}

func (s *Store) ListAppAPIKeyAuditLogs(ctx context.Context, input store.ListAppAPIKeyAuditLogsInput) ([]store.AppAPIKeyAuditLog, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := `
		SELECT id, app_api_key_id, user_id, route, status_code, error_code, created_at
		FROM app_api_key_audit_logs
	`
	args := make([]any, 0, 2)
	if strings.TrimSpace(input.UserID) != "" {
		query += " WHERE user_id = ?"
		args = append(args, strings.TrimSpace(input.UserID))
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.AppAPIKeyAuditLog, 0)
	for rows.Next() {
		item, err := scanAppAPIKeyAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateAuditLog(ctx context.Context, input store.CreateAuditLogInput) (store.AuditLog, error) {
	createdAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_logs(
			id, actor_user_id, actor_role, actor_source, event_type, resource_type,
			resource_id, action, outcome, ip_address, user_agent, metadata_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		input.ActorUserID,
		input.ActorRole,
		input.ActorSource,
		input.EventType,
		input.ResourceType,
		input.ResourceID,
		input.Action,
		input.Outcome,
		input.IPAddress,
		input.UserAgent,
		mustJSON(ensureMap(input.Metadata)),
		formatTime(createdAt),
	); err != nil {
		return store.AuditLog{}, err
	}
	return store.AuditLog{
		ID:           input.ID,
		ActorUserID:  input.ActorUserID,
		ActorRole:    input.ActorRole,
		ActorSource:  input.ActorSource,
		EventType:    input.EventType,
		ResourceType: input.ResourceType,
		ResourceID:   input.ResourceID,
		Action:       input.Action,
		Outcome:      input.Outcome,
		IPAddress:    input.IPAddress,
		UserAgent:    input.UserAgent,
		Metadata:     ensureMap(input.Metadata),
		CreatedAt:    createdAt,
	}, nil
}

func (s *Store) ListAuditLogs(ctx context.Context, input store.ListAuditLogsInput) ([]store.AuditLog, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := `
		SELECT id, actor_user_id, actor_role, actor_source, event_type, resource_type,
			resource_id, action, outcome, ip_address, user_agent, metadata_json, created_at
		FROM audit_logs
	`
	args := make([]any, 0, 3)
	conditions := make([]string, 0, 2)
	if strings.TrimSpace(input.EventType) != "" {
		conditions = append(conditions, "event_type = ?")
		args = append(args, strings.TrimSpace(input.EventType))
	}
	if strings.TrimSpace(input.ActorUserID) != "" {
		conditions = append(conditions, "actor_user_id = ?")
		args = append(args, strings.TrimSpace(input.ActorUserID))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateModelAPIKey(ctx context.Context, input store.CreateModelAPIKeyInput) (store.ModelAPIKey, error) {
	createdAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_api_keys(
			id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		input.UserID,
		input.Name,
		input.KeyCiphertext,
		input.KeyPrefix,
		input.Model,
		nil,
		formatTime(createdAt),
		nil,
	); err != nil {
		return store.ModelAPIKey{}, err
	}
	return store.ModelAPIKey{
		ID:            input.ID,
		UserID:        input.UserID,
		Name:          input.Name,
		KeyCiphertext: input.KeyCiphertext,
		KeyPrefix:     input.KeyPrefix,
		Model:         input.Model,
		CreatedAt:     createdAt,
	}, nil
}

func (s *Store) ListModelAPIKeysByUser(ctx context.Context, userID string) ([]store.ModelAPIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.ModelAPIKey, 0)
	for rows.Next() {
		item, err := scanModelAPIKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetModelAPIKeyByPrefix(ctx context.Context, prefix string) (store.ModelAPIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE key_prefix = ?
		LIMIT 1
	`, prefix)
	item, err := scanModelAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ModelAPIKey{}, errNotFound
		}
		return store.ModelAPIKey{}, err
	}
	return item, nil
}

func (s *Store) GetModelAPIKeyByID(ctx context.Context, id string) (store.ModelAPIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE id = ?
		LIMIT 1
	`, id)
	item, err := scanModelAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.ModelAPIKey{}, errNotFound
		}
		return store.ModelAPIKey{}, err
	}
	return item, nil
}

func (s *Store) UpdateModelAPIKeyLastUsedAt(ctx context.Context, id string, usedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_api_keys
		SET last_used_at = ?
		WHERE id = ?
	`, formatTime(usedAt), id)
	return err
}

func (s *Store) RevokeModelAPIKey(ctx context.Context, id string, userID string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE user_api_keys
		SET revoked_at = ?
		WHERE id = ? AND user_id = ? AND revoked_at IS NULL
	`, formatTime(time.Now().UTC()), id, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return errNotFound
	}
	return nil
}

func (s *Store) ListAutomationRulesByUser(ctx context.Context, userID string) ([]store.AutomationRule, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, enabled, rule_json, created_at, updated_at
		FROM automation_rules
		WHERE user_id = ?
		ORDER BY updated_at DESC, id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.AutomationRule, 0)
	for rows.Next() {
		item, err := scanAutomationRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ReplaceAutomationRulesForUser(ctx context.Context, userID string, replaceIDs map[string]struct{}, inputs []store.UpsertAutomationRuleInput) ([]store.AutomationRule, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if len(replaceIDs) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM automation_rules WHERE user_id = ?`, userID); err != nil {
			return nil, err
		}
	} else {
		for ruleID := range replaceIDs {
			if _, err := tx.ExecContext(ctx, `DELETE FROM automation_rules WHERE user_id = ? AND id = ?`, userID, ruleID); err != nil {
				return nil, err
			}
		}
	}

	now := formatTime(time.Now().UTC())
	for _, input := range inputs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO automation_rules(id, user_id, enabled, rule_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(user_id, id) DO UPDATE SET
				enabled = excluded.enabled,
				rule_json = excluded.rule_json,
				updated_at = excluded.updated_at
		`,
			input.ID,
			userID,
			boolInt(input.Enabled),
			mustJSON(ensureMap(input.Payload)),
			now,
			now,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListAutomationRulesByUser(ctx, userID)
}

func (s *Store) CreateUploadedImage(ctx context.Context, input store.CreateUploadedImageInput) (store.UploadedImage, error) {
	createdAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO uploaded_images(
			id, owner_id, filename, original_filename, content_type, bytes, url, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		input.OwnerID,
		input.Filename,
		input.OriginalFilename,
		input.ContentType,
		input.Bytes,
		input.URL,
		formatTime(createdAt),
	); err != nil {
		return store.UploadedImage{}, err
	}
	return store.UploadedImage{
		ID:               input.ID,
		OwnerID:          input.OwnerID,
		Filename:         input.Filename,
		OriginalFilename: input.OriginalFilename,
		ContentType:      input.ContentType,
		Bytes:            input.Bytes,
		URL:              input.URL,
		CreatedAt:        createdAt,
	}, nil
}

func (s *Store) ListUploadedImagesByOwner(ctx context.Context, ownerID string) ([]store.UploadedImage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, filename, original_filename, content_type, bytes, url, created_at
		FROM uploaded_images
		WHERE owner_id = ?
		ORDER BY created_at DESC, id DESC
	`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.UploadedImage, 0)
	for rows.Next() {
		item, err := scanUploadedImage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListUploadedImages(ctx context.Context) ([]store.UploadedImage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_id, filename, original_filename, content_type, bytes, url, created_at
		FROM uploaded_images
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.UploadedImage, 0)
	for rows.Next() {
		item, err := scanUploadedImage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteUploadedImagesByFilenames(ctx context.Context, filenames []string) (store.DeleteUploadedImagesResult, error) {
	filenames = uniqueNonEmptyStrings(filenames)
	if len(filenames) == 0 {
		return store.DeleteUploadedImagesResult{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(filenames)), ",")
	args := make([]any, 0, len(filenames))
	for _, filename := range filenames {
		args = append(args, filename)
	}
	query := fmt.Sprintf(`DELETE FROM uploaded_images WHERE filename IN (%s)`, placeholders)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return store.DeleteUploadedImagesResult{}, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return store.DeleteUploadedImagesResult{}, err
	}
	return store.DeleteUploadedImagesResult{DeletedImages: int(rowsAffected)}, nil
}

func (s *Store) UpsertStorageFileDeletionFailure(ctx context.Context, input store.UpsertStorageFileDeletionFailureInput) (store.StorageFileDeletionFailure, error) {
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO storage_file_deletion_failures(
			path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, 1, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			filename = excluded.filename,
			owner_id = excluded.owner_id,
			bytes = excluded.bytes,
			last_error = excluded.last_error,
			attempts = attempts + 1,
			updated_at = excluded.updated_at
	`,
		input.Path,
		input.Filename,
		input.OwnerID,
		input.Bytes,
		input.LastError,
		formatTime(now),
		formatTime(now),
	); err != nil {
		return store.StorageFileDeletionFailure{}, err
	}
	return s.getStorageFileDeletionFailure(ctx, input.Path)
}

func (s *Store) ListStorageFileDeletionFailures(ctx context.Context, limit int) ([]store.StorageFileDeletionFailure, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		FROM storage_file_deletion_failures
		ORDER BY updated_at ASC, path ASC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.StorageFileDeletionFailure, 0)
	for rows.Next() {
		item, err := scanStorageFileDeletionFailure(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteStorageFileDeletionFailures(ctx context.Context, paths []string) error {
	paths = uniqueNonEmptyStrings(paths)
	if len(paths) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths))
	for _, path := range paths {
		args = append(args, path)
	}
	query := fmt.Sprintf(`DELETE FROM storage_file_deletion_failures WHERE path IN (%s)`, placeholders)
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) getStorageFileDeletionFailure(ctx context.Context, path string) (store.StorageFileDeletionFailure, error) {
	item, err := scanStorageFileDeletionFailure(s.db.QueryRowContext(ctx, `
		SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		FROM storage_file_deletion_failures
		WHERE path = ?
	`, path))
	if err != nil {
		return store.StorageFileDeletionFailure{}, err
	}
	return item, nil
}

func (s *Store) ListStorageUserQuotas(ctx context.Context) ([]store.StorageUserQuota, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT owner_id, quota_bytes, created_at, updated_at
		FROM storage_user_quotas
		ORDER BY owner_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.StorageUserQuota, 0)
	for rows.Next() {
		item, err := scanStorageUserQuota(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetStorageUserQuota(ctx context.Context, ownerID string) (store.StorageUserQuota, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT owner_id, quota_bytes, created_at, updated_at
		FROM storage_user_quotas
		WHERE owner_id = ?
	`, ownerID)
	item, err := scanStorageUserQuota(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.StorageUserQuota{}, errNotFound
		}
		return store.StorageUserQuota{}, err
	}
	return item, nil
}

func (s *Store) SetStorageUserQuota(ctx context.Context, ownerID string, quotaBytes int64) (store.StorageUserQuota, error) {
	now := formatTime(time.Now().UTC())
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO storage_user_quotas(owner_id, quota_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(owner_id) DO UPDATE SET
			quota_bytes = excluded.quota_bytes,
			updated_at = excluded.updated_at
	`, ownerID, quotaBytes, now, now); err != nil {
		return store.StorageUserQuota{}, err
	}
	return s.GetStorageUserQuota(ctx, ownerID)
}

func (s *Store) DeleteStorageUserQuota(ctx context.Context, ownerID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = ?`, ownerID)
	return err
}

func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]store.Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, content, created_at, status, response_id, metadata_json
		FROM messages
		WHERE conversation_id = ?
		ORDER BY created_at ASC, id ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]store.Message, 0)
	for rows.Next() {
		var item store.Message
		var createdAt string
		var status sql.NullString
		var responseID sql.NullString
		var metadataJSON string
		if err := rows.Scan(&item.ID, &item.Role, &item.Content, &createdAt, &status, &responseID, &metadataJSON); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		item.Status = status.String
		if responseID.Valid {
			item.ResponseID = &responseID.String
		}
		item.Metadata = parseJSONMap(metadataJSON)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteConversations(ctx context.Context, conversationIDs []string) (store.DeleteConversationsResult, error) {
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	if len(conversationIDs) == 0 {
		return store.DeleteConversationsResult{}, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(conversationIDs)), ",")
	args := make([]any, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		args = append(args, id)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var result store.DeleteConversationsResult
	countMessagesQuery := fmt.Sprintf(`SELECT COUNT(*) FROM messages WHERE conversation_id IN (%s)`, placeholders)
	if err := tx.QueryRowContext(ctx, countMessagesQuery, args...).Scan(&result.DeletedMessages); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	deleteQuery := fmt.Sprintf(`DELETE FROM conversations WHERE id IN (%s)`, placeholders)
	res, err := tx.ExecContext(ctx, deleteQuery, args...)
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return store.DeleteConversationsResult{}, err
	}
	result.DeletedConversations = int(rowsAffected)
	if err := tx.Commit(); err != nil {
		return store.DeleteConversationsResult{}, err
	}
	return result, nil
}

func (s *Store) ExpirePendingTurns(ctx context.Context, cutoff time.Time) (store.ExpirePendingTurnsResult, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, metadata_json
		FROM conversations
		WHERE last_message_at < ?
			AND COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
	`, formatTime(cutoff))
	if err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	type candidate struct {
		id       string
		metadata map[string]any
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var item candidate
		var metadataJSON string
		if err := rows.Scan(&item.id, &metadataJSON); err != nil {
			_ = rows.Close()
			return store.ExpirePendingTurnsResult{}, err
		}
		item.metadata = ensureMap(parseJSONMap(metadataJSON))
		item.metadata["realtime_status"] = "expired"
		item.metadata["realtime_draft_text"] = ""
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	if err := rows.Err(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	if len(candidates) == 0 {
		return store.ExpirePendingTurnsResult{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	var result store.ExpirePendingTurnsResult
	for _, item := range candidates {
		res, err := tx.ExecContext(ctx, `
			UPDATE conversations
			SET updated_at = ?, metadata_json = ?
			WHERE id = ?
				AND COALESCE(json_extract(metadata_json, '$.realtime_status'), '') IN ('waiting', 'streaming')
		`, formatTime(now), mustJSON(item.metadata), item.id)
		if err != nil {
			return store.ExpirePendingTurnsResult{}, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return store.ExpirePendingTurnsResult{}, err
		}
		result.ExpiredConversations += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return store.ExpirePendingTurnsResult{}, err
	}
	return result, nil
}

func (s *Store) CreatePendingTurn(ctx context.Context, input store.CreatePendingInput) (store.Conversation, store.Message, error) {
	now := time.Now().UTC()
	metadata := map[string]any{
		"owner_id":            input.OwnerID,
		"request_format":      input.RequestFormat,
		"realtime_status":     "waiting",
		"realtime_draft_text": "",
		"response_id":         input.ResponseID,
		"model":               input.Model,
	}
	userMessageMetadata := map[string]any{
		"request_format": input.RequestFormat,
		"model":          input.Model,
		"request_debug": map[string]any{
			"request_id":     input.RequestID,
			"response_id":    input.ResponseID,
			"model":          input.Model,
			"request_format": input.RequestFormat,
			"request_keys":   keysOf(input.RequestBody),
			"input_text":     input.UserContent,
			"request_body":   input.RequestBody,
			"tool_schemas":   input.ToolSchemas,
		},
	}
	conversation := store.Conversation{
		ID:                 input.ConversationID,
		Title:              buildConversationTitle(input.UserContent),
		LastUserText:       input.UserContent,
		CreatedAt:          now,
		UpdatedAt:          now,
		LastMessageAt:      now,
		MessageCount:       1,
		LastMessagePreview: input.UserContent,
		Metadata:           metadata,
		ResponseID:         input.ResponseID,
	}
	responseID := input.ResponseID
	message := store.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "user",
		Content:    input.UserContent,
		CreatedAt:  now,
		Status:     "pending",
		ResponseID: &responseID,
		Metadata:   userMessageMetadata,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO conversations(
			id, title, created_at, updated_at, last_message_at,
			message_count, last_message_preview, last_user_text, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		conversation.ID,
		conversation.Title,
		formatTime(now),
		formatTime(now),
		formatTime(now),
		conversation.MessageCount,
		conversation.LastMessagePreview,
		conversation.LastUserText,
		mustJSON(metadata),
	); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		message.ID,
		conversation.ID,
		message.Role,
		message.Content,
		formatTime(now),
		message.Status,
		responseID,
		mustJSON(userMessageMetadata),
	); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if err := tx.Commit(); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	return conversation, message, nil
}

func (s *Store) UpdateDraft(ctx context.Context, input store.UpdateDraftInput) (store.Conversation, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isDraftWritable(metadata) {
		return store.Conversation{}, errConflict
	}
	metadata["realtime_draft_text"] = input.DraftText
	metadata["realtime_status"] = "streaming"
	conversation.Metadata = metadata
	conversation.UpdatedAt = time.Now().UTC()

	if _, err := s.db.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(conversation.UpdatedAt), mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, err
	}
	return conversation, nil
}

func (s *Store) CompletePendingTurn(ctx context.Context, input store.CompletePendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return store.Conversation{}, store.Message{}, errConflict
	}
	draftText, _ := metadata["realtime_draft_text"].(string)
	finalText := strings.TrimSpace(input.OutputText)
	if finalText == "" {
		finalText = draftText
	}
	messageContent := finalText
	if input.Mode == "thinking" && finalText != "" {
		messageContent = "<think>" + finalText + "</think>"
	}
	now := time.Now().UTC()
	metadata["realtime_status"] = "closed"
	metadata["realtime_draft_text"] = ""

	messageMetadata := map[string]any{
		"response_mode": input.Mode,
	}
	if input.ToolName != "" {
		messageMetadata["tool_name"] = input.ToolName
	}
	if input.ToolCallID != "" {
		messageMetadata["tool_call_id"] = input.ToolCallID
	}
	if input.Mode == "tool_call" {
		messageMetadata["arguments"] = finalText
	}
	if input.Mode == "tool_result" {
		messageMetadata["output"] = stringValue(input.ToolOutput, finalText)
	}
	if input.ReasoningStreamMode != "" {
		messageMetadata["reasoning_stream_mode"] = input.ReasoningStreamMode
	}
	responseID := input.ResponseID
	message := store.Message{
		ID:         "msg_" + uuid.NewString(),
		Role:       "assistant",
		Content:    messageContent,
		CreatedAt:  now,
		Status:     "completed",
		ResponseID: &responseID,
		Metadata:   messageMetadata,
	}

	conversation.Metadata = metadata
	conversation.UpdatedAt = now
	conversation.LastMessageAt = now
	conversation.MessageCount += 1
	conversation.LastMessagePreview = finalText

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ID, conversation.ID, message.Role, message.Content, formatTime(now), message.Status, responseID, mustJSON(messageMetadata)); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, last_message_at = ?, message_count = ?, last_message_preview = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, store.Message{}, err
	}

	if err := tx.Commit(); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	return conversation, message, nil
}

func (s *Store) AbortPendingTurn(ctx context.Context, input store.AbortPendingInput) (store.Conversation, store.Message, error) {
	conversation, err := s.GetConversation(ctx, input.ConversationID)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	metadata := ensureMap(conversation.Metadata)
	if !isTurnCompletable(metadata) {
		return store.Conversation{}, store.Message{}, errConflict
	}
	metadata["realtime_status"] = "aborted"
	metadata["realtime_draft_text"] = ""
	now := time.Now().UTC()

	message := store.Message{
		ID:        "msg_" + uuid.NewString(),
		Role:      "assistant",
		Content:   input.Reason,
		CreatedAt: now,
		Status:    "aborted",
		Metadata: map[string]any{
			"response_mode": "assistant_message",
		},
	}
	conversation.Metadata = metadata
	conversation.UpdatedAt = now
	conversation.LastMessageAt = now
	conversation.MessageCount += 1
	conversation.LastMessagePreview = input.Reason

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO messages(
			id, conversation_id, role, content, created_at, status, response_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, message.ID, conversation.ID, message.Role, message.Content, formatTime(now), message.Status, nil, mustJSON(message.Metadata)); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET updated_at = ?, last_message_at = ?, message_count = ?, last_message_preview = ?, metadata_json = ?
		WHERE id = ?
	`, formatTime(now), formatTime(now), conversation.MessageCount, conversation.LastMessagePreview, mustJSON(metadata), conversation.ID); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.Conversation{}, store.Message{}, err
	}
	return conversation, message, nil
}

func parseTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return t
	}
	t, err = time.Parse(time.RFC3339, raw)
	if err == nil {
		return t
	}
	return time.Time{}
}

func parseJSONMap(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil
	}
	return value
}

func parseJSONStringArray(raw string) []string {
	if raw == "" {
		return nil
	}
	var value []string
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return nil
	}
	return value
}

func ensureMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func formatNullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func keysOf(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
	return result
}

func buildConversationTitle(userContent string) string {
	text := strings.TrimSpace(userContent)
	if text == "" {
		return "新会话"
	}
	runes := []rune(text)
	if len(runes) > 24 {
		return string(runes[:24])
	}
	return text
}

func stringValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

type requestScanner interface {
	Scan(dest ...any) error
}

type appAPIKeyScanner interface {
	Scan(dest ...any) error
}

type appAPIKeyAuditLogScanner interface {
	Scan(dest ...any) error
}

type modelAPIKeyScanner interface {
	Scan(dest ...any) error
}

type automationRuleScanner interface {
	Scan(dest ...any) error
}

type uploadedImageScanner interface {
	Scan(dest ...any) error
}

type storageFileDeletionFailureScanner interface {
	Scan(dest ...any) error
}

type storageUserQuotaScanner interface {
	Scan(dest ...any) error
}

type auditLogScanner interface {
	Scan(dest ...any) error
}

func scanRequestRow(scanner requestScanner) (store.Request, error) {
	var item store.Request
	var createdAt string
	var updatedAt string
	var messageMetadataJSON string
	var conversationMetadataJSON string
	if err := scanner.Scan(
		&item.ConversationID,
		&createdAt,
		&messageMetadataJSON,
		&updatedAt,
		&conversationMetadataJSON,
	); err != nil {
		return store.Request{}, err
	}

	messageMetadata := parseJSONMap(messageMetadataJSON)
	requestDebug, _ := messageMetadata["request_debug"].(map[string]any)
	conversationMetadata := parseJSONMap(conversationMetadataJSON)

	item.RequestID = metadataString(requestDebug, "request_id", "")
	item.OwnerID = metadataString(conversationMetadata, "owner_id", "")
	item.ResponseID = metadataString(requestDebug, "response_id", "")
	item.RequestFormat = metadataString(requestDebug, "request_format", "")
	item.Model = metadataString(requestDebug, "model", "")
	item.InputText = metadataString(requestDebug, "input_text", "")
	item.Status = metadataString(conversationMetadata, "realtime_status", "")
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	item.Metadata = messageMetadata
	item.RequestBody, _ = requestDebug["request_body"].(map[string]any)
	item.ToolSchemas, _ = requestDebug["tool_schemas"].([]any)
	return item, nil
}

func scanAppAPIKey(scanner appAPIKeyScanner) (store.AppAPIKey, error) {
	var item store.AppAPIKey
	var scopesJSON string
	var resourceLimitsJSON string
	var expiresAt sql.NullString
	var lastUsedAt sql.NullString
	var createdAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.KeyHash,
		&item.KeyPrefix,
		&scopesJSON,
		&resourceLimitsJSON,
		&expiresAt,
		&lastUsedAt,
		&createdAt,
		&revokedAt,
	); err != nil {
		return store.AppAPIKey{}, err
	}
	item.Scopes = parseJSONStringArray(scopesJSON)
	item.ResourceLimits = parseJSONMap(resourceLimitsJSON)
	if expiresAt.Valid {
		value := parseTime(expiresAt.String)
		item.ExpiresAt = &value
	}
	if lastUsedAt.Valid {
		value := parseTime(lastUsedAt.String)
		item.LastUsedAt = &value
	}
	item.CreatedAt = parseTime(createdAt)
	if revokedAt.Valid {
		value := parseTime(revokedAt.String)
		item.RevokedAt = &value
	}
	return item, nil
}

func scanAppAPIKeyAuditLog(scanner appAPIKeyAuditLogScanner) (store.AppAPIKeyAuditLog, error) {
	var item store.AppAPIKeyAuditLog
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.AppAPIKeyID,
		&item.UserID,
		&item.Route,
		&item.StatusCode,
		&item.ErrorCode,
		&createdAt,
	); err != nil {
		return store.AppAPIKeyAuditLog{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanModelAPIKey(scanner modelAPIKeyScanner) (store.ModelAPIKey, error) {
	var item store.ModelAPIKey
	var model string
	var lastUsedAt sql.NullString
	var createdAt string
	var revokedAt sql.NullString
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.KeyCiphertext,
		&item.KeyPrefix,
		&model,
		&lastUsedAt,
		&createdAt,
		&revokedAt,
	); err != nil {
		return store.ModelAPIKey{}, err
	}
	item.Model = strings.TrimSpace(model)
	if lastUsedAt.Valid {
		value := parseTime(lastUsedAt.String)
		item.LastUsedAt = &value
	}
	item.CreatedAt = parseTime(createdAt)
	if revokedAt.Valid {
		value := parseTime(revokedAt.String)
		item.RevokedAt = &value
	}
	return item, nil
}

func scanAutomationRule(scanner automationRuleScanner) (store.AutomationRule, error) {
	var item store.AutomationRule
	var enabled int
	var payloadJSON string
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&enabled,
		&payloadJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return store.AutomationRule{}, err
	}
	item.Enabled = enabled != 0
	item.Payload = parseJSONMap(payloadJSON)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanUploadedImage(scanner uploadedImageScanner) (store.UploadedImage, error) {
	var item store.UploadedImage
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.OwnerID,
		&item.Filename,
		&item.OriginalFilename,
		&item.ContentType,
		&item.Bytes,
		&item.URL,
		&createdAt,
	); err != nil {
		return store.UploadedImage{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func scanStorageFileDeletionFailure(scanner storageFileDeletionFailureScanner) (store.StorageFileDeletionFailure, error) {
	var item store.StorageFileDeletionFailure
	var createdAt string
	var updatedAt string
	if err := scanner.Scan(
		&item.Path,
		&item.Filename,
		&item.OwnerID,
		&item.Bytes,
		&item.LastError,
		&item.Attempts,
		&createdAt,
		&updatedAt,
	); err != nil {
		return store.StorageFileDeletionFailure{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanStorageUserQuota(scanner storageUserQuotaScanner) (store.StorageUserQuota, error) {
	var item store.StorageUserQuota
	var createdAt, updatedAt string
	if err := scanner.Scan(
		&item.OwnerID,
		&item.QuotaBytes,
		&createdAt,
		&updatedAt,
	); err != nil {
		return store.StorageUserQuota{}, err
	}
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

func scanAuditLog(scanner auditLogScanner) (store.AuditLog, error) {
	var item store.AuditLog
	var metadataJSON string
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.ActorUserID,
		&item.ActorRole,
		&item.ActorSource,
		&item.EventType,
		&item.ResourceType,
		&item.ResourceID,
		&item.Action,
		&item.Outcome,
		&item.IPAddress,
		&item.UserAgent,
		&metadataJSON,
		&createdAt,
	); err != nil {
		return store.AuditLog{}, err
	}
	item.Metadata = parseJSONMap(metadataJSON)
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}

func isDraftWritable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func isTurnCompletable(metadata map[string]any) bool {
	status := metadataString(metadata, "realtime_status", "waiting")
	return status == "waiting" || status == "streaming"
}

func metadataString(metadata map[string]any, key string, fallback string) string {
	value, _ := metadata[key].(string)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
