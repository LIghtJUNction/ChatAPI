package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf/chatapi/internal/actor"
	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/http/httpx"
	"github.com/zyf/chatapi/internal/repository/common"
	"github.com/zyf/chatapi/internal/service/chat/turn"
	"github.com/zyf/chatapi/internal/service/chat/turnquery"
	"go.uber.org/zap"
)

type LabHandler struct {
	Config config.Config
	Query  *turnquery.Service
	Turn   *turn.Service
	Logger *zap.Logger
}

func (h LabHandler) Workspace(w http.ResponseWriter, r *http.Request) {
	ownerID := actor.OwnerIDFromContext(r.Context())
	items, err := h.Query.ListConversationsForOwner(r.Context(), ownerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"mode":          h.Config.Mode,
		"owner_id":      ownerID,
		"conversations": items,
	})
}

func (h LabHandler) PingInfo(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "lab mode active",
	})
}

func (h LabHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	items, err := h.Query.ListRequestsForOwner(r.Context(), actor.OwnerIDFromContext(r.Context()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h LabHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Query.GetRequestForOwner(r.Context(), requestID, actor.OwnerIDFromContext(r.Context()))
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquery.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "request": item})
}

func (h LabHandler) CopyRequestCurl(w http.ResponseWriter, r *http.Request) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Query.GetRequestForOwner(r.Context(), requestID, actor.OwnerIDFromContext(r.Context()))
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquery.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"request_id": requestID,
		"curl":       httpx.BuildReplayCurl(httpx.RequestBaseURL(r), item),
	})
}

func (h LabHandler) RequestDelta(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, turn.TurnControlStreamDelta)
}

func (h LabHandler) RequestComplete(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, turn.TurnControlStreamComplete)
}

func (h LabHandler) RequestAbort(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, turn.TurnControlAbort)
}

func (h LabHandler) executeRequestTurnControl(w http.ResponseWriter, r *http.Request, kind turn.TurnControlKind) {
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	if requestID == "" {
		http.Error(w, "request_id is required", http.StatusBadRequest)
		return
	}
	item, err := h.Query.GetRequestForOwner(r.Context(), requestID, actor.OwnerIDFromContext(r.Context()))
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquery.ErrForbidden) {
			status = http.StatusForbidden
		}
		http.Error(w, err.Error(), status)
		return
	}
	body := decodeBodyOrEmpty(r)
	result, err := h.Turn.ExecuteTurnControl(r.Context(), turn.TurnControlCommand{
		Kind:                kind,
		ConversationID:      strings.TrimSpace(item.ConversationID),
		ResponseID:          stringValue(body["response_id"], ""),
		OutputText:          stringValue(body["text"], ""),
		Mode:                stringValue(body["mode"], "assistant_message"),
		ToolName:            stringValue(body["tool_name"], ""),
		ToolCallID:          stringValue(body["tool_call_id"], ""),
		ToolOutput:          stringValue(body["output"], stringValue(body["text"], "")),
		ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
		AbortReason:         stringValue(body["error"], ""),
	})
	if err != nil {
		switch {
		case errors.Is(err, common.ErrTurnConflict):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func decodeBodyOrEmpty(r *http.Request) map[string]any {
	if r == nil || r.Body == nil {
		return map[string]any{}
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return map[string]any{}
	}
	return body
}
