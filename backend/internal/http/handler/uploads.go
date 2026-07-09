package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zyf2007/ChatAPI/internal/repository/storage"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
)

type UploadHandler struct {
	Storage storage.Store
}

func (h UploadHandler) GetImage(w http.ResponseWriter, r *http.Request) {
	principal, ok := session.PrincipalFromContext(r.Context())
	if !ok || strings.TrimSpace(principal.UserID) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	fileID := sanitizePathSegment(chi.URLParam(r, "fileID"))
	if fileID == "" {
		http.NotFound(w, r)
		return
	}
	asset, err := h.Storage.GetMediaAssetByFileID(r.Context(), fileID)
	if err != nil || strings.TrimSpace(asset.OwnerID) != strings.TrimSpace(principal.UserID) {
		http.NotFound(w, r)
		return
	}
	if _, err := os.Stat(asset.Path); err != nil {
		if os.IsNotExist(err) {
			applyUploadSecurityHeaders(w)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte("image unavailable"))
			return
		}
		http.Error(w, "failed to read image", http.StatusInternalServerError)
		return
	}
	applyUploadSecurityHeaders(w)
	w.Header().Set("Content-Type", asset.MediaType)
	w.Header().Set("Content-Disposition", "inline; filename=\""+filepath.Base(fileID)+".avif\"")
	http.ServeFile(w, r, asset.Path)
}

func sanitizePathSegment(value string) string {
	value = filepath.Base(strings.TrimSpace(value))
	if value == "." || value == "/" || value == "" {
		return ""
	}
	return value
}

func applyUploadSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; sandbox")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
}
