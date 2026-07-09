package localstore

import (
	"context"
	"fmt"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	RootDir string
}

func (s Store) PersistDraft(_ context.Context, draft media.DraftAsset) (media.StoredAsset, error) {
	root := strings.TrimSpace(s.RootDir)
	if root == "" {
		return media.StoredAsset{}, fmt.Errorf("localstore root dir is required")
	}
	ownerID := sanitizeSegment(draft.OwnerID)
	if ownerID == "" {
		ownerID = "anonymous"
	}
	dir := filepath.Join(root, ownerID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return media.StoredAsset{}, fmt.Errorf("create derived media dir: %w", err)
	}
	fileID := sanitizeSegment(draft.FileID)
	if fileID == "" {
		return media.StoredAsset{}, fmt.Errorf("file id is required")
	}
	filename := media.ChatAssetFilename(fileID)
	fullPath := filepath.Join(dir, filename)
	if err := os.WriteFile(fullPath, draft.Data, 0o644); err != nil {
		return media.StoredAsset{}, fmt.Errorf("write avif file: %w", err)
	}
	return media.StoredAsset{
		FileID:    fileID,
		OwnerID:   ownerID,
		Path:      fullPath,
		PublicURL: media.ChatAssetPublicURL(fileID),
		MediaType: firstNonEmpty(draft.MediaType, "image/avif"),
		Bytes:     int64(len(draft.Data)),
	}, nil
}

func (s Store) DeletePreparedImage(_ context.Context, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete prepared image: %w", err)
	}
	return nil
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	value = strings.ReplaceAll(value, "..", "_")
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
