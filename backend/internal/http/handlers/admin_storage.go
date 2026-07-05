package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

type AdminStorageHandler struct {
	Monitor *service.StorageMonitorService
}

func (h AdminStorageHandler) Summary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.Monitor.Summary(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"summary": summary,
	})
}

func (h AdminStorageHandler) Users(w http.ResponseWriter, r *http.Request) {
	users, err := h.Monitor.Users(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"items": users,
	})
}

type storageCleanupRequest struct {
	DryRun                  bool   `json:"dry_run"`
	OwnerID                 string `json:"owner_id"`
	KeepRecentConversations int    `json:"keep_recent_conversations"`
	KeepRecentDays          int    `json:"keep_recent_days"`
}

func (h AdminStorageHandler) Cleanup(w http.ResponseWriter, r *http.Request) {
	var input storageCleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid cleanup request", http.StatusBadRequest)
		return
	}
	if !input.DryRun {
		http.Error(w, "storage cleanup only supports dry_run currently", http.StatusBadRequest)
		return
	}
	preview, err := h.Monitor.CleanupPreview(r.Context(), service.StorageCleanupPreviewInput{
		OwnerID:                 strings.TrimSpace(input.OwnerID),
		KeepRecentConversations: input.KeepRecentConversations,
		KeepRecentDays:          input.KeepRecentDays,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"dry_run": true,
		"plan":    preview,
	})
}
