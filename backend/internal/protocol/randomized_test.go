package protocol

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestNormalizeRequestRandomizedAcrossProtocols(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	formats := []string{"responses", "chat_completions", "anthropic_messages"}
	for i := 0; i < 24; i++ {
		format := formats[rng.Intn(len(formats))]
		text := fmt.Sprintf("hello-%d", rng.Intn(100000))
		toolName := fmt.Sprintf("tool_%d", rng.Intn(1000))
		stream := rng.Intn(2) == 0
		includeImage := rng.Intn(2) == 0
		includeTool := rng.Intn(2) == 0
		body := randomizedBody(format, text, toolName, stream, includeImage, includeTool)
		request, err := NormalizeRequest(format, body)
		if err != nil {
			t.Fatalf("normalize %s failed: %v body=%#v", format, err, body)
		}
		if request.Protocol.String() != format {
			t.Fatalf("unexpected protocol: got=%s want=%s", request.Protocol, format)
		}
		if request.UserContent == "" {
			t.Fatalf("missing user content for format=%s body=%#v", format, body)
		}
		if request.Stream != stream {
			t.Fatalf("unexpected stream flag for format=%s got=%v want=%v", format, request.Stream, stream)
		}
		if includeTool && len(request.ToolSchemas) == 0 {
			t.Fatalf("expected tool schema for format=%s body=%#v", format, body)
		}
		if includeImage && len(request.InputParts) < 2 {
			t.Fatalf("expected image part for format=%s body=%#v input=%#v", format, body, request.InputParts)
		}
	}
}

func TestNormalizeRequestRandomizedNegativeCases(t *testing.T) {
	cases := []struct {
		format string
		body   map[string]any
	}{
		{"responses", map[string]any{"input": ""}},
		{"chat_completions", map[string]any{"model": "x", "messages": "bad"}},
		{"anthropic_messages", map[string]any{"model": "x", "messages": []any{map[string]any{"role": "user"}}}},
	}
	for _, tc := range cases {
		if _, err := NormalizeRequest(tc.format, tc.body); err == nil {
			t.Fatalf("expected error for format=%s body=%#v", tc.format, tc.body)
		}
	}
}

func randomizedBody(format string, text string, toolName string, stream bool, includeImage bool, includeTool bool) map[string]any {
	switch format {
	case "responses":
		content := []any{map[string]any{"type": "input_text", "text": text}}
		if includeImage {
			content = append(content, map[string]any{"type": "input_image", "image_url": "https://example.com/a.png", "media_type": "image/png"})
		}
		body := map[string]any{
			"model":  "gpt-random",
			"stream": stream,
			"input": []any{
				map[string]any{
					"type":    "message",
					"role":    "user",
					"content": content,
				},
			},
		}
		if includeTool {
			body["tools"] = []any{map[string]any{
				"type":       "function",
				"name":       toolName,
				"parameters": map[string]any{"type": "object"},
			}}
		}
		return body
	case "anthropic_messages":
		content := []any{map[string]any{"type": "text", "text": text}}
		if includeImage {
			content = append(content, map[string]any{
				"type": "image",
				"source": map[string]any{
					"type":       "base64",
					"media_type": "image/png",
					"data":       "ZmFrZQ==",
				},
			})
		}
		body := map[string]any{
			"model":  "claude-random",
			"stream": stream,
			"messages": []any{
				map[string]any{"role": "user", "content": content},
			},
		}
		if includeTool {
			body["tools"] = []any{map[string]any{
				"name":         toolName,
				"description":  "tool",
				"input_schema": map[string]any{"type": "object"},
			}}
		}
		return body
	default:
		content := []any{map[string]any{"type": "text", "text": text}}
		if includeImage {
			content = append(content, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": "https://example.com/a.png"},
			})
		}
		body := map[string]any{
			"model":  "gpt-random",
			"stream": stream,
			"messages": []any{
				map[string]any{"role": "user", "content": content},
			},
		}
		if includeTool {
			body["tools"] = []any{map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        toolName,
					"description": "tool",
					"parameters":  map[string]any{"type": "object"},
				},
			}}
		}
		return body
	}
}
