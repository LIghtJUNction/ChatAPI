package streaming

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type Service struct{}

func New() *Service {
	return &Service{}
}

func (s *Service) StreamPendingTurn(ctx context.Context, w http.ResponseWriter, turn *turnsvc.PendingTurn) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return http.ErrNotSupported
	}
	if turn == nil {
		return errors.New("nil pending turn")
	}

	for _, event := range startEvents(turn) {
		if err := writeSSEEvent(w, event); err != nil {
			return err
		}
		flusher.Flush()
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-turn.Events:
			if !ok {
				return nil
			}
			for _, streamEvent := range event.StreamEvents {
				if err := writeSSEEvent(w, streamEvent); err != nil {
					return err
				}
				flusher.Flush()
			}
		}
	}
}

func startEvents(turn *turnsvc.PendingTurn) []protocol.StreamEvent {
	if turn == nil || turn.Runtime == nil {
		return nil
	}
	return turn.Runtime.Start()
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
