package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/http/middleware"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type AppAPIHandler struct {
	Service *service.ChatAPIService
}

func (h AppAPIHandler) Me(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"app_api_key": map[string]any{
			"id":         principal.KeyID,
			"name":       principal.Name,
			"key_prefix": principal.KeyPrefix,
		},
		"user": map[string]any{
			"id": principal.UserID,
		},
	})
}

func (h AppAPIHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Service.ListRequestsForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Service.GetRequestForOwner(r.Context(), requestID, principal.UserID)
	if err != nil {
		h.writeRequestError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "request": item})
}

func (h AppAPIHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Service.ListConversationsForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Service.ListMessagesForOwner(r.Context(), conversationID, principal.UserID)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) RequestDelta(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, "delta", service.TurnControlStreamDelta)
}

func (h AppAPIHandler) RequestComplete(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, "complete", service.TurnControlStreamComplete)
}

func (h AppAPIHandler) RequestAbort(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, "abort", service.TurnControlAbort)
}

func (h AppAPIHandler) executeRequestTurnControl(w http.ResponseWriter, r *http.Request, action string, kind service.TurnControlKind) {
	principal, ok := middleware.AppAPIPrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	if len(principal.AllowedActions) > 0 {
		if _, ok := principal.AllowedActions[action]; !ok {
			http.Error(w, "app api key forbidden", http.StatusForbidden)
			return
		}
	}

	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	request, err := h.Service.GetRequestForOwner(r.Context(), requestID, principal.UserID)
	if err != nil {
		h.writeRequestError(w, err)
		return
	}
	body := decodeBodyOrEmpty(r)
	command := service.TurnControlCommand{
		Kind:                kind,
		ConversationID:      request.ConversationID,
		ResponseID:          stringValue(body["response_id"], ""),
		OutputText:          stringValue(body["text"], ""),
		Mode:                stringValue(body["mode"], "assistant_message"),
		ToolName:            stringValue(body["tool_name"], ""),
		ToolCallID:          stringValue(body["tool_call_id"], ""),
		ToolOutput:          stringValue(body["output"], stringValue(body["text"], "")),
		ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
		AbortReason:         stringValue(body["error"], ""),
	}
	result, err := h.Service.ExecuteTurnControl(r.Context(), command)
	if err != nil {
		if errors.Is(err, service.ErrPendingConflict) || errors.Is(err, store.ErrTurnConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrPendingNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if errors.Is(err, service.ErrForbidden) {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h AppAPIHandler) writeRequestError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrForbidden):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, service.ErrPendingNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		http.Error(w, err.Error(), http.StatusNotFound)
	}
}
