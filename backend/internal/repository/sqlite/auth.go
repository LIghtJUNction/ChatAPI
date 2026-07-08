package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/store"
)

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
	if err != nil {
		s.logger(ctx).Warn("sqlite create app api key audit log failed", zap.String("app_api_key.id", item.AppAPIKeyID), zap.Error(err))
	}
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
		s.logger(ctx).Warn("sqlite list app api key audit logs failed", zap.Error(err))
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
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list app api key audit logs row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
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
		s.logger(ctx).Warn("sqlite create audit log failed", zap.String("audit.event_type", input.EventType), zap.String("audit.action", input.Action), zap.Error(err))
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
		s.logger(ctx).Warn("sqlite list audit logs failed", zap.Error(err))
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
	if err := rows.Err(); err != nil {
		s.logger(ctx).Warn("sqlite list audit logs row iteration failed", zap.Error(err))
		return nil, err
	}
	return items, nil
}

func (s *Store) CountAuditLogs(ctx context.Context, input store.CountAuditLogsInput) (int, error) {
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

func (s *Store) CreateUser(ctx context.Context, input store.CreateUserInput) (store.User, error) {
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
		return store.User{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return store.User{}, err
	}
	if affected == 0 {
		return store.User{}, errNotFound
	}
	return s.GetUser(ctx, input.ID)
}

func (s *Store) GetUser(ctx context.Context, id string) (store.User, error) {
	item, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE id = ?
	`, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.User{}, errNotFound
		}
		return store.User{}, err
	}
	return item, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, email string) (store.User, error) {
	item, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE email = ?
	`, strings.TrimSpace(email)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.User{}, errNotFound
		}
		return store.User{}, err
	}
	return item, nil
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (store.User, error) {
	item, err := scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, password_hash, role, is_active, local_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE username = ?
	`, strings.TrimSpace(username)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.User{}, errNotFound
		}
		return store.User{}, err
	}
	return item, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]store.User, error) {
	rows, err := s.db.QueryContext(ctx, `
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

	if counts.Identities, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_identities WHERE user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.UserConfigs, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_configs WHERE user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AutomationRules, err = s.countInt(ctx, `SELECT COUNT(*) FROM automation_rules WHERE user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AppAPIKeys, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_app_api_keys WHERE user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AppAPIKeyAuditLogs, err = s.countInt(ctx, `SELECT COUNT(*) FROM app_api_key_audit_logs WHERE user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.ModelAPIKeys, err = s.countInt(ctx, `SELECT COUNT(*) FROM user_api_keys WHERE user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.StorageUserQuotas, err = s.countInt(ctx, `SELECT COUNT(*) FROM storage_user_quotas WHERE owner_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.StorageDeletionFailures, err = s.countInt(ctx, `SELECT COUNT(*) FROM storage_file_deletion_failures WHERE owner_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.OwnedConversations, err = s.countInt(ctx, `SELECT COUNT(*) FROM conversations WHERE COALESCE(json_extract(metadata_json, '$.owner_id'), '') = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.OwnedUploadedImages, err = s.countInt(ctx, `SELECT COUNT(*) FROM uploaded_images WHERE owner_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AuditActorLogs, err = s.countInt(ctx, `SELECT COUNT(*) FROM audit_logs WHERE actor_user_id = ?`, userID); err != nil {
		return store.UserDeletionPreview{}, err
	}
	if counts.AuditMetadataUserReferences, err = s.countInt(ctx, `SELECT COUNT(*) FROM audit_logs WHERE COALESCE(json_extract(metadata_json, '$.user_id'), '') = ?`, userID); err != nil {
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

func (s *Store) TransferUserOwnership(ctx context.Context, sourceUserID string, targetUserID string) (store.UserOwnershipTransferResult, error) {
	sourceUserID = strings.TrimSpace(sourceUserID)
	targetUserID = strings.TrimSpace(targetUserID)
	if sourceUserID == "" || targetUserID == "" || sourceUserID == targetUserID {
		return store.UserOwnershipTransferResult{}, errConflict
	}
	if _, err := s.GetUser(ctx, sourceUserID); err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	if _, err := s.GetUser(ctx, targetUserID); err != nil {
		return store.UserOwnershipTransferResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result := store.UserOwnershipTransferResult{
		SourceUserID: sourceUserID,
		TargetUserID: targetUserID,
	}

	conversationsRes, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET metadata_json = json_set(COALESCE(metadata_json, '{}'), '$.owner_id', ?)
		WHERE COALESCE(json_extract(metadata_json, '$.owner_id'), '') = ?
	`, targetUserID, sourceUserID)
	if err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	if rows, err := conversationsRes.RowsAffected(); err == nil {
		result.TransferredConversations = int(rows)
	} else {
		return store.UserOwnershipTransferResult{}, err
	}

	imagesRes, err := tx.ExecContext(ctx, `
		UPDATE uploaded_images
		SET owner_id = ?
		WHERE owner_id = ?
	`, targetUserID, sourceUserID)
	if err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	if rows, err := imagesRes.RowsAffected(); err == nil {
		result.TransferredUploadedImages = int(rows)
	} else {
		return store.UserOwnershipTransferResult{}, err
	}

	failuresRes, err := tx.ExecContext(ctx, `
		UPDATE storage_file_deletion_failures
		SET owner_id = ?
		WHERE owner_id = ?
	`, targetUserID, sourceUserID)
	if err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	if rows, err := failuresRes.RowsAffected(); err == nil {
		result.TransferredDeletionFailures = int(rows)
	} else {
		return store.UserOwnershipTransferResult{}, err
	}

	var sourceQuota int64
	sourceQuotaErr := tx.QueryRowContext(ctx, `
		SELECT quota_bytes
		FROM storage_user_quotas
		WHERE owner_id = ?
	`, sourceUserID).Scan(&sourceQuota)
	if sourceQuotaErr != nil && !errors.Is(sourceQuotaErr, sql.ErrNoRows) {
		return store.UserOwnershipTransferResult{}, sourceQuotaErr
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
				return store.UserOwnershipTransferResult{}, err
			}
			result.TargetQuotaCreatedFromSource = true
		case targetQuotaErr == nil:
			result.TargetQuotaPreserved = true
		default:
			return store.UserOwnershipTransferResult{}, targetQuotaErr
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = ?`, sourceUserID); err != nil {
			return store.UserOwnershipTransferResult{}, err
		}
		result.SourceQuotaDeleted = true
	}

	if err := tx.Commit(); err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	return result, nil
}

func (s *Store) TransferUserOwnershipSelection(ctx context.Context, sourceUserID string, targetUserID string, conversationIDs []string, filenames []string) (store.UserOwnershipTransferResult, error) {
	sourceUserID = strings.TrimSpace(sourceUserID)
	targetUserID = strings.TrimSpace(targetUserID)
	conversationIDs = uniqueNonEmptyStrings(conversationIDs)
	filenames = uniqueNonEmptyStrings(filenames)
	if sourceUserID == "" || targetUserID == "" || sourceUserID == targetUserID || (len(conversationIDs) == 0 && len(filenames) == 0) {
		return store.UserOwnershipTransferResult{}, errConflict
	}
	if _, err := s.GetUser(ctx, sourceUserID); err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	if _, err := s.GetUser(ctx, targetUserID); err != nil {
		return store.UserOwnershipTransferResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	result := store.UserOwnershipTransferResult{
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
			return store.UserOwnershipTransferResult{}, err
		}
		if rows, err := res.RowsAffected(); err == nil {
			result.TransferredConversations = int(rows)
		} else {
			return store.UserOwnershipTransferResult{}, err
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
			return store.UserOwnershipTransferResult{}, err
		}
		if rows, err := res.RowsAffected(); err == nil {
			result.TransferredUploadedImages = int(rows)
		} else {
			return store.UserOwnershipTransferResult{}, err
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
			return store.UserOwnershipTransferResult{}, err
		}
		if rows, err := failureRes.RowsAffected(); err == nil {
			result.TransferredDeletionFailures = int(rows)
		} else {
			return store.UserOwnershipTransferResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return store.UserOwnershipTransferResult{}, err
	}
	return result, nil
}

func (s *Store) UpsertUserIdentity(ctx context.Context, input store.UpsertUserIdentityInput) (store.UserIdentity, error) {
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
		return store.UserIdentity{}, err
	}
	return s.GetUserIdentity(ctx, strings.TrimSpace(input.Provider), strings.TrimSpace(input.Subject))
}

func (s *Store) GetUserIdentity(ctx context.Context, provider string, subject string) (store.UserIdentity, error) {
	item, err := scanUserIdentity(s.db.QueryRowContext(ctx, `
		SELECT id, user_id, provider, subject, email, email_verified, profile_json, created_at, updated_at, last_login_at
		FROM user_identities
		WHERE provider = ? AND subject = ?
	`, strings.TrimSpace(provider), strings.TrimSpace(subject)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.UserIdentity{}, errNotFound
		}
		return store.UserIdentity{}, err
	}
	return item, nil
}

func (s *Store) ListUserIdentities(ctx context.Context, userID string) ([]store.UserIdentity, error) {
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
