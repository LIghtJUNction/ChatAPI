package ops

import (
	"context"
	"os"
	"strings"

	"github.com/zyf/chatapi/internal/platform/media/localstore"
	"github.com/zyf/chatapi/internal/repository/common"
)

type Store interface {
	ListOrphanMediaAssets(context.Context) ([]common.MediaAsset, error)
	DeleteMediaAssetsByIDs(context.Context, []string) (int, error)
	UpsertStorageFileDeletionFailure(context.Context, common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error)
}

type Service struct {
	store   Store
	cleaner localstore.Store
}

type CleanupOrphanImagesResult struct {
	Scanned              int      `json:"scanned"`
	DeletedFiles         int      `json:"deleted_files"`
	DeletedAssetRecords  int      `json:"deleted_asset_records"`
	SkippedMissingFiles  int      `json:"skipped_missing_files"`
	DeletionFailurePaths []string `json:"deletion_failure_paths,omitempty"`
}

func New(store Store, cleaner localstore.Store) *Service {
	return &Service{store: store, cleaner: cleaner}
}

func (s *Service) CleanupOrphanImages(ctx context.Context) (CleanupOrphanImagesResult, error) {
	items, err := s.store.ListOrphanMediaAssets(ctx)
	if err != nil {
		return CleanupOrphanImagesResult{}, err
	}
	result := CleanupOrphanImagesResult{Scanned: len(items)}
	deleteIDs := make([]string, 0, len(items))
	for _, item := range items {
		err := s.cleaner.DeletePreparedImage(ctx, item.Path)
		switch {
		case err == nil:
			if _, statErr := os.Stat(strings.TrimSpace(item.Path)); os.IsNotExist(statErr) {
				result.DeletedFiles++
			}
			deleteIDs = append(deleteIDs, item.ID)
		case os.IsNotExist(err):
			result.SkippedMissingFiles++
			deleteIDs = append(deleteIDs, item.ID)
		default:
			result.DeletionFailurePaths = append(result.DeletionFailurePaths, item.Path)
			_, _ = s.store.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
				Path:      item.Path,
				Filename:  item.FileID + ".avif",
				OwnerID:   item.OwnerID,
				Bytes:     item.Bytes,
				LastError: err.Error(),
			})
		}
	}
	deleted, err := s.store.DeleteMediaAssetsByIDs(ctx, deleteIDs)
	if err != nil {
		return result, err
	}
	result.DeletedAssetRecords = deleted
	return result, nil
}
