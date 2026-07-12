package conversations_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	userconv "github.com/zyf2007/ChatAPI/internal/service/usercontrol/conversations"
)

type fakeQuery struct {
	conversations   map[string][]common.Conversation
	messages        map[string][]common.Message
	messageErr      error
	conversationErr error
}

func (f *fakeQuery) ListConversationsForOwner(_ context.Context, ownerID string) ([]common.Conversation, error) {
	if f.conversationErr != nil {
		return nil, f.conversationErr
	}
	return append([]common.Conversation(nil), f.conversations[ownerID]...), nil
}

func (f *fakeQuery) ListMessagesForOwner(_ context.Context, conversationID string, _ string) ([]common.Message, error) {
	if f.messageErr != nil {
		return nil, f.messageErr
	}
	return append([]common.Message(nil), f.messages[conversationID]...), nil
}

type fakeTurn struct {
	lastOwnerID        string
	lastConversationID string
	lastRequestID      string
	lastReason         string
	err                error
}

func (f *fakeTurn) Execute(_ context.Context, cmd controlsvc.Command) (controlsvc.Result, error) {
	f.lastOwnerID = cmd.OwnerID
	f.lastConversationID = cmd.ConversationID
	f.lastRequestID = cmd.RequestID
	f.lastReason = cmd.Action.AbortReason
	_ = turnsvc.TurnControlAbort
	if f.err != nil {
		return controlsvc.Result{}, f.err
	}
	return controlsvc.Result{Body: map[string]any{"ok": true}}, nil
}

type eventSpy struct {
	events []chatevents.Event
}

func (s *eventSpy) Publish(_ context.Context, event chatevents.Event) {
	s.events = append(s.events, event)
}

func TestConversationsDeleteConversationBranches(t *testing.T) {
	deleteCalled := 0
	events := &eventSpy{}
	svc := userconv.New(userconv.Deps{
		Query: &fakeQuery{
			conversations: map[string][]common.Conversation{
				"user_a": {
					{ID: "conv_wait", Metadata: map[string]any{"realtime_status": "waiting"}},
					{ID: "conv_ok", Metadata: map[string]any{"realtime_status": "done"}},
				},
			},
		},
		Turn: &fakeTurn{},
		DeleteOne: func(_ context.Context, id string) (common.DeleteConversationsResult, error) {
			deleteCalled++
			return common.DeleteConversationsResult{
				DeletedConversations: 1,
				DeletedConversationItems: []common.DeletedConversation{{
					ID:      "conv_result",
					OwnerID: "user_result",
				}},
			}, nil
		},
		DeleteMany: func(context.Context, []string) (common.DeleteConversationsResult, error) {
			return common.DeleteConversationsResult{}, nil
		},
		Events: events,
	})

	if _, err := svc.DeleteConversation(context.Background(), "user_a", "conv_wait"); !errors.Is(err, userconv.ErrWaitingConversationDelete) {
		t.Fatalf("expected waiting delete error, got %v", err)
	}
	if _, err := svc.DeleteConversation(context.Background(), "user_a", "missing"); !errors.Is(err, common.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	result, err := svc.DeleteConversation(context.Background(), "user_a", "conv_ok")
	if err != nil || result.DeletedConversations != 1 || deleteCalled != 1 {
		t.Fatalf("unexpected delete success result=%#v err=%v calls=%d", result, err, deleteCalled)
	}
	if len(events.events) != 1 {
		t.Fatalf("expected one delete event, got %#v", events.events)
	}
	event := events.events[0]
	if event.Type != chatevents.TypeConversationDeleted || event.OwnerID != "user_result" || event.ConversationID != "conv_result" {
		t.Fatalf("delete event should use mutation result identity, got %#v", event)
	}
}

func TestConversationsPruneRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	items := make([]common.Conversation, 0, 30)
	waitingCount := 0
	for i := 0; i < 30; i++ {
		status := "done"
		if rng.Intn(4) == 0 {
			status = "waiting"
			waitingCount++
		}
		items = append(items, common.Conversation{
			ID:        fmt.Sprintf("conv_%02d", i),
			UpdatedAt: time.Unix(int64(rng.Intn(100000)), 0).UTC(),
			Metadata:  map[string]any{"realtime_status": status},
		})
	}

	var deleted []string
	events := &eventSpy{}
	svc := userconv.New(userconv.Deps{
		Query: &fakeQuery{conversations: map[string][]common.Conversation{"user_a": items}},
		Turn:  &fakeTurn{},
		DeleteOne: func(context.Context, string) (common.DeleteConversationsResult, error) {
			return common.DeleteConversationsResult{}, nil
		},
		DeleteMany: func(_ context.Context, ids []string) (common.DeleteConversationsResult, error) {
			deleted = append([]string(nil), ids...)
			deletedItems := make([]common.DeletedConversation, 0, len(ids))
			for _, id := range ids {
				deletedItems = append(deletedItems, common.DeletedConversation{ID: id + "_result", OwnerID: "owner_from_result"})
			}
			return common.DeleteConversationsResult{
				DeletedConversations:     len(ids),
				DeletedConversationItems: deletedItems,
			}, nil
		},
		Events: events,
	})

	result, skipped, err := svc.PruneConversations(context.Background(), "user_a", 5)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if result.DeletedConversations != len(deleted) {
		t.Fatalf("deleted count mismatch: result=%d ids=%d", result.DeletedConversations, len(deleted))
	}
	if skipped < 0 || skipped > waitingCount {
		t.Fatalf("unexpected skipped count: %d waiting=%d", skipped, waitingCount)
	}
	if len(events.events) != len(deleted) {
		t.Fatalf("expected delete events to match mutation result count, events=%d deleted=%d", len(events.events), len(deleted))
	}
	for i, event := range events.events {
		if event.Type != chatevents.TypeConversationDeleted || event.OwnerID != "owner_from_result" || event.ConversationID != deleted[i]+"_result" {
			t.Fatalf("delete event %d should use mutation result identity, got %#v", i, event)
		}
	}
}

func TestConversationsAbortConversationChecksOwnershipAndForwardsReason(t *testing.T) {
	turn := &fakeTurn{}
	svc := userconv.New(userconv.Deps{
		Query: &fakeQuery{
			messages: map[string][]common.Message{
				"conv_ok": {{ID: "msg_1", Role: "user", Content: "hello"}},
			},
		},
		Turn: turn,
		DeleteOne: func(context.Context, string) (common.DeleteConversationsResult, error) {
			return common.DeleteConversationsResult{}, nil
		},
		DeleteMany: func(context.Context, []string) (common.DeleteConversationsResult, error) {
			return common.DeleteConversationsResult{}, nil
		},
	})

	if _, err := svc.AbortConversation(context.Background(), "user_a", "missing", "req_missing", "  stop now  "); err != nil {
		t.Fatalf("expected missing conversation ownership check to pass for empty message list, got %v", err)
	}

	result, err := svc.AbortConversation(context.Background(), "user_a", "conv_ok", "req_ok", "  stop now  ")
	if err != nil {
		t.Fatalf("abort conversation: %v", err)
	}
	if result["ok"] != true {
		t.Fatalf("unexpected abort result: %#v", result)
	}
	if turn.lastOwnerID != "user_a" || turn.lastConversationID != "conv_ok" || turn.lastRequestID != "req_ok" || turn.lastReason != "stop now" {
		t.Fatalf("unexpected turn command forwarding: owner=%q conversation=%q request=%q reason=%q", turn.lastOwnerID, turn.lastConversationID, turn.lastRequestID, turn.lastReason)
	}
}

func TestConversationsAbortConversationRejectsForbidden(t *testing.T) {
	svc := userconv.New(userconv.Deps{
		Query: &fakeQuery{messageErr: userconv.ErrForbidden},
		Turn:  &fakeTurn{err: userconv.ErrForbidden},
		DeleteOne: func(context.Context, string) (common.DeleteConversationsResult, error) {
			return common.DeleteConversationsResult{}, nil
		},
		DeleteMany: func(context.Context, []string) (common.DeleteConversationsResult, error) {
			return common.DeleteConversationsResult{}, nil
		},
	})

	if _, err := svc.AbortConversation(context.Background(), "user_a", "conv_x", "req_x", "stop"); !errors.Is(err, userconv.ErrForbidden) {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}
