package workspace

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type pagedConversationQueryStub struct{ items []common.Conversation }

func (s *pagedConversationQueryStub) ListConversationsForOwnerPage(_ context.Context, ownerID string, before time.Time, beforeID string, limit int) ([]common.Conversation, error) {
	items := make([]common.Conversation, 0, limit)
	for _, item := range s.items {
		itemOwner, _ := item.Metadata["owner_id"].(string)
		if itemOwner != ownerID {
			continue
		}
		if beforeID != "" && !(item.UpdatedAt.Before(before) || (item.UpdatedAt.Equal(before) && item.ID < beforeID)) {
			continue
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func TestConversationPagesUseOwnerScopedOpaqueCursor(t *testing.T) {
	base := time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	items := make([]common.Conversation, 0, 36)
	for index := 0; index < 35; index++ {
		items = append(items, common.Conversation{
			ID:        "conversation-" + twoDigits(index),
			Title:     "conversation",
			UpdatedAt: base.Add(-time.Duration(index) * time.Minute),
			Metadata:  map[string]any{"owner_id": "owner-a"},
		})
	}
	items = append(items, common.Conversation{ID: "other-owner", UpdatedAt: base.Add(time.Hour), Metadata: map[string]any{"owner_id": "owner-b"}})
	service := New(&pagedConversationQueryStub{items: items}, nil, nil)

	snapshot, err := service.Snapshot(context.Background(), "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Conversations) != conversationPageSize || !snapshot.HasMore || snapshot.NextCursor == "" {
		t.Fatalf("unexpected first page: %#v", snapshot)
	}
	page, err := service.ConversationPage(context.Background(), "owner-a", "page-1", snapshot.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if page.CommandID != "page-1" || len(page.Conversations) != 5 || page.HasMore || page.NextCursor != "" {
		t.Fatalf("unexpected second page: %#v", page)
	}
	seen := map[string]bool{}
	for _, item := range append(snapshot.Conversations, page.Conversations...) {
		if seen[item.ID] || item.ID == "other-owner" {
			t.Fatalf("duplicate or foreign conversation in pages: %q", item.ID)
		}
		seen[item.ID] = true
	}
	if len(seen) != 35 {
		t.Fatalf("expected all owner conversations, got %d", len(seen))
	}
	if _, err := service.ConversationPage(context.Background(), "owner-b", "page-2", snapshot.NextCursor); err == nil {
		t.Fatal("cursor must not be reusable by another owner")
	}
	if _, err := service.ConversationPage(context.Background(), "owner-a", "page-3", "not-base64"); err == nil {
		t.Fatal("malformed cursor must be rejected")
	}
}

func TestConnectionQueuesRealtimeFramesUntilSnapshotActivation(t *testing.T) {
	sent := make([]string, 0, 3)
	connection := NewConnection(func(payload any) {
		sent = append(sent, payload.(string))
	})
	connection.BeginInitialization()
	connection.Send("upsert")
	connection.Send("remove")
	if len(sent) != 0 {
		t.Fatalf("realtime frames escaped initialization barrier: %#v", sent)
	}
	connection.Activate("snapshot")
	if len(sent) != 3 || sent[0] != "snapshot" || sent[1] != "upsert" || sent[2] != "remove" {
		t.Fatalf("unexpected activation order: %#v", sent)
	}
}

func twoDigits(value int) string {
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
