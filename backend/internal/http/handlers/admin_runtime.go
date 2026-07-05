package handlers

import (
	"net/http"

	"github.com/zyf/chatapi/internal/service"
)

type AdminRuntimeHandler struct {
	Monitor *service.RuntimeMonitorService
}

func (h AdminRuntimeHandler) Summary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": h.Monitor.Summary(),
	})
}

func (h AdminRuntimeHandler) Memory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"memory": h.Monitor.Memory(),
	})
}

func (h AdminRuntimeHandler) Connections(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"connections": h.Monitor.Connections(),
	})
}

func (h AdminRuntimeHandler) Queue(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"queue": h.Monitor.Queue(),
	})
}

func (h AdminRuntimeHandler) GC(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"memory": h.Monitor.ForceGC(),
	})
}
