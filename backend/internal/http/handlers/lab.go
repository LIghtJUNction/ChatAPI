package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/service"
	"github.com/zyf/chatapi/internal/store"
)

type LabHandler struct {
	Config  config.Config
	Store   store.Store
	Service *service.ChatAPIService
}

func (h LabHandler) Workspace(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListConversations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"mode":          h.Config.Mode,
		"conversations": items,
	})
}

func (h LabHandler) PingInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "use websocket on /api/ws",
	})
}

func (h LabHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	if requestID == "" {
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}
	item, err := h.Service.GetRequest(r.Context(), requestID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"request": item,
	})
}

func (h LabHandler) RequestDelta(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, service.TurnControlStreamDelta)
}

func (h LabHandler) RequestComplete(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, service.TurnControlStreamComplete)
}

func (h LabHandler) RequestAbort(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, service.TurnControlAbort)
}

func (h LabHandler) executeRequestTurnControl(w http.ResponseWriter, r *http.Request, kind service.TurnControlKind) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	if requestID == "" {
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}
	body := decodeBodyOrEmpty(r)
	command := service.TurnControlCommand{
		Kind:                kind,
		ResponseID:          stringValue(body["response_id"], ""),
		OutputText:          stringValue(body["text"], ""),
		Mode:                stringValue(body["mode"], "assistant_message"),
		ToolName:            stringValue(body["tool_name"], ""),
		ToolCallID:          stringValue(body["tool_call_id"], ""),
		ToolOutput:          stringValue(body["output"], stringValue(body["text"], "")),
		ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
		AbortReason:         stringValue(body["error"], ""),
	}
	result, err := h.Service.ExecuteTurnControlByRequestID(r.Context(), requestID, command)
	if err != nil {
		if err == store.ErrTurnConflict || err == service.ErrPendingConflict {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func decodeBodyOrEmpty(r *http.Request) map[string]any {
	if r.Body == nil {
		return map[string]any{}
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return map[string]any{}
	}
	return body
}
