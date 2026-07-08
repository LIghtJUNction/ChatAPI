package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zyf/chatapi/internal/ops/observability/httpmetrics"
)

type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *metricsResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

func (w *metricsResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *metricsResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func RecordHTTPMetrics(registry *httpmetrics.Registry) func(http.Handler) http.Handler {
	if registry == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startedAt := time.Now()
			recorder := &metricsResponseWriter{ResponseWriter: w}
			next.ServeHTTP(recorder, r)
			status := recorder.statusCode
			if status == 0 {
				status = http.StatusOK
			}
			route := "unmatched"
			if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
				if pattern := routeContext.RoutePattern(); pattern != "" {
					route = pattern
				}
			}
			registry.Observe(r.Method, route, status, time.Since(startedAt))
		})
	}
}
