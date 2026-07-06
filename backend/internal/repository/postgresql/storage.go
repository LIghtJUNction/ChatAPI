package postgresql

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/zyf/chatapi/internal/store"
)

func (s *Store) CreateUploadedImage(ctx context.Context, input store.CreateUploadedImageInput) (store.UploadedImage, error) {
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
		return store.UploadedImage{}, err
	}
	return store.UploadedImage{
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

func (s *Store) ListUploadedImagesByOwner(ctx context.Context, ownerID string) ([]store.UploadedImage, error) {
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
	rows, err := s.pool.Query(ctx, `
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
	tag, err := s.pool.Exec(ctx, `DELETE FROM uploaded_images WHERE filename = ANY($1)`, filenames)
	if err != nil {
		return store.DeleteUploadedImagesResult{}, err
	}
	return store.DeleteUploadedImagesResult{DeletedImages: int(tag.RowsAffected())}, nil
}

func (s *Store) UpsertStorageFileDeletionFailure(ctx context.Context, input store.UpsertStorageFileDeletionFailureInput) (store.StorageFileDeletionFailure, error) {
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
	_, err := s.pool.Exec(ctx, `DELETE FROM storage_file_deletion_failures WHERE path = ANY($1)`, paths)
	return err
}

func (s *Store) ListStorageUserQuotas(ctx context.Context) ([]store.StorageUserQuota, error) {
	rows, err := s.pool.Query(ctx, `
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
	return scanStorageUserQuota(s.pool.QueryRow(ctx, `
		SELECT owner_id, quota_bytes, created_at, updated_at
		FROM storage_user_quotas
		WHERE owner_id = $1
	`, strings.TrimSpace(ownerID)))
}

func (s *Store) SetStorageUserQuota(ctx context.Context, ownerID string, quotaBytes int64) (store.StorageUserQuota, error) {
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
		return store.StorageUserQuota{}, err
	}
	return s.GetStorageUserQuota(ctx, ownerID)
}

func (s *Store) DeleteStorageUserQuota(ctx context.Context, ownerID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM storage_user_quotas WHERE owner_id = $1`, strings.TrimSpace(ownerID))
	return err
}

func (s *Store) getStorageFileDeletionFailure(ctx context.Context, path string) (store.StorageFileDeletionFailure, error) {
	return scanStorageFileDeletionFailure(s.pool.QueryRow(ctx, `
		SELECT path, filename, owner_id, bytes, last_error, attempts, created_at, updated_at
		FROM storage_file_deletion_failures
		WHERE path = $1
	`, strings.TrimSpace(path)))
}

func scanUploadedImage(row rowScanner) (store.UploadedImage, error) {
	var item store.UploadedImage
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
			return store.UploadedImage{}, store.ErrNotFound
		}
		return store.UploadedImage{}, err
	}
	return item, nil
}

func scanStorageFileDeletionFailure(row rowScanner) (store.StorageFileDeletionFailure, error) {
	var item store.StorageFileDeletionFailure
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
			return store.StorageFileDeletionFailure{}, store.ErrNotFound
		}
		return store.StorageFileDeletionFailure{}, err
	}
	return item, nil
}

func scanStorageUserQuota(row rowScanner) (store.StorageUserQuota, error) {
	var item store.StorageUserQuota
	if err := row.Scan(
		&item.OwnerID,
		&item.QuotaBytes,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.StorageUserQuota{}, store.ErrNotFound
		}
		return store.StorageUserQuota{}, err
	}
	return item, nil
}
