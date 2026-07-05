package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

type LabHandler struct {
	Config config.Config
	Store  store.Store
}

func (h LabHandler) Workspace(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListConversations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":            true,
		"mode":          h.Config.Mode,
		"conversations": items,
	})
}

func (h LabHandler) WSInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"message": "websocket hub not implemented yet",
	})
}

func (h LabHandler) VirtualModel(w http.ResponseWriter, r *http.Request) {
	type requestEnvelope struct {
		Model  string         `json:"model"`
		Stream bool           `json:"stream"`
		Input  any            `json:"input"`
		Body   map[string]any `json:"-"`
	}

	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}

	requestID := "lab-request"
	model := "chatapi-lab"
	if value, ok := body["model"].(string); ok && value != "" {
		model = value
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":     requestID,
		"object": "response",
		"model":  model,
		"status": "pending",
		"output": []map[string]any{},
		"metadata": map[string]any{
			"lab_mode":     true,
			"request_body": body,
			"note":         "pending turn/state machine not implemented yet",
		},
	})
}
