package service

import (
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/zyf/chatapi/internal/config"
)

type RuntimeMonitorService struct {
	cfg      config.Config
	realtime *RealtimeHub
	pending  *PendingRegistry
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

func NewRuntimeMonitorService(cfg config.Config, realtime *RealtimeHub, pending *PendingRegistry) *RuntimeMonitorService {
	return &RuntimeMonitorService{cfg: cfg, realtime: realtime, pending: pending}
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

func (s *RuntimeMonitorService) ForceGC() MemorySnapshot {
	runtime.GC()
	debug.FreeOSMemory()
	return ReadMemorySnapshot()
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
