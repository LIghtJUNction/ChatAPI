package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/zyf/chatapi/internal/service"
)

type AdminStorageHandler struct {
	Monitor *service.StorageMonitorService
	Audit   *service.AuditService
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

func (h AdminStorageHandler) Orphans(w http.ResponseWriter, r *http.Request) {
	preview, err := h.Monitor.OrphanImagesPreview(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"dry_run": true,
		"preview": preview,
	})
}

type storageOrphanCleanupRequest struct {
	DryRun *bool `json:"dry_run"`
}

func (h AdminStorageHandler) CleanupOrphans(w http.ResponseWriter, r *http.Request) {
	var input storageOrphanCleanupRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid orphan cleanup request", http.StatusBadRequest)
		return
	}
	if input.DryRun == nil || *input.DryRun {
		http.Error(w, "orphan cleanup requires dry_run=false after preview", http.StatusBadRequest)
		return
	}
	result, err := h.Monitor.DeleteOrphanImages(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.storage",
		ResourceType: "storage_orphans",
		Action:       "cleanup",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"dry_run":       false,
			"file_count":    result.FileCount,
			"bytes":         result.Bytes,
			"deleted_count": result.DeletedCount,
			"deleted_bytes": result.DeletedBytes,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"result": result,
	})
}

type storageCleanupRequest struct {
	DryRun                  *bool  `json:"dry_run"`
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
	if input.DryRun == nil {
		http.Error(w, "storage cleanup requires explicit dry_run", http.StatusBadRequest)
		return
	}
	cleanupInput := service.StorageCleanupPreviewInput{
		OwnerID:                 strings.TrimSpace(input.OwnerID),
		KeepRecentConversations: input.KeepRecentConversations,
		KeepRecentDays:          input.KeepRecentDays,
	}
	if *input.DryRun {
		preview, err := h.Monitor.CleanupPreview(r.Context(), cleanupInput)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.Audit.Record(r.Context(), service.AuditEventInput{
			EventType:    "admin.storage",
			ResourceType: "storage",
			ResourceID:   strings.TrimSpace(input.OwnerID),
			Action:       "cleanup_preview",
			Outcome:      "success",
			IPAddress:    clientIP(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]any{
				"dry_run":                     true,
				"keep_recent_conversations":   input.KeepRecentConversations,
				"keep_recent_days":            input.KeepRecentDays,
				"candidate_conversations":     preview.CandidateConversations,
				"candidate_messages":          preview.CandidateMessages,
				"estimated_reclaimable_bytes": preview.EstimatedReclaimableBytes,
			},
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"dry_run": true,
			"plan":    preview,
		})
		return
	}
	result, err := h.Monitor.DeleteCleanupCandidates(r.Context(), cleanupInput)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "admin.storage",
		ResourceType: "storage",
		ResourceID:   strings.TrimSpace(input.OwnerID),
		Action:       "cleanup",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"dry_run":                     false,
			"keep_recent_conversations":   input.KeepRecentConversations,
			"keep_recent_days":            input.KeepRecentDays,
			"candidate_conversations":     result.CandidateConversations,
			"candidate_messages":          result.CandidateMessages,
			"estimated_reclaimable_bytes": result.EstimatedReclaimableBytes,
			"deleted_conversations":       result.DeletedConversations,
			"deleted_messages":            result.DeletedMessages,
		},
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"dry_run": false,
		"result":  result,
	})
}
