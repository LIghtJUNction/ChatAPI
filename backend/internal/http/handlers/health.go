package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/zyf/chatapi/internal/config"
	"github.com/zyf/chatapi/internal/store"
)

type HealthHandler struct {
	Config config.Config
	Store  store.Store
}

func (h HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	type response struct {
		OK     bool   `json:"ok"`
		Mode   string `json:"mode"`
		Driver string `json:"driver"`
	}

	if err := h.Store.Ping(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response{
		OK:     true,
		Mode:   string(h.Config.Mode),
		Driver: h.Config.DatabaseDriver,
	})
}
