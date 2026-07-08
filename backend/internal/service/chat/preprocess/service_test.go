package preprocess

import (
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/platform/media/localstore"
	"github.com/zyf/chatapi/internal/protocol"
)

func TestPrepareTranscodesBase64ImageToStoredAVIF(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	cfg.MediaDerivedDir = filepath.Join(t.TempDir(), "derived")
	svc := New(cfg, localstore.Store{RootDir: cfg.MediaDerivedDir})

	request := protocol.TurnRequest{
		Protocol: protocol.ProtocolResponses,
		Model:    "demo",
		InputParts: []protocol.InputPart{
			{Type: "text", Text: "hello"},
			{Type: "image", MediaType: "image/png", URL: base64.StdEncoding.EncodeToString(tinyPNG(t))},
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
	imagePart := processed.Request.InputParts[1]
	if imagePart.MediaType != "image/avif" || imagePart.URL == "" {
		t.Fatalf("unexpected processed image part: %#v", imagePart)
	}
	if len(processed.PreparedImages) != 1 {
		t.Fatalf("unexpected prepared images: %#v", processed.PreparedImages)
	}
	if processed.PreparedImages[0].FileID == "" || processed.PreparedImages[0].Path == "" {
		t.Fatalf("unexpected prepared image payload: %#v", processed.PreparedImages[0])
	}
}

func TestPrepareRejectsSVGWhenDisabled(t *testing.T) {
	cfg := config.Default(config.ModeServe, filepath.Join(t.TempDir(), "backend"))
	cfg.MediaDerivedDir = filepath.Join(t.TempDir(), "derived")
	cfg.MediaAllowSVG = false
	svc := New(cfg, localstore.Store{RootDir: cfg.MediaDerivedDir})

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

type bytesBuffer struct{ data []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bytesBuffer) Bytes() []byte { return append([]byte(nil), b.data...) }
