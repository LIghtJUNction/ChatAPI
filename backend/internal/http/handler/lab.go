package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	"github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
	"go.uber.org/zap"
)

type LabHandler struct {
	Config  config.Config
	Query   *turnquery.Service
	Turn    *turnsvc.Service
	Control *controlsvc.Service
	Logger  *zap.Logger
}

func (h LabHandler) Workspace(w http.ResponseWriter, r *http.Request) {
	ownerID := actor.OwnerIDFromContext(r.Context())
	items, err := h.Query.ListConversationsForOwner(r.Context(), ownerID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	summaries := make([]workspacesvc.ConversationSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, workspacesvc.SummaryFromConversation(item))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"mode":          h.Config.Mode,
		"owner_id":      ownerID,
		"conversations": summaries,
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
	h.executeRequestTurnControl(w, r, turnsvc.TurnControlStreamDelta)
}

func (h LabHandler) RequestComplete(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, turnsvc.TurnControlStreamComplete)
}

func (h LabHandler) RequestAbort(w http.ResponseWriter, r *http.Request) {
	h.executeRequestTurnControl(w, r, turnsvc.TurnControlAbort)
}

func (h LabHandler) executeRequestTurnControl(w http.ResponseWriter, r *http.Request, kind turnsvc.TurnControlKind) {
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
	result, err := h.control().Execute(r.Context(), controlsvc.Command{
		OwnerID:        actor.OwnerIDFromContext(r.Context()),
		ConversationID: strings.TrimSpace(item.ConversationID),
		ResponseID:     stringValue(body["response_id"], ""),
		Action: turnsvc.OutputAction{
			Kind:                kind,
			OutputText:          stringValue(body["text"], ""),
			Mode:                stringValue(body["mode"], ""),
			ToolName:            stringValue(body["tool_name"], ""),
			ToolCallID:          stringValue(body["tool_call_id"], ""),
			ToolOutput:          stringValue(body["output"], ""),
			ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
			AbortReason:         stringValue(body["error"], ""),
		},
	})
	if err != nil {
		if writeControlError(w, err) {
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result.Body)
}

func (h LabHandler) control() *controlsvc.Service {
	if h.Control != nil {
		return h.Control
	}
	return controlsvc.New(h.Query, h.Turn, h.Logger)
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
