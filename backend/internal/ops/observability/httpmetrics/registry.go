package httpmetrics

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Registry struct {
	mu    sync.RWMutex
	items map[Key]Value
}

type Key struct {
	Method string
	Route  string
	Status int
}

type Value struct {
	Count           int64
	DurationSeconds float64
}

type Sample struct {
	Key   Key
	Value Value
}

func NewRegistry() *Registry {
	return &Registry{items: map[Key]Value{}}
}

func (r *Registry) Observe(method string, route string, status int, duration time.Duration) {
	if r == nil {
		return
	}
	if strings.TrimSpace(route) == "" {
		route = "unmatched"
	}
	key := Key{Method: method, Route: route, Status: status}
	r.mu.Lock()
	value := r.items[key]
	value.Count++
	value.DurationSeconds += duration.Seconds()
	r.items[key] = value
	r.mu.Unlock()
}

func (r *Registry) Snapshot() []Sample {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Sample, 0, len(r.items))
	for key, value := range r.items {
		items = append(items, Sample{Key: key, Value: value})
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

func (r *Registry) PrometheusText() string {
	var builder strings.Builder
	items := r.Snapshot()
	builder.WriteString("# HELP chatapi_http_requests_total HTTP requests by method, route, and status.\n")
	builder.WriteString("# TYPE chatapi_http_requests_total counter\n")
	for _, item := range items {
		fmt.Fprintf(&builder, "chatapi_http_requests_total{method=%q,route=%q,status=%q} %d\n", item.Key.Method, item.Key.Route, fmt.Sprintf("%d", item.Key.Status), item.Value.Count)
	}
	builder.WriteString("# HELP chatapi_http_request_duration_seconds_sum Total HTTP request duration seconds by method, route, and status.\n")
	builder.WriteString("# TYPE chatapi_http_request_duration_seconds_sum counter\n")
	for _, item := range items {
		fmt.Fprintf(&builder, "chatapi_http_request_duration_seconds_sum{method=%q,route=%q,status=%q} %.6f\n", item.Key.Method, item.Key.Route, fmt.Sprintf("%d", item.Key.Status), item.Value.DurationSeconds)
	}
	builder.WriteString("# HELP chatapi_http_request_duration_seconds_count HTTP request duration sample count by method, route, and status.\n")
	builder.WriteString("# TYPE chatapi_http_request_duration_seconds_count counter\n")
	for _, item := range items {
		fmt.Fprintf(&builder, "chatapi_http_request_duration_seconds_count{method=%q,route=%q,status=%q} %d\n", item.Key.Method, item.Key.Route, fmt.Sprintf("%d", item.Key.Status), item.Value.Count)
	}
	return builder.String()
}
