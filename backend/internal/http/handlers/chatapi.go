package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/protocol"
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

	parsed := protocol.ParseRequest(requestFormat, body)
	if parsed.Stream {
		h.handleStreamRequest(w, r, requestFormat, body)
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

func (h ChatAPIHandler) RespondConversation(w http.ResponseWriter, r *http.Request) {
	h.completeOutput(w, r, conversationIDFromPath(r))
}

func (h ChatAPIHandler) CompleteOutput(w http.ResponseWriter, r *http.Request) {
	h.completeOutput(w, r, "")
}

func (h ChatAPIHandler) completeOutput(w http.ResponseWriter, r *http.Request, conversationIDFromRoute string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	conversationID, err := mustConversationID(body, conversationIDFromRoute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	input := store.CompletePendingInput{
		ConversationID:      conversationID,
		ResponseID:          stringValue(body["response_id"], ""),
		OutputText:          stringValue(body["text"], ""),
		Mode:                stringValue(body["mode"], "assistant_message"),
		ToolName:            stringValue(body["tool_name"], ""),
		ToolCallID:          stringValue(body["tool_call_id"], ""),
		ToolOutput:          stringValue(body["output"], stringValue(body["text"], "")),
		ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
	}

	result, err := h.Service.CompleteConversation(r.Context(), input)
	if err != nil {
		if errors.Is(err, service.ErrPendingConflict) || errors.Is(err, store.ErrTurnConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrPendingNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h ChatAPIHandler) StreamDeltaConversation(w http.ResponseWriter, r *http.Request) {
	h.deltaOutput(w, r, conversationIDFromPath(r))
}

func (h ChatAPIHandler) DeltaOutput(w http.ResponseWriter, r *http.Request) {
	h.deltaOutput(w, r, "")
}

func (h ChatAPIHandler) deltaOutput(w http.ResponseWriter, r *http.Request, conversationIDFromRoute string) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	conversationID, err := mustConversationID(body, conversationIDFromRoute)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := h.Service.UpdateDraft(r.Context(), conversationID, stringValue(body["text"], ""))
	if err != nil {
		if errors.Is(err, service.ErrPendingConflict) || errors.Is(err, store.ErrTurnConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if errors.Is(err, service.ErrPendingNotFound) {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
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
		if errors.Is(err, service.ErrPendingConflict) || errors.Is(err, store.ErrTurnConflict) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
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

func conversationIDFromPath(r *http.Request) string {
	return strings.TrimSpace(chi.URLParam(r, "conversationID"))
}

func mustConversationID(input map[string]any, fromRoute string) (string, error) {
	if strings.TrimSpace(fromRoute) != "" {
		return strings.TrimSpace(fromRoute), nil
	}
	return service.MustConversationID(input)
}

func (h ChatAPIHandler) handleStreamRequest(w http.ResponseWriter, r *http.Request, requestFormat string, body map[string]any) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	turn, conversation, err := h.Service.CreatePendingStream(r.Context(), requestFormat, body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	anthropicBlockStarted := false

	for _, event := range protocol.BuildStreamStart(conversation) {
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
			streamEvents, nextAnthropicBlockStarted := buildPendingStreamEvents(conversation, event, anthropicBlockStarted)
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

func buildPendingStreamEvents(conversation store.Conversation, event service.PendingEvent, anthropicBlockStarted bool) ([]protocol.StreamEvent, bool) {
	format := stringValue(conversation.Metadata["request_format"], "responses")
	if format == "anthropic_messages" {
		payload := protocol.CompletePayload{
			ResponseID: stringValue(conversation.ResponseID, ""),
			OutputText: event.OutputText,
			Mode:       event.Mode,
			ToolName:   event.ToolName,
			ToolCallID: event.ToolCallID,
			ToolOutput: event.ToolOutput,
		}
		switch event.Type {
		case "delta":
			if !anthropicBlockStarted {
				return append([]protocol.StreamEvent{protocol.BuildAnthropicContentBlockStart(payload)}, protocol.BuildStreamDelta(conversation, event.DeltaText)...), true
			}
			return protocol.BuildStreamDelta(conversation, event.DeltaText), true
		case "complete":
			streamEvents := make([]protocol.StreamEvent, 0, 4)
			if !anthropicBlockStarted {
				streamEvents = append(streamEvents, protocol.BuildAnthropicContentBlockStart(payload))
			}
			streamEvents = append(streamEvents, protocol.BuildStreamComplete(conversation, payload)...)
			return streamEvents, true
		case "abort":
			return protocol.BuildStreamAbort(conversation, event.ErrorBody), anthropicBlockStarted
		default:
			return nil, anthropicBlockStarted
		}
	}

	switch event.Type {
	case "delta":
		return protocol.BuildStreamDelta(conversation, event.DeltaText), anthropicBlockStarted
	case "abort":
		return protocol.BuildStreamAbort(conversation, event.ErrorBody), anthropicBlockStarted
	case "complete":
		return protocol.BuildStreamComplete(conversation, protocol.CompletePayload{
			ResponseID: stringValue(conversation.ResponseID, ""),
			OutputText: event.OutputText,
			Mode:       event.Mode,
			ToolName:   event.ToolName,
			ToolCallID: event.ToolCallID,
			ToolOutput: event.ToolOutput,
		}), anthropicBlockStarted
	default:
		return nil, anthropicBlockStarted
	}
}

func writeSSEEvent(w http.ResponseWriter, event protocol.StreamEvent) error {
	if event.Event != "" {
		if _, err := w.Write([]byte("event: " + event.Event + "\n")); err != nil {
			return err
		}
	}

	switch data := event.Data.(type) {
	case string:
		if _, err := w.Write([]byte("data: " + data + "\n\n")); err != nil {
			return err
		}
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte("data: " + string(raw) + "\n\n")); err != nil {
			return err
		}
	}
	return nil
}
