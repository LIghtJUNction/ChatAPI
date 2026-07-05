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
	writeMetric(&builder, "chatapi_pending_turns", "Current pending turns.", "gauge", float64(summary.Pending.Active))
	writeMetric(&builder, "chatapi_realtime_subscribers", "Current realtime subscribers.", "gauge", float64(summary.Realtime.Subscribers))
	writeMetric(&builder, "chatapi_realtime_queued_events", "Current queued realtime events.", "gauge", float64(summary.Realtime.QueuedEvents))
	writeMetric(&builder, "chatapi_realtime_recoverable_drops_total", "Recoverable realtime event drops.", "counter", float64(summary.Realtime.RecoverableDrops))
	writeMetric(&builder, "chatapi_realtime_critical_drops_total", "Critical realtime event drops.", "counter", float64(summary.Realtime.CriticalDrops))
	if summary.Database.Driver == "sqlite" {
		writeMetric(&builder, "chatapi_sqlite_database_bytes", "SQLite database file bytes.", "gauge", float64(summary.Database.SQLiteBytes))
		writeMetric(&builder, "chatapi_sqlite_wal_bytes", "SQLite WAL file bytes.", "gauge", float64(summary.Database.SQLiteWALBytes))
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
