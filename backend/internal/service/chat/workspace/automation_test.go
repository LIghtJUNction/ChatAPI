package workspace

import (
	"context"
	"testing"

	automationsvc "github.com/zyf2007/ChatAPI/internal/service/automation"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
)

type recorderStub struct {
	startedOwner        string
	startedConversation string
	snapshot            automationsvc.StateSnapshot
}

type turnExecutorStub struct{ command controlsvc.Command }

func (s *turnExecutorStub) Execute(_ context.Context, command controlsvc.Command) (controlsvc.Result, error) {
	s.command = command
	return controlsvc.Result{Body: map[string]any{"ok": true}}, nil
}

func (r *recorderStub) StartRecording(_ context.Context, ownerID string, conversationID string) (automationsvc.RecordingState, error) {
	r.startedOwner, r.startedConversation = ownerID, conversationID
	return automationsvc.RecordingState{Active: true, ConversationID: conversationID, RequestID: "req", Steps: []automationsvc.Step{}}, nil
}

func (r *recorderStub) StopRecording(context.Context, string) (automationsvc.RecordingState, error) {
	return automationsvc.RecordingState{Steps: []automationsvc.Step{}}, nil
}

func (r *recorderStub) CancelRecording(context.Context, string) (automationsvc.RecordingState, error) {
	return automationsvc.RecordingState{Steps: []automationsvc.Step{}}, nil
}

func (r *recorderStub) RecordingState(string) automationsvc.RecordingState {
	return automationsvc.RecordingState{Steps: []automationsvc.Step{}}
}

func (r *recorderStub) ExecutionStates(string) []automationsvc.ExecutionState { return nil }
func (r *recorderStub) StateSnapshot(string) automationsvc.StateSnapshot {
	if r.snapshot.Revision != 0 {
		return r.snapshot
	}
	return automationsvc.StateSnapshot{Recording: r.RecordingState(""), Executions: []automationsvc.ExecutionState{}}
}

func TestAutomationRecordMessageRequiresIdentity(t *testing.T) {
	if _, err := ParseClientMessage(map[string]any{"type": "automation.record.start", "conversation_id": "conv"}); err == nil {
		t.Fatal("expected missing command id to be rejected")
	}
	msg, err := ParseClientMessage(map[string]any{
		"type": "automation.record.start", "command_id": "cmd", "conversation_id": "conv",
	})
	if err != nil || msg.CommandID != "cmd" || msg.ConversationID != "conv" {
		t.Fatalf("unexpected parsed message: %#v err=%v", msg, err)
	}
}

func TestAutomationRecordSnapshotDoesNotRequireConversation(t *testing.T) {
	msg, err := ParseClientMessage(map[string]any{
		"type": "automation.record.get", "command_id": "cmd",
	})
	if err != nil || msg.CommandID != "cmd" || msg.ConversationID != "" {
		t.Fatalf("unexpected parsed snapshot request: %#v err=%v", msg, err)
	}
}

func TestWorkspaceCommandRequiresExpectedRequestIdentity(t *testing.T) {
	if _, err := ParseClientMessage(map[string]any{
		"type":    "workspace.command",
		"command": map[string]any{"command_id": "cmd", "kind": "stream_delta", "conversation_id": "conv"},
	}); err == nil {
		t.Fatal("expected request-less workspace command to be rejected")
	}
	msg, err := ParseClientMessage(map[string]any{
		"type": "workspace.command",
		"command": map[string]any{
			"command_id": "cmd", "kind": "stream_delta", "conversation_id": "conv", "request_id": "req",
		},
	})
	if err != nil || msg.Command == nil || msg.Command.RequestID != "req" {
		t.Fatalf("unexpected parsed workspace command: %#v err=%v", msg, err)
	}
}

func TestWorkspaceCommandPropagatesAndAcknowledgesRequestIdentity(t *testing.T) {
	executor := &turnExecutorStub{}
	service := New(nil, nil, executor)
	ack, err := service.ExecuteCommand(context.Background(), "owner", Command{
		ID: "cmd", Kind: "stream_delta", ConversationID: "conv", RequestID: "req", Text: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if executor.command.RequestID != "req" || ack.RequestID != "req" {
		t.Fatalf("request identity was not preserved: command=%#v ack=%#v", executor.command, ack)
	}
}

func TestHubDispatchesAutomationRecordingAndAcknowledges(t *testing.T) {
	recorder := &recorderStub{snapshot: automationsvc.StateSnapshot{
		Revision:   7,
		Executions: []automationsvc.ExecutionState{{Revision: 7, ConversationID: "conv", RequestID: "req", Status: "running"}},
	}}
	service := New(nil, nil, nil, recorder)
	hub := NewHub(service)
	var sent any
	connection := NewConnection(func(payload any) { sent = payload })
	err := hub.HandleClientMessage(context.Background(), "owner", connection, ClientMessage{
		Type: "automation.record.start", CommandID: "cmd", ConversationID: "conv",
	})
	if err != nil {
		t.Fatal(err)
	}
	if recorder.startedOwner != "owner" || recorder.startedConversation != "conv" {
		t.Fatalf("recording was not dispatched: %#v", recorder)
	}
	ack, ok := sent.(map[string]any)
	if !ok || ack["type"] != "automation.record.ack" || ack["command_id"] != "cmd" {
		t.Fatalf("unexpected acknowledgment: %#v", sent)
	}
	if ack["revision"] != uint64(7) {
		t.Fatalf("acknowledgment omitted snapshot revision: %#v", ack)
	}
}
