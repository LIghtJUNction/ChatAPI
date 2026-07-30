package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

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

type DraftAsset struct {
	FileID            string
	OwnerID           string
	Path              string
	MediaType         string
	PublicURL         string
	Bytes             int64
	SHA256            string
	Width             int
	Height            int
	SourceKind        string
	OriginalName      string
	OriginalMediaType string
	InputPartIndex    int
	ContentPartIndex  int
	Data              []byte
}

type StoredAsset struct {
	FileID    string
	OwnerID   string
	Path      string
	PublicURL string
	MediaType string
	Bytes     int64
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

func ParseImageBytes(data []byte, mediaType string, maxBytes int64) (ParsedImage, error) {
	if len(data) == 0 {
		return ParsedImage{}, ErrInvalidImageInput
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return ParsedImage{}, ErrImageTooLarge
	}
	return inspectDecoded("", SourceKind("upload"), strings.TrimSpace(mediaType), data, maxBytes)
}

func SHA256Hex(data []byte) string {
	return sha256Hex(data)
}

func InspectImageBytes(data []byte) (mediaType string, width int, height int, err error) {
	if hasAVIFBrand(data) {
		if width, height, ok := inspectAVIFContainer(data); ok {
			return "image/avif", width, height, nil
		}
		return "", 0, 0, fmt.Errorf("%w: invalid AVIF container", ErrInvalidImageInput)
	}
	_, mediaType, width, height, err = decodeImage(bytes.NewReader(data))
	return
}

func inspectAVIFContainer(data []byte) (width int, height int, ok bool) {
	if !hasAVIFBrand(data) || !hasCompleteAVIFMediaData(data) {
		return 0, 0, false
	}
	width, height, ok = findAVIFSpatialExtents(data, 0)
	return width, height, ok && width > 0 && height > 0
}

func hasCompleteAVIFMediaData(data []byte) bool {
	foundMediaData := false
	for len(data) > 0 {
		boxType, payload, rest, ok := nextISOBMFFBox(data)
		if !ok {
			return false
		}
		if boxType == "mdat" && len(payload) > 0 {
			foundMediaData = true
		}
		data = rest
	}
	return foundMediaData
}

func hasAVIFBrand(data []byte) bool {
	boxType, payload, rest, ok := nextISOBMFFBox(data)
	for ok {
		if boxType == "ftyp" && len(payload) >= 8 {
			if brand := string(payload[:4]); brand == "avif" || brand == "avis" {
				return true
			}
			for offset := 8; offset+4 <= len(payload); offset += 4 {
				if brand := string(payload[offset : offset+4]); brand == "avif" || brand == "avis" {
					return true
				}
			}
		}
		boxType, payload, rest, ok = nextISOBMFFBox(rest)
	}
	return false
}

func findAVIFSpatialExtents(data []byte, depth int) (width int, height int, ok bool) {
	if depth > 8 {
		return 0, 0, false
	}
	boxType, payload, rest, valid := nextISOBMFFBox(data)
	for valid {
		switch boxType {
		case "ispe":
			if len(payload) >= 12 {
				width = int(binary.BigEndian.Uint32(payload[4:8]))
				height = int(binary.BigEndian.Uint32(payload[8:12]))
				if width > 0 && height > 0 {
					return width, height, true
				}
			}
		case "meta":
			if len(payload) >= 4 {
				if width, height, ok = findAVIFSpatialExtents(payload[4:], depth+1); ok {
					return width, height, true
				}
			}
		case "iprp", "ipco":
			if width, height, ok = findAVIFSpatialExtents(payload, depth+1); ok {
				return width, height, true
			}
		}
		boxType, payload, rest, valid = nextISOBMFFBox(rest)
	}
	return 0, 0, false
}

func nextISOBMFFBox(data []byte) (boxType string, payload []byte, rest []byte, ok bool) {
	if len(data) < 8 {
		return "", nil, nil, false
	}
	size := uint64(binary.BigEndian.Uint32(data[:4]))
	headerSize := uint64(8)
	if size == 1 {
		if len(data) < 16 {
			return "", nil, nil, false
		}
		size = binary.BigEndian.Uint64(data[8:16])
		headerSize = 16
	} else if size == 0 {
		size = uint64(len(data))
	}
	if size < headerSize || size > uint64(len(data)) {
		return "", nil, nil, false
	}
	return string(data[4:8]), data[headerSize:size], data[size:], true
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
	decoded, err := decodeBase64Payload(payload)
	if err != nil {
		return ParsedImage{}, fmt.Errorf("%w: decode data url: %v", ErrInvalidImageInput, err)
	}
	return inspectDecoded(raw, SourceDataURL, declared, decoded, maxBytes)
}

func parseRawBase64(raw string, declared string, maxBytes int64) (ParsedImage, error) {
	decoded, err := decodeBase64Payload(raw)
	if err != nil {
		return ParsedImage{}, fmt.Errorf("%w: decode base64: %v", ErrInvalidImageInput, err)
	}
	return inspectDecoded(raw, SourceBase64, declared, decoded, maxBytes)
}

func decodeBase64Payload(raw string) ([]byte, error) {
	normalized := stripBase64Whitespace(raw)
	if normalized == "" {
		return nil, ErrInvalidImageInput
	}
	var lastErr error
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(normalized)
		if err == nil {
			return decoded, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func stripBase64Whitespace(raw string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t', '\f', '\v':
			return -1
		default:
			return r
		}
	}, raw)
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

func ChatAssetPublicURL(fileID string) string {
	fileID = sanitizeAssetSegment(fileID)
	if fileID == "" {
		return ""
	}
	return "/api/media/assets/" + fileID
}

func ChatAssetFilename(fileID string) string {
	fileID = sanitizeAssetSegment(fileID)
	if fileID == "" {
		return ""
	}
	return fileID + ".avif"
}

func sanitizeAssetSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "..", "_")
	return value
}
