package preprocess

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
)

func TestPrepareTranscodesBase64ImageToDraftAVIF(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	svc := New(cfg, localProcessor(t))

	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "demo",
		InputParts: []protocol.InputPart{
			{Type: "text", Text: "hello"},
			{Type: "tool_result", ToolResult: &protocol.ToolResult{CallID: "call_view_image", Content: []protocol.ContentPart{
				{Type: "text", Text: "loaded image"},
				{Type: "image", MediaType: "image/png", URL: base64.StdEncoding.EncodeToString(tinyPNG(t))},
			}}},
		},
		UserContent: "hello",
	}
	processed, err := svc.Prepare(context.Background(), "user_a", request)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if len(processed.Request.InputParts) != 2 {
		t.Fatalf("unexpected parts: %#v", processed.Request.InputParts)
	}
	toolResult := processed.Request.InputParts[1].ToolResult
	if toolResult == nil || toolResult.CallID != "call_view_image" || len(toolResult.Content) != 2 {
		t.Fatalf("tool result structure was lost: %#v", processed.Request.InputParts[1])
	}
	imagePart := toolResult.Content[1]
	if imagePart.MediaType != "image/avif" || imagePart.URL == "" {
		t.Fatalf("unexpected processed image part: %#v", imagePart)
	}
	if !strings.HasPrefix(imagePart.URL, "/api/media/assets/file_") {
		t.Fatalf("expected public image url, got %#v", imagePart)
	}
	if len(processed.PreparedImages) != 1 {
		t.Fatalf("unexpected prepared images: %#v", processed.PreparedImages)
	}
	if processed.PreparedImages[0].FileID == "" || len(processed.PreparedImages[0].Data) == 0 {
		t.Fatalf("unexpected prepared image payload: %#v", processed.PreparedImages[0])
	}
	if processed.PreparedImages[0].SHA256 != media.SHA256Hex(processed.PreparedImages[0].Data) {
		t.Fatalf("prepared image hash must describe processed bytes: %#v", processed.PreparedImages[0])
	}
	sanitized := mustJSON(t, protocol.BuildRequestBody(processed.Request))
	if strings.Contains(sanitized, "base64") || strings.Contains(sanitized, "iVBOR") {
		t.Fatalf("sanitized body should not contain base64 payload: %s", sanitized)
	}
}

func TestPrepareRejectsSVGWhenDisabled(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	cfg.MediaAllowSVG = false
	svc := New(cfg, localProcessor(t))

	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolAnthropicMessages,
		Model:    "demo",
		InputParts: []protocol.InputPart{
			{Type: "image", MediaType: "image/svg+xml", URL: base64.StdEncoding.EncodeToString([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))},
		},
	}
	if _, err := svc.Prepare(context.Background(), "user_a", request); err == nil {
		t.Fatal("expected svg rejection")
	}
}

func TestPrepareTranscodesJPEGDataURLWithWhitespaceToDraftAVIF(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	svc := New(cfg, localProcessor(t))

	base64Payload := base64.StdEncoding.EncodeToString(tinyJPEG(t))
	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "demo",
		InputParts: []protocol.InputPart{
			{Type: "image", MediaType: "image/jpeg", URL: "data:image/jpeg;base64," + base64Payload[:24] + " \n" + base64Payload[24:]},
		},
	}
	processed, err := svc.Prepare(context.Background(), "user_a", request)
	if err != nil {
		t.Fatalf("prepare jpeg data url: %v", err)
	}
	if len(processed.PreparedImages) != 1 {
		t.Fatalf("unexpected prepared images: %#v", processed.PreparedImages)
	}
	if processed.Request.InputParts[0].MediaType != "image/avif" {
		t.Fatalf("unexpected processed input part: %#v", processed.Request.InputParts[0])
	}
	if !strings.HasPrefix(processed.Request.InputParts[0].URL, "/api/media/assets/file_") {
		t.Fatalf("expected public url, got %#v", processed.Request.InputParts[0])
	}
}

func TestPreparePreservesProcessorOperationalError(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	svc := New(cfg, failingProcessor{err: media.ErrProcessorUnavailable})
	request := protocol.TurnRequest{InputParts: []protocol.InputPart{{
		Type: "image", MediaType: "image/png", URL: base64.StdEncoding.EncodeToString(tinyPNG(t)),
	}}}
	_, err := svc.Prepare(context.Background(), "user_a", request)
	if !errors.Is(err, media.ErrProcessorUnavailable) {
		t.Fatalf("expected processor error to propagate, got %v", err)
	}
	var requestErr *protocol.RequestError
	if errors.As(err, &requestErr) {
		t.Fatalf("operational error must not become invalid request: %#v", requestErr)
	}
}

func TestPrepareMapsProcessorInputErrorToInvalidRequest(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	svc := New(cfg, failingProcessor{err: media.ErrInvalidImageInput})
	request := protocol.TurnRequest{InputParts: []protocol.InputPart{{
		Type: "image", MediaType: "image/png", URL: base64.StdEncoding.EncodeToString(tinyPNG(t)),
	}}}
	_, err := svc.Prepare(context.Background(), "user_a", request)
	var requestErr *protocol.RequestError
	if !errors.As(err, &requestErr) || requestErr.StatusCode != 400 {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}

type failingProcessor struct{ err error }

func (p failingProcessor) EncodeAVIF(context.Context, media.ParsedImage, media.AVIFOptions) (media.ProcessedImage, error) {
	return media.ProcessedImage{}, p.err
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

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(body)
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	img.Set(1, 0, color.NRGBA{B: 255, A: 255})
	var buf bytesBuffer
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
	var buf bytesBuffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuffer) Bytes() []byte { return append([]byte(nil), b.data...) }
