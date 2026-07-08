package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	"github.com/gen2brain/avif"
	"golang.org/x/image/webp"
)

var (
	ErrInvalidImageInput = errors.New("invalid image input")
	ErrImageTooLarge     = errors.New("image too large")
	ErrSVGDetected       = errors.New("svg image detected")
)

type SourceKind string

const (
	SourceRemoteURL SourceKind = "remote_url"
	SourceDataURL   SourceKind = "data_url"
	SourceBase64    SourceKind = "base64"
)

type ParsedImage struct {
	SourceKind        SourceKind
	Raw               string
	DeclaredMediaType string
	DetectedMediaType string
	Bytes             []byte
	SHA256            string
	Width             int
	Height            int
}

type AVIFOptions struct {
	Quality int
}

func ParseImageInput(raw string, mediaType string, maxBytes int64) (ParsedImage, error) {
	raw = strings.TrimSpace(raw)
	mediaType = strings.TrimSpace(mediaType)
	if raw == "" {
		return ParsedImage{}, ErrInvalidImageInput
	}
	switch {
	case strings.HasPrefix(strings.ToLower(raw), "data:"):
		return parseDataURL(raw, maxBytes)
	case isLikelyRemoteURL(raw):
		return ParsedImage{
			SourceKind:        SourceRemoteURL,
			Raw:               raw,
			DeclaredMediaType: mediaType,
			DetectedMediaType: mediaType,
		}, nil
	default:
		return parseRawBase64(raw, mediaType, maxBytes)
	}
}

func EncodeAVIF(data ParsedImage, opts AVIFOptions) ([]byte, error) {
	if len(data.Bytes) == 0 {
		return nil, ErrInvalidImageInput
	}
	img, detected, _, _, err := decodeImage(bytes.NewReader(data.Bytes))
	if err != nil {
		return nil, err
	}
	if quality := opts.Quality; quality <= 0 {
		opts.Quality = avif.DefaultQuality
	}
	var buf bytes.Buffer
	if err := avif.Encode(&buf, img, avif.Options{Quality: opts.Quality, QualityAlpha: opts.Quality, Speed: avif.DefaultSpeed}); err != nil {
		return nil, fmt.Errorf("encode avif from %s: %w", detected, err)
	}
	return buf.Bytes(), nil
}

func InspectImageBytes(data []byte) (mediaType string, width int, height int, err error) {
	_, mediaType, width, height, err = decodeImage(bytes.NewReader(data))
	return
}

func parseDataURL(raw string, maxBytes int64) (ParsedImage, error) {
	comma := strings.Index(raw, ",")
	if comma <= 5 {
		return ParsedImage{}, ErrInvalidImageInput
	}
	header := raw[:comma]
	payload := raw[comma+1:]
	declared := ""
	if semi := strings.Index(header, ";"); semi > 5 {
		declared = strings.TrimSpace(strings.TrimPrefix(header[:semi], "data:"))
	} else {
		declared = strings.TrimSpace(strings.TrimPrefix(header, "data:"))
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ParsedImage{}, fmt.Errorf("%w: decode data url: %v", ErrInvalidImageInput, err)
	}
	return inspectDecoded(raw, SourceDataURL, declared, decoded, maxBytes)
}

func parseRawBase64(raw string, declared string, maxBytes int64) (ParsedImage, error) {
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(raw)
	}
	if err != nil {
		return ParsedImage{}, fmt.Errorf("%w: decode base64: %v", ErrInvalidImageInput, err)
	}
	return inspectDecoded(raw, SourceBase64, declared, decoded, maxBytes)
}

func inspectDecoded(raw string, kind SourceKind, declared string, decoded []byte, maxBytes int64) (ParsedImage, error) {
	if maxBytes > 0 && int64(len(decoded)) > maxBytes {
		return ParsedImage{}, ErrImageTooLarge
	}
	mediaType, width, height, err := InspectImageBytes(decoded)
	if err != nil {
		if looksLikeSVG(decoded) {
			return ParsedImage{
				SourceKind:        kind,
				Raw:               raw,
				DeclaredMediaType: declared,
				DetectedMediaType: "image/svg+xml",
				Bytes:             decoded,
				SHA256:            sha256Hex(decoded),
			}, nil
		}
		return ParsedImage{}, err
	}
	return ParsedImage{
		SourceKind:        kind,
		Raw:               raw,
		DeclaredMediaType: declared,
		DetectedMediaType: mediaType,
		Bytes:             decoded,
		SHA256:            sha256Hex(decoded),
		Width:             width,
		Height:            height,
	}, nil
}

func decodeImage(r io.Reader) (image.Image, string, int, int, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, "", 0, 0, err
	}
	mediaType := http.DetectContentType(data)
	if strings.EqualFold(mediaType, "image/webp") {
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, "", 0, 0, err
		}
		bounds := img.Bounds()
		return img, mediaType, bounds.Dx(), bounds.Dy(), nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", 0, 0, err
	}
	if mediaType == "application/octet-stream" {
		mediaType = "image/" + format
	}
	return img, mediaType, cfg.Width, cfg.Height, nil
}

func looksLikeSVG(data []byte) bool {
	trimmed := strings.TrimSpace(strings.ToLower(string(data)))
	return strings.HasPrefix(trimmed, "<svg") || strings.Contains(trimmed, "<svg")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func isLikelyRemoteURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
