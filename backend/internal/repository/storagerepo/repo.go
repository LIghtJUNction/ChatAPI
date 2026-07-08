package storagerepo

import (
	"context"

	"github.com/zyf/chatapi/internal/store"
)

type Store interface {
	CreateUploadedImage(context.Context, store.CreateUploadedImageInput) (store.UploadedImage, error)
	ListUploadedImages(context.Context) ([]store.UploadedImage, error)
	ListUploadedImagesByOwner(context.Context, string) ([]store.UploadedImage, error)
	DeleteUploadedImagesByFilenames(context.Context, []string) (store.DeleteUploadedImagesResult, error)
	ListMediaAssets(context.Context) ([]store.MediaAsset, error)
	ListOrphanMediaAssets(context.Context) ([]store.MediaAsset, error)
	DeleteMediaAssetsByIDs(context.Context, []string) (int, error)
	UpsertStorageFileDeletionFailure(context.Context, store.UpsertStorageFileDeletionFailureInput) (store.StorageFileDeletionFailure, error)
	ListStorageFileDeletionFailures(context.Context, int) ([]store.StorageFileDeletionFailure, error)
	DeleteStorageFileDeletionFailures(context.Context, []string) error
	ListStorageUserQuotas(context.Context) ([]store.StorageUserQuota, error)
	GetStorageUserQuota(context.Context, string) (store.StorageUserQuota, error)
	SetStorageUserQuota(context.Context, string, int64) (store.StorageUserQuota, error)
	DeleteStorageUserQuota(context.Context, string) error
}
