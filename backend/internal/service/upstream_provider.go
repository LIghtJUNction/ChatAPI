package service

import (
	"context"
	"net/http"
	"strings"
)

type UpstreamProvider interface {
	ProviderName() string
	ChatCompletions(ctx context.Context, userID string, body any) (map[string]any, error)
}

type UpstreamStreamingProvider interface {
	UpstreamProvider
	ChatCompletionsRaw(ctx context.Context, userID string, body any) (*http.Response, error)
}

func normalizeProviderName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
