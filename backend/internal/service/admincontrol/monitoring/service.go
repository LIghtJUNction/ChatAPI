package monitoring

import (
	"context"
	"runtime"
	"time"

	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
)

type Presence interface {
	PresenceSnapshot([]string) workspacesvc.PresenceSnapshot
	SubscribePresence([]string) (<-chan workspacesvc.PresenceEvent, func())
}

type Metrics struct {
	SampledAt      time.Time `json:"sampled_at"`
	UptimeSeconds  int64     `json:"uptime_seconds"`
	CPUCount       int       `json:"cpu_count"`
	Goroutines     int       `json:"goroutines"`
	HeapAllocBytes uint64    `json:"heap_alloc_bytes"`
	HeapInuseBytes uint64    `json:"heap_inuse_bytes"`
	SysBytes       uint64    `json:"sys_bytes"`
}

type Event struct {
	Type             string         `json:"type"`
	UserID           string         `json:"user_id,omitempty"`
	ConnectionCount  int            `json:"connection_count,omitempty"`
	TotalConnections int            `json:"total_connections"`
	UserConnections  map[string]int `json:"user_connections,omitempty"`
	Metrics          *Metrics       `json:"metrics,omitempty"`
	Sequence         uint64         `json:"sequence"`
}

type Service struct {
	presence  Presence
	startedAt time.Time
	interval  time.Duration
}

func New(presence Presence) *Service {
	return &Service{presence: presence, startedAt: time.Now(), interval: 2 * time.Second}
}

func (s *Service) Stream(ctx context.Context, userIDs []string) <-chan Event {
	out := make(chan Event, 16)
	go s.run(ctx, userIDs, out)
	return out
}

func (s *Service) run(ctx context.Context, userIDs []string, out chan<- Event) {
	defer close(out)
	if s == nil || s.presence == nil {
		return
	}
	presenceEvents, unsubscribe := s.presence.SubscribePresence(userIDs)
	defer unsubscribe()
	snapshot := s.presence.PresenceSnapshot(userIDs)
	if !send(ctx, out, Event{
		Type:             "monitor.snapshot",
		TotalConnections: snapshot.TotalConnections,
		UserConnections:  snapshot.UserConnections,
		Metrics:          s.metrics(),
		Sequence:         snapshot.Sequence,
	}) {
		return
	}
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-presenceEvents:
			if !ok {
				return
			}
			if !send(ctx, out, Event{
				Type:             "user.connection.updated",
				UserID:           event.UserID,
				ConnectionCount:  event.ConnectionCount,
				TotalConnections: event.TotalConnections,
				Sequence:         event.Sequence,
			}) {
				return
			}
		case <-ticker.C:
			snapshot := s.presence.PresenceSnapshot(userIDs)
			if !send(ctx, out, Event{
				Type:             "system.metrics.updated",
				TotalConnections: snapshot.TotalConnections,
				UserConnections:  snapshot.UserConnections,
				Metrics:          s.metrics(),
				Sequence:         snapshot.Sequence,
			}) {
				return
			}
		}
	}
}

func (s *Service) metrics() *Metrics {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return &Metrics{
		SampledAt:      time.Now().UTC(),
		UptimeSeconds:  int64(time.Since(s.startedAt).Seconds()),
		CPUCount:       runtime.NumCPU(),
		Goroutines:     runtime.NumGoroutine(),
		HeapAllocBytes: memory.HeapAlloc,
		HeapInuseBytes: memory.HeapInuse,
		SysBytes:       memory.Sys,
	}
}

func send(ctx context.Context, out chan<- Event, event Event) bool {
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
