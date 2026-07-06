package service

import (
	"bufio"
	"context"
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

const defaultRuntimeGOGC = 100
const unlimitedMemoryLimitBytes = int64(^uint64(0) >> 1)

type RuntimeMonitorService struct {
	cfg              config.Config
	store            store.Store
	realtime         *RealtimeHub
	pending          *PendingRegistry
	automation       *AutomationObserver
	settingsMu       sync.Mutex
	gogc             int
	memoryLimitBytes int64
}

type RuntimeSummary struct {
	GeneratedAt time.Time          `json:"generated_at"`
	Mode        config.Mode        `json:"mode"`
	Go          GoRuntimeInfo      `json:"go"`
	System      SystemSnapshot     `json:"system"`
	Memory      MemorySnapshot     `json:"memory"`
	Automation  AutomationSnapshot `json:"automation"`
	Pending     PendingStats       `json:"pending"`
	Realtime    RealtimeStats      `json:"realtime"`
	Database    DatabaseInfo       `json:"database"`
}

type AutomationSnapshot struct {
	Hits         int            `json:"hits"`
	Failures     int            `json:"failures"`
	NoRules      int            `json:"no_rules"`
	NoMatch      int            `json:"no_match"`
	SkipByReason map[string]int `json:"skip_by_reason,omitempty"`
}

type ConnectionSnapshot struct {
	WebUISubscribers    int `json:"webui_subscribers"`
	APIConnections      int `json:"api_connections"`
	SSEConnections      int `json:"sse_connections"`
	TotalSubscribers    int `json:"total_subscribers"`
	TotalConnections    int `json:"total_connections"`
	RejectedConnections int `json:"rejected_connections"`
}

type QueueSnapshot struct {
	QueuedEvents     int `json:"queued_events"`
	MaxQueueCapacity int `json:"max_queue_capacity"`
	RecoverableDrops int `json:"recoverable_drops"`
	CriticalDrops    int `json:"critical_drops"`
	SlowDisconnects  int `json:"slow_disconnects"`
}

type SystemSnapshot struct {
	OS                         string  `json:"os"`
	Hostname                   string  `json:"hostname,omitempty"`
	NumCPU                     int     `json:"num_cpu"`
	LoadAverage1               float64 `json:"load_average_1,omitempty"`
	LoadAverage5               float64 `json:"load_average_5,omitempty"`
	LoadAverage15              float64 `json:"load_average_15,omitempty"`
	SystemMemoryTotalBytes     uint64  `json:"system_memory_total_bytes,omitempty"`
	SystemMemoryAvailableBytes uint64  `json:"system_memory_available_bytes,omitempty"`
	ProcessRSSBytes            uint64  `json:"process_rss_bytes,omitempty"`
	ProcessOpenFDs             int     `json:"process_open_fds,omitempty"`
	DataDir                    string  `json:"data_dir,omitempty"`
	DataDirDiskTotalBytes      uint64  `json:"data_dir_disk_total_bytes,omitempty"`
	DataDirDiskAvailableBytes  uint64  `json:"data_dir_disk_available_bytes,omitempty"`
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
	Driver                       string `json:"driver"`
	SQLitePath                   string `json:"sqlite_path,omitempty"`
	SQLiteBytes                  int64  `json:"sqlite_bytes,omitempty"`
	SQLiteWAL                    string `json:"sqlite_wal,omitempty"`
	SQLiteWALBytes               int64  `json:"sqlite_wal_bytes,omitempty"`
	PostgresMaxConns             int32  `json:"postgres_max_conns,omitempty"`
	PostgresTotalConns           int32  `json:"postgres_total_conns,omitempty"`
	PostgresAcquiredConns        int32  `json:"postgres_acquired_conns,omitempty"`
	PostgresIdleConns            int32  `json:"postgres_idle_conns,omitempty"`
	PostgresConstructingConns    int32  `json:"postgres_constructing_conns,omitempty"`
	PostgresEmptyAcquireCount    int64  `json:"postgres_empty_acquire_count,omitempty"`
	PostgresCanceledAcquireCount int64  `json:"postgres_canceled_acquire_count,omitempty"`
}

type RuntimeSettings struct {
	GOGC             int   `json:"gogc"`
	MemoryLimitBytes int64 `json:"memory_limit_bytes"`
}

type UpdateRuntimeSettingsInput struct {
	GOGC             *int   `json:"gogc,omitempty"`
	MemoryLimitBytes *int64 `json:"memory_limit_bytes,omitempty"`
}

func NewRuntimeMonitorService(cfg config.Config, dataStore store.Store, realtime *RealtimeHub, pending *PendingRegistry) *RuntimeMonitorService {
	service := &RuntimeMonitorService{
		cfg:              cfg,
		store:            dataStore,
		realtime:         realtime,
		pending:          pending,
		automation:       NewAutomationObserver(),
		gogc:             cfg.RuntimeGOGC,
		memoryLimitBytes: cfg.RuntimeMemoryLimitBytes,
	}
	service.ApplyConfiguredSettings()
	return service
}

func (s *RuntimeMonitorService) SetAutomationObserver(observer *AutomationObserver) {
	if s == nil || observer == nil {
		return
	}
	s.automation = observer
}

func (s *RuntimeMonitorService) Summary() RuntimeSummary {
	return RuntimeSummary{
		GeneratedAt: time.Now().UTC(),
		Mode:        s.cfg.Mode,
		Go:          goRuntimeInfo(),
		System:      s.System(),
		Memory:      ReadMemorySnapshot(),
		Automation:  s.Automation(),
		Pending:     s.pending.Stats(),
		Realtime:    s.realtime.Stats(),
		Database:    s.databaseInfo(),
	}
}

func (s *RuntimeMonitorService) Memory() MemorySnapshot {
	return ReadMemorySnapshot()
}

func (s *RuntimeMonitorService) System() SystemSnapshot {
	return ReadSystemSnapshot(s.cfg.DataDir)
}

func (s *RuntimeMonitorService) Connections() ConnectionSnapshot {
	stats := s.realtime.Stats()
	return ConnectionSnapshot{
		WebUISubscribers:    stats.WebUISubscribers,
		APIConnections:      stats.APIConnections,
		SSEConnections:      stats.SSEConnections,
		TotalSubscribers:    stats.Subscribers,
		TotalConnections:    stats.TotalConnections,
		RejectedConnections: stats.RejectedConnections,
	}
}

func (s *RuntimeMonitorService) Automation() AutomationSnapshot {
	if s == nil || s.store == nil {
		return AutomationSnapshot{}
	}
	snapshot := AutomationSnapshot{}
	if s.automation != nil {
		observed := s.automation.Snapshot()
		snapshot.NoRules = observed.NoRules
		snapshot.NoMatch = observed.NoMatch
		snapshot.SkipByReason = observed.SkipByReason
	}
	count, err := s.store.CountAuditLogs(context.Background(), store.CountAuditLogsInput{
		EventType: "automation.rule",
		Action:    "auto_complete",
		Outcome:   "success",
	})
	if err != nil {
		return snapshot
	}
	snapshot.Hits = count
	failures, err := s.store.CountAuditLogs(context.Background(), store.CountAuditLogsInput{
		EventType: "automation.rule",
		Action:    "auto_complete",
		Outcome:   "failure",
	})
	if err == nil {
		snapshot.Failures = failures
	}
	return snapshot
}

func ReadSystemSnapshot(dataDir string) SystemSnapshot {
	hostname, _ := os.Hostname()
	snapshot := SystemSnapshot{
		OS:       runtime.GOOS,
		Hostname: hostname,
		NumCPU:   runtime.NumCPU(),
		DataDir:  dataDir,
	}
	load1, load5, load15 := readLoadAverage()
	snapshot.LoadAverage1 = load1
	snapshot.LoadAverage5 = load5
	snapshot.LoadAverage15 = load15
	total, available := readSystemMemory()
	snapshot.SystemMemoryTotalBytes = total
	snapshot.SystemMemoryAvailableBytes = available
	snapshot.ProcessRSSBytes = readProcessRSS()
	snapshot.ProcessOpenFDs = countOpenFDs()
	diskTotal, diskAvailable := readDiskUsage(dataDir)
	snapshot.DataDirDiskTotalBytes = diskTotal
	snapshot.DataDirDiskAvailableBytes = diskAvailable
	return snapshot
}

func readLoadAverage() (float64, float64, float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15
}

func readSystemMemory() (uint64, uint64) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer file.Close()

	var total uint64
	var available uint64
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "MemTotal":
			total = value * 1024
		case "MemAvailable":
			available = value * 1024
		}
	}
	return total, available
}

func readProcessRSS() uint64 {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || strings.TrimSuffix(fields[0], ":") != "VmRSS" {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return value * 1024
	}
	return 0
}

func countOpenFDs() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0
	}
	return len(entries)
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
	return databaseInfoFromStore(s.cfg, s.store)
}
