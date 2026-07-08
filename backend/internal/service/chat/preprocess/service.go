package preprocess

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type AssetStore interface {
	PersistAVIF(context.Context, string, string, media.ParsedImage, []byte) (localstore.StoredAsset, error)
}

type Service struct {
	cfg        config.Config
	assetStore AssetStore
}

func New(cfg config.Config, assetStore AssetStore) *Service {
	return &Service{cfg: cfg, assetStore: assetStore}
}

func (s *Service) Prepare(ctx context.Context, ownerID string, req protocol.TurnRequest) (PreparedRequest, error) {
	if !s.cfg.MediaProcessEnabled {
		return PreparedRequest{Request: req}, nil
	}
	processed := req
	if len(req.InputParts) == 0 {
		return PreparedRequest{Request: processed}, nil
	}
	out := make([]protocol.InputPart, 0, len(req.InputParts))
	preparedImages := make([]PreparedImage, 0)
	imageCount := 0
	for idx, part := range req.InputParts {
		if part.Type != "image" {
			out = append(out, part)
			continue
		}
		imageCount++
		if s.cfg.MediaMaxImagesPerRequest > 0 && imageCount > s.cfg.MediaMaxImagesPerRequest {
			return PreparedRequest{}, protocol.InvalidRequest("too many images in request", "input")
		}
		next, prepared, err := s.processImagePart(ctx, ownerID, idx, part)
		if err != nil {
			return PreparedRequest{}, err
		}
		out = append(out, next)
		if prepared.FileID != "" {
			preparedImages = append(preparedImages, prepared)
		}
	}
	processed.InputParts = out
	return PreparedRequest{Request: processed, PreparedImages: preparedImages}, nil
}

func (s *Service) processImagePart(ctx context.Context, ownerID string, inputPartIndex int, part protocol.InputPart) (protocol.InputPart, PreparedImage, error) {
	parsed, err := media.ParseImageInput(part.URL, part.MediaType, s.cfg.MediaMaxBytes)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrImageTooLarge):
			return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("image exceeds maximum allowed bytes", "input")
		default:
			return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("invalid image input", "input")
		}
	}
	switch parsed.SourceKind {
	case media.SourceRemoteURL:
		if !s.cfg.MediaAllowRemoteURL {
			return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("remote image url is not allowed", "input")
		}
		return part, PreparedImage{}, nil
	case media.SourceDataURL:
		if !s.cfg.MediaAllowDataURL {
			return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("data url image is not allowed", "input")
		}
	case media.SourceBase64:
		if !s.cfg.MediaAllowBase64 {
			return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("base64 image is not allowed", "input")
		}
	}
	if strings.EqualFold(parsed.DetectedMediaType, "image/svg+xml") && !s.cfg.MediaAllowSVG {
		return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("svg image is not allowed", "input")
	}
	encoded, err := media.EncodeAVIF(parsed, media.AVIFOptions{Quality: s.cfg.MediaAVIFQuality})
	if err != nil {
		return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("failed to transcode image to avif", "input")
	}
	if s.assetStore == nil {
		return protocol.InputPart{}, PreparedImage{}, protocol.InvalidRequest("media asset store is not configured", "input")
	}
	fileID := "file_" + uuid.NewString()
	stored, err := s.assetStore.PersistAVIF(ctx, ownerID, fileID, parsed, encoded)
	if err != nil {
		return protocol.InputPart{}, PreparedImage{}, protocol.InternalError(err.Error())
	}
	prepared := PreparedImage{
		FileID:            fileID,
		Path:              stored.Path,
		MediaType:         stored.MediaType,
		Bytes:             stored.Bytes,
		SHA256:            parsed.SHA256,
		Width:             parsed.Width,
		Height:            parsed.Height,
		SourceKind:        string(parsed.SourceKind),
		OriginalMediaType: parsed.DetectedMediaType,
		InputPartIndex:    inputPartIndex,
	}
	return protocol.InputPart{
		Type:      "image",
		MediaType: stored.MediaType,
		URL:       stored.Path,
	}, prepared, nil
}
