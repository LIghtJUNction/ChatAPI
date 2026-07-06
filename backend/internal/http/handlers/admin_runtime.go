package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

type AdminRuntimeHandler struct {
	Monitor *service.RuntimeMonitorService
	Audit   *service.AuditService
}

func (h AdminRuntimeHandler) Schema(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"schema": service.BuildAdminRuntimeSchema(),
	})
}

func (h AdminRuntimeHandler) Summary(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": h.Monitor.Summary(),
	})
}

func (h AdminRuntimeHandler) Automation(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"automation": h.Monitor.AutomationDiagnostics(service.AutomationDiagnosticsInput{
			Limit:  limit,
			Reason: strings.TrimSpace(r.URL.Query().Get("reason")),
			RuleID: strings.TrimSpace(r.URL.Query().Get("rule_id")),
		}),
	})
}

func (h AdminRuntimeHandler) Memory(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"memory": h.Monitor.Memory(),
	})
}

func (h AdminRuntimeHandler) System(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"system": h.Monitor.System(),
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

func (h AdminRuntimeHandler) Settings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"settings": h.Monitor.Settings(),
	})
}

func (h AdminRuntimeHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	var input service.UpdateRuntimeSettingsInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid runtime settings request", http.StatusBadRequest)
		return
	}
	settings, err := h.Monitor.ApplySettings(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.runtime",
		ResourceType: "runtime",
		Action:       "settings_update",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"gogc":               settings.GOGC,
			"memory_limit_bytes": settings.MemoryLimitBytes,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"settings": settings,
	})
}

func (h AdminRuntimeHandler) GC(w http.ResponseWriter, r *http.Request) {
	memory := h.Monitor.ForceGC()
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.runtime",
		ResourceType: "runtime",
		Action:       "gc",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"heap_alloc_bytes": memory.HeapAllocBytes,
			"sys_bytes":        memory.SysBytes,
			"num_gc":           memory.NumGC,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"memory": memory,
	})
}
