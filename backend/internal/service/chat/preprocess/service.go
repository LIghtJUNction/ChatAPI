package preprocess

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type Service struct {
	cfg config.Config
}

func New(cfg config.Config) *Service {
	return &Service{cfg: cfg}
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
	preparedImages := make([]media.DraftAsset, 0)
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
	return PreparedRequest{
		Request:        processed,
		PreparedImages: preparedImages,
	}, nil
}

func (s *Service) processImagePart(_ context.Context, ownerID string, inputPartIndex int, part protocol.InputPart) (protocol.InputPart, media.DraftAsset, error) {
	parsed, err := media.ParseImageInput(part.URL, part.MediaType, s.cfg.MediaMaxBytes)
	if err != nil {
		switch {
		case errors.Is(err, media.ErrImageTooLarge):
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("image exceeds maximum allowed bytes", "input")
		default:
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("invalid image input", "input")
		}
	}
	switch parsed.SourceKind {
	case media.SourceRemoteURL:
		if !s.cfg.MediaAllowRemoteURL {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("remote image url is not allowed", "input")
		}
		return part, media.DraftAsset{}, nil
	case media.SourceDataURL:
		if !s.cfg.MediaAllowDataURL {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("data url image is not allowed", "input")
		}
	case media.SourceBase64:
		if !s.cfg.MediaAllowBase64 {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("base64 image is not allowed", "input")
		}
	}
	if strings.EqualFold(parsed.DetectedMediaType, "image/svg+xml") && !s.cfg.MediaAllowSVG {
		return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("svg image is not allowed", "input")
	}
	encoded, err := media.EncodeAVIF(parsed, media.AVIFOptions{Quality: s.cfg.MediaAVIFQuality})
	if err != nil {
		return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("failed to transcode image to avif", "input")
	}
	fileID := "file_" + uuid.NewString()
	ownerID = mediaOwner(ownerID)
	prepared := media.DraftAsset{
		FileID:            fileID,
		OwnerID:           ownerID,
		MediaType:         "image/avif",
		PublicURL:         media.ChatAssetPublicURL(fileID),
		Bytes:             int64(len(encoded)),
		SHA256:            parsed.SHA256,
		Width:             parsed.Width,
		Height:            parsed.Height,
		SourceKind:        string(parsed.SourceKind),
		OriginalMediaType: parsed.DetectedMediaType,
		InputPartIndex:    inputPartIndex,
		Data:              encoded,
	}
	return protocol.InputPart{
		Type:      "image",
		MediaType: prepared.MediaType,
		URL:       prepared.PublicURL,
	}, prepared, nil
}

func mediaOwner(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return "anonymous"
	}
	return ownerID
}
