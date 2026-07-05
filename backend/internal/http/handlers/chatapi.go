package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type ChatAPIHandler struct {
	Service *service.ChatAPIService
	Pending *service.PendingRegistry
}

func (h ChatAPIHandler) Responses(w http.ResponseWriter, r *http.Request) {
	h.handleProtocolRequest(w, r, "responses")
}

func (h ChatAPIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.handleProtocolRequest(w, r, "chat_completions")
}

func (h ChatAPIHandler) AnthropicMessages(w http.ResponseWriter, r *http.Request) {
	h.handleProtocolRequest(w, r, "anthropic_messages")
}

func (h ChatAPIHandler) handleProtocolRequest(w http.ResponseWriter, r *http.Request, requestFormat string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	responseBody, err := h.Service.CreatePendingResponse(r.Context(), requestFormat, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, responseBody)
}

func (h ChatAPIHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := chi.URLParam(r, "conversationID")
	items, err := h.Service.ListMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": items,
	})
}

func (h ChatAPIHandler) CompleteOutput(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	conversationID, err := service.MustConversationID(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	input := store.CompletePendingInput{
		ConversationID: conversationID,
		ResponseID:     stringValue(body["response_id"], ""),
		OutputText:     stringValue(body["text"], ""),
		Mode:           stringValue(body["mode"], "assistant_message"),
		ToolName:       stringValue(body["tool_name"], ""),
		ToolCallID:     stringValue(body["tool_call_id"], ""),
	}

	result, err := h.Service.CompleteConversation(r.Context(), input)
	if err != nil {
		if errors.Is(err, service.ErrPendingNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h ChatAPIHandler) DeltaOutput(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	conversationID, err := service.MustConversationID(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := h.Service.UpdateDraft(r.Context(), conversationID, stringValue(body["text"], ""))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h ChatAPIHandler) AbortConversation(w http.ResponseWriter, r *http.Request) {
	conversationID := chi.URLParam(r, "conversationID")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	reason := stringValue(body["error"], "")
	if reason == "" {
		http.Error(w, "error is required", http.StatusBadRequest)
		return
	}
	if err := h.Service.AbortConversation(r.Context(), conversationID, reason); err != nil {
		if errors.Is(err, service.ErrPendingNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
