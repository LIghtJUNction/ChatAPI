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
	Audit   *service.AuditService
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
	file, originalFilename, err := firstUploadFile(r)
	if err != nil {
		h.Audit.Record(r.Context(), service.AuditEventInput{
			EventType:    "upload",
			ResourceType: "image",
			Action:       "create",
			Outcome:      "failure",
			IPAddress:    clientIP(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]any{
				"error": "missing_file",
			},
		})
		http.Error(w, "upload file is required", http.StatusBadRequest)
		return
	}
	defer file.Close()
	result, err := h.Service.SaveImage(r.Context(), file, originalFilename)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnsupportedUploadType):
			http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		case errors.Is(err, service.ErrUploadTooLarge):
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		case errors.Is(err, service.ErrStorageQuotaExceeded):
			http.Error(w, err.Error(), http.StatusInsufficientStorage)
		case errors.Is(err, service.ErrInvalidUploadPath):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		h.Audit.Record(r.Context(), service.AuditEventInput{
			EventType:    "upload",
			ResourceType: "image",
			Action:       "create",
			Outcome:      "failure",
			IPAddress:    clientIP(r),
			UserAgent:    r.UserAgent(),
			Metadata: map[string]any{
				"original_filename": originalFilename,
				"error":             err.Error(),
			},
		})
		return
	}
	h.Audit.Record(r.Context(), service.AuditEventInput{
		EventType:    "upload",
		ResourceType: "image",
		ResourceID:   result.ID,
		Action:       "create",
		Outcome:      "success",
		IPAddress:    clientIP(r),
		UserAgent:    r.UserAgent(),
		Metadata: map[string]any{
			"filename":          result.Filename,
			"original_filename": result.OriginalFilename,
			"content_type":      result.ContentType,
			"bytes":             result.Bytes,
		},
	})
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
