package handlers

import (
	"net/http"

	"github.com/zyf/chatapi/internal/service"
)

type MetricsHandler struct {
	Service *service.MetricsService
}

func (h MetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(h.Service.PrometheusText()))
}
