package service

import (
	"context"
	"errors"
	"testing"
)

func TestToolCallAssistServiceRejectsUnsupportedProviderWhenRegistryEmpty(t *testing.T) {
	svc := NewToolCallAssistService(nil)
	if _, err := svc.Execute(context.Background(), "user", "kirari", "model", "req", ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden without workspace, got %v", err)
	}
}

func TestToolCallAssistServiceRegistersProvidersByNormalizedName(t *testing.T) {
	svc := NewToolCallAssistService(&WorkspaceToolCallService{}, stubUpstreamProvider{name: " Kirari "})
	if svc.providers[providerKirari] == nil {
		t.Fatalf("expected normalized kirari provider registration: %#v", svc.providers)
	}
}

type stubUpstreamProvider struct {
	name string
}

func (s stubUpstreamProvider) ProviderName() string {
	return s.name
}

func (s stubUpstreamProvider) ChatCompletions(ctx context.Context, userID string, body any) (map[string]any, error) {
	return nil, nil
}
