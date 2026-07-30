package turn

import (
	"context"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/actor"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
)

type capturedChatEvents struct {
	events []chatevents.Event
}

func (c *capturedChatEvents) Publish(_ context.Context, event chatevents.Event) {
	c.events = append(c.events, event)
}

func TestPublishTurnWaitingIncludesOnlyVirtualModelKeyID(t *testing.T) {
	tests := []struct {
		name string
		act  actor.Actor
		want string
	}{
		{
			name: "virtual model request",
			act:  actor.Actor{EntryPoint: "virtual_model", Source: "model_api_key", PrincipalID: " modelkey_123 "},
			want: "modelkey_123",
		},
		{
			name: "ordinary request",
			act:  actor.Actor{EntryPoint: "web", Source: "session", PrincipalID: "session_123"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			publisher := &capturedChatEvents{}
			service := &Service{Events: publisher}
			service.publishTurnWaiting(context.Background(), &PendingTurn{
				OwnerID: "owner", RequestID: "request", ConversationID: "conversation",
				RequestFormat: "responses", Model: "demo-model", Actor: tc.act,
			}, "hello")
			if len(publisher.events) != 1 || publisher.events[0].WaitingTurn == nil {
				t.Fatalf("waiting event was not published: %#v", publisher.events)
			}
			if got := publisher.events[0].WaitingTurn.ModelKeyID; got != tc.want {
				t.Fatalf("model key ID = %q, want %q", got, tc.want)
			}
		})
	}
}
