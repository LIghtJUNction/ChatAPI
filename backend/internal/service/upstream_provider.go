package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type UpstreamProviderDescriptor struct {
	Name              string              `json:"name"`
	DisplayName       string              `json:"display_name"`
	Protocols         []string            `json:"protocols,omitempty"`
	SupportsStreaming bool                `json:"supports_streaming"`
	RequiresUserLink  bool                `json:"requires_user_link"`
	Capabilities      map[string]any      `json:"capabilities,omitempty"`
	Notes             []string            `json:"notes,omitempty"`
	ErrorCodes        []UpstreamErrorCode `json:"error_codes,omitempty"`
	RequestHints      map[string]any      `json:"request_hints,omitempty"`
	ResponseHints     map[string]any      `json:"response_hints,omitempty"`
}

type ToolCallAssistProviderError struct {
	Provider   string `json:"provider,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	Retryable  bool   `json:"retryable"`
}

func (e *ToolCallAssistProviderError) Error() string {
	if e == nil {
		return ""
	}
	return strings.TrimSpace(e.Message)
}

type UpstreamProvider interface {
	ProviderName() string
	ProviderDescriptor() UpstreamProviderDescriptor
	ChatCompletions(ctx context.Context, userID string, body any) (map[string]any, error)
}

type UpstreamStreamingProvider interface {
	UpstreamProvider
	ChatCompletionsRaw(ctx context.Context, userID string, body any) (*http.Response, error)
}

func normalizeProviderName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func NormalizeToolCallAssistProviderError(provider string, err error) error {
	if err == nil {
		return nil
	}
	var providerErr *ToolCallAssistProviderError
	if errors.As(err, &providerErr) {
		if providerErr.Provider == "" {
			providerErr.Provider = normalizeProviderName(provider)
		}
		if providerErr.HTTPStatus == 0 {
			providerErr.HTTPStatus = http.StatusBadGateway
		}
		if providerErr.Code == "" {
			providerErr.Code = "provider_request_failed"
		}
		if strings.TrimSpace(providerErr.Message) == "" {
			providerErr.Message = err.Error()
		}
		return providerErr
	}

	provider = normalizeProviderName(provider)
	switch {
	case errors.Is(err, ErrKirariNotConnected):
		return &ToolCallAssistProviderError{
			Provider:   provider,
			Code:       "provider_not_connected",
			Message:    err.Error(),
			HTTPStatus: http.StatusConflict,
			Retryable:  false,
		}
	default:
		return &ToolCallAssistProviderError{
			Provider:   provider,
			Code:       "provider_request_failed",
			Message:    err.Error(),
			HTTPStatus: http.StatusBadGateway,
			Retryable:  true,
		}
	}
}
