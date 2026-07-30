package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) CreateAppAPIKey(ctx context.Context, input common.CreateAppAPIKeyInput) (common.AppAPIKey, error) {
	createdAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_app_api_keys(
			id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		input.UserID,
		input.Name,
		input.KeyHash,
		input.KeyCiphertext,
		input.KeyPrefix,
		mustJSON(input.Scopes),
		mustJSON(input.ResourceLimits),
		formatNullableTime(input.ExpiresAt),
		nil,
		formatTime(createdAt),
		nil,
	); err != nil {
		return common.AppAPIKey{}, err
	}
	return common.AppAPIKey{
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

func (s *Store) ListAppAPIKeysByUser(ctx context.Context, userID string) ([]common.AppAPIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		FROM user_app_api_keys
		WHERE user_id = ?
		ORDER BY created_at DESC, id DESC
	`, userID)
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
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at
		FROM user_app_api_keys
		WHERE key_prefix = ?
		LIMIT 1
	`, prefix)

	item, err := scanAppAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.AppAPIKey{}, errNotFound
		}
		return common.AppAPIKey{}, err
	}
	return item, nil
}

func (s *Store) GetAppAPIKeyByID(ctx context.Context, id string) (common.AppAPIKey, error) {
	item, err := scanAppAPIKey(s.db.QueryRowContext(ctx, `SELECT id, user_id, name, key_hash, key_ciphertext, key_prefix, scopes_json, resource_limits_json, expires_at, last_used_at, created_at, revoked_at FROM user_app_api_keys WHERE id = ?`, strings.TrimSpace(id)))
	if errors.Is(err, sql.ErrNoRows) {
		return common.AppAPIKey{}, errNotFound
	}
	return item, err
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
		DELETE FROM user_app_api_keys WHERE id = ? AND user_id = ?
	`, id, userID)
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

func (s *Store) CreateAppAPIKeyAuditLog(ctx context.Context, item common.AppAPIKeyAuditLog) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_api_key_audit_logs(
			id, app_api_key_id, user_id, route, status_code, error_code, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.AppAPIKeyID, item.UserID, item.Route, item.StatusCode, item.ErrorCode, formatTime(item.CreatedAt))
	if err != nil {
		s.logger(ctx).Warn("sqlite create app api key audit log failed", zap.String("app_api_key.id", item.AppAPIKeyID), zap.Error(err))
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
		s.logger(ctx).Warn("sqlite list app api key audit logs failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.AppAPIKeyAuditLog, 0)
	for rows.Next() {
		item, err := scanAppAPIKeyAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list app api key audit logs row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) CreateAuditLog(ctx context.Context, input common.CreateAuditLogInput) (common.AuditLog, error) {
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
		s.logger(ctx).Warn("sqlite create audit log failed", zap.String("audit.event_type", input.EventType), zap.String("audit.action", input.Action), zap.Error(err))
		return common.AuditLog{}, err
	}
	return common.AuditLog{
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

func (s *Store) ListAuditLogs(ctx context.Context, input common.ListAuditLogsInput) ([]common.AuditLog, error) {
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
		s.logger(ctx).Warn("sqlite list audit logs failed", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	items := make([]common.AuditLog, 0)
	for rows.Next() {
		item, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list audit logs row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) CountAuditLogs(ctx context.Context, input common.CountAuditLogsInput) (int, error) {
	query := `SELECT COUNT(*) FROM audit_logs`
	args := make([]any, 0, 5)
	conditions := make([]string, 0, 5)
	if strings.TrimSpace(input.EventType) != "" {
		conditions = append(conditions, "event_type = ?")
		args = append(args, strings.TrimSpace(input.EventType))
	}
	if strings.TrimSpace(input.ActorUserID) != "" {
		conditions = append(conditions, "actor_user_id = ?")
		args = append(args, strings.TrimSpace(input.ActorUserID))
	}
	if strings.TrimSpace(input.ResourceType) != "" {
		conditions = append(conditions, "resource_type = ?")
		args = append(args, strings.TrimSpace(input.ResourceType))
	}
	if strings.TrimSpace(input.Action) != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, strings.TrimSpace(input.Action))
	}
	if strings.TrimSpace(input.Outcome) != "" {
		conditions = append(conditions, "outcome = ?")
		args = append(args, strings.TrimSpace(input.Outcome))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		s.logger(ctx).Warn("sqlite count audit logs failed", zap.Error(err))
		return 0, err
	}
	return count, nil
}

func (s *Store) CreateModelAPIKey(ctx context.Context, input common.CreateModelAPIKeyInput) (common.ModelAPIKey, error) {
	createdAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_api_keys(
			id, user_id, name, key_ciphertext, key_hash, key_prefix, model, last_used_at, created_at, revoked_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		input.UserID,
		input.Name,
		input.KeyCiphertext,
		input.KeyHash,
		input.KeyPrefix,
		input.Model,
		nil,
		formatTime(createdAt),
		nil,
	); err != nil {
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

func (s *Store) CreateVirtualModel(ctx context.Context, input common.CreateVirtualModelInput) (common.VirtualModel, error) {
	createdAt := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_virtual_models(id, user_id, name, created_at) VALUES (?, ?, ?, ?)`, input.ID, input.UserID, input.Name, formatTime(createdAt))
	if err != nil {
		return common.VirtualModel{}, err
	}
	return common.VirtualModel{ID: input.ID, UserID: input.UserID, Name: input.Name, CreatedAt: createdAt}, nil
}

func (s *Store) ListVirtualModelsByUser(ctx context.Context, userID string) ([]common.VirtualModel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, name, created_at FROM user_virtual_models WHERE user_id = ? ORDER BY created_at DESC, id DESC`, strings.TrimSpace(userID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]common.VirtualModel, 0)
	for rows.Next() {
		var item common.VirtualModel
		var createdAt string
		if err := rows.Scan(&item.ID, &item.UserID, &item.Name, &createdAt); err != nil {
			return nil, err
		}
		item.CreatedAt = parseTime(createdAt)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteVirtualModel(ctx context.Context, id, userID string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM user_virtual_models WHERE id = ? AND user_id = ?`, strings.TrimSpace(id), strings.TrimSpace(userID))
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

func (s *Store) GetModelAPIKeyByPrefix(ctx context.Context, prefix string) (common.ModelAPIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE key_prefix = ?
		LIMIT 1
	`, prefix)
	item, err := scanModelAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.ModelAPIKey{}, errNotFound
		}
		return common.ModelAPIKey{}, err
	}
	return item, nil
}

func (s *Store) GetModelAPIKeyByID(ctx context.Context, id string) (common.ModelAPIKey, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, key_ciphertext, key_prefix, model, last_used_at, created_at, revoked_at
		FROM user_api_keys
		WHERE id = ?
		LIMIT 1
	`, id)
	item, err := scanModelAPIKey(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.ModelAPIKey{}, errNotFound
		}
		return common.ModelAPIKey{}, err
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
		DELETE FROM user_api_keys WHERE id = ? AND user_id = ?
	`, id, userID)
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

func (s *Store) CreateUser(ctx context.Context, input common.CreateUserInput) (common.User, error) {
	now := time.Now().UTC()
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO users(
			id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.ID,
		strings.TrimSpace(input.Username),
		strings.TrimSpace(input.Email),
		input.PasswordHash,
		role,
		boolInt(input.IsActive),
		boolInt(input.LocalAdmin),
		formatTime(now),
		formatTime(now),
		nil,
	); err != nil {
		return common.User{}, err
	}
	return common.User{
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

func (s *Store) UpdateUser(ctx context.Context, input common.UpdateUserInput) (common.User, error) {
	now := time.Now().UTC()
	role := strings.TrimSpace(input.Role)
	if role == "" {
		role = "user"
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE users
		SET username = ?, email = ?, password_hash = ?, role = ?, is_active = ?, local_admin = ?, updated_at = ?, last_login_at = ?
		WHERE id = ?
	`,
		strings.TrimSpace(input.Username),
		strings.TrimSpace(input.Email),
		input.PasswordHash,
		role,
		boolInt(input.IsActive),
		boolInt(input.LocalAdmin),
		formatTime(now),
		formatNullableTime(input.LastLoginAt),
		input.ID,
	)
	if err != nil {
		return common.User{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return common.User{}, err
	}
	if affected == 0 {
		return common.User{}, errNotFound
	}
	return s.GetUser(ctx, input.ID)
}

func (s *Store) GetUser(ctx context.Context, id string) (common.User, error) {
	item, err := scanUserWithKeyCounts(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.is_active, u.local_admin, u.created_at, u.updated_at, u.last_login_at,
		(SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = u.id), (SELECT COUNT(*) FROM user_api_keys WHERE user_id = u.id)
		FROM users u
		WHERE id = ?
	`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.User{}, errNotFound
		}
		return common.User{}, err
	}
	return item, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (common.User, error) {
	item, err := scanUserWithKeyCounts(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.is_active, u.local_admin, u.created_at, u.updated_at, u.last_login_at,
		(SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = u.id), (SELECT COUNT(*) FROM user_api_keys WHERE user_id = u.id)
		FROM users u
		WHERE email = ?
	`, strings.TrimSpace(email)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.User{}, errNotFound
		}
		return common.User{}, err
	}
	return item, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (common.User, error) {
	item, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE username = ?
	`, strings.TrimSpace(username)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.User{}, errNotFound
		}
		return common.User{}, err
	}
	return item, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]common.User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.is_active, u.local_admin, u.created_at, u.updated_at, u.last_login_at,
		(SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = u.id), (SELECT COUNT(*) FROM user_api_keys WHERE user_id = u.id)
		FROM users u
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.User, 0)
	for rows.Next() {
		item, err := scanUserWithKeyCounts(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListUsersPage(ctx context.Context, offset int, limit int, query string) ([]common.User, int, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	query = strings.ToLower(strings.TrimSpace(query))
	const filter = `
		(? = '' OR instr(lower(u.username), ?) > 0
			OR EXISTS (SELECT 1 FROM user_app_api_keys ak WHERE ak.user_id = u.id AND ak.revoked_at IS NULL AND instr(lower(ak.key_prefix), ?) > 0)
			OR EXISTS (SELECT 1 FROM user_api_keys mk WHERE mk.user_id = u.id AND mk.revoked_at IS NULL AND instr(lower(mk.key_prefix), ?) > 0)
			OR EXISTS (
				SELECT 1 FROM conversations c
				WHERE COALESCE(json_extract(c.metadata_json, '$.owner_id'), '') = u.id
					AND (instr(lower(c.title), ?) > 0 OR EXISTS (
						SELECT 1 FROM messages m WHERE m.conversation_id = c.id AND instr(lower(m.content), ?) > 0
					))
			)
		)`
	filterArgs := []any{query, query, query, query, query, query}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users u WHERE `+filter, filterArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.password_hash, u.role, u.is_active, u.local_admin, u.created_at, u.updated_at, u.last_login_at,
		(SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = u.id), (SELECT COUNT(*) FROM user_api_keys WHERE user_id = u.id)
		FROM users u
		WHERE `+filter+`
		ORDER BY created_at DESC, id DESC
		LIMIT ? OFFSET ?
	`, append(filterArgs, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]common.User, 0, limit)
	for rows.Next() {
		item, err := scanUserWithKeyCounts(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Store) PreviewUserDeletion(ctx context.Context, userID string) (common.UserDeletionPreview, error) {
	userID = strings.TrimSpace(userID)
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return common.UserDeletionPreview{}, err
	}

	preview := common.UserDeletionPreview{
		User:      user,
		CanDelete: true,
		PreserveRef: common.UserDeletionPreserveRef{
			AuditLogs:     true,
			Conversations: true,
			Uploads:       true,
		},
	}
	counts := &preview.Counts

	if counts.Identities, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_identities WHERE user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.UserConfigs, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_configs WHERE user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.AutomationRules, err = s.countInt(ctx, `SELECT COUNT(*) FROM automation_rules WHERE user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.AppAPIKeys, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.AppAPIKeyAuditLogs, err = s.countInt(ctx, `SELECT COUNT(*) FROM app_api_key_audit_logs WHERE user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.ModelAPIKeys, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.StorageUserQuotas, err = s.countInt(ctx, `SELECT COUNT(*) FROM storage_user_quotas WHERE owner_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.StorageDeletionFailures, err = s.countInt(ctx, `SELECT COUNT(*) FROM storage_file_deletion_failures WHERE owner_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.OwnedConversations, err = s.countInt(ctx, `SELECT COUNT(*) FROM conversations WHERE COALESCE(json_extract(metadata_json, '$.owner_id'), '') = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.OwnedUploadedImages, err = s.countInt(ctx, `SELECT COUNT(*) FROM uploaded_images WHERE owner_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.AuditActorLogs, err = s.countInt(ctx, `SELECT COUNT(*) FROM audit_logs WHERE actor_user_id = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
	}
	if counts.AuditMetadataUserReferences, err = s.countInt(ctx, `SELECT COUNT(*) FROM audit_logs WHERE COALESCE(json_extract(metadata_json, '$.user_id'), '') = ?`, userID); err != nil {
		return common.UserDeletionPreview{}, err
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DELETE FROM app_api_key_audit_logs WHERE user_id = ?`,
		`DELETE FROM user_app_api_keys WHERE user_id = ?`,
		`DELETE FROM user_api_keys WHERE user_id = ?`,
		`DELETE FROM user_configs WHERE user_id = ?`,
		`DELETE FROM automation_rules WHERE user_id = ?`,
		`DELETE FROM storage_file_deletion_failures WHERE owner_id = ?`,
		`DELETE FROM storage_user_quotas WHERE owner_id = ?`,
		`DELETE FROM user_identities WHERE user_id = ?`,
		`DELETE FROM users WHERE id = ?`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement, userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) TransferUserOwnership(ctx context.Context, sourceUserID string, targetUserID string) (common.UserOwnershipTransferResult, error) {
	sourceUserID = strings.TrimSpace(sourceUserID)
	targetUserID = strings.TrimSpace(targetUserID)
	if sourceUserID == "" || targetUserID == "" || sourceUserID == targetUserID {
		return common.UserOwnershipTransferResult{}, errConflict
	}
	if _, err := s.GetUser(ctx, sourceUserID); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	if _, err := s.GetUser(ctx, targetUserID); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result := common.UserOwnershipTransferResult{
		SourceUserID: sourceUserID,
		TargetUserID: targetUserID,
	}

	conversationsRes, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET metadata_json = json_set(COALESCE(metadata_json, '{}'), '$.owner_id', ?)
		WHERE COALESCE(json_extract(metadata_json, '$.owner_id'), '') = ?
	`, targetUserID, sourceUserID)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	if rows, err := conversationsRes.RowsAffected(); err == nil {
		result.TransferredConversations = int(rows)
	} else {
		return common.UserOwnershipTransferResult{}, err
	}

	imagesRes, err := tx.ExecContext(ctx, `
		UPDATE uploaded_images
		SET owner_id = ?
		WHERE owner_id = ?
	`, targetUserID, sourceUserID)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	if rows, err := imagesRes.RowsAffected(); err == nil {
		result.TransferredUploadedImages = int(rows)
	} else {
		return common.UserOwnershipTransferResult{}, err
	}

	failuresRes, err := tx.ExecContext(ctx, `
		UPDATE storage_file_deletion_failures
		SET owner_id = ?
		WHERE owner_id = ?
	`, targetUserID, sourceUserID)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	if rows, err := failuresRes.RowsAffected(); err == nil {
		result.TransferredDeletionFailures = int(rows)
	} else {
		return common.UserOwnershipTransferResult{}, err
	}

	var sourceQuota int64
	sourceQuotaErr := tx.QueryRowContext(ctx, `
		SELECT quota_bytes
		FROM storage_user_quotas
		WHERE owner_id = ?
	`, sourceUserID).Scan(&sourceQuota)
	if sourceQuotaErr != nil && !errors.Is(sourceQuotaErr, sql.ErrNoRows) {
		return common.UserOwnershipTransferResult{}, sourceQuotaErr
	}
	if sourceQuotaErr == nil {
		var targetQuota int64
		targetQuotaErr := tx.QueryRowContext(ctx, `
			SELECT quota_bytes
			FROM storage_user_quotas
			WHERE owner_id = ?
		`, targetUserID).Scan(&targetQuota)
		switch {
		case errors.Is(targetQuotaErr, sql.ErrNoRows):
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO storage_user_quotas(owner_id, quota_bytes, created_at, updated_at)
				SELECT ?, quota_bytes, created_at, ?
				FROM storage_user_quotas
				WHERE owner_id = ?
			`, targetUserID, formatTime(time.Now().UTC()), sourceUserID); err != nil {
				return common.UserOwnershipTransferResult{}, err
			}
			result.TargetQuotaCreatedFromSource = true
		case targetQuotaErr == nil:
			result.TargetQuotaPreserved = true
		default:
			return common.UserOwnershipTransferResult{}, targetQuotaErr
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = ?`, sourceUserID); err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		result.SourceQuotaDeleted = true
	}

	if err := tx.Commit(); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	return result, nil
}

func (s *Store) TransferUserOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (common.UserOwnershipTransferResult, error) {
	sourceUserID = strings.TrimSpace(sourceUserID)
	targetUserID = strings.TrimSpace(targetUserID)
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	filenames = uniqueNonEmptyStrings(filenames)
	if sourceUserID == "" || targetUserID == "" || sourceUserID == targetUserID || (len(conversationIDs) == 0 && len(filenames) == 0) {
		return common.UserOwnershipTransferResult{}, errConflict
	}
	if _, err := s.GetUser(ctx, sourceUserID); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	if _, err := s.GetUser(ctx, targetUserID); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result := common.UserOwnershipTransferResult{
		SourceUserID: sourceUserID,
		TargetUserID: targetUserID,
	}

	if len(conversationIDs) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(conversationIDs)), ",")
		args := make([]any, 0, len(conversationIDs)+2)
		args = append(args, targetUserID, sourceUserID)
		for _, id := range conversationIDs {
			args = append(args, id)
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE conversations
			SET metadata_json = json_set(COALESCE(metadata_json, '{}'), '$.owner_id', ?)
			WHERE COALESCE(json_extract(metadata_json, '$.owner_id'), '') = ?
				AND id IN (`+placeholders+`)
		`, args...)
		if err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		if rows, err := res.RowsAffected(); err == nil {
			result.TransferredConversations = int(rows)
		} else {
			return common.UserOwnershipTransferResult{}, err
		}
	}

	if len(filenames) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(filenames)), ",")
		imageArgs := make([]any, 0, len(filenames)+2)
		imageArgs = append(imageArgs, targetUserID, sourceUserID)
		for _, filename := range filenames {
			imageArgs = append(imageArgs, filename)
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE uploaded_images
			SET owner_id = ?
			WHERE owner_id = ?
				AND filename IN (`+placeholders+`)
		`, imageArgs...)
		if err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		if rows, err := res.RowsAffected(); err == nil {
			result.TransferredUploadedImages = int(rows)
		} else {
			return common.UserOwnershipTransferResult{}, err
		}

		failureArgs := make([]any, 0, len(filenames)+2)
		failureArgs = append(failureArgs, targetUserID, sourceUserID)
		for _, filename := range filenames {
			failureArgs = append(failureArgs, filename)
		}
		failureRes, err := tx.ExecContext(ctx, `
			UPDATE storage_file_deletion_failures
			SET owner_id = ?
			WHERE owner_id = ?
				AND filename IN (`+placeholders+`)
		`, failureArgs...)
		if err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		if rows, err := failureRes.RowsAffected(); err == nil {
			result.TransferredDeletionFailures = int(rows)
		} else {
			return common.UserOwnershipTransferResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	return result, nil
}

func (s *Store) UpsertUserIdentity(ctx context.Context, input common.UpsertUserIdentityInput) (common.UserIdentity, error) {
	now := time.Now().UTC()
	profile := ensureMap(input.Profile)
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO user_identities(
			id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		boolInt(input.EmailVerified),
		mustJSON(profile),
		formatTime(now),
		formatTime(now),
		formatNullableTime(input.LastLoginAt),
	); err != nil {
		return common.UserIdentity{}, err
	}
	return s.GetUserIdentity(ctx, strings.TrimSpace(input.Provider), strings.TrimSpace(input.Subject))
}

func (s *Store) GetUserIdentity(ctx context.Context, provider string, subject string) (common.UserIdentity, error) {
	item, err := scanUserIdentity(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE provider = ? AND subject = ?
	`, strings.TrimSpace(provider), strings.TrimSpace(subject)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.UserIdentity{}, errNotFound
		}
		return common.UserIdentity{}, err
	}
	return item, nil
}

func (s *Store) ListUserIdentities(ctx context.Context, userID string) ([]common.UserIdentity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE user_id = ?
		ORDER BY provider ASC, created_at DESC, id DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.UserIdentity, 0)
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
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM user_identities
		WHERE id = ? AND user_id = ?
	`, strings.TrimSpace(id), strings.TrimSpace(userID))
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

func (s *Store) countInt(ctx context.Context, query string, args ...any) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}
