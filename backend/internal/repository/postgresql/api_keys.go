package postgresql

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) CreateAppAPIKey(ctx context.Context, input common.CreateAppAPIKeyInput) (common.AppAPIKey, error) {
	createdAt := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_app_api_keys(
			id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9, NULL, $10, NULL)
	`,
		input.ID,
		input.UserID,
		input.Name,
		input.KeyHash,
		input.KeyCiphertext,
		input.KeyPrefix,
		mustJSON(input.Scopes),
		mustJSON(ensureMap(input.ResourceLimits)),
		input.ExpiresAt,
		createdAt,
	)
	if err != nil {
		return common.AppAPIKey{}, err
	}
	return common.AppAPIKey{
		ID:             input.ID,
		UserID:         input.UserID,
		Name:           input.Name,
		KeyHash:        input.KeyHash,
		KeyPrefix:      input.KeyPrefix,
		Scopes:         input.Scopes,
		ResourceLimits: ensureMap(input.ResourceLimits),
		ExpiresAt:      input.ExpiresAt,
		CreatedAt:      createdAt,
	}, nil
}

func (s *Store) ListAppAPIKeysByUser(ctx context.Context, userID string) ([]common.AppAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		FROM user_app_api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.AppAPIKey, 0)
	for rows.Next() {
		item, err := scanAppAPIKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAppAPIKeyByPrefix(ctx context.Context, prefix string) (common.AppAPIKey, error) {
	return scanAppAPIKey(s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		FROM user_app_api_keys
		WHERE key_prefix = $1
		LIMIT 1
	`, strings.TrimSpace(prefix)))
}

func (s *Store) GetAppAPIKeyByID(ctx context.Context, id string) (common.AppAPIKey, error) {
	return scanAppAPIKey(s.pool.QueryRow(ctx, `SELECT id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at FROM user_app_api_keys WHERE id = $1`, strings.TrimSpace(id)))
}

func (s *Store) UpdateAppAPIKeyLastUsedAt(ctx context.Context, id string, usedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE user_app_api_keys
		SET last_used_at = $1
		WHERE id = $2
	`, usedAt, strings.TrimSpace(id))
	return err
}

func (s *Store) RevokeAppAPIKey(ctx context.Context, id string, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_app_api_keys WHERE id = $1 AND user_id = $2
	`, strings.TrimSpace(id), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return common.ErrNotFound
	}
	return nil
}

func (s *Store) CreateAppAPIKeyAuditLog(ctx context.Context, item common.AppAPIKeyAuditLog) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO app_api_key_audit_logs(id, app_api_key_id, user_id, route, status_code, error_code, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, item.ID, item.AppAPIKeyID, item.UserID, item.Route, item.StatusCode, item.ErrorCode, item.CreatedAt)
	if err != nil {
		s.logger(ctx).Warn("postgresql create app api key audit log failed", zap.String("app_api_key.id", item.AppAPIKeyID), zap.Error(err))
	}
	return err
}

func (s *Store) ListAppAPIKeyAuditLogs(ctx context.Context, input common.ListAppAPIKeyAuditLogsInput) ([]common.AppAPIKeyAuditLog, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	args := []any{limit}
	query := `
		SELECT id, app_api_key_id, user_id, route, status_code, error_code, created_at
		FROM app_api_key_audit_logs
	`
	if strings.TrimSpace(input.UserID) != "" {
		query += " WHERE user_id = $2"
		args = append(args, strings.TrimSpace(input.UserID))
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT $1"
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		s.logger(ctx).Warn("postgresql list app api key audit logs failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.AppAPIKeyAuditLog, 0)
	for rows.Next() {
		var item common.AppAPIKeyAuditLog
		if err := rows.Scan(&item.ID, &item.AppAPIKeyID, &item.UserID, &item.Route, &item.StatusCode, &item.ErrorCode, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("postgresql list app api key audit logs row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) CreateModelAPIKey(ctx context.Context, input common.CreateModelAPIKeyInput) (common.ModelAPIKey, error) {
	createdAt := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO user_api_keys(id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULL, $7, NULL)
	`, input.ID, input.UserID, input.Name, input.KeyCiphertext, input.KeyPrefix, input.Model, createdAt)
	if err != nil {
		return common.ModelAPIKey{}, err
	}
	return common.ModelAPIKey{
		ID:            input.ID,
		UserID:        input.UserID,
		Name:          input.Name,
		KeyCiphertext: input.KeyCiphertext,
		KeyPrefix:     input.KeyPrefix,
		Model:         input.Model,
		CreatedAt:     createdAt,
	}, nil
}

func (s *Store) ListModelAPIKeysByUser(ctx context.Context, userID string) ([]common.ModelAPIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
	`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.ModelAPIKey, 0)
	for rows.Next() {
		item, err := scanModelAPIKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetModelAPIKeyByPrefix(ctx context.Context, prefix string) (common.ModelAPIKey, error) {
	return scanModelAPIKey(s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE key_prefix = $1
		LIMIT 1
	`, strings.TrimSpace(prefix)))
}

func (s *Store) GetModelAPIKeyByID(ctx context.Context, id string) (common.ModelAPIKey, error) {
	return scanModelAPIKey(s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE id = $1
		LIMIT 1
	`, strings.TrimSpace(id)))
}

func (s *Store) UpdateModelAPIKeyLastUsedAt(ctx context.Context, id string, usedAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE user_api_keys
		SET last_used_at = $1
		WHERE id = $2
	`, usedAt, strings.TrimSpace(id))
	return err
}

func (s *Store) RevokeModelAPIKey(ctx context.Context, id string, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM user_api_keys WHERE id = $1 AND user_id = $2
	`, strings.TrimSpace(id), strings.TrimSpace(userID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return common.ErrNotFound
	}
	return nil
}

func scanAppAPIKey(row rowScanner) (common.AppAPIKey, error) {
	var item common.AppAPIKey
	var scopesJSON []byte
	var limitsJSON []byte
	var expiresAt *time.Time
	var lastUsedAt *time.Time
	var revokedAt *time.Time
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.KeyHash,
		&item.KeyCiphertext,
		&item.KeyPrefix,
		&scopesJSON,
		&limitsJSON,
		&expiresAt,
		&lastUsedAt,
		&item.CreatedAt,
		&revokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.AppAPIKey{}, common.ErrNotFound
		}
		return common.AppAPIKey{}, err
	}
	item.Scopes = parseJSONStringSlice(scopesJSON)
	item.ResourceLimits = parseJSONMap(limitsJSON)
	item.ExpiresAt = expiresAt
	item.LastUsedAt = lastUsedAt
	item.RevokedAt = revokedAt
	return item, nil
}

func scanModelAPIKey(row rowScanner) (common.ModelAPIKey, error) {
	var item common.ModelAPIKey
	var lastUsedAt *time.Time
	var revokedAt *time.Time
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.Name,
		&item.KeyCiphertext,
		&item.KeyPrefix,
		&item.Model,
		&lastUsedAt,
		&item.CreatedAt,
		&revokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.ModelAPIKey{}, common.ErrNotFound
		}
		return common.ModelAPIKey{}, err
	}
	item.LastUsedAt = lastUsedAt
	item.RevokedAt = revokedAt
	return item, nil
}

func parseJSONStringSlice(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	var result []string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}
	return result
}
