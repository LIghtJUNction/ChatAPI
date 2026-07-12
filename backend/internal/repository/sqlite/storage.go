package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func (s *Store) CreateUploadedImage(ctx context.Context, input common.CreateUploadedImageInput) (common.UploadedImage, error) {
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
		return common.UploadedImage{}, err
	}
	return common.UploadedImage{
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

func (s *Store) ListUploadedImagesByOwner(ctx context.Context, ownerID string) ([]common.UploadedImage, error) {
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
	rows, err := s.db.QueryContext(ctx, `
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
	placeholders := strings.TrimRight(strings.Repeat("?,", len(filenames)), ",")
	args := make([]any, 0, len(filenames))
	for _, filename := range filenames {
		args = append(args, filename)
	}
	query := fmt.Sprintf(`DELETE FROM uploaded_images WHERE filename IN (%s)`, placeholders)
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return common.DeleteUploadedImagesResult{}, err
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return common.DeleteUploadedImagesResult{}, err
	}
	return common.DeleteUploadedImagesResult{DeletedImages: int(rowsAffected)}, nil
}

func (s *Store) UpsertStorageFileDeletionFailure(ctx context.Context, input common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error) {
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
	placeholders := strings.TrimRight(strings.Repeat("?,", len(paths)), ",")
	args := make([]any, 0, len(paths))
	for _, path := range paths {
		args = append(args, path)
	}
	query := fmt.Sprintf(`DELETE FROM storage_file_deletion_failures WHERE path IN (%s)`, placeholders)
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) getStorageFileDeletionFailure(ctx context.Context, path string) (common.StorageFileDeletionFailure, error) {
	item, err := scanStorageFileDeletionFailure(s.db.QueryRowContext(ctx, `
		SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		FROM storage_file_deletion_failures
		WHERE path = ?
	`, path))
	if err != nil {
		return common.StorageFileDeletionFailure{}, err
	}
	return item, nil
}

func (s *Store) ListStorageUserQuotas(ctx context.Context) ([]common.StorageUserQuota, error) {
	rows, err := s.db.QueryContext(ctx, `
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
	row := s.db.QueryRowContext(ctx, `
		SELECT owner_id, quota_bytes, created_at, updated_at
		FROM storage_user_quotas
		WHERE owner_id = ?
	`, ownerID)
	item, err := scanStorageUserQuota(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return common.StorageUserQuota{}, errNotFound
		}
		return common.StorageUserQuota{}, err
	}
	return item, nil
}

func (s *Store) SetStorageUserQuota(ctx context.Context, ownerID string, quotaBytes int64) (common.StorageUserQuota, error) {
	now := formatTime(time.Now().UTC())
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO storage_user_quotas(owner_id, quota_bytes, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(owner_id) DO UPDATE SET
			quota_bytes = excluded.quota_bytes,
			updated_at = excluded.updated_at
	`, ownerID, quotaBytes, now, now); err != nil {
		return common.StorageUserQuota{}, err
	}
	return s.GetStorageUserQuota(ctx, ownerID)
}

func (s *Store) DeleteStorageUserQuota(ctx context.Context, ownerID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = ?`, ownerID)
	return err
}
