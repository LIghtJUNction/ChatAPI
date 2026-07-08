package postgresql

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) CreateUploadedImage(ctx context.Context, input common.CreateUploadedImageInput) (common.UploadedImage, error) {
	createdAt := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO uploaded_images(
			id, owner_id, filename, original_filename, content_type, bytes, url, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		strings.TrimSpace(input.ID),
		strings.TrimSpace(input.OwnerID),
		strings.TrimSpace(input.Filename),
		strings.TrimSpace(input.OriginalFilename),
		strings.TrimSpace(input.ContentType),
		input.Bytes,
		strings.TrimSpace(input.URL),
		createdAt,
	)
	if err != nil {
		return common.UploadedImage{}, err
	}
	return common.UploadedImage{
		ID:               strings.TrimSpace(input.ID),
		OwnerID:          strings.TrimSpace(input.OwnerID),
		Filename:         strings.TrimSpace(input.Filename),
		OriginalFilename: strings.TrimSpace(input.OriginalFilename),
		ContentType:      strings.TrimSpace(input.ContentType),
		Bytes:            input.Bytes,
		URL:              strings.TrimSpace(input.URL),
		CreatedAt:        createdAt,
	}, nil
}

func (s *Store) ListUploadedImagesByOwner(ctx context.Context, ownerID string) ([]common.UploadedImage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, filename, original_filename, content_type, bytes, url, created_at
		FROM uploaded_images
		WHERE owner_id = $1
		ORDER BY created_at DESC, id DESC
	`, strings.TrimSpace(ownerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.UploadedImage, 0)
	for rows.Next() {
		item, err := scanUploadedImage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListUploadedImages(ctx context.Context) ([]common.UploadedImage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, filename, original_filename, content_type, bytes, url, created_at
		FROM uploaded_images
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.UploadedImage, 0)
	for rows.Next() {
		item, err := scanUploadedImage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteUploadedImagesByFilenames(ctx context.Context, filenames []string) (common.DeleteUploadedImagesResult, error) {
	filenames = uniqueNonEmptyStrings(filenames)
	if len(filenames) == 0 {
		return common.DeleteUploadedImagesResult{}, nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM uploaded_images WHERE filename = ANY($1)`, filenames)
	if err != nil {
		return common.DeleteUploadedImagesResult{}, err
	}
	return common.DeleteUploadedImagesResult{DeletedImages: int(tag.RowsAffected())}, nil
}

func (s *Store) ListMediaAssets(ctx context.Context) ([]common.MediaAsset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, owner_id, file_id, path, media_type, bytes, sha256, width, height, source_kind, original_name, original_media_type, created_at
		FROM media_assets
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListOrphanMediaAssets(ctx context.Context) ([]common.MediaAsset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.id, a.owner_id, a.file_id, a.path, a.media_type, a.bytes, a.sha256, a.width, a.height, a.source_kind, a.original_name, a.original_media_type, a.created_at
		FROM media_assets a
		LEFT JOIN media_asset_refs r ON r.asset_id = a.id
		WHERE r.id IS NULL
		ORDER BY a.created_at ASC, a.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteMediaAssetsByIDs(ctx context.Context, ids []string) (int, error) {
	ids = uniqueNonEmptyStrings(ids)
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM media_assets WHERE id = ANY($1)`, ids)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) UpsertStorageFileDeletionFailure(ctx context.Context, input common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO storage_file_deletion_failures(
			path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, 1, $6, $7)
		ON CONFLICT(path) DO UPDATE SET
			filename = excluded.filename,
			owner_id = excluded.owner_id,
			bytes = excluded.bytes,
			last_error = excluded.last_error,
			attempts = storage_file_deletion_failures.attempts + 1,
			updated_at = excluded.updated_at
	`,
		strings.TrimSpace(input.Path),
		strings.TrimSpace(input.Filename),
		strings.TrimSpace(input.OwnerID),
		input.Bytes,
		strings.TrimSpace(input.LastError),
		now,
		now,
	)
	if err != nil {
		return common.StorageFileDeletionFailure{}, err
	}
	return s.getStorageFileDeletionFailure(ctx, input.Path)
}

func (s *Store) ListStorageFileDeletionFailures(ctx context.Context, limit int) ([]common.StorageFileDeletionFailure, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.pool.Query(ctx, `
		SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		FROM storage_file_deletion_failures
		ORDER BY updated_at ASC, path ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.StorageFileDeletionFailure, 0)
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
	_, err := s.pool.Exec(ctx, `DELETE FROM storage_file_deletion_failures WHERE path = ANY($1)`, paths)
	return err
}

func (s *Store) ListStorageUserQuotas(ctx context.Context) ([]common.StorageUserQuota, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT owner_id, quota_bytes, created_at, updated_at
		FROM storage_user_quotas
		ORDER BY owner_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]common.StorageUserQuota, 0)
	for rows.Next() {
		item, err := scanStorageUserQuota(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetStorageUserQuota(ctx context.Context, ownerID string) (common.StorageUserQuota, error) {
	return scanStorageUserQuota(s.pool.QueryRow(ctx, `
		SELECT owner_id, quota_bytes, created_at, updated_at
		FROM storage_user_quotas
		WHERE owner_id = $1
	`, strings.TrimSpace(ownerID)))
}

func (s *Store) SetStorageUserQuota(ctx context.Context, ownerID string, quotaBytes int64) (common.StorageUserQuota, error) {
	now := time.Now().UTC()
	ownerID = strings.TrimSpace(ownerID)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO storage_user_quotas(owner_id, quota_bytes, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT(owner_id) DO UPDATE SET
			quota_bytes = excluded.quota_bytes,
			updated_at = excluded.updated_at
	`, ownerID, quotaBytes, now, now)
	if err != nil {
		return common.StorageUserQuota{}, err
	}
	return s.GetStorageUserQuota(ctx, ownerID)
}

func (s *Store) DeleteStorageUserQuota(ctx context.Context, ownerID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = $1`, strings.TrimSpace(ownerID))
	return err
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := common.UserOwnershipTransferResult{
		SourceUserID: sourceUserID,
		TargetUserID: targetUserID,
	}

	conversationsTag, err := tx.Exec(ctx, `
		UPDATE conversations
		SET metadata_json = jsonb_set(COALESCE(metadata_json, '{}'::jsonb), '{owner_id}', to_jsonb($1::text), true)
		WHERE COALESCE(metadata_json->>'owner_id', '') = $2
	`, targetUserID, sourceUserID)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	result.TransferredConversations = int(conversationsTag.RowsAffected())

	imagesTag, err := tx.Exec(ctx, `
		UPDATE uploaded_images
		SET owner_id = $1
		WHERE owner_id = $2
	`, targetUserID, sourceUserID)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	result.TransferredUploadedImages = int(imagesTag.RowsAffected())

	failuresTag, err := tx.Exec(ctx, `
		UPDATE storage_file_deletion_failures
		SET owner_id = $1
		WHERE owner_id = $2
	`, targetUserID, sourceUserID)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	result.TransferredDeletionFailures = int(failuresTag.RowsAffected())

	var sourceQuota int64
	sourceQuotaErr := tx.QueryRow(ctx, `
		SELECT quota_bytes
		FROM storage_user_quotas
		WHERE owner_id = $1
	`, sourceUserID).Scan(&sourceQuota)
	if sourceQuotaErr != nil && !errors.Is(sourceQuotaErr, pgx.ErrNoRows) {
		return common.UserOwnershipTransferResult{}, sourceQuotaErr
	}
	if sourceQuotaErr == nil {
		var targetQuota int64
		targetQuotaErr := tx.QueryRow(ctx, `
			SELECT quota_bytes
			FROM storage_user_quotas
			WHERE owner_id = $1
		`, targetUserID).Scan(&targetQuota)
		switch {
		case errors.Is(targetQuotaErr, pgx.ErrNoRows):
			if _, err := tx.Exec(ctx, `
				INSERT INTO storage_user_quotas(owner_id, quota_bytes, created_at, updated_at)
				SELECT $1, quota_bytes, created_at, $2
				FROM storage_user_quotas
				WHERE owner_id = $3
			`, targetUserID, time.Now().UTC(), sourceUserID); err != nil {
				return common.UserOwnershipTransferResult{}, err
			}
			result.TargetQuotaCreatedFromSource = true
		case targetQuotaErr == nil:
			result.TargetQuotaPreserved = true
		default:
			return common.UserOwnershipTransferResult{}, targetQuotaErr
		}
		if _, err := tx.Exec(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = $1`, sourceUserID); err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		result.SourceQuotaDeleted = true
	}

	if err := tx.Commit(ctx); err != nil {
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

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result := common.UserOwnershipTransferResult{
		SourceUserID: sourceUserID,
		TargetUserID: targetUserID,
	}

	if len(conversationIDs) > 0 {
		tag, err := tx.Exec(ctx, `
			UPDATE conversations
			SET metadata_json = jsonb_set(COALESCE(metadata_json, '{}'::jsonb), '{owner_id}', to_jsonb($1::text), true)
			WHERE COALESCE(metadata_json->>'owner_id', '') = $2
				AND id = ANY($3)
		`, targetUserID, sourceUserID, conversationIDs)
		if err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		result.TransferredConversations = int(tag.RowsAffected())
	}

	if len(filenames) > 0 {
		tag, err := tx.Exec(ctx, `
			UPDATE uploaded_images
			SET owner_id = $1
			WHERE owner_id = $2
				AND filename = ANY($3)
		`, targetUserID, sourceUserID, filenames)
		if err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		result.TransferredUploadedImages = int(tag.RowsAffected())

		failureTag, err := tx.Exec(ctx, `
			UPDATE storage_file_deletion_failures
			SET owner_id = $1
			WHERE owner_id = $2
				AND filename = ANY($3)
		`, targetUserID, sourceUserID, filenames)
		if err != nil {
			return common.UserOwnershipTransferResult{}, err
		}
		result.TransferredDeletionFailures = int(failureTag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return common.UserOwnershipTransferResult{}, err
	}
	return result, nil
}

func (s *Store) getStorageFileDeletionFailure(ctx context.Context, path string) (common.StorageFileDeletionFailure, error) {
	return scanStorageFileDeletionFailure(s.pool.QueryRow(ctx, `
		SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		FROM storage_file_deletion_failures
		WHERE path = $1
	`, strings.TrimSpace(path)))
}

func scanUploadedImage(row rowScanner) (common.UploadedImage, error) {
	var item common.UploadedImage
	if err := row.Scan(
		&item.ID,
		&item.OwnerID,
		&item.Filename,
		&item.OriginalFilename,
		&item.ContentType,
		&item.Bytes,
		&item.URL,
		&item.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.UploadedImage{}, common.ErrNotFound
		}
		return common.UploadedImage{}, err
	}
	return item, nil
}

func scanMediaAsset(row rowScanner) (common.MediaAsset, error) {
	var item common.MediaAsset
	if err := row.Scan(
		&item.ID,
		&item.OwnerID,
		&item.FileID,
		&item.Path,
		&item.MediaType,
		&item.Bytes,
		&item.SHA256,
		&item.Width,
		&item.Height,
		&item.SourceKind,
		&item.OriginalName,
		&item.OriginalMediaType,
		&item.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.MediaAsset{}, common.ErrNotFound
		}
		return common.MediaAsset{}, err
	}
	return item, nil
}

func scanStorageFileDeletionFailure(row rowScanner) (common.StorageFileDeletionFailure, error) {
	var item common.StorageFileDeletionFailure
	if err := row.Scan(
		&item.Path,
		&item.Filename,
		&item.OwnerID,
		&item.Bytes,
		&item.LastError,
		&item.Attempts,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.StorageFileDeletionFailure{}, common.ErrNotFound
		}
		return common.StorageFileDeletionFailure{}, err
	}
	return item, nil
}

func scanStorageUserQuota(row rowScanner) (common.StorageUserQuota, error) {
	var item common.StorageUserQuota
	if err := row.Scan(
		&item.OwnerID,
		&item.QuotaBytes,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return common.StorageUserQuota{}, common.ErrNotFound
		}
		return common.StorageUserQuota{}, err
	}
	return item, nil
}
