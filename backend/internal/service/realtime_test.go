package service

import (
	"context"
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
	sub, _, err := hub.Subscribe(context.Background())
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
}
