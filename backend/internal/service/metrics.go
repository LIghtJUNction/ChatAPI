package service

import (
	"fmt"
	"strings"
)

type MetricsService struct {
	runtime *RuntimeMonitorService
}

func NewMetricsService(runtime *RuntimeMonitorService) *MetricsService {
	return &MetricsService{runtime: runtime}
}

func (s *MetricsService) PrometheusText() string {
	summary := s.runtime.Summary()
	var builder strings.Builder
	writeMetric(&builder, "chatapi_go_goroutines", "Current goroutine count.", "gauge", float64(summary.Go.NumGoroutine))
	writeMetric(&builder, "chatapi_go_heap_alloc_bytes", "Current Go heap allocation bytes.", "gauge", float64(summary.Memory.HeapAllocBytes))
	writeMetric(&builder, "chatapi_go_sys_bytes", "Current Go runtime sys bytes.", "gauge", float64(summary.Memory.SysBytes))
	writeMetric(&builder, "chatapi_go_gc_total", "Completed Go GC cycles.", "counter", float64(summary.Memory.NumGC))
	writeMetric(&builder, "chatapi_pending_turns", "Current pending turns.", "gauge", float64(summary.Pending.Active))
	writeMetric(&builder, "chatapi_realtime_subscribers", "Current realtime subscribers.", "gauge", float64(summary.Realtime.Subscribers))
	writeMetric(&builder, "chatapi_realtime_queued_events", "Current queued realtime events.", "gauge", float64(summary.Realtime.QueuedEvents))
	writeMetric(&builder, "chatapi_realtime_recoverable_drops_total", "Recoverable realtime event drops.", "counter", float64(summary.Realtime.RecoverableDrops))
	writeMetric(&builder, "chatapi_realtime_critical_drops_total", "Critical realtime event drops.", "counter", float64(summary.Realtime.CriticalDrops))
	if summary.Database.Driver == "sqlite" {
		writeMetric(&builder, "chatapi_sqlite_database_bytes", "SQLite database file bytes.", "gauge", float64(summary.Database.SQLiteBytes))
		writeMetric(&builder, "chatapi_sqlite_wal_bytes", "SQLite WAL file bytes.", "gauge", float64(summary.Database.SQLiteWALBytes))
	}
	return builder.String()
}

func writeMetric(builder *strings.Builder, name string, help string, metricType string, value float64) {
	fmt.Fprintf(builder, "# HELP %s %s\n", name, help)
	fmt.Fprintf(builder, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(builder, "%s %.0f\n", name, value)
}
