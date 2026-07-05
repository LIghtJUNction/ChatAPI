package handlers

import (
	"errors"
	"mime/multipart"
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

func (h UploadsHandler) CreateImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, h.Service.MaxRequestBytes())
	file, _, err := firstUploadFile(r)
	if err != nil {
		http.Error(w, "upload file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	result, err := h.Service.SaveImage(r.Context(), file)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnsupportedUploadType):
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		case errors.Is(err, service.ErrUploadTooLarge):
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		case errors.Is(err, service.ErrInvalidUploadPath):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"upload": result,
	})
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

func firstUploadFile(r *http.Request) (multipart.File, string, error) {
	for _, field := range []string{"file", "image", "upload"} {
		file, header, err := r.FormFile(field)
		if err == nil {
			filename := ""
			if header != nil {
				filename = header.Filename
			}
			return file, filename, nil
		}
	}
	return nil, "", http.ErrMissingFile
}
