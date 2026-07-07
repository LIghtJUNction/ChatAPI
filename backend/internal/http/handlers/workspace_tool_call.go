package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type WorkspaceToolCallHandler struct {
	Config        config.Config
	Service       *service.WorkspaceToolCallService
	AssistService *service.ToolCallAssistService
}

func (h WorkspaceToolCallHandler) Schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": service.BuildWorkspaceToolCallContextSchema(),
	})
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
		"ok":                          true,
		"request":                     contextPayload.Request,
		"parsed":                      requestParsedView(requestBaseURL(r), contextPayload.Request),
		"conversation":                contextPayload.Conversation,
		"messages":                    contextPayload.Messages,
		"assist_schema":               h.Service.AssistSchema(),
		"upstream_assistant_schema":   service.BuildUpstreamAssistantSchema(),
		"upstream_protocol_templates": service.BuildUpstreamProtocolTemplates(),
		"upstream_hints":              service.BuildUpstreamAssistantHints(h.Config, requestBaseURL(r), strings.TrimSpace(r.URL.Query().Get("candidate_base_url"))),
		"upstream_input_hints":        service.BuildUpstreamInputHints(contextPayload.Messages, contextPayload.DraftText, 20),
		"draft": map[string]any{
			"text":   contextPayload.DraftText,
			"length": contextPayload.DraftLength,
		},
	})
}

func (h WorkspaceToolCallHandler) Assist(w http.ResponseWriter, r *http.Request) {
	actor, ok := service.RequestActorFromContext(r.Context())
	if !ok || !service.IsInteractiveUserActor(actor) {
		http.Error(w, "session required", http.StatusUnauthorized)
		return
	}
	var body struct {
		Provider       string `json:"provider"`
		Model          string `json:"model"`
		RequestID      string `json:"request_id"`
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	result, err := h.AssistService.Execute(r.Context(), actor.UserID, body.Provider, body.Model, body.RequestID, body.ConversationID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrToolCallAssistProviderRequired),
			errors.Is(err, service.ErrToolCallAssistModelRequired),
			errors.Is(err, service.ErrToolCallAssistTargetRequired),
			errors.Is(err, service.ErrToolCallAssistUnsupported),
			errors.Is(err, service.ErrToolCallAssistNoTools):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrKirariNotConnected):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, service.ErrForbidden):
			http.Error(w, err.Error(), http.StatusForbidden)
		case errors.Is(err, store.ErrNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"assist": result,
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
