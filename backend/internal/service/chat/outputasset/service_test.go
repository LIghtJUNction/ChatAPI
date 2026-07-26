package outputasset

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"sync"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type memoryStore struct {
	mu             sync.Mutex
	asset          common.MediaAsset
	conversationID string
	requestID      string
	consumed       bool
}

func (s *memoryStore) CreateStagedMediaAsset(_ context.Context, staged common.CreateStagedMediaAssetInput) (common.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	input := staged.Asset
	s.asset = common.MediaAsset{
		ID: input.ID, OwnerID: input.OwnerID, FileID: input.FileID, Path: input.Path,
		MediaType: input.MediaType, Bytes: input.Bytes, SHA256: input.SHA256,
		Width: input.Width, Height: input.Height, SourceKind: input.SourceKind,
		OriginalName: input.OriginalName, OriginalMediaType: input.OriginalMediaType,
		CreatedAt: input.CreatedAt,
	}
	s.conversationID = staged.ConversationID
	s.requestID = staged.RequestID
	s.consumed = false
	return s.asset, nil
}

func (s *memoryStore) GetStagedMediaAsset(_ context.Context, id string, ownerID string, conversationID string, requestID string) (common.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumed || s.asset.ID != id || s.asset.OwnerID != ownerID || s.conversationID != conversationID || s.requestID != requestID {
		return common.MediaAsset{}, common.ErrNotFound
	}
	return s.asset, nil
}

func (s *memoryStore) markConsumed() {
	s.mu.Lock()
	s.consumed = true
	s.mu.Unlock()
}

func (s *memoryStore) GetMediaAssetByID(_ context.Context, id string) (common.MediaAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.asset.ID != id {
		return common.MediaAsset{}, common.ErrNotFound
	}
	return s.asset, nil
}

type countingBinaryStore struct {
	local localstore.Store
	mu    sync.Mutex
	opens int
}

func (s *countingBinaryStore) PersistDraft(ctx context.Context, draft media.DraftAsset) (media.StoredAsset, error) {
	return s.local.PersistDraft(ctx, draft)
}

func (s *countingBinaryStore) OpenPreparedImage(ctx context.Context, path string) (io.ReadCloser, error) {
	s.mu.Lock()
	s.opens++
	s.mu.Unlock()
	return s.local.OpenPreparedImage(ctx, path)
}

func (s *countingBinaryStore) DeletePreparedImage(ctx context.Context, path string) error {
	return s.local.DeletePreparedImage(ctx, path)
}

func (s *countingBinaryStore) openCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opens
}

func (s *memoryStore) UpsertStorageFileDeletionFailure(_ context.Context, input common.UpsertStorageFileDeletionFailureInput) (common.StorageFileDeletionFailure, error) {
	return common.StorageFileDeletionFailure{Path: input.Path}, nil
}

func TestUploadPersistsAVIFAndResolveDerivesBase64(t *testing.T) {
	store := &memoryStore{}
	service := New(config.Config{
		UploadMaxBytes:   1 << 20,
		MediaMaxBytes:    1 << 20,
		MediaAVIFQuality: 50,
	}, store, localstore.Store{RootDir: t.TempDir()}, localProcessor(t))
	raw := tinyPNG(t)
	uploaded, err := service.Upload(context.Background(), "user_a", "conv_a", "req_a", "result.png", "image/png", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("upload output image: %v", err)
	}
	if uploaded.AssetID == "" || uploaded.MediaType != "image/avif" || uploaded.URL == "" {
		t.Fatalf("unexpected upload result: %#v", uploaded)
	}
	resolved, err := service.Consume(context.Background(), "user_a", "conv_a", "req_a", uploaded.AssetID, func(Resolved) error { return nil })
	if err != nil {
		t.Fatalf("resolve output image: %v", err)
	}
	data, err := base64.StdEncoding.DecodeString(resolved.Base64)
	if err != nil {
		t.Fatalf("decode derived base64: %v", err)
	}
	mediaType, width, height, err := media.InspectImageBytes(data)
	if err != nil || mediaType != "image/avif" || width != 2 || height != 1 {
		t.Fatalf("unexpected resolved image: media=%q size=%dx%d err=%v", mediaType, width, height, err)
	}
	if store.asset.Path == "" || store.asset.SourceKind != "output_upload" || store.asset.OriginalName != "result.png" {
		t.Fatalf("unexpected stored asset: %#v", store.asset)
	}
}

func TestResolveRejectsDifferentOwner(t *testing.T) {
	store := &memoryStore{asset: common.MediaAsset{ID: "asset_1", OwnerID: "user_a", MediaType: "image/avif"}, conversationID: "conv_a", requestID: "req_a"}
	service := New(config.Config{}, store, localstore.Store{RootDir: t.TempDir()}, localProcessor(t))
	if _, err := service.Consume(context.Background(), "user_b", "conv_a", "req_a", "asset_1", func(Resolved) error { return nil }); err != ErrAssetNotFound {
		t.Fatalf("expected owner-scoped not found, got %v", err)
	}
}

func TestConsumeSerializesBase64DerivationPerAsset(t *testing.T) {
	store := &memoryStore{}
	binaries := &countingBinaryStore{local: localstore.Store{RootDir: t.TempDir()}}
	service := New(config.Config{UploadMaxBytes: 1 << 20, MediaMaxBytes: 1 << 20}, store, binaries, localProcessor(t))
	uploaded, err := service.Upload(context.Background(), "user_a", "conv_a", "req_a", "result.png", "image/png", bytes.NewReader(tinyPNG(t)))
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsByConsumer := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, consumeErr := service.Consume(context.Background(), "user_a", "conv_a", "req_a", uploaded.AssetID, func(Resolved) error {
				store.markConsumed()
				return nil
			})
			errorsByConsumer <- consumeErr
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByConsumer)
	succeeded := 0
	notFound := 0
	for consumeErr := range errorsByConsumer {
		switch consumeErr {
		case nil:
			succeeded++
		case ErrAssetNotFound:
			notFound++
		default:
			t.Fatalf("unexpected consume error: %v", consumeErr)
		}
	}
	if succeeded != 1 || notFound != 1 || binaries.openCount() != 1 {
		t.Fatalf("asset was not consumed once: success=%d not_found=%d opens=%d", succeeded, notFound, binaries.openCount())
	}
}

func localProcessor(t *testing.T) media.Processor {
	t.Helper()
	processor, err := media.NewProcessor(media.ProcessorConfig{})
	if errors.Is(err, media.ErrProcessorConfig) {
		t.Skip("local image processor is excluded from this build")
	}
	if err != nil {
		t.Fatalf("create local processor: %v", err)
	}
	return processor
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	img.Set(1, 0, color.RGBA{B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
