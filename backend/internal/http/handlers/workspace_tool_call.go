package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type WorkspaceToolCallHandler struct {
	Config  config.Config
	Service *service.WorkspaceToolCallService
}

func (h WorkspaceToolCallHandler) AssistContext(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
	conversationID := strings.TrimSpace(r.URL.Query().Get("conversation_id"))
	contextPayload, err := h.Service.AssistContext(r.Context(), actor.UserID, requestID, conversationID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrToolCallAssistTargetRequired):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                        true,
		"request":                   contextPayload.Request,
		"parsed":                    requestParsedView(contextPayload.Request),
		"conversation":              contextPayload.Conversation,
		"messages":                  contextPayload.Messages,
		"assist_schema":             h.Service.AssistSchema(),
		"upstream_assistant_schema": service.BuildUpstreamAssistantSchema(),
		"upstream_hints":            service.BuildUpstreamAssistantHints(h.Config, requestBaseURL(r), strings.TrimSpace(r.URL.Query().Get("candidate_base_url"))),
		"upstream_input_hints":      service.BuildUpstreamInputHints(contextPayload.Messages, contextPayload.DraftText, 20),
		"draft": map[string]any{
			"text":   contextPayload.DraftText,
			"length": contextPayload.DraftLength,
		},
	})
}

func requestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}
	return (&url.URL{Scheme: scheme, Host: host}).String()
}
