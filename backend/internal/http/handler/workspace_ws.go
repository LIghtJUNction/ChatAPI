package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
	"go.uber.org/zap"
)

type WorkspaceHandler struct {
	Hub    *workspacesvc.Hub
	Logger *zap.Logger
}

var workspaceUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h WorkspaceHandler) ServeWS(w http.ResponseWriter, r *http.Request) {
	if h.Hub == nil {
		logging.BindContext(h.Logger, r.Context()).Warn("workspace websocket rejected", zap.String("reason", "hub_unavailable"))
		http.Error(w, "workspace hub unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerID := actor.OwnerIDFromContext(r.Context())
	if ownerID == "" {
		logging.BindContext(h.Logger, r.Context()).Warn("workspace websocket rejected", zap.String("reason", "unauthorized"))
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ctx := logging.WithConnectionID(r.Context(), logging.NewConnectionID("ws"))
	logger := logging.BindContext(h.Logger, ctx,
		zap.String("owner.id", ownerID),
		zap.String("transport", "ws"),
		zap.String("http.path", r.URL.Path),
		zap.String("http.remote_addr", r.RemoteAddr),
	)
	logger.Debug("workspace websocket upgrade starting")
	conn, err := workspaceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Warn("workspace websocket upgrade failed", zap.Error(err))
		return
	}
	connectedAt := time.Now()
	disconnectReason := "handler_returned"
	logger.Info("workspace websocket connected")
	defer conn.Close()
	stopShutdownWatch := closeWebSocketOnContext(r.Context(), conn)
	defer stopShutdownWatch()

	wsConn := workspacesvc.NewConnection(func(payload any) {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(payload); err != nil {
			logger.Debug("workspace websocket outbound send failed",
				zap.String("message.type", workspacePayloadType(payload)),
				zap.Error(err),
			)
			return
		}
		logger.Debug("workspace websocket outbound sent",
			zap.String("message.type", workspacePayloadType(payload)),
		)
	})

	connectionCount, registerErr := h.Hub.TryRegister(r.Context(), ownerID, wsConn)
	if registerErr != nil {
		disconnectReason = "connection_limit"
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, registerErr.Error()), time.Now().Add(time.Second))
		logger.Warn("workspace websocket registration rejected", zap.Error(registerErr))
		return
	}
	logger.Debug("workspace websocket registered", zap.Int("workspace.connection_count", connectionCount))
	defer func() {
		connectionCount := h.Hub.Unregister(ownerID, wsConn)
		logger.Info("workspace websocket disconnected",
			zap.Int("workspace.connection_count", connectionCount),
			zap.Duration("connection.duration", time.Since(connectedAt)),
			zap.String("connection.end_reason", disconnectReason),
		)
		h.Hub.PublishConnectionCount(ownerID)
	}()

	snapshot, err := h.Hub.Snapshot(r.Context(), ownerID)
	if err != nil {
		logger.Debug("workspace websocket snapshot skipped", zap.Error(err))
	} else {
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := conn.WriteJSON(snapshot); err != nil {
			disconnectReason = "snapshot_write_failed"
			logger.Warn("workspace websocket snapshot send failed", zap.Error(err))
			return
		}
		logger.Debug("workspace websocket snapshot sent", zap.Int("conversations.count", len(snapshot.Conversations)))
	}
	h.Hub.PublishConnectionCount(ownerID)
	logger.Debug("workspace websocket connection count published")

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if r.Context().Err() != nil {
				disconnectReason = "server_shutdown"
			} else {
				disconnectReason = "read_stopped"
			}
			logger.Debug("workspace websocket read stopped", zap.Error(err))
			return
		}
		if len(data) == 0 {
			logger.Debug("workspace websocket inbound ignored", zap.Int("ws.message_type", messageType), zap.String("reason", "empty_payload"))
			continue
		}

		logger.Debug("workspace websocket inbound received",
			zap.Int("ws.message_type", messageType),
			zap.Int("ws.payload_bytes", len(data)),
		)

		var payload map[string]any
		if err := json.Unmarshal(data, &payload); err != nil {
			logger.Debug("workspace websocket inbound invalid json",
				zap.Int("ws.payload_bytes", len(data)),
				zap.Error(err),
			)
			continue
		}

		msg, parseErr := workspacesvc.ParseClientMessage(payload)
		if parseErr != nil {
			logger.Debug("workspace websocket inbound unsupported message",
				zap.String("message.type", strings.TrimSpace(stringValue(payload["type"], ""))),
				zap.Error(parseErr),
			)
			continue
		}

		logger.Debug("workspace websocket inbound parsed",
			zap.String("message.type", msg.Type),
			zap.String("conversation.id", strings.TrimSpace(msg.ConversationID)),
		)
		if err := h.Hub.HandleClientMessage(ctx, ownerID, wsConn, msg); err != nil {
			logger.Debug("workspace websocket inbound handle failed",
				zap.String("message.type", msg.Type),
				zap.String("conversation.id", strings.TrimSpace(msg.ConversationID)),
				zap.Error(err),
			)
			continue
		}
		logger.Debug("workspace websocket inbound handled",
			zap.String("message.type", msg.Type),
			zap.String("conversation.id", strings.TrimSpace(msg.ConversationID)),
		)
	}
}

func closeWebSocketOnContext(ctx context.Context, conn *websocket.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
				time.Now().Add(time.Second),
			)
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func workspacePayloadType(payload any) string {
	item, ok := payload.(map[string]any)
	if !ok {
		return ""
	}
	return strings.TrimSpace(stringValue(item["type"], ""))
}
