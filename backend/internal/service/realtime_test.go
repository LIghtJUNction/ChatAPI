package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/zyf/chatapi/internal/repository/migrations"
	sqlitestore "github.com/zyf/chatapi/internal/repository/sqlite"
)

func TestRealtimeHubDisconnectsSlowSubscribers(t *testing.T) {
	dataStore, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.DB().Close() })
	if err := migrations.Bootstrap(context.Background(), dataStore.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	hub := NewRealtimeHub(dataStore)
	sub, _, err := hub.Subscribe(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_slow",
		Kind:    RealtimeConnectionWebUI,
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	for i := 0; i < cap(sub.Events)+realtimeSlowDisconnectThreshold+1; i++ {
		hub.PublishConversationDelete("conv_slow")
	}

	stats := hub.Stats()
	if stats.Subscribers != 0 {
		t.Fatalf("expected slow subscriber to be disconnected: %#v", stats)
	}
	if stats.RecoverableDrops < realtimeSlowDisconnectThreshold || stats.SlowDisconnects != 1 {
		t.Fatalf("expected backpressure counters to increase: %#v", stats)
	}
	if stats.TotalConnections != 0 {
		t.Fatalf("expected slow subscriber lease to be released: %#v", stats)
	}
}

func TestRealtimeHubReservesWebUISlotPerOwner(t *testing.T) {
	dataStore, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.DB().Close() })
	if err := migrations.Bootstrap(context.Background(), dataStore.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	hub := NewRealtimeHub(dataStore, NewRealtimeLimits(0, 2, 1))
	apiLease, err := hub.Acquire(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_a",
		Kind:    RealtimeConnectionAPI,
	})
	if err != nil {
		t.Fatalf("first api connection should be accepted: %v", err)
	}
	defer apiLease.Release()
	if _, err := hub.Acquire(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_a",
		Kind:    RealtimeConnectionAPI,
	}); !errors.Is(err, ErrRealtimeConnectionLimitExceeded) {
		t.Fatalf("second api connection should be rejected to reserve WebUI slot: %v", err)
	}

	webUISub, _, err := hub.Subscribe(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_a",
		Kind:    RealtimeConnectionWebUI,
	})
	if err != nil {
		t.Fatalf("webui connection should use reserved slot: %v", err)
	}
	defer hub.Unsubscribe(webUISub)

	stats := hub.Stats()
	if stats.APIConnections != 1 || stats.WebUISubscribers != 1 || stats.TotalConnections != 2 {
		t.Fatalf("unexpected connection stats: %#v", stats)
	}
	if stats.RejectedConnections != 1 {
		t.Fatalf("expected rejected connection counter: %#v", stats)
	}
}

func TestRealtimeHubAllowsOtherOwnersWhenOneOwnerHitsLimit(t *testing.T) {
	dataStore, err := sqlitestore.Open(filepath.Join(t.TempDir(), "chatapi.sqlite3"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.DB().Close() })
	if err := migrations.Bootstrap(context.Background(), dataStore.DB()); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	hub := NewRealtimeHub(dataStore, NewRealtimeLimits(0, 1, 1))
	ownerA, _, err := hub.Subscribe(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_a",
		Kind:    RealtimeConnectionWebUI,
	})
	if err != nil {
		t.Fatalf("owner a webui connection should be accepted: %v", err)
	}
	defer hub.Unsubscribe(ownerA)
	ownerB, _, err := hub.Subscribe(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_b",
		Kind:    RealtimeConnectionWebUI,
	})
	if err != nil {
		t.Fatalf("owner b webui connection should be accepted: %v", err)
	}
	defer hub.Unsubscribe(ownerB)

	if _, err := hub.Acquire(context.Background(), RealtimeSubscribeOptions{
		OwnerID: "owner_a",
		Kind:    RealtimeConnectionSSE,
	}); !errors.Is(err, ErrRealtimeConnectionLimitExceeded) {
		t.Fatalf("owner a extra connection should be rejected: %v", err)
	}
	stats := hub.Stats()
	if stats.WebUISubscribers != 2 || stats.TotalConnections != 2 || stats.RejectedConnections != 1 {
		t.Fatalf("unexpected multi-owner stats: %#v", stats)
	}
}
