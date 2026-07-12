package router

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"go.uber.org/zap"
)

func (d Deps) logger(layer string) *zap.Logger {
	if d.LoggerFactory == nil {
		return zap.NewNop()
	}
	return d.LoggerFactory.Layer(layer)
}

func chainMiddleware(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(data)
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func requestLoggingMiddleware(factory *logging.Factory, base *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			requestID := strings.TrimSpace(r.Header.Get(logging.RequestIDHeader))
			if requestID == "" {
				requestID = logging.NewRequestID()
			}
			w.Header().Set(logging.RequestIDHeader, requestID)
			logger := base.With(
				zap.String("request_id", requestID),
				zap.String("http.method", r.Method),
				zap.String("http.path", r.URL.Path),
				zap.String("http.remote_addr", r.RemoteAddr),
			)
			ctx := logging.WithRequestID(r.Context(), requestID)
			ctx = logging.WithLogger(ctx, logger)
			entry := logging.HTTPAccessEntry{
				Timestamp: start,
				Phase:     "start",
				RequestID: requestID,
				Kind:      httpAccessKind(r),
				Method:    r.Method,
				Path:      r.URL.RequestURI(),
				Remote:    r.RemoteAddr,
			}
			if factory != nil {
				factory.LogHTTPStart(entry)
			}
			logger.Info("http request started")
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))
			completedAt := time.Now()
			entry.Timestamp = completedAt
			entry.Phase = "complete"
			entry.Status = logging.HTTPStatusFromRecorder(rec.status)
			entry.Duration = completedAt.Sub(start)
			if factory != nil {
				factory.LogHTTPAccess(entry)
				return
			}
			logger = logger.With(
				zap.Int("http.status_code", entry.Status),
				zap.Duration("http.duration", entry.Duration),
			)
			logger.Info(logging.HTTPAccessMessage())
		})
	}
}

func httpAccessKind(r *http.Request) string {
	if r == nil {
		return "HTTP"
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return "WS"
	}
	if strings.Contains(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept"))), "text/event-stream") {
		return "SSE"
	}
	path := strings.ToLower(strings.TrimSpace(r.URL.Path))
	if strings.Contains(path, "/events") || strings.Contains(path, "/poll") {
		return "POLL"
	}
	return "HTTP"
}
