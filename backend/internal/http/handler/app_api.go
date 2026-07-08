package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zyf/chatapi/internal/http/httpx"
	"github.com/zyf/chatapi/internal/ops/observability/logging"
	appkey "github.com/zyf/chatapi/internal/service/auth/authz/appkey"
	turnsvc "github.com/zyf/chatapi/internal/service/chat/turn"
	turnquerysvc "github.com/zyf/chatapi/internal/service/chat/turnquery"
)

type AppAPIHandler struct {
	Turn   *turnsvc.Service
	Query  *turnquerysvc.Service
	Logger *zap.Logger
}

func (h AppAPIHandler) ListRequests(w http.ResponseWriter, r *http.Request) {
	principal, ok := appkey.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Query.ListRequestsForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logging.BindContext(h.Logger, r.Context(), zap.Int("requests.count", len(items))).Debug("listed requests for owner")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) GetRequest(w http.ResponseWriter, r *http.Request) {
	principal, ok := appkey.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	requestID := strings.TrimSpace(chi.URLParam(r, "requestID"))
	item, err := h.Query.GetRequestForOwner(r.Context(), requestID, principal.UserID)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquerysvc.ErrForbidden) {
			status = http.StatusForbidden
		}
		logging.BindContext(h.Logger, r.Context(),
			zap.String("request.id", requestID),
			zap.Int("http.status_code", status),
		).Warn("request lookup failed")
		http.Error(w, err.Error(), status)
		return
	}
	logging.BindContext(h.Logger, r.Context(), zap.String("request.id", requestID)).Debug("fetched request for owner")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "request": item})
}

func (h AppAPIHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	principal, ok := appkey.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	items, err := h.Query.ListConversationsForOwner(r.Context(), principal.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	logging.BindContext(h.Logger, r.Context(), zap.Int("conversations.count", len(items))).Debug("listed conversations for owner")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h AppAPIHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	principal, ok := appkey.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "app api key unauthorized", http.StatusUnauthorized)
		return
	}
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Query.ListMessagesForOwner(r.Context(), conversationID, principal.UserID)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, turnquerysvc.ErrForbidden) {
			status = http.StatusForbidden
		}
		logging.BindContext(h.Logger, r.Context(),
			zap.String("conversation.id", conversationID),
			zap.Int("http.status_code", status),
		).Warn("conversation message lookup failed")
		http.Error(w, err.Error(), status)
		return
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.String("conversation.id", conversationID),
		zap.Int("messages.count", len(items)),
	).Debug("listed conversation messages for owner")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}
