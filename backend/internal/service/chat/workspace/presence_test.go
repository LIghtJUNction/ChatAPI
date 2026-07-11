package workspace

import (
	"testing"
	"time"
)

func TestHubPresenceSnapshotAndDeltas(t *testing.T) {
	hub := NewHub(nil)
	events, unsubscribe := hub.SubscribePresence([]string{"user-1"})
	defer unsubscribe()
	connection := NewConnection(func(any) {})
	if count := hub.Register("user-1", connection); count != 1 {
		t.Fatalf("register count=%d", count)
	}
	select {
	case event := <-events:
		if event.UserID != "user-1" || event.ConnectionCount != 1 || event.TotalConnections != 1 || event.Sequence != 1 {
			t.Fatalf("unexpected register event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for register event")
	}
	snapshot := hub.PresenceSnapshot([]string{"user-1"})
	if snapshot.TotalConnections != 1 || snapshot.UserConnections["user-1"] != 1 || snapshot.Sequence != 1 {
		t.Fatalf("unexpected snapshot: %#v", snapshot)
	}
	if count := hub.Unregister("user-1", connection); count != 0 {
		t.Fatalf("unregister count=%d", count)
	}
	select {
	case event := <-events:
		if event.ConnectionCount != 0 || event.TotalConnections != 0 || event.Sequence != 2 {
			t.Fatalf("unexpected unregister event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unregister event")
	}
}

func TestHubPresenceSubscriptionFiltersOtherUsers(t *testing.T) {
	hub := NewHub(nil)
	events, unsubscribe := hub.SubscribePresence([]string{"watched"})
	defer unsubscribe()
	hub.Register("other", NewConnection(func(any) {}))
	select {
	case event := <-events:
		t.Fatalf("received event for unrequested user: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	snapshot := hub.PresenceSnapshot([]string{"watched"})
	if len(snapshot.UserConnections) != 1 || snapshot.UserConnections["watched"] != 0 || snapshot.TotalConnections != 1 {
		t.Fatalf("unexpected filtered snapshot: %#v", snapshot)
	}
}
