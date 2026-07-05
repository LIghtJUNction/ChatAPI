package handlers

import (
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/zyf/chatapi/internal/service"
)

type RealtimeHandler struct {
	Hub *service.RealtimeHub
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (h RealtimeHandler) WebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub, snapshot, err := h.Hub.Subscribe(r.Context())
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "disconnect", "reason": err.Error()})
		return
	}
	defer h.Hub.Unsubscribe(sub)

	if err := conn.WriteJSON(snapshot); err != nil {
		return
	}

	pingTicker := time.NewTicker(20 * time.Second)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-sub.Events:
			if !ok {
				return
			}
			if err := conn.WriteJSON(event); err != nil {
				return
			}
		case <-pingTicker.C:
			if err := conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
				return
			}
		}
	}
}
