package postgresql

import (
	"context"
	"strings"
	"time"

	"github.com/zyf/chatapi/internal/store"
)

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
