package outputasset

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	preprocesssettings "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess/settings"
)

var ErrAssetNotFound = errors.New("output image asset not found")

type Store interface {
	CreateStagedMediaAsset(context.Context, common.CreateStagedMediaAssetInput) (common.MediaAsset, error)
	GetStagedMediaAsset(context.Context, string, string, string, string) (common.MediaAsset, error)
	UpsertStorageFileDeletionFailure(context.Context, common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error)
}

type BinaryStore interface {
	PersistDraft(context.Context, media.DraftAsset) (media.StoredAsset, error)
	OpenPreparedImage(context.Context, string) (io.ReadCloser, error)
	DeletePreparedImage(context.Context, string) error
}

type Service struct {
	cfg       config.Config
	store     Store
	binaries  BinaryStore
	processor media.Processor
	locksMu   sync.Mutex
	locks     map[string]*consumeLock
	Settings  *preprocesssettings.Service
}

type consumeLock struct {
	mu   sync.Mutex
	refs int
}

type Uploaded struct {
	AssetID        string `json:"asset_id"`
	FileID         string `json:"file_id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id"`
	URL            string `json:"url"`
	MediaType      string `json:"media_type"`
	Bytes          int64  `json:"bytes"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
}

type Resolved struct {
	Asset  common.MediaAsset
	URL    string
	Base64 string
}

func New(cfg config.Config, store Store, binaries BinaryStore, processor media.Processor) *Service {
	if processor == nil {
		panic("output asset image processor is required")
	}
	return &Service{cfg: cfg, store: store, binaries: binaries, processor: processor, locks: make(map[string]*consumeLock)}
}

func (s *Service) Upload(ctx context.Context, ownerID string, conversationID string, requestID string, originalName string, mediaType string, reader io.Reader) (Uploaded, error) {
	if s == nil || s.store == nil || s.binaries == nil {
		return Uploaded{}, errors.New("output image storage is unavailable")
	}
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return Uploaded{}, errors.New("conversation id is required")
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return Uploaded{}, errors.New("request id is required")
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return Uploaded{}, errors.New("owner id is required")
	}
	cfg := s.current(ctx)
	limit := cfg.MediaMaxBytes
	if limit <= 0 {
		limit = cfg.UploadMaxBytes
	}
	if limit <= 0 {
		limit = 10 << 20
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return Uploaded{}, err
	}
	if int64(len(data)) > limit {
		return Uploaded{}, media.ErrImageTooLarge
	}
	parsed, err := media.ParseImageBytes(data, mediaType, cfg.MediaMaxBytes)
	if err != nil {
		return Uploaded{}, err
	}
	processed, err := s.processor.EncodeAVIF(ctx, parsed, media.AVIFOptions{
		Quality: cfg.MediaAVIFQuality,
	})
	if err != nil {
		return Uploaded{}, err
	}
	mediaLimit := cfg.MediaMaxBytes
	if mediaLimit <= 0 {
		mediaLimit = 10 << 20
	}
	if int64(len(processed.Bytes)) > mediaLimit {
		return Uploaded{}, media.ErrImageTooLarge
	}
	fileID := "file_" + uuid.NewString()
	draft := media.DraftAsset{
		FileID: fileID, OwnerID: ownerID, MediaType: processed.MediaType,
		PublicURL: media.ChatAssetPublicURL(fileID), Bytes: int64(len(processed.Bytes)),
		SHA256: media.SHA256Hex(processed.Bytes), Width: processed.Width, Height: processed.Height,
		SourceKind: "output_upload", OriginalName: strings.TrimSpace(originalName),
		OriginalMediaType: parsed.DetectedMediaType, Data: processed.Bytes,
	}
	stored, err := s.binaries.PersistDraft(ctx, draft)
	if err != nil {
		return Uploaded{}, err
	}
	asset, err := s.store.CreateStagedMediaAsset(ctx, common.CreateStagedMediaAssetInput{
		Asset: common.CreateMediaAssetInput{
			ID: "asset_" + uuid.NewString(), OwnerID: ownerID, FileID: stored.FileID,
			Path: stored.Path, MediaType: stored.MediaType, Bytes: stored.Bytes,
			SHA256: draft.SHA256, Width: draft.Width, Height: draft.Height,
			SourceKind: draft.SourceKind, OriginalName: draft.OriginalName,
			OriginalMediaType: draft.OriginalMediaType, CreatedAt: time.Now().UTC(),
		},
		ConversationID: conversationID,
		RequestID:      requestID,
	})
	if err != nil {
		if cleanupErr := s.binaries.DeletePreparedImage(ctx, stored.Path); cleanupErr != nil {
			_, _ = s.store.UpsertStorageFileDeletionFailure(ctx, common.UpsertStorageFileDeletionFailureInput{
				Path: stored.Path, Filename: media.ChatAssetFilename(stored.FileID), OwnerID: ownerID,
				Bytes: stored.Bytes, LastError: cleanupErr.Error(),
			})
		}
		if errors.Is(err, common.ErrNotFound) {
			return Uploaded{}, ErrAssetNotFound
		}
		return Uploaded{}, err
	}
	return Uploaded{
		AssetID: asset.ID, FileID: asset.FileID, ConversationID: conversationID, RequestID: requestID,
		URL:       media.ChatAssetPublicURL(asset.FileID),
		MediaType: asset.MediaType, Bytes: asset.Bytes, Width: asset.Width, Height: asset.Height,
	}, nil
}

func (s *Service) Consume(ctx context.Context, ownerID string, conversationID string, requestID string, assetID string, persist func(Resolved) error) (Resolved, error) {
	if s == nil {
		return Resolved{}, ErrAssetNotFound
	}
	if persist == nil {
		return Resolved{}, errors.New("output image consumer is required")
	}
	lock := s.acquireConsumeLock(strings.TrimSpace(assetID))
	lock.mu.Lock()
	defer func() {
		lock.mu.Unlock()
		s.releaseConsumeLock(strings.TrimSpace(assetID), lock)
	}()
	resolved, err := s.resolve(ctx, ownerID, conversationID, requestID, assetID)
	if err != nil {
		return Resolved{}, err
	}
	if err := persist(resolved); err != nil {
		return Resolved{}, err
	}
	return resolved, nil
}

func (s *Service) resolve(ctx context.Context, ownerID string, conversationID string, requestID string, assetID string) (Resolved, error) {
	if s == nil || s.store == nil || s.binaries == nil {
		return Resolved{}, ErrAssetNotFound
	}
	asset, err := s.store.GetStagedMediaAsset(ctx, strings.TrimSpace(assetID), strings.TrimSpace(ownerID), strings.TrimSpace(conversationID), strings.TrimSpace(requestID))
	if errors.Is(err, common.ErrNotFound) {
		return Resolved{}, ErrAssetNotFound
	}
	if err != nil {
		return Resolved{}, err
	}
	if strings.TrimSpace(asset.OwnerID) != strings.TrimSpace(ownerID) {
		return Resolved{}, ErrAssetNotFound
	}
	if asset.MediaType != "image/avif" {
		return Resolved{}, fmt.Errorf("output image must be image/avif")
	}
	file, err := s.binaries.OpenPreparedImage(ctx, asset.Path)
	if err != nil {
		return Resolved{}, err
	}
	defer file.Close()
	limit := s.current(ctx).MediaMaxBytes
	if limit <= 0 {
		limit = 10 << 20
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return Resolved{}, err
	}
	if int64(len(data)) > limit {
		return Resolved{}, media.ErrImageTooLarge
	}
	return Resolved{
		Asset:  asset,
		URL:    media.ChatAssetPublicURL(asset.FileID),
		Base64: base64.StdEncoding.EncodeToString(data),
	}, nil
}

func (s *Service) current(ctx context.Context) config.Config {
	cfg := s.cfg
	if s.Settings == nil {
		return cfg
	}
	value, err := s.Settings.Current(ctx)
	if err != nil {
		return cfg
	}
	cfg.MediaProcessEnabled = value.Enabled
	cfg.MediaAllowRemoteURL = value.AllowRemoteURL
	cfg.MediaAllowDataURL = value.AllowDataURL
	cfg.MediaAllowBase64 = value.AllowBase64
	cfg.MediaAllowSVG = value.AllowSVG
	cfg.MediaMaxBytes = value.MaxBytes
	cfg.MediaMaxImagesPerRequest = value.MaxImages
	cfg.MediaAVIFQuality = value.AVIFQuality
	return cfg
}

func (s *Service) acquireConsumeLock(assetID string) *consumeLock {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if s.locks == nil {
		s.locks = make(map[string]*consumeLock)
	}
	lock := s.locks[assetID]
	if lock == nil {
		lock = &consumeLock{}
		s.locks[assetID] = lock
	}
	lock.refs++
	return lock
}

func (s *Service) releaseConsumeLock(assetID string, lock *consumeLock) {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	lock.refs--
	if lock.refs == 0 && s.locks[assetID] == lock {
		delete(s.locks, assetID)
	}
}
