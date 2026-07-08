package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	catalogsvc "github.com/zyf2007/ChatAPI/internal/service/chat/catalog"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	streamingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/streaming"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

type ChatAPIHandler struct {
	Turn      *turnsvc.Service
	Query     *turnquerysvc.Service
	Ingress   *ingresssvc.Service
	Streaming *streamingsvc.Service
	Catalog   *catalogsvc.Service
	Logger    *zap.Logger
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

func (h ChatAPIHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	principal, ok := modelkey.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" || h.Catalog == nil {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]any{
				"message": "unauthorized",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	items, err := h.Catalog.ListModelsForPrincipal(r.Context())
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("owner.id", principal.UserID)).Error("list models failed", zap.Error(err))
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{
				"message": "internal server error",
				"type":    "server_error",
			},
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

func (h ChatAPIHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Query.ListMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h ChatAPIHandler) DeltaOutput(w http.ResponseWriter, r *http.Request) {
	h.executeTurnControl(w, r, turnsvc.TurnControlStreamDelta)
}

func (h ChatAPIHandler) CompleteOutput(w http.ResponseWriter, r *http.Request) {
	h.executeTurnControl(w, r, turnsvc.TurnControlStreamComplete)
}

func (h ChatAPIHandler) AbortOutput(w http.ResponseWriter, r *http.Request) {
	h.executeTurnControl(w, r, turnsvc.TurnControlAbort)
}

func (h ChatAPIHandler) handleProtocolRequest(w http.ResponseWriter, r *http.Request, requestFormat string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("protocol", requestFormat)).Warn("invalid protocol request json")
		httpx.WriteJSON(w, http.StatusBadRequest, protocol.InvalidJSONError(requestFormat))
		return
	}
	requestMeta := httpx.CaptureRequestMeta(r)
	parsedReq, err := h.Ingress.Parse(r.Context(), ownerIDForPreprocess(r.Context()), requestFormat, body, requestMeta)
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("protocol", requestFormat)).Warn("protocol ingress failed", zap.Error(err))
		httpx.WriteJSON(w, protocol.HTTPStatus(err), protocol.BuildErrorBody(requestFormat, err))
		return
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.String("protocol", requestFormat),
		zap.String("model", parsedReq.Request.Model),
		zap.Bool("stream", parsedReq.Request.Stream),
	).Info("accepted protocol request")
	if parsedReq.Request.Stream {
		h.handleStreamRequest(w, r, parsedReq)
		return
	}

	responseBody, err := h.Ingress.SubmitResponse(r.Context(), parsedReq)
	if err != nil {
		requestErr := protocol.InternalError(err.Error())
		logging.BindContext(h.Logger, r.Context(), zap.String("protocol", requestFormat)).Error("create pending response failed", zap.Error(err))
		httpx.WriteJSON(w, protocol.HTTPStatus(requestErr), protocol.BuildErrorBody(requestFormat, requestErr))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, responseBody)
}

func (h ChatAPIHandler) handleStreamRequest(w http.ResponseWriter, r *http.Request, parsed ingresssvc.ParsedRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		logging.BindContext(h.Logger, r.Context()).Error("streaming not supported by response writer")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	turn, conversation, err := h.Ingress.SubmitStream(r.Context(), parsed)
	if err != nil {
		requestErr := protocol.InternalError(err.Error())
		logging.BindContext(h.Logger, r.Context(),
			zap.String("protocol", parsed.Request.Protocol.String()),
			zap.Error(err),
		).Error("create pending stream failed")
		httpx.WriteJSON(w, protocol.HTTPStatus(requestErr), protocol.BuildErrorBody(parsed.Request.Protocol.String(), requestErr))
		return
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
	).Info("stream pending turn created")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	anthropicBlockStarted := false
	for _, event := range h.Streaming.BuildStartEvents(conversation) {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-turn.Events:
			if !ok {
				return
			}
			streamEvents, nextAnthropicBlockStarted := h.Streaming.BuildPendingEvents(conversation, event, anthropicBlockStarted)
			anthropicBlockStarted = nextAnthropicBlockStarted
			for _, streamEvent := range streamEvents {
				if err := writeSSEEvent(w, streamEvent); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func (h ChatAPIHandler) executeTurnControl(w http.ResponseWriter, r *http.Request, kind turnsvc.TurnControlKind) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("turn.control", string(kind))).Warn("invalid turn control json")
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	command, err := buildTurnControlCommand(kind, body)
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("turn.control", string(kind))).Warn("invalid turn control command")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if principal, ok := appkey.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		if _, err := h.Query.ListMessagesForOwner(r.Context(), command.ConversationID, principal.UserID); err != nil {
			status := http.StatusNotFound
			if errors.Is(err, turnquerysvc.ErrForbidden) {
				status = http.StatusForbidden
			}
			logging.BindContext(h.Logger, r.Context(),
				zap.String("turn.control", string(kind)),
				zap.String("conversation.id", command.ConversationID),
				zap.Int("http.status_code", status),
			).Warn("turn control owner check failed")
			http.Error(w, err.Error(), status)
			return
		}
	}
	if principal, ok := session.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		if _, err := h.Query.ListMessagesForOwner(r.Context(), command.ConversationID, principal.UserID); err != nil {
			status := http.StatusNotFound
			if errors.Is(err, turnquerysvc.ErrForbidden) {
				status = http.StatusForbidden
			}
			logging.BindContext(h.Logger, r.Context(),
				zap.String("turn.control", string(kind)),
				zap.String("conversation.id", command.ConversationID),
				zap.Int("http.status_code", status),
			).Warn("session turn control owner check failed")
			http.Error(w, err.Error(), status)
			return
		}
	}
	result, err := h.Turn.ExecuteTurnControl(r.Context(), command)
	if err != nil {
		switch {
		case errors.Is(err, pendingsvc.ErrPendingConflict), errors.Is(err, common.ErrTurnConflict):
			logging.BindContext(h.Logger, r.Context(),
				zap.String("turn.control", string(kind)),
				zap.String("conversation.id", command.ConversationID),
			).Warn("turn control conflict")
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, pendingsvc.ErrPendingNotFound):
			logging.BindContext(h.Logger, r.Context(),
				zap.String("turn.control", string(kind)),
				zap.String("conversation.id", command.ConversationID),
			).Warn("turn control target not found")
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			logging.BindContext(h.Logger, r.Context(),
				zap.String("turn.control", string(kind)),
				zap.String("conversation.id", command.ConversationID),
			).Error("turn control failed", zap.Error(err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.String("turn.control", string(kind)),
		zap.String("conversation.id", command.ConversationID),
	).Info("turn control executed")
	httpx.WriteJSON(w, http.StatusOK, result)
}

func buildTurnControlCommand(kind turnsvc.TurnControlKind, body map[string]any) (turnsvc.TurnControlCommand, error) {
	conversationID, err := mustConversationID(body)
	if err != nil {
		return turnsvc.TurnControlCommand{}, err
	}
	return turnsvc.TurnControlCommand{
		Kind:                kind,
		ConversationID:      conversationID,
		ResponseID:          stringValue(body["response_id"], ""),
		OutputText:          stringValue(body["text"], ""),
		Mode:                stringValue(body["mode"], "assistant_message"),
		ToolName:            stringValue(body["tool_name"], ""),
		ToolCallID:          stringValue(body["tool_call_id"], ""),
		ToolOutput:          stringValue(body["output"], stringValue(body["text"], "")),
		ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
		AbortReason:         stringValue(body["error"], ""),
	}, nil
}

func ownerIDForPreprocess(ctx context.Context) string {
	if principal, ok := appkey.PrincipalFromContext(ctx); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID)
	}
	if principal, ok := session.PrincipalFromContext(ctx); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID)
	}
	return ""
}

func writeSSEEvent(w http.ResponseWriter, event protocol.StreamEvent) error {
	if event.Event != "" {
		if _, err := w.Write([]byte("event: " + event.Event + "\n")); err != nil {
			return err
		}
	}
	switch data := event.Data.(type) {
	case string:
		_, err := w.Write([]byte("data: " + data + "\n\n"))
		return err
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("data: " + string(raw) + "\n\n"))
		return err
	}
}

func mustConversationID(input map[string]any) (string, error) {
	conversationID, ok := input["conversation_id"].(string)
	if !ok || strings.TrimSpace(conversationID) == "" {
		return "", errors.New("conversation_id is required")
	}
	return strings.TrimSpace(conversationID), nil
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
