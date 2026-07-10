package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

type serialTurn struct {
	mu        sync.Mutex
	active    int
	maxActive int
	failText  string
	entered   chan struct{}
}

func (t *serialTurn) ActiveRequestID(string) (string, bool) { return "req", true }

func (t *serialTurn) ExecuteTurnControl(_ context.Context, command turnsvc.TurnControlCommand) (map[string]any, error) {
	if command.Action.OutputText == t.failText {
		return nil, errors.New("rejected")
	}
	t.mu.Lock()
	t.active++
	if t.entered != nil {
		close(t.entered)
		t.entered = nil
	}
	if t.active > t.maxActive {
		t.maxActive = t.active
	}
	t.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	t.mu.Lock()
	t.active--
	t.mu.Unlock()
	return map[string]any{"ok": true}, nil
}

type countObserver struct {
	mu    sync.Mutex
	count int
}

func (o *countObserver) ControlApplied(context.Context, AppliedCommand) {
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
}

func TestServiceSerializesConversationCommandsAndObservesOnlySuccess(t *testing.T) {
	turn := &serialTurn{failText: "fail"}
	observer := &countObserver{}
	service := New(nil, turn, nil)
	service.Subscribe(observer)

	var wg sync.WaitGroup
	for index := 0; index < 8; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Execute(context.Background(), Command{
				ConversationID: "conv", Source: SourceAutomation,
				Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "ok"},
			})
			if err != nil {
				t.Errorf("execute: %v", err)
			}
		}()
	}
	wg.Wait()
	if turn.maxActive != 1 {
		t.Fatalf("commands were not serialized: max active=%d", turn.maxActive)
	}
	if _, err := service.Execute(context.Background(), Command{
		ConversationID: "conv", Source: SourceWorkspace,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "fail"},
	}); err == nil {
		t.Fatal("expected failed command")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if observer.count != 8 {
		t.Fatalf("failed command was observed as applied: count=%d", observer.count)
	}
}

func TestSynchronizeWaitsForAppliedCommand(t *testing.T) {
	entered := make(chan struct{})
	turn := &serialTurn{entered: entered}
	service := New(nil, turn, nil)
	observer := &countObserver{}
	service.Subscribe(observer)
	go func() {
		_, _ = service.Execute(context.Background(), Command{
			ConversationID: "conv", Source: SourceWorkspace,
			Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamDelta, OutputText: "ok"},
		})
	}()
	<-entered
	if err := service.Synchronize(context.Background(), "conv", func() error {
		observer.mu.Lock()
		defer observer.mu.Unlock()
		if observer.count != 1 {
			return errors.New("barrier ran before earlier command completed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
