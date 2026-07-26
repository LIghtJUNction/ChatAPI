package media

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestParseImageInputAndEncodeAVIF(t *testing.T) {
	rawPNG := tinyPNG(t)
	b64 := base64.StdEncoding.EncodeToString(rawPNG)

	parsed, err := ParseImageInput(b64, "image/png", 1<<20)
	if err != nil {
		t.Fatalf("parse image input: %v", err)
	}
	if parsed.SourceKind != SourceBase64 || parsed.DetectedMediaType != "image/png" || parsed.Width != 2 || parsed.Height != 1 {
		t.Fatalf("unexpected parsed image: %#v", parsed)
	}

	processor, err := NewProcessor(ProcessorConfig{})
	if errors.Is(err, ErrProcessorConfig) {
		t.Skip("local image processor is excluded from this build")
	}
	if err != nil {
		t.Fatalf("create local processor: %v", err)
	}
	processed, err := processor.EncodeAVIF(context.Background(), parsed, AVIFOptions{Quality: 50})
	if err != nil {
		t.Fatalf("encode avif: %v", err)
	}
	if len(processed.Bytes) == 0 {
		t.Fatal("expected avif bytes")
	}
	if processed.MediaType != "image/avif" || processed.Width != 2 || processed.Height != 1 {
		t.Fatalf("unexpected processed image: %#v", processed)
	}
	if mediaType, width, height, err := InspectImageBytes(processed.Bytes); err != nil || mediaType != "image/avif" || width != 2 || height != 1 {
		t.Fatalf("unexpected avif inspect result: media=%q width=%d height=%d err=%v", mediaType, width, height, err)
	}
}

func TestInspectImageBytesReadsAVIFContainerMetadata(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(testAVIFBase64)
	if err != nil {
		t.Fatal(err)
	}
	mediaType, width, height, err := InspectImageBytes(data)
	if err != nil || mediaType != "image/avif" || width != 2 || height != 2 {
		t.Fatalf("unexpected AVIF metadata: media=%q size=%dx%d err=%v", mediaType, width, height, err)
	}
}

func TestInspectImageBytesRejectsTruncatedAVIFContainer(t *testing.T) {
	data, err := base64.StdEncoding.DecodeString(testAVIFBase64)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := InspectImageBytes(data[:len(data)-5]); err == nil {
		t.Fatal("expected truncated AVIF to be rejected")
	}
}

func TestParseImageInputDetectsSVGPayload(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`))
	parsed, err := ParseImageInput(payload, "image/svg+xml", 1<<20)
	if err != nil {
		t.Fatalf("parse svg base64: %v", err)
	}
	if parsed.DetectedMediaType != "image/svg+xml" {
		t.Fatalf("expected svg media type, got %#v", parsed)
	}
}

func TestParseImageInputSupportsDataURL(t *testing.T) {
	rawPNG := tinyPNG(t)
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(rawPNG)
	parsed, err := ParseImageInput(dataURL, "", 1<<20)
	if err != nil {
		t.Fatalf("parse data url: %v", err)
	}
	if parsed.SourceKind != SourceDataURL || parsed.DetectedMediaType != "image/png" {
		t.Fatalf("unexpected parsed data url: %#v", parsed)
	}
}

func TestParseImageInputSupportsJPEGDataURLWithWhitespace(t *testing.T) {
	rawJPEG := tinyJPEG(t)
	base64Payload := base64.StdEncoding.EncodeToString(rawJPEG)
	dataURL := "data:image/jpeg;base64," + base64Payload[:24] + " \n\t" + base64Payload[24:]
	parsed, err := ParseImageInput(dataURL, "", 1<<20)
	if err != nil {
		t.Fatalf("parse jpeg data url with whitespace: %v", err)
	}
	if parsed.SourceKind != SourceDataURL || parsed.DetectedMediaType != "image/jpeg" {
		t.Fatalf("unexpected parsed jpeg data url: %#v", parsed)
	}
	if parsed.Width != 2 || parsed.Height != 1 {
		t.Fatalf("unexpected jpeg dimensions: %#v", parsed)
	}
}

func TestParseImageInputSupportsRemoteURL(t *testing.T) {
	parsed, err := ParseImageInput("https://example.com/cat.png", "image/png", 1<<20)
	if err != nil {
		t.Fatalf("parse remote url: %v", err)
	}
	if parsed.SourceKind != SourceRemoteURL || parsed.Raw != "https://example.com/cat.png" {
		t.Fatalf("unexpected remote parse: %#v", parsed)
	}
}

func TestParseImageInputRejectsOversizedInput(t *testing.T) {
	rawPNG := tinyPNG(t)
	b64 := base64.StdEncoding.EncodeToString(rawPNG)
	if _, err := ParseImageInput(b64, "image/png", 4); err == nil || !strings.Contains(err.Error(), ErrImageTooLarge.Error()) {
		t.Fatalf("expected oversized image error, got %v", err)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{B: 255, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}
