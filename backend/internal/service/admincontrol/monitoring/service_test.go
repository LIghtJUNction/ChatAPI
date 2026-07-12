package monitoring

import (
	"context"
	"testing"
	"time"

	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
)

type presenceStub struct {
	snapshot workspacesvc.PresenceSnapshot
	events   chan workspacesvc.PresenceEvent
}

func (s *presenceStub) PresenceSnapshot([]string) workspacesvc.PresenceSnapshot { return s.snapshot }
func (s *presenceStub) SubscribePresence([]string) (<-chan workspacesvc.PresenceEvent, func()) {
	return s.events, func() {}
}

func TestStreamSendsSnapshotAndConnectionDelta(t *testing.T) {
	presence := &presenceStub{
		snapshot: workspacesvc.PresenceSnapshot{UserConnections: map[string]int{"user-1": 1}, TotalConnections: 1, Sequence: 4},
		events:   make(chan workspacesvc.PresenceEvent, 1),
	}
	service := New(presence)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := service.Stream(ctx, []string{"user-1"})
	snapshot := <-events
	if snapshot.Type != "monitor.snapshot" || snapshot.TotalConnections != 1 || snapshot.UserConnections["user-1"] != 1 || snapshot.Metrics == nil || snapshot.Sequence != 4 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	presence.events <- workspacesvc.PresenceEvent{UserID: "user-1", ConnectionCount: 2, TotalConnections: 2, Sequence: 5}
	select {
	case event := <-events:
		if event.Type != "user.connection.updated" || event.ConnectionCount != 2 || event.TotalConnections != 2 || event.Sequence != 5 {
			t.Fatalf("unexpected delta: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for connection delta")
	}
}
