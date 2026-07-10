package automation

import (
	"context"
	"sync"
)

type StateEvent struct {
	OwnerID   string
	Recording *RecordingState
	Execution *ExecutionState
}

type StatePublisher interface {
	PublishAutomationState(context.Context, StateEvent)
}

type StateSubscriber interface {
	HandleAutomationState(context.Context, StateEvent)
}

type Dispatcher struct {
	mu          sync.RWMutex
	subscribers []StateSubscriber
}

func NewDispatcher(subscribers ...StateSubscriber) *Dispatcher {
	dispatcher := &Dispatcher{}
	for _, subscriber := range subscribers {
		dispatcher.Subscribe(subscriber)
	}
	return dispatcher
}

func (d *Dispatcher) Subscribe(subscriber StateSubscriber) {
	if d == nil || subscriber == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscribers = append(d.subscribers, subscriber)
}

func (d *Dispatcher) PublishAutomationState(ctx context.Context, event StateEvent) {
	if d == nil {
		return
	}
	d.mu.RLock()
	subscribers := append([]StateSubscriber(nil), d.subscribers...)
	d.mu.RUnlock()
	for _, subscriber := range subscribers {
		subscriber.HandleAutomationState(ctx, event)
	}
}
