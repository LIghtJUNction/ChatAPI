package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/zyf2007/ChatAPI/internal/actor"
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
		http.Error(w, "workspace hub unavailable", http.StatusServiceUnavailable)
		return
	}
	ownerID := actor.OwnerIDFromContext(r.Context())
	if ownerID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := workspaceUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sendMu := make(chan struct{}, 1)
	sendMu <- struct{}{}
	wsConn := workspacesvc.NewConnection(func(payload any) {
		select {
		case <-sendMu:
			defer func() { sendMu <- struct{}{} }()
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(payload)
		default:
		}
	})

	h.Hub.Register(ownerID, wsConn)
	defer func() {
		h.Hub.Unregister(ownerID, wsConn)
		h.Hub.PublishConnectionCount(ownerID)
	}()

	snapshot, err := h.Hub.Snapshot(r.Context(), ownerID)
	if err == nil {
		_ = conn.WriteJSON(snapshot)
	}
	h.Hub.PublishConnectionCount(ownerID)

	for {
		if _, data, err := conn.ReadMessage(); err != nil {
			return
		} else if len(data) > 0 {
			var payload map[string]any
			if json.Unmarshal(data, &payload) == nil {
				if stringValue(payload["type"], "") == "ping" {
					_ = conn.WriteJSON(map[string]any{"type": "ping"})
				}
			}
		}
	}
}
