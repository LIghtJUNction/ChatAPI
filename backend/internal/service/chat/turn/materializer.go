package turn

import (
	"context"
	"fmt"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
)

type PreparedImageCleaner interface {
	DeletePreparedImage(context.Context, string) error
}

type Preprocessor interface {
	Prepare(context.Context, string, protocol.TurnRequest) (preprocesssvc.PreparedRequest, error)
}

type AssetPersister interface {
	PersistDraft(context.Context, media.DraftAsset) (media.StoredAsset, error)
}

type StorageDeletionFailureRecorder interface {
	UpsertStorageFileDeletionFailure(context.Context, common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error)
}

type MaterializedRequest struct {
	Request        protocol.TurnRequest
	RequestBody    map[string]any
	PreparedImages []media.DraftAsset
}

type RequestMaterializer struct {
	Preprocessor       Preprocessor
	AssetPersister     AssetPersister
	DeletionFailures   StorageDeletionFailureRecorder
	PreparedImageClean PreparedImageCleaner
}

func (m *RequestMaterializer) Materialize(ctx context.Context, ownerID string, request protocol.TurnRequest) (MaterializedRequest, error) {
	prepared, err := m.prepareRequest(ctx, ownerID, request)
	if err != nil {
		return MaterializedRequest{}, err
	}
	storedImages, err := m.persistPreparedImages(ctx, prepared.PreparedImages)
	if err != nil {
		return MaterializedRequest{}, err
	}
	prepared.Request = rewritePreparedImageURLs(prepared.Request, storedImages)
	return MaterializedRequest{
		Request:        prepared.Request,
		RequestBody:    protocol.BuildRequestBody(prepared.Request),
		PreparedImages: storedImages,
	}, nil
}

func (m *RequestMaterializer) Cleanup(ctx context.Context, images []media.DraftAsset) {
	if m == nil || m.PreparedImageClean == nil {
		return
	}
	for _, image := range images {
		if strings.TrimSpace(image.Path) == "" {
			continue
		}
		if err := m.PreparedImageClean.DeletePreparedImage(ctx, image.Path); err != nil {
			m.recordDeletionFailure(ctx, image, err)
		}
	}
}

func (m *RequestMaterializer) prepareRequest(ctx context.Context, ownerID string, request protocol.TurnRequest) (preprocesssvc.PreparedRequest, error) {
	if m == nil || m.Preprocessor == nil {
		return preprocesssvc.PreparedRequest{Request: request}, nil
	}
	return m.Preprocessor.Prepare(ctx, ownerID, request)
}

func (m *RequestMaterializer) persistPreparedImages(ctx context.Context, drafts []media.DraftAsset) ([]media.DraftAsset, error) {
	if len(drafts) == 0 {
		return nil, nil
	}
	if m == nil || m.AssetPersister == nil {
		return nil, fmt.Errorf("media asset persister is not configured")
	}
	stored := make([]media.DraftAsset, 0, len(drafts))
	for _, draft := range drafts {
		asset, err := m.AssetPersister.PersistDraft(ctx, draft)
		if err != nil {
			m.Cleanup(ctx, stored)
			return nil, err
		}
		copy := draft
		copy.OwnerID = asset.OwnerID
		copy.Path = asset.Path
		copy.PublicURL = asset.PublicURL
		copy.MediaType = asset.MediaType
		copy.Bytes = asset.Bytes
		copy.Data = nil
		stored = append(stored, copy)
	}
	return stored, nil
}

func (m *RequestMaterializer) recordDeletionFailure(ctx context.Context, image media.DraftAsset, err error) {
	if m == nil || m.DeletionFailures == nil || err == nil {
		return
	}
	_, _ = m.DeletionFailures.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
		Path:      strings.TrimSpace(image.Path),
		Filename:  media.ChatAssetFilename(image.FileID),
		OwnerID:   strings.TrimSpace(image.OwnerID),
		Bytes:     image.Bytes,
		LastError: err.Error(),
	})
}

func rewritePreparedImageURLs(request protocol.TurnRequest, images []media.DraftAsset) protocol.TurnRequest {
	if len(images) == 0 || len(request.InputParts) == 0 {
		return request
	}
	rewritten := request
	out := make([]protocol.InputPart, 0, len(request.InputParts))
	imagesByIndex := make(map[int]media.DraftAsset, len(images))
	for _, image := range images {
		imagesByIndex[image.InputPartIndex] = image
	}
	for index, part := range request.InputParts {
		if image, ok := imagesByIndex[index]; ok {
			part.URL = image.PublicURL
			part.MediaType = image.MediaType
		}
		out = append(out, part)
	}
	rewritten.InputParts = out
	return rewritten
}
