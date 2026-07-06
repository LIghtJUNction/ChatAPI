package service

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MetricsService struct {
	runtime *RuntimeMonitorService
	http    *HTTPMetricsRegistry
}

type HTTPMetricsRegistry struct {
	mu    sync.RWMutex
	items map[HTTPMetricKey]HTTPMetricValue
}

type HTTPMetricKey struct {
	Method string
	Route  string
	Status int
}

type HTTPMetricValue struct {
	Count           int64
	DurationSeconds float64
}

type HTTPMetricSample struct {
	Key   HTTPMetricKey
	Value HTTPMetricValue
}

func NewHTTPMetricsRegistry() *HTTPMetricsRegistry {
	return &HTTPMetricsRegistry{items: map[HTTPMetricKey]HTTPMetricValue{}}
}

func (r *HTTPMetricsRegistry) Observe(method string, route string, status int, duration time.Duration) {
	if r == nil {
		return
	}
	if strings.TrimSpace(route) == "" {
		route = "unmatched"
	}
	key := HTTPMetricKey{Method: method, Route: route, Status: status}
	r.mu.Lock()
	item := r.items[key]
	item.Count++
	item.DurationSeconds += duration.Seconds()
	r.items[key] = item
	r.mu.Unlock()
}

func (r *HTTPMetricsRegistry) Snapshot() []HTTPMetricSample {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]HTTPMetricSample, 0, len(r.items))
	for key, value := range r.items {
		items = append(items, HTTPMetricSample{Key: key, Value: value})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Key.Route != items[j].Key.Route {
			return items[i].Key.Route < items[j].Key.Route
		}
		if items[i].Key.Method != items[j].Key.Method {
			return items[i].Key.Method < items[j].Key.Method
		}
		return items[i].Key.Status < items[j].Key.Status
	})
	return items
}

func NewMetricsService(runtime *RuntimeMonitorService, httpMetrics *HTTPMetricsRegistry) *MetricsService {
	return &MetricsService{runtime: runtime, http: httpMetrics}
}

func (s *MetricsService) PrometheusText() string {
	summary := s.runtime.Summary()
	var builder strings.Builder
	writeMetric(&builder, "chatapi_go_goroutines", "Current goroutine count.", "gauge", float64(summary.Go.NumGoroutine))
	writeMetric(&builder, "chatapi_go_heap_alloc_bytes", "Current Go heap allocation bytes.", "gauge", float64(summary.Memory.HeapAllocBytes))
	writeMetric(&builder, "chatapi_go_sys_bytes", "Current Go runtime sys bytes.", "gauge", float64(summary.Memory.SysBytes))
	writeMetric(&builder, "chatapi_go_gc_total", "Completed Go GC cycles.", "counter", float64(summary.Memory.NumGC))
	writeMetric(&builder, "chatapi_system_load_average_1", "System 1-minute load average.", "gauge", summary.System.LoadAverage1)
	writeMetric(&builder, "chatapi_system_memory_total_bytes", "System total memory bytes.", "gauge", float64(summary.System.SystemMemoryTotalBytes))
	writeMetric(&builder, "chatapi_system_memory_available_bytes", "System available memory bytes.", "gauge", float64(summary.System.SystemMemoryAvailableBytes))
	writeMetric(&builder, "chatapi_process_rss_bytes", "ChatAPI process resident set size bytes.", "gauge", float64(summary.System.ProcessRSSBytes))
	writeMetric(&builder, "chatapi_process_open_fds", "ChatAPI process open file descriptor count.", "gauge", float64(summary.System.ProcessOpenFDs))
	writeMetric(&builder, "chatapi_data_dir_disk_total_bytes", "Data directory filesystem total bytes.", "gauge", float64(summary.System.DataDirDiskTotalBytes))
	writeMetric(&builder, "chatapi_data_dir_disk_available_bytes", "Data directory filesystem available bytes.", "gauge", float64(summary.System.DataDirDiskAvailableBytes))
	writeMetric(&builder, "chatapi_automation_hits_total", "Successful automation rule auto-completions.", "counter", float64(summary.Automation.Hits))
	writeMetric(&builder, "chatapi_automation_failures_total", "Failed automation rule auto-completions.", "counter", float64(summary.Automation.Failures))
	writeMetric(&builder, "chatapi_automation_no_rules_total", "Requests observed with no enabled automation rules.", "counter", float64(summary.Automation.NoRules))
	writeMetric(&builder, "chatapi_automation_no_match_total", "Requests observed with enabled automation rules but no match.", "counter", float64(summary.Automation.NoMatch))
	writeMetric(&builder, "chatapi_pending_turns", "Current pending turns.", "gauge", float64(summary.Pending.Active))
	writeMetric(&builder, "chatapi_realtime_subscribers", "Current realtime subscribers.", "gauge", float64(summary.Realtime.Subscribers))
	writeMetric(&builder, "chatapi_realtime_webui_subscribers", "Current WebUI realtime subscribers.", "gauge", float64(summary.Realtime.WebUISubscribers))
	writeMetric(&builder, "chatapi_realtime_api_connections", "Current API realtime connections.", "gauge", float64(summary.Realtime.APIConnections))
	writeMetric(&builder, "chatapi_realtime_sse_connections", "Current SSE realtime connections.", "gauge", float64(summary.Realtime.SSEConnections))
	writeMetric(&builder, "chatapi_realtime_connections", "Current realtime connections.", "gauge", float64(summary.Realtime.TotalConnections))
	writeMetric(&builder, "chatapi_realtime_queued_events", "Current queued realtime events.", "gauge", float64(summary.Realtime.QueuedEvents))
	writeMetric(&builder, "chatapi_realtime_recoverable_drops_total", "Recoverable realtime event drops.", "counter", float64(summary.Realtime.RecoverableDrops))
	writeMetric(&builder, "chatapi_realtime_critical_drops_total", "Critical realtime event drops.", "counter", float64(summary.Realtime.CriticalDrops))
	writeMetric(&builder, "chatapi_realtime_slow_disconnects_total", "Realtime subscribers disconnected due to backpressure.", "counter", float64(summary.Realtime.SlowDisconnects))
	writeMetric(&builder, "chatapi_realtime_rejected_connections_total", "Realtime connections rejected by configured limits.", "counter", float64(summary.Realtime.RejectedConnections))
	if summary.Database.Driver == "sqlite" {
		writeMetric(&builder, "chatapi_sqlite_database_bytes", "SQLite database file bytes.", "gauge", float64(summary.Database.SQLiteBytes))
		writeMetric(&builder, "chatapi_sqlite_wal_bytes", "SQLite WAL file bytes.", "gauge", float64(summary.Database.SQLiteWALBytes))
	} else if summary.Database.Driver == "postgres" || summary.Database.Driver == "postgresql" {
		writeMetric(&builder, "chatapi_postgres_pool_max_conns", "PostgreSQL pool max connections.", "gauge", float64(summary.Database.PostgresMaxConns))
		writeMetric(&builder, "chatapi_postgres_pool_total_conns", "PostgreSQL pool total connections.", "gauge", float64(summary.Database.PostgresTotalConns))
		writeMetric(&builder, "chatapi_postgres_pool_acquired_conns", "PostgreSQL pool acquired connections.", "gauge", float64(summary.Database.PostgresAcquiredConns))
		writeMetric(&builder, "chatapi_postgres_pool_idle_conns", "PostgreSQL pool idle connections.", "gauge", float64(summary.Database.PostgresIdleConns))
		writeMetric(&builder, "chatapi_postgres_pool_constructing_conns", "PostgreSQL pool constructing connections.", "gauge", float64(summary.Database.PostgresConstructingConns))
		writeMetric(&builder, "chatapi_postgres_pool_empty_acquire_total", "PostgreSQL pool empty acquire count.", "counter", float64(summary.Database.PostgresEmptyAcquireCount))
		writeMetric(&builder, "chatapi_postgres_pool_canceled_acquire_total", "PostgreSQL pool canceled acquire count.", "counter", float64(summary.Database.PostgresCanceledAcquireCount))
	}
	writeHTTPMetrics(&builder, s.http.Snapshot())
	return builder.String()
}

func writeMetric(builder *strings.Builder, name string, help string, metricType string, value float64) {
	fmt.Fprintf(builder, "# HELP %s %s\n", name, help)
	fmt.Fprintf(builder, "# TYPE %s %s\n", name, metricType)
	fmt.Fprintf(builder, "%s %.0f\n", name, value)
}

func writeHTTPMetrics(builder *strings.Builder, items []HTTPMetricSample) {
	builder.WriteString("# HELP chatapi_http_requests_total HTTP requests by method, route, and status.\n")
	builder.WriteString("# TYPE chatapi_http_requests_total counter\n")
	for _, item := range items {
		fmt.Fprintf(builder, "chatapi_http_requests_total{method=%q,route=%q,status=%q} %d\n", item.Key.Method, item.Key.Route, fmt.Sprintf("%d", item.Key.Status), item.Value.Count)
	}
	builder.WriteString("# HELP chatapi_http_request_duration_seconds_sum Total HTTP request duration seconds by method, route, and status.\n")
	builder.WriteString("# TYPE chatapi_http_request_duration_seconds_sum counter\n")
	for _, item := range items {
		fmt.Fprintf(builder, "chatapi_http_request_duration_seconds_sum{method=%q,route=%q,status=%q} %.6f\n", item.Key.Method, item.Key.Route, fmt.Sprintf("%d", item.Key.Status), item.Value.DurationSeconds)
	}
	builder.WriteString("# HELP chatapi_http_request_duration_seconds_count HTTP request duration sample count by method, route, and status.\n")
	builder.WriteString("# TYPE chatapi_http_request_duration_seconds_count counter\n")
	for _, item := range items {
		fmt.Fprintf(builder, "chatapi_http_request_duration_seconds_count{method=%q,route=%q,status=%q} %d\n", item.Key.Method, item.Key.Route, fmt.Sprintf("%d", item.Key.Status), item.Value.Count)
	}
}
