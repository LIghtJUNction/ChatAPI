package service

import (
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/zyf/chatapi/internal/config"
)

const defaultRuntimeGOGC = 100
const unlimitedMemoryLimitBytes = int64(^uint64(0) >> 1)

type RuntimeMonitorService struct {
	cfg              config.Config
	realtime         *RealtimeHub
	pending          *PendingRegistry
	settingsMu       sync.Mutex
	gogc             int
	memoryLimitBytes int64
}

type RuntimeSummary struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Mode        config.Mode    `json:"mode"`
	Go          GoRuntimeInfo  `json:"go"`
	Memory      MemorySnapshot `json:"memory"`
	Pending     PendingStats   `json:"pending"`
	Realtime    RealtimeStats  `json:"realtime"`
	Database    DatabaseInfo   `json:"database"`
}

type ConnectionSnapshot struct {
	WebUISubscribers int `json:"webui_subscribers"`
	TotalSubscribers int `json:"total_subscribers"`
}

type QueueSnapshot struct {
	QueuedEvents     int `json:"queued_events"`
	MaxQueueCapacity int `json:"max_queue_capacity"`
	RecoverableDrops int `json:"recoverable_drops"`
	CriticalDrops    int `json:"critical_drops"`
	SlowDisconnects  int `json:"slow_disconnects"`
}

type GoRuntimeInfo struct {
	Version      string `json:"version"`
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	NumCPU       int    `json:"num_cpu"`
	GOMAXPROCS   int    `json:"gomaxprocs"`
	NumGoroutine int    `json:"num_goroutine"`
}

type MemorySnapshot struct {
	AllocBytes        uint64 `json:"alloc_bytes"`
	TotalAllocBytes   uint64 `json:"total_alloc_bytes"`
	SysBytes          uint64 `json:"sys_bytes"`
	HeapAllocBytes    uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes    uint64 `json:"heap_inuse_bytes"`
	HeapIdleBytes     uint64 `json:"heap_idle_bytes"`
	HeapReleasedBytes uint64 `json:"heap_released_bytes"`
	StackInuseBytes   uint64 `json:"stack_inuse_bytes"`
	NextGCBytes       uint64 `json:"next_gc_bytes"`
	LastGCTime        string `json:"last_gc_time,omitempty"`
	NumGC             uint32 `json:"num_gc"`
	PauseTotalNs      uint64 `json:"pause_total_ns"`
}

type DatabaseInfo struct {
	Driver         string `json:"driver"`
	SQLitePath     string `json:"sqlite_path,omitempty"`
	SQLiteBytes    int64  `json:"sqlite_bytes,omitempty"`
	SQLiteWAL      string `json:"sqlite_wal,omitempty"`
	SQLiteWALBytes int64  `json:"sqlite_wal_bytes,omitempty"`
}

type RuntimeSettings struct {
	GOGC             int   `json:"gogc"`
	MemoryLimitBytes int64 `json:"memory_limit_bytes"`
}

type UpdateRuntimeSettingsInput struct {
	GOGC             *int   `json:"gogc,omitempty"`
	MemoryLimitBytes *int64 `json:"memory_limit_bytes,omitempty"`
}

func NewRuntimeMonitorService(cfg config.Config, realtime *RealtimeHub, pending *PendingRegistry) *RuntimeMonitorService {
	service := &RuntimeMonitorService{
		cfg:              cfg,
		realtime:         realtime,
		pending:          pending,
		gogc:             cfg.RuntimeGOGC,
		memoryLimitBytes: cfg.RuntimeMemoryLimitBytes,
	}
	service.ApplyConfiguredSettings()
	return service
}

func (s *RuntimeMonitorService) Summary() RuntimeSummary {
	return RuntimeSummary{
		GeneratedAt: time.Now().UTC(),
		Mode:        s.cfg.Mode,
		Go:          goRuntimeInfo(),
		Memory:      ReadMemorySnapshot(),
		Pending:     s.pending.Stats(),
		Realtime:    s.realtime.Stats(),
		Database:    s.databaseInfo(),
	}
}

func (s *RuntimeMonitorService) Memory() MemorySnapshot {
	return ReadMemorySnapshot()
}

func (s *RuntimeMonitorService) Connections() ConnectionSnapshot {
	stats := s.realtime.Stats()
	return ConnectionSnapshot{
		WebUISubscribers: stats.Subscribers,
		TotalSubscribers: stats.Subscribers,
	}
}

func (s *RuntimeMonitorService) Queue() QueueSnapshot {
	stats := s.realtime.Stats()
	return QueueSnapshot{
		QueuedEvents:     stats.QueuedEvents,
		MaxQueueCapacity: stats.MaxQueueCapacity,
		RecoverableDrops: stats.RecoverableDrops,
		CriticalDrops:    stats.CriticalDrops,
		SlowDisconnects:  stats.SlowDisconnects,
	}
}

func (s *RuntimeMonitorService) ForceGC() MemorySnapshot {
	runtime.GC()
	debug.FreeOSMemory()
	return ReadMemorySnapshot()
}

func (s *RuntimeMonitorService) Settings() RuntimeSettings {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	return RuntimeSettings{
		GOGC:             s.gogc,
		MemoryLimitBytes: s.memoryLimitBytes,
	}
}

func (s *RuntimeMonitorService) ApplySettings(input UpdateRuntimeSettingsInput) (RuntimeSettings, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	applyGOGC := input.GOGC != nil
	applyMemoryLimit := input.MemoryLimitBytes != nil
	if input.GOGC != nil {
		if *input.GOGC < 0 {
			return RuntimeSettings{}, errors.New("gogc must be non-negative")
		}
		s.gogc = *input.GOGC
	}
	if input.MemoryLimitBytes != nil {
		if *input.MemoryLimitBytes < 0 {
			return RuntimeSettings{}, errors.New("memory_limit_bytes must be non-negative")
		}
		s.memoryLimitBytes = *input.MemoryLimitBytes
	}
	s.applyRuntimeSettingsLocked(applyGOGC, applyMemoryLimit, true)
	return RuntimeSettings{
		GOGC:             s.gogc,
		MemoryLimitBytes: s.memoryLimitBytes,
	}, nil
}

func (s *RuntimeMonitorService) ApplyConfiguredSettings() {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	s.applyRuntimeSettingsLocked(s.gogc > 0, s.memoryLimitBytes > 0, false)
}

func (s *RuntimeMonitorService) applyRuntimeSettingsLocked(applyGOGC bool, applyMemoryLimit bool, resetZero bool) {
	if applyGOGC {
		if s.gogc > 0 {
			debug.SetGCPercent(s.gogc)
		} else if resetZero {
			debug.SetGCPercent(defaultRuntimeGOGC)
		}
	}
	if applyMemoryLimit {
		if s.memoryLimitBytes > 0 {
			debug.SetMemoryLimit(s.memoryLimitBytes)
		} else if resetZero {
			debug.SetMemoryLimit(unlimitedMemoryLimitBytes)
		}
	}
}

func ReadMemorySnapshot() MemorySnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	snapshot := MemorySnapshot{
		AllocBytes:        stats.Alloc,
		TotalAllocBytes:   stats.TotalAlloc,
		SysBytes:          stats.Sys,
		HeapAllocBytes:    stats.HeapAlloc,
		HeapInuseBytes:    stats.HeapInuse,
		HeapIdleBytes:     stats.HeapIdle,
		HeapReleasedBytes: stats.HeapReleased,
		StackInuseBytes:   stats.StackInuse,
		NextGCBytes:       stats.NextGC,
		NumGC:             stats.NumGC,
		PauseTotalNs:      stats.PauseTotalNs,
	}
	if stats.LastGC > 0 {
		snapshot.LastGCTime = time.Unix(0, int64(stats.LastGC)).UTC().Format(time.RFC3339Nano)
	}
	return snapshot
}

func goRuntimeInfo() GoRuntimeInfo {
	return GoRuntimeInfo{
		Version:      runtime.Version(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		NumCPU:       runtime.NumCPU(),
		GOMAXPROCS:   runtime.GOMAXPROCS(0),
		NumGoroutine: runtime.NumGoroutine(),
	}
}

func (s *RuntimeMonitorService) databaseInfo() DatabaseInfo {
	info := DatabaseInfo{Driver: s.cfg.DatabaseDriver}
	if s.cfg.DatabaseDriver != "sqlite" {
		return info
	}
	info.SQLitePath = s.cfg.DatabaseDSN
	if stat, err := os.Stat(s.cfg.DatabaseDSN); err == nil {
		info.SQLiteBytes = stat.Size()
	}
	walPath := s.cfg.DatabaseDSN + "-wal"
	info.SQLiteWAL = walPath
	if stat, err := os.Stat(walPath); err == nil {
		info.SQLiteWALBytes = stat.Size()
	}
	return info
}
