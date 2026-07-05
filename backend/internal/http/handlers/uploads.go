package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zyf/chatapi/internal/service"
)

type UploadsHandler struct {
	Service *service.UploadService
}

func (h UploadsHandler) Image(w http.ResponseWriter, r *http.Request) {
	path, err := h.Service.ResolveImagePath(chi.URLParam(r, "filename"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUploadPath):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, service.ErrUploadNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	http.ServeFile(w, r, path)
}

func (h UploadsHandler) Usage(w http.ResponseWriter, r *http.Request) {
	usage, err := h.Service.Usage(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":    true,
		"usage": usage,
	})
}
