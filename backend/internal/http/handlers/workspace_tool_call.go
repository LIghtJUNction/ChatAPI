package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type WorkspaceToolCallHandler struct {
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
		"ok":           true,
		"request":      contextPayload.Request,
		"parsed":       requestParsedView(contextPayload.Request),
		"conversation": contextPayload.Conversation,
		"messages":     contextPayload.Messages,
		"draft": map[string]any{
			"text":   contextPayload.DraftText,
			"length": contextPayload.DraftLength,
		},
	})
}
