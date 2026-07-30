package preprocess

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	preprocesssettings "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess/settings"
)

type Service struct {
	cfg       config.Config
	processor media.Processor
	Settings  *preprocesssettings.Service
}

func New(cfg config.Config, processor media.Processor) *Service {
	if processor == nil {
		panic("preprocess image processor is required")
	}
	return &Service{cfg: cfg, processor: processor}
}

func (s *Service) Prepare(ctx context.Context, ownerID string, req protocol.TurnRequest) (PreparedRequest, error) {
	cfg := s.current(ctx)
	if !cfg.MediaProcessEnabled {
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
		if part.Type == "tool_result" && part.ToolResult != nil {
			next := part
			nextResult := *part.ToolResult
			nextResult.Content = append([]protocol.ContentPart(nil), part.ToolResult.Content...)
			for contentIdx, content := range nextResult.Content {
				if content.Type != "image" {
					continue
				}
				imageCount++
				if cfg.MediaMaxImagesPerRequest > 0 && imageCount > cfg.MediaMaxImagesPerRequest {
					return PreparedRequest{}, protocol.InvalidRequest("too many images in request", "input")
				}
				imagePart := protocol.InputPart{Type: "image", MediaType: content.MediaType, URL: content.URL}
				processedPart, prepared, err := s.processImagePart(ctx, cfg, ownerID, idx, contentIdx, imagePart)
				if err != nil {
					return PreparedRequest{}, err
				}
				nextResult.Content[contentIdx] = protocol.ContentPart{Type: "image", MediaType: processedPart.MediaType, URL: processedPart.URL}
				if prepared.FileID != "" {
					preparedImages = append(preparedImages, prepared)
				}
			}
			next.ToolResult = &nextResult
			out = append(out, next)
			continue
		}
		if part.Type != "image" {
			out = append(out, part)
			continue
		}
		imageCount++
		if cfg.MediaMaxImagesPerRequest > 0 && imageCount > cfg.MediaMaxImagesPerRequest {
			return PreparedRequest{}, protocol.InvalidRequest("too many images in request", "input")
		}
		next, prepared, err := s.processImagePart(ctx, cfg, ownerID, idx, -1, part)
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

func (s *Service) processImagePart(ctx context.Context, cfg config.Config, ownerID string, inputPartIndex int, contentPartIndex int, part protocol.InputPart) (protocol.InputPart, media.DraftAsset, error) {
	parsed, err := media.ParseImageInput(part.URL, part.MediaType, cfg.MediaMaxBytes)
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
		if !cfg.MediaAllowRemoteURL {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("remote image url is not allowed", "input")
		}
		return part, media.DraftAsset{}, nil
	case media.SourceDataURL:
		if !cfg.MediaAllowDataURL {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("data url image is not allowed", "input")
		}
	case media.SourceBase64:
		if !cfg.MediaAllowBase64 {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("base64 image is not allowed", "input")
		}
	}
	if strings.EqualFold(parsed.DetectedMediaType, "image/svg+xml") && !cfg.MediaAllowSVG {
		return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("svg image is not allowed", "input")
	}
	processed, err := s.processor.EncodeAVIF(ctx, parsed, media.AVIFOptions{Quality: cfg.MediaAVIFQuality})
	if err != nil {
		if errors.Is(err, media.ErrInvalidImageInput) || errors.Is(err, media.ErrImageTooLarge) {
			return protocol.InputPart{}, media.DraftAsset{}, protocol.InvalidRequest("failed to transcode image to avif", "input")
		}
		return protocol.InputPart{}, media.DraftAsset{}, err
	}
	fileID := "file_" + uuid.NewString()
	ownerID = mediaOwner(ownerID)
	prepared := media.DraftAsset{
		FileID:            fileID,
		OwnerID:           ownerID,
		MediaType:         processed.MediaType,
		PublicURL:         media.ChatAssetPublicURL(fileID),
		Bytes:             int64(len(processed.Bytes)),
		SHA256:            media.SHA256Hex(processed.Bytes),
		Width:             processed.Width,
		Height:            processed.Height,
		SourceKind:        string(parsed.SourceKind),
		OriginalMediaType: parsed.DetectedMediaType,
		InputPartIndex:    inputPartIndex,
		ContentPartIndex:  contentPartIndex,
		Data:              processed.Bytes,
	}
	return protocol.InputPart{
		Type:      "image",
		MediaType: prepared.MediaType,
		URL:       prepared.PublicURL,
	}, prepared, nil
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

func mediaOwner(ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return "anonymous"
	}
	return ownerID
}
