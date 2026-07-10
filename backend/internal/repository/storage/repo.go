package storage

import (
	"context"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Store interface {
	CreateUploadedImage(context.Context, common.CreateUploadedImageInput) (common.UploadedImage, error)
	ListUploadedImages(context.Context) ([]common.UploadedImage, error)
	ListUploadedImagesByOwner(context.Context, string) ([]common.UploadedImage, error)
	DeleteUploadedImagesByFilenames(context.Context, []string) (common.DeleteUploadedImagesResult, error)
	ListMediaAssets(context.Context) ([]common.MediaAsset, error)
	CreateMediaAsset(context.Context, common.CreateMediaAssetInput) (common.MediaAsset, error)
	CreateStagedMediaAsset(context.Context, common.CreateStagedMediaAssetInput) (common.MediaAsset, error)
	GetMediaAssetByID(context.Context, string) (common.MediaAsset, error)
	GetStagedMediaAsset(context.Context, string, string, string, string) (common.MediaAsset, error)
	GetMediaAssetByFileID(context.Context, string) (common.MediaAsset, error)
	ListOrphanMediaAssets(context.Context) ([]common.MediaAsset, error)
	DeleteMediaAssetsByIDs(context.Context, []string) (int, error)
	UpsertStorageFileDeletionFailure(context.Context, common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error)
	ListStorageFileDeletionFailures(context.Context, int) ([]common.StorageFileDeletionFailure, error)
	DeleteStorageFileDeletionFailures(context.Context, []string) error
	ListStorageUserQuotas(context.Context) ([]common.StorageUserQuota, error)
	GetStorageUserQuota(context.Context, string) (common.StorageUserQuota, error)
	SetStorageUserQuota(context.Context, string, int64) (common.StorageUserQuota, error)
	DeleteStorageUserQuota(context.Context, string) error
}
